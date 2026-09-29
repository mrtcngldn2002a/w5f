package sitecat

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"w5f/internal/books"
	"w5f/internal/doc"
	"w5f/internal/fetch"
	"w5f/internal/store"
)

// Env carries what the catalog pages need.
type Env struct {
	Fetcher  *fetch.Fetcher
	DB       *store.DB
	Path     string // catalogs.toml
	OpenBook func(ctx context.Context, b store.Book) (*doc.Document, error)
}

// IsTarget reports whether target is a site-catalog address.
func IsTarget(target string) bool { return strings.HasPrefix(target, "w5f:catalog") }

func link(d *doc.Document, href, text string) int {
	d.Links = append(d.Links, doc.Link{Href: href, Text: text})
	return len(d.Links)
}

func para(spans ...doc.Span) doc.Paragraph { return doc.Paragraph{Text: spans} }

// Route builds the page for a catalog address.
func Route(ctx context.Context, target string, env Env) (*doc.Document, error) {
	f, err := env.Fetcher.ForCatalog()
	if err != nil {
		return nil, err
	}
	env.Fetcher = f
	u, err := url.Parse(target)
	if err != nil {
		return nil, err
	}
	p := strings.TrimSuffix(u.Opaque, "/")
	q := u.Query()
	ps, err := LoadAll(env.Path)
	if err != nil {
		return nil, err
	}
	switch {
	case p == "catalogs":
		return manageDoc(ps, ""), nil
	case p == "catalog/check":
		rep, err := Probe(ctx, env.Fetcher, q.Get("url"), q.Get("w"))
		if err != nil {
			return nil, err
		}
		return checkDoc(rep, q.Get("url"), q.Get("w")), nil
	case p == "catalog/add":
		rep, err := Probe(ctx, env.Fetcher, q.Get("url"), q.Get("w"))
		if err != nil {
			return nil, err
		}
		if !rep.CanAdd {
			return checkDoc(rep, q.Get("url"), q.Get("w")), nil
		}
		pr := rep.Profile
		pr.ID = IDFor(pr.Home, ps)
		var buildErr error
		if pr.Search.Kind == "index" {
			pr.Search.Index = filepath.Join(filepath.Dir(env.Path), "catalogs", pr.ID+".index.json")
			buildErr = buildIndex(ctx, env.Fetcher, &pr)
			if buildErr != nil && !errors.Is(buildErr, context.Canceled) {
				return nil, buildErr
			}
		}
		ps = append(ps, pr)
		if err := SaveAll(env.Path, ps); err != nil {
			return nil, err
		}
		if buildErr != nil { // cancelled: the partial list is kept
			return nil, buildErr
		}
		return manageDoc(ps, "Added “"+pr.Name+"”. Search it with g → cat "+pr.ID+" <words>."), nil
	}
	rest := strings.TrimPrefix(p, "catalog/")
	id, action, _ := strings.Cut(rest, "/")
	i := Find(ps, id)
	if i < 0 {
		return nil, errors.New("unknown catalog " + id + " (see g → catalogs)")
	}
	pr := &ps[i]
	switch action {
	case "":
		if pr.Search.Kind == "index" && q.Get("q") == "" {
			dir := q.Get("dir")
			if dir == "" {
				dir = pr.Search.Template
			}
			return dirDoc(ctx, env, *pr, dir)
		}
		if q.Get("q") == "" && pr.Search.Kind == "browse" {
			hu, err := url.Parse(pr.Home)
			if err != nil {
				return nil, err
			}
			resp, err := env.Fetcher.Get(ctx, hu, fetch.Options{})
			if err != nil {
				return nil, err
			}
			rs, _ := Extract(resp.Body, resp.URL.String(), pr.Layout)
			return resultsDoc(env, *pr, "front page", &Found{Results: rs, Pages: 1}, nil), nil
		}
		if q.Get("q") == "" {
			return catalogDoc(*pr), nil
		}
		found, err := Search(ctx, env.Fetcher, pr, q.Get("q"), nil)
		if err != nil && (found == nil || errors.Is(err, context.Canceled)) {
			return nil, err
		}
		return resultsDoc(env, *pr, q.Get("q"), found, err), nil
	case "item":
		ds, err := itemDownloads(ctx, env.Fetcher, pr, q.Get("u"))
		if err != nil {
			var nd *notDownloadable
			if errors.As(err, &nd) {
				return itemDoc(env, *pr, q.Get("u"), q.Get("t"), nil, nd.Reason), nil
			}
			return nil, err
		}
		return itemDoc(env, *pr, q.Get("u"), q.Get("t"), ds, ""), nil
	case "get":
		if pr.Search.Kind == "libgen" {
			b, err := getLibgen(ctx, env.Fetcher, env.DB, pr, q.Get("src"))
			if err != nil {
				return nil, err
			}
			return env.OpenBook(ctx, b)
		}
		src := "site:" + pr.ID + ":" + q.Get("src")
		if b, ok := env.DB.BookBySource(src); ok {
			if _, err := os.Stat(b.Path); err == nil {
				return env.OpenBook(ctx, b)
			}
			// The file was deleted: fetch it again.
		}
		tmp, format, err := FetchFile(ctx, env.Fetcher, Download{URL: q.Get("u"), Format: q.Get("f")}, books.LibraryDir())
		if err != nil {
			return nil, err
		}
		defer os.Remove(tmp) // nothing left behind if the book cannot be added
		b, err := books.AddFile(env.DB, tmp, format, q.Get("t"), "", src)
		if err != nil {
			return nil, err
		}
		return env.OpenBook(ctx, b)
	case "recheck":
		rep, err := Probe(ctx, env.Fetcher, pr.Home, "")
		if err != nil {
			return nil, err
		}
		if rep.CanAdd {
			keep := *pr
			*pr = rep.Profile
			pr.ID, pr.Name, pr.Added = keep.ID, keep.Name, keep.Added
			pr.Download = keep.Download
			// Hand-tuned limits survive a re-check only when the kind stays;
			// a new kind gets its own defaults (the engine's are lower).
			if pr.Search.Kind == keep.Search.Kind {
				pr.Search.MaxPages, pr.Search.MaxResults = keep.Search.MaxPages, keep.Search.MaxResults
				pr.Search.MaxFolders = keep.Search.MaxFolders
			}
			pr.ApplyDefaults()
			if pr.Search.Kind == "index" {
				pr.Search.Index = filepath.Join(filepath.Dir(env.Path), "catalogs", pr.ID+".index.json")
				if err := buildIndex(ctx, env.Fetcher, pr); err != nil {
					return nil, err
				}
			}
			if err := SaveAll(env.Path, ps); err != nil {
				return nil, err
			}
		}
		return checkDoc(rep, pr.Home, ""), nil
	case "remove":
		name := pr.Name
		ps = append(ps[:i], ps[i+1:]...)
		if err := SaveAll(env.Path, ps); err != nil {
			return nil, err
		}
		return manageDoc(ps, "Removed “"+name+"”. Books already downloaded stay in your library."), nil
	}
	return nil, errors.New("unknown catalog address: " + target)
}

