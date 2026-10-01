package books

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"

	"w5f/internal/catalog"
	"w5f/internal/doc"
	"w5f/internal/fetch"
	"w5f/internal/store"
)

// Env carries what the Library pages need.
type Env struct {
	Fetcher *fetch.Fetcher
	DB      *store.DB
	// LoadFile opens non-EPUB readable files (txt, html, md) as documents.
	LoadFile func(path string) (*doc.Document, error)
	// Catalogs lists added site catalogs for the "Find books" section.
	Catalogs func() []CatalogLink
}

// CatalogLink is a site catalog shown in the Library.
type CatalogLink struct{ ID, Name, Home string }

// suggestedCatalogs are offered in the Library until they are added: sites
// whose own pages refuse W5F but whose books are reachable another way.
var suggestedCatalogs = []struct{ name, home, test string }{
	{"Biodiversity Heritage Library — old natural history, bestiaries, herbals (its Internet Archive copy)",
		"https://archive.org/details/biodiversity", "serpents"},
}

// IsTarget reports whether target belongs to the Library.
func IsTarget(target string) bool {
	return strings.HasPrefix(target, "w5f:books") || strings.HasPrefix(target, "w5f:book/")
}

// Route builds the document for a Library address.
func Route(ctx context.Context, target string, env Env) (*doc.Document, error) {
	u, err := url.Parse(target)
	if err != nil {
		return nil, err
	}
	p := strings.TrimSuffix(u.Opaque, "/")
	q := u.Query()
	num := func(k string) int { n, _ := strconv.Atoi(q.Get(k)); return n }
	switch {
	case p == "books":
		return homeDoc(env)
	case p == "books/libgen" || strings.HasPrefix(p, "books/libgen/"):
		return libgenRoute(ctx, p, q, env)
	case p == "books/gutenberg":
		start := num("start")
		hits, err := SearchGutenberg(ctx, env.Fetcher, q.Get("q"), start)
		if err != nil {
			return nil, err
		}
		title := "Project Gutenberg — most downloaded"
		if q.Get("q") != "" {
			title = "Project Gutenberg: " + q.Get("q")
		}
		next := ""
		if len(hits) >= 25 {
			v := url.Values{"start": {strconv.Itoa(max(start, 1) + 25)}}
			if q.Get("q") != "" {
				v.Set("q", q.Get("q"))
			}
			next = "w5f:books/gutenberg?" + v.Encode()
		}
		return hitsDoc(env, title, hits, next), nil
	case p == "books/se":
		page := max(num("page"), 1)
		hits, err := SearchSE(ctx, env.Fetcher, q.Get("q"), page)
		if err != nil {
			return nil, err
		}
		title := "Standard Ebooks — newest"
		if q.Get("q") != "" {
			title = "Standard Ebooks: " + q.Get("q")
		}
		next := ""
		if len(hits) >= 12 {
			v := url.Values{"page": {strconv.Itoa(page + 1)}}
			if q.Get("q") != "" {
				v.Set("q", q.Get("q"))
			}
			next = "w5f:books/se?" + v.Encode()
		}
		return hitsDoc(env, title, hits, next), nil
	case strings.HasPrefix(p, "books/get/"):
		src := strings.TrimPrefix(p, "books/get/")
		src = strings.Replace(src, "/", ":", 1) // gutenberg/84 → gutenberg:84
		b, err := Download(ctx, env.Fetcher, env.DB, src)
		if err != nil {
			return nil, err
		}
		return chapterDoc(env, b, 0, 0)
	case strings.HasPrefix(p, "books/open/"):
		id, _ := strconv.ParseInt(strings.TrimPrefix(p, "books/open/"), 10, 64)
		b, err := env.DB.Book(id)
		if err != nil {
			return nil, err
		}
		d := &doc.Document{Title: b.Title, URL: target, Origin: "local", Lang: "en"}
		if err := OpenExternal(b.Path); err != nil {
			d.Blocks = []doc.Block{doc.Notice{Kind: "warn", Text: err.Error()}}
		} else {
			_ = env.DB.SaveProgress(b.ID, 0, 0)
			d.Blocks = []doc.Block{doc.Notice{Kind: "info", Text: "Opened in the external viewer. Press ← to come back."}}
		}
		return d, nil
	case strings.HasPrefix(p, "book/"):
		parts := strings.Split(strings.TrimPrefix(p, "book/"), "/")
		id, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return nil, errors.New("bad book address")
		}
		b, err := env.DB.Book(id)
		if err != nil {
			return nil, err
		}
		switch {
		case len(parts) == 1:
			return chapterDoc(env, b, b.Chapter, b.Pos) // resume
		case len(parts) == 2 && parts[1] == "toc":
			return tocDoc(b)
		case len(parts) == 3 && parts[1] == "ch":
			n, _ := strconv.Atoi(parts[2])
			return chapterDoc(env, b, n, 0)
		}
	}
	return nil, errors.New("unknown library address: " + target)
}