func checkDoc(rep *Report, raw, word string) *doc.Document {
	d := &doc.Document{Title: "Catalog check: " + rep.Profile.Name, URL: raw, Origin: "live", Lang: "en"}
	d.Meta = []doc.KV{{Key: "·", Value: rep.Profile.Home}}
	for _, fi := range rep.Findings {
		mark := "✗ "
		if fi.OK {
			mark = "✓ "
		}
		d.Blocks = append(d.Blocks, para(doc.Span{Text: mark, Style: doc.Bold}, doc.Span{Text: fi.Text}))
	}
	if len(rep.Sample) > 0 {
		var items [][]doc.Block
		for _, r := range rep.Sample {
			items = append(items, []doc.Block{para(doc.Span{Text: r.Title, Style: doc.Bold}, doc.Span{Text: "  " + r.Extra, Style: doc.Italic})})
		}
		d.Blocks = append(d.Blocks, doc.Heading{Level: 2, Text: doc.Inline{{Text: "Sample results"}}}, doc.List{Ordered: true, Items: items})
	}
	d.Blocks = append(d.Blocks, doc.Rule{})
	if rep.CanAdd {
		add := "w5f:catalog/add?" + url.Values{"url": {raw}, "w": {word}}.Encode()
		d.Blocks = append(d.Blocks, para(doc.Span{Text: "✓ add this catalog", Style: doc.Bold, Link: link(d, add, "add")}))
	} else {
		d.Blocks = append(d.Blocks, doc.Notice{Kind: "warn", Text: "This site cannot be added as it is."})
	}
	d.Blocks = append(d.Blocks, para(doc.Span{Text: "Another test word: g → catalog-add " + raw + " <word>", Style: doc.Italic}))
	return d
}