func link(d *doc.Document, href, text string) int {
	d.Links = append(d.Links, doc.Link{Href: href, Text: text})
	return len(d.Links)
}

func progress(b store.Book) string {
	if b.Opened.IsZero() {
		return "new"
	}
	if b.Chapters > 1 {
		pct := int(math.Round((float64(b.Chapter) + b.Pos) / float64(b.Chapters) * 100))
		return fmt.Sprintf("%d%% · ch %d/%d", min(pct, 100), b.Chapter+1, b.Chapters)
	}
	return fmt.Sprintf("%d%%", int(b.Pos*100))
}

func bookHref(b store.Book) string {
	if opensExternally(b.Path) {
		return fmt.Sprintf("w5f:books/open/%d", b.ID)
	}
	return fmt.Sprintf("w5f:book/%d", b.ID)
}

// homeDoc is The Stacks: what is being read beside where to find more,
// then every book.
func homeDoc(env Env) (*doc.Document, error) {
	n, scanErr := Scan(env.DB)
	d := &doc.Document{Title: "The Stacks", URL: "w5f:books", Origin: "local", Lang: "en"}
	d.Meta = []doc.KV{{Key: "books", Value: strconv.Itoa(n)}}
	if scanErr != nil {
		d.Blocks = append(d.Blocks, doc.Notice{Kind: "warn", Text: "Library folder: " + scanErr.Error()})
	}
	left := []doc.Block{doc.Heading{Level: 2, Text: doc.Inline{{Text: "Continue reading"}}}}
	if recent, _ := env.DB.Books("recent", 5); len(recent) > 0 {
		var items [][]doc.Block
		for _, b := range recent {
			items = append(items, []doc.Block{doc.Paragraph{Text: doc.Inline{
				{Text: b.Title, Style: doc.Bold, Link: link(d, bookHref(b), b.Title)},
				{Text: "  " + progress(b), Style: doc.Italic}}}})
		}
		left = append(left, doc.List{Items: items})
	} else {
		left = append(left, doc.Paragraph{Text: doc.Inline{{Text: "No book opened yet.", Style: doc.Italic}}})
	}
	find := [][]doc.Block{
		{doc.Paragraph{Text: doc.Inline{{Text: "Library Genesis", Link: link(d, "w5f:books/libgen", "Library Genesis")},
			{Text: "   search: g → libgen <words>", Style: doc.Italic}}}},
		{doc.Paragraph{Text: doc.Inline{{Text: "Project Gutenberg — most downloaded", Link: link(d, "w5f:books/gutenberg", "Gutenberg")},
			{Text: "   search: g → gut <words>", Style: doc.Italic}}}},
		{doc.Paragraph{Text: doc.Inline{{Text: "Standard Ebooks — newest", Link: link(d, "w5f:books/se", "Standard Ebooks")},
			{Text: "   search: g → se <words>", Style: doc.Italic}}}},
	}
	var added []CatalogLink
	if env.Catalogs != nil {
		added = env.Catalogs()
		for _, c := range added {
			find = append(find, []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: c.Name, Link: link(d, "w5f:catalog/"+c.ID, c.Name)},
				{Text: "   search: g → cat " + c.ID + " <words>", Style: doc.Italic}}}})
		}
	}
	for _, s := range suggestedCatalogs {
		have := false
		for _, c := range added {
			if strings.TrimSuffix(c.Home, "/") == s.home {
				have = true
			}
		}
		if !have {
			check := "w5f:catalog/check?" + url.Values{"url": {s.home}, "w": {s.test}}.Encode()
			find = append(find, []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: "+ suggested: ", Style: doc.Italic},
				{Text: s.name, Link: link(d, check, "add "+s.name)}}}})
		}
	}
	find = append(find, []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: "+ add a site: g → catalog-add <address>", Style: doc.Italic}, {Text: "   "},
		{Text: "manage catalogs", Link: link(d, "w5f:catalogs", "manage")}}}})
	d.Blocks = append(d.Blocks, doc.Columns{Cols: [][]doc.Block{left, {doc.Heading{Level: 2, Text: doc.Inline{{Text: "Find books"}}}, doc.List{Items: find}}}}, doc.Rule{})
	all, _ := env.DB.Books("author", 0)
	if len(all) == 0 {
		d.Blocks = append(d.Blocks, doc.Heading{Level: 2, Text: doc.Inline{{Text: "Your books"}}},
			doc.Paragraph{Text: doc.Inline{{Text: "No books yet. Download one from a catalog above, or put EPUB, MOBI/AZW3, FB2, PDF or TXT files into", Style: doc.Italic},
				{Text: LibraryDir(), Style: doc.Code}}})
		return d, nil
	}
	var items [][]doc.Block
	for _, b := range all {
		in := doc.Inline{{Text: b.Title, Link: link(d, bookHref(b), b.Title)}}
		sub := []string{}
		if b.Author != "" {
			sub = append(sub, b.Author)
		}
		sub = append(sub, strings.ToUpper(b.Format))
		if !b.Opened.IsZero() {
			sub = append(sub, progress(b))
		}
		in = append(in, doc.Span{Text: "  · " + strings.Join(sub, " · "), Style: doc.Italic})
		items = append(items, []doc.Block{doc.Paragraph{Text: in}})
	}
	d.Blocks = append(d.Blocks, doc.Heading{Level: 2, Text: doc.Inline{{Text: fmt.Sprintf("Your books (%d)", len(all))}}}, doc.List{Items: items},
		doc.Rule{}, doc.Paragraph{Text: doc.Inline{{Text: "Library folder: ", Style: doc.Italic}, {Text: LibraryDir(), Style: doc.Code}}})
	return d, nil
}

func hitsDoc(env Env, title string, hits []Hit, next string) *doc.Document {
	d := &doc.Document{Title: title, Origin: "live", Lang: "en"}
	if len(hits) == 0 {
		d.Blocks = []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: "No books found.", Style: doc.Italic}}}}
		return d
	}
	var items [][]doc.Block
	for _, h := range hits {
		href := "w5f:books/get/" + strings.Replace(h.Source, ":", "/", 1)
		mark := ""
		if b, ok := env.DB.BookBySource(h.Source); ok {
			href, mark = fmt.Sprintf("w5f:book/%d", b.ID), "✓ "
		}
		sub := []string{}
		if h.Author != "" {
			sub = append(sub, h.Author)
		}
		if h.Extra != "" {
			sub = append(sub, h.Extra)
		}
		items = append(items, []doc.Block{
			doc.Paragraph{Text: doc.Inline{{Text: mark, Style: doc.Bold}, {Text: h.Title, Style: doc.Bold, Link: link(d, href, h.Title)}}},
			doc.Paragraph{Text: doc.Inline{{Text: strings.Join(sub, " · "), Style: doc.Italic}}},
		})
	}
	d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: "Open a title to download it into your library and start reading. ✓ = already in your library.", Style: doc.Italic}}},
		doc.List{Ordered: true, Items: items})
	if next != "" {
		d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: "more results ›", Link: link(d, next, "more")}}})
		d.Next = next
	}
	return d
}