func manageDoc(ps []Profile, note string) *doc.Document {
	d := &doc.Document{Title: "Site catalogs", URL: "w5f:catalogs", Origin: "local", Lang: "en"}
	if note != "" {
		d.Blocks = append(d.Blocks, doc.Notice{Kind: "info", Text: note})
	}
	if len(ps) == 0 {
		d.Blocks = append(d.Blocks, para(doc.Span{Text: "No site catalogs yet. Add one with g → catalog-add <address>.", Style: doc.Italic}))
		return d
	}
	var items [][]doc.Block
	for _, p := range ps {
		items = append(items, []doc.Block{
			para(doc.Span{Text: p.Name, Style: doc.Bold, Link: link(d, "w5f:catalog/"+p.ID, p.Name)},
				doc.Span{Text: "  · " + p.ID + " · " + p.Search.Kind, Style: doc.Italic}),
			para(doc.Span{Text: "search: g → cat " + p.ID + " <words>   ", Style: doc.Italic},
				doc.Span{Text: "re-check", Link: link(d, "w5f:catalog/"+p.ID+"/recheck", "re-check")}, doc.Span{Text: "   "},
				doc.Span{Text: "remove", Link: link(d, "w5f:catalog/"+p.ID+"/remove", "remove")}),
		})
	}
	d.Blocks = append(d.Blocks, doc.List{Items: items},
		para(doc.Span{Text: "Profiles are kept in " + Path() + " (safe to edit).", Style: doc.Italic}))
	return d
}

func catalogDoc(p Profile) *doc.Document {
	d := &doc.Document{Title: p.Name, URL: "w5f:catalog/" + p.ID, Origin: "local", Lang: "en"}
	d.Meta = []doc.KV{{Key: "·", Value: p.Home}}
	d.Blocks = append(d.Blocks,
		para(doc.Span{Text: "Search: g → cat " + p.ID + " <words>", Style: doc.Bold}),
		para(doc.Span{Text: "Only an author: cat " + p.ID + " author:<name> · only a title: cat " + p.ID + " title:<words>", Style: doc.Italic}),
		para(doc.Span{Text: fmt.Sprintf("Full search reads up to %d result pages (%d results).", p.Search.MaxPages, p.Search.MaxResults), Style: doc.Italic}),
		para(doc.Span{Text: "open the site's front page", Link: link(d, p.Home, "front page")}))
	return d
}