// chapterDoc opens chapter n of a book; resume > 0 scrolls to that fraction.
func chapterDoc(env Env, b store.Book, n int, resume float64) (*doc.Document, error) {
	if !chaptered(b.Path) {
		if opensExternally(b.Path) {
			return Route(context.Background(), fmt.Sprintf("w5f:books/open/%d", b.ID), env)
		}
		if env.LoadFile == nil {
			return nil, errors.New("cannot open " + b.Path)
		}
		d, err := env.LoadFile(b.Path)
		if err != nil {
			return nil, err
		}
		if d.Title == "" {
			d.Title = b.Title
		}
		d.Ref, d.Resume = fmt.Sprintf("book:%d:0", b.ID), resume
		d.Catalog = catalog.Book(b.Source, b.ID)
		_ = env.DB.SaveProgress(b.ID, 0, resume)
		return d, nil
	}
	e, err := Open(b.Path)
	if bookProblem(err) {
		return problemDoc(b, err), nil
	}
	if err != nil {
		return nil, err
	}
	defer e.Close()
	chs := e.Contents()
	if n < 0 || n >= len(chs) {
		n, resume = 0, 0
	}
	chapterURL := func(ch int) string { return fmt.Sprintf("w5f:book/%d/ch/%d", b.ID, ch) }
	d, err := e.ChapterDoc(n, chapterURL)
	if err != nil {
		return nil, err
	}
	// First time a book is opened: skip leading parts without text (cover,
	// title-page image) so the reader starts on something to read.
	if b.Opened.IsZero() && n == 0 && resume == 0 {
		for n+1 < len(chs) && doc.TextLength(d.Blocks) < 40 {
			n++
			if d, err = e.ChapterDoc(n, chapterURL); err != nil {
				return nil, err
			}
		}
	}
	d.Title = b.Title
	d.URL = chapterURL(n)
	d.Origin = "book"
	if b.Lang != "" {
		d.Lang = b.Lang
	}
	chTitle := chs[n].Title
	meta := []string{}
	if b.Author != "" {
		meta = append(meta, b.Author)
	}
	part := fmt.Sprintf("chapter %d/%d", n+1, len(chs))
	if chTitle != "" {
		part += " · " + chTitle
	}
	meta = append(meta, part)
	d.Meta = []doc.KV{{Key: "·", Value: strings.Join(meta, " · ")}}
	d.Ref = fmt.Sprintf("book:%d:%d", b.ID, n)
	d.Catalog = catalog.Book(b.Source, b.ID)
	d.Resume = resume
	var nav doc.Inline
	if n > 0 {
		d.Prev = chapterURL(n - 1)
		nav = append(nav, doc.Span{Text: "‹ previous", Link: link(d, d.Prev, "previous chapter")}, doc.Span{Text: "   "})
	}
	nav = append(nav, doc.Span{Text: "contents", Link: link(d, fmt.Sprintf("w5f:book/%d/toc", b.ID), "contents")})
	if bookExt(b.Path) == ".pdf" {
		nav = append(nav, doc.Span{Text: "   "}, doc.Span{Text: "external viewer", Link: link(d, fmt.Sprintf("w5f:books/open/%d", b.ID), "external viewer")})
	}
	if n < len(chs)-1 {
		d.Next = chapterURL(n + 1)
		nav = append(nav, doc.Span{Text: "   "}, doc.Span{Text: "next chapter ›", Link: link(d, d.Next, "next chapter")})
	} else {
		nav = append(nav, doc.Span{Text: "   — the end —", Style: doc.Italic})
	}
	if len(d.Blocks) == 0 {
		d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: "(this part has no text — press ] for the next chapter)", Style: doc.Italic}}})
	}
	d.Blocks = append(d.Blocks, doc.Rule{}, doc.Paragraph{Text: nav})
	d.Renumber()
	if b.Chapters != len(chs) {
		b.Chapters = len(chs)
		_, _ = env.DB.UpsertBook(b)
	}
	_ = env.DB.SaveProgress(b.ID, n, resume)
	return d, nil
}

func tocDoc(b store.Book) (*doc.Document, error) {
	e, err := Open(b.Path)
	if bookProblem(err) {
		return problemDoc(b, err), nil
	}
	if err != nil {
		return nil, err
	}
	defer e.Close()
	d := &doc.Document{Title: b.Title + " — contents", URL: fmt.Sprintf("w5f:book/%d/toc", b.ID), Origin: "book", Lang: "en"}
	if b.Author != "" {
		d.Meta = []doc.KV{{Key: "·", Value: b.Author}}
	}
	var items [][]doc.Block
	for i, c := range e.Contents() {
		t := c.Title
		if t == "" {
			t = fmt.Sprintf("Part %d", i+1)
		}
		in := doc.Inline{{Text: t, Link: link(d, fmt.Sprintf("w5f:book/%d/ch/%d", b.ID, i), t)}}
		if i == b.Chapter && !b.Opened.IsZero() {
			in = append(in, doc.Span{Text: "  ◀ you are here", Style: doc.Italic})
		}
		items = append(items, []doc.Block{doc.Paragraph{Text: in}})
	}
	d.Blocks = []doc.Block{doc.List{Ordered: true, Items: items}}
	return d, nil
}

// SaveFromRef records reading progress for a "book:<id>:<chapter>" page.
func SaveFromRef(db *store.DB, ref string, frac float64) {
	parts := strings.Split(strings.TrimPrefix(ref, "book:"), ":")
	if len(parts) != 2 {
		return
	}
	id, err1 := strconv.ParseInt(parts[0], 10, 64)
	ch, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return
	}
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	_ = db.SaveProgress(id, ch, frac)
}