func resultsDoc(env Env, p Profile, q string, f *Found, searchErr error) *doc.Document {
	d := &doc.Document{Title: p.Name + ": " + q, URL: "w5f:catalog/" + p.ID + "?" + url.Values{"q": {q}}.Encode(), Origin: "live", Lang: "en"}
	summary := fmt.Sprintf("%d results from %d pages", len(f.Results), f.Pages)
	if f.Pages == 1 {
		summary = fmt.Sprintf("%d results from 1 page", len(f.Results))
	}
	d.Blocks = append(d.Blocks, para(doc.Span{Text: summary, Style: doc.Italic}))
	if p.Search.Kind == "engine" {
		d.Blocks = append(d.Blocks, doc.Notice{Kind: "info", Text: "Results via DuckDuckGo (site:" + p.Search.Template + ") — they depend on its index."})
	}
	if f.Capped {
		d.Blocks = append(d.Blocks, doc.Notice{Kind: "info", Text: fmt.Sprintf("Stopped at the limit (%d pages / %d results) — use more specific words or raise max_pages in %s.", p.Search.MaxPages, p.Search.MaxResults, Path())})
	}
	if f.FieldIgnored {
		d.Blocks = append(d.Blocks, doc.Notice{Kind: "info", Text: "This site has no separate author/title search; the words were searched everywhere."})
	}
	if searchErr != nil {
		d.Blocks = append(d.Blocks, doc.Notice{Kind: "warn", Text: "The search stopped early: " + searchErr.Error()})
	}
	var items [][]doc.Block
	for _, r := range f.Results {
		href := "w5f:catalog/" + p.ID + "/item?" + url.Values{"u": {r.URL}, "t": {r.Title}}.Encode()
		mark := ""
		if _, ok := env.DB.BookBySource(catalogSource(p, r.URL)); ok {
			mark = "✓ "
		}
		item := []doc.Block{para(doc.Span{Text: mark, Style: doc.Bold}, doc.Span{Text: r.Title, Style: doc.Bold, Link: link(d, href, r.Title)})}
		if r.Extra != "" {
			item = append(item, para(doc.Span{Text: r.Extra, Style: doc.Italic}))
		}
		items = append(items, item)
	}
	if len(items) == 0 {
		d.Blocks = append(d.Blocks, para(doc.Span{Text: "Nothing found. If the site does list books for these words, its pages may have changed — re-check the catalog (g → catalogs).", Style: doc.Italic}))
		return d
	}
	d.Blocks = append(d.Blocks, doc.List{Ordered: true, Items: items})
	return d
}

func itemDoc(env Env, p Profile, itemURL, title string, ds []Download, note string) *doc.Document {
	d := &doc.Document{Title: title, URL: itemURL, Origin: "live", Lang: "en"}
	d.Meta = []doc.KV{{Key: "·", Value: p.Name}}
	if b, ok := env.DB.BookBySource(catalogSource(p, itemURL)); ok {
		d.Blocks = append(d.Blocks, para(doc.Span{Text: "✓ in your library — ", Style: doc.Bold},
			doc.Span{Text: "open it", Link: link(d, fmt.Sprintf("w5f:book/%d", b.ID), "open")}))
	}
	switch {
	case note != "":
		d.Blocks = append(d.Blocks, doc.Notice{Kind: "info", Text: note})
	case len(ds) == 0:
		d.Blocks = append(d.Blocks, doc.Notice{Kind: "info", Text: "No downloadable book file was found on this page."})
	default:
		var items [][]doc.Block
		for _, dl := range ds {
			get := "w5f:catalog/" + p.ID + "/get?" + url.Values{"u": {dl.URL}, "f": {dl.Format}, "t": {title}, "src": {itemURL}}.Encode()
			label := strings.ToUpper(dl.Format)
			if dl.Label != "" && !strings.EqualFold(dl.Label, dl.Format) {
				label += " — " + dl.Label
			}
			items = append(items, []doc.Block{para(doc.Span{Text: label, Link: link(d, get, label)})})
		}
		d.Blocks = append(d.Blocks, doc.Heading{Level: 2, Text: doc.Inline{{Text: "Download into your library"}}}, doc.List{Items: items})
	}
	if strings.HasPrefix(itemURL, "libgen:") {
		d.Blocks = append(d.Blocks, doc.Rule{}, para(doc.Span{Text: "Downloads use Library Genesis and are verified by MD5."}))
	} else {
		d.Blocks = append(d.Blocks, doc.Rule{}, para(doc.Span{Text: "→ the book's page on the site", Link: link(d, itemURL, "page")}))
	}
	return d
}
