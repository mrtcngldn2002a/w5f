package fiction

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"w5f/internal/books"
	"w5f/internal/doc"
)

// ao3Root is AO3's address for pages W5F builds itself (a variable for tests).
var ao3Root = ao3Base

var reAO3WorkLink = regexp.MustCompile(`archiveofourown\.org/works/(\d+)`)

func init() {
	extraRoutes["fiction/ao3/save"] = func(ctx context.Context, q url.Values, env Env) (*doc.Document, error) {
		return ao3SaveWork(ctx, env, q.Get("u"))
	}
	extraRoutes["fiction/ao3/me"] = func(ctx context.Context, q url.Values, env Env) (*doc.Document, error) {
		return ao3MeDoc(ctx, env)
	}
	extraRoutes["fiction/ao3/list"] = func(ctx context.Context, q url.Values, env Env) (*doc.Document, error) {
		return ao3ListDoc(ctx, env, q.Get("u"), q.Get("t"))
	}
	extraRoutes["fiction/ao3/import"] = func(ctx context.Context, q url.Values, env Env) (*doc.Document, error) {
		n, err := ImportAO3Downloads(env)
		d := &doc.Document{Title: "AO3 downloads", URL: "w5f:fiction/ao3/import", Origin: "local", Lang: "en"}
		switch {
		case err != nil:
			d.Blocks = []doc.Block{doc.Notice{Kind: "warn", Text: err.Error()}}
		case n == 0:
			d.Blocks = []doc.Block{para(plain("No new AO3 books in "+downloadsDir(env)+".", doc.Italic))}
		default:
			d.Blocks = []doc.Block{para(plain(fmt.Sprintf("%d AO3 books added to the Library from %s.", n, downloadsDir(env)), 0))}
		}
		d.Blocks = append(d.Blocks, para(doc.Span{Text: "open The Stacks", Link: link(d, "w5f:books", "The Stacks")}))
		return d, nil
	}
}

// libraryPut puts an EPUB into the Library under source src: a new book, or
// the same book replaced when src is already there. The file is copied, so
// the original stays where it is.
func libraryPut(env Env, path, src string) (int64, error) {
	dir := books.LibraryDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}
	tmp, err := os.CreateTemp(dir, ".import-*.epub")
	if err != nil {
		return 0, err
	}
	in, err := os.Open(path)
	if err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return 0, err
	}
	_, err = io.Copy(tmp, in)
	in.Close()
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp.Name())
		return 0, err
	}
	if b, ok := env.DB.BookBySource(src); ok {
		if err := os.Rename(tmp.Name(), b.Path); err != nil {
			os.Remove(tmp.Name())
			return 0, err
		}
		return b.ID, nil
	}
	b, err := books.AddFile(env.DB, tmp.Name(), "epub", "", "", src)
	if err != nil {
		os.Remove(tmp.Name())
		return 0, err
	}
	return b.ID, nil
}

func openBook(ctx context.Context, env Env, id int64) (*doc.Document, error) {
	return books.Route(ctx, fmt.Sprintf("w5f:book/%d", id), books.Env{Fetcher: env.Fetcher, DB: env.DB})
}

// ao3SaveWork saves a whole work with AO3's own EPUB download: one request
// instead of one per chapter.
func ao3SaveWork(ctx context.Context, env Env, work string) (*doc.Document, error) {
	u, err := url.Parse(work)
	if err != nil || u.Host == "" {
		return nil, errors.New("save needs an AO3 work address")
	}
	m := reAO3Work.FindStringSubmatch(u.Path)
	if m == nil {
		return nil, errors.New("not an AO3 work address")
	}
	gq, base, err := ao3Page(ctx, env.Fetcher, work, false)
	if err != nil {
		return nil, err
	}
	var adult errAdult
	if err := adultWarning(gq, work); errors.As(err, &adult) {
		return adultDoc(adult), nil
	}
	href, _ := gq.Find(`li.download a[href*=".epub"]`).First().Attr("href")
	if href == "" {
		return nil, errors.New("AO3: no EPUB download link on the work page")
	}
	data, _, err := ao3Get(ctx, env.Fetcher, resolve(base, href), true)
	if err != nil {
		return nil, err
	}
	if !bytes.HasPrefix(data, []byte("PK")) {
		return nil, errors.New("AO3: the download is not an EPUB file")
	}
	tmp := filepath.Join(os.TempDir(), fmt.Sprintf("w5f-ao3-%s.epub", m[1]))
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return nil, err
	}
	defer os.Remove(tmp)
	id, err := libraryPut(env, tmp, "ao3:"+m[1])
	if err != nil {
		return nil, err
	}
	return openBook(ctx, env, id)
}

// downloadsDir is where the browser saves downloads (config.Downloads,
// given by the caller; else ~/Downloads).
func downloadsDir(env Env) string {
	if env.Downloads != "" {
		return env.Downloads
	}
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Join(h, "Downloads")
	}
	return ""
}

// ao3WorkOf finds the AO3 work an EPUB was downloaded from ("" if none).
func ao3WorkOf(path string) string {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return ""
	}
	defer zr.Close()
	read := 0
	for _, f := range zr.File {
		ext := strings.ToLower(filepath.Ext(f.Name))
		if ext != ".opf" && ext != ".xhtml" && ext != ".html" && ext != ".htm" {
			continue
		}
		if read++; read > 6 || f.UncompressedSize64 > 2<<20 {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			continue
		}
		b, _ := io.ReadAll(io.LimitReader(rc, 2<<20))
		rc.Close()
		if m := reAO3WorkLink.FindSubmatch(b); m != nil {
			return string(m[1])
		}
	}
	return ""
}

// ImportAO3Downloads adds AO3 EPUBs from the Downloads folder to the
// Library (each file once; a newer download of the same work replaces the
// book). Other files are left alone.
func ImportAO3Downloads(env Env) (int, error) {
	dir := downloadsDir(env)
	if dir == "" {
		return 0, nil
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.epub"))
	if err != nil {
		return 0, err
	}
	n := 0
	for _, p := range files {
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		key := fmt.Sprintf("ao3-import:%s|%d|%d", p, st.Size(), st.ModTime().Unix())
		if env.DB.Get(key) != "" {
			continue
		}
		if w := ao3WorkOf(p); w != "" {
			if _, err := libraryPut(env, p, "ao3:"+w); err != nil {
				continue
			}
			n++
		}
		_ = env.DB.Set(key, "done")
	}
	return n, nil
}

// ao3MeDoc is the owner's AO3: bookmarks, subscriptions, history.
func ao3MeDoc(ctx context.Context, env Env) (*doc.Document, error) {
	d := &doc.Document{Title: "My AO3", URL: "w5f:fiction/ao3/me", Origin: "live", Lang: "en"}
	if LoadAO3Session() == "" {
		d.Blocks = []doc.Block{
			para(plain("Connect your own AO3 account to see your bookmarks, subscriptions and history, and works only logged-in users can read.", 0)),
			para(plain("In a browser where you are logged in to AO3, copy the value of the _otwarchive_session cookie (developer tools → Storage/Application → Cookies → archiveofourown.org). In W5F press g, type ", 0),
				plain("ao3-login", doc.Code), plain(", paste it (it stays hidden) and press enter. It is stored only on this computer and sent only to archiveofourown.org.", 0)),
			para(plain("Simpler with Chromium or Firefox: g → ", 0), plain("browser archiveofourown.org/users/login", doc.Code),
				plain(", log in there, then g → ", 0), plain("ao3-login browser", doc.Code), plain(" takes the session from that browser.", 0)),
		}
		return d, nil
	}
	gq, _, err := ao3Page(ctx, env.Fetcher, ao3Root+"/", true)
	if err != nil {
		return nil, err
	}
	user := ""
	if href, ok := gq.Find(`#greeting a[href^="/users/"]`).First().Attr("href"); ok {
		user = strings.Split(strings.TrimPrefix(href, "/users/"), "/")[0]
	}
	if user == "" {
		d.Blocks = []doc.Block{doc.Notice{Kind: "warn", Text: "AO3 does not see you as logged in: the session may have expired. Connect again with g → ao3-login."}}
		return d, nil
	}
	d.Meta = []doc.KV{{Key: "·", Value: "logged in as " + user}}
	var items [][]doc.Block
	for _, it := range []struct{ title, path string }{
		{"Bookmarks", "/users/" + user + "/bookmarks"},
		{"Subscriptions", "/users/" + user + "/subscriptions"},
		{"History", "/users/" + user + "/readings"},
		{"Marked for later", "/users/" + user + "/readings?show=to-read"},
		{"Your works", "/users/" + user + "/works"},
	} {
		href := "w5f:fiction/ao3/list?" + url.Values{"u": {ao3Root + it.path}, "t": {it.title}}.Encode()
		items = append(items, []doc.Block{para(doc.Span{Text: it.title, Link: link(d, href, it.title)})})
	}
	d.Blocks = []doc.Block{doc.List{Items: items}}
	return d, nil
}

// ao3Blurbs lists the works of an AO3 listing page.
func ao3Blurbs(d *doc.Document, gq *goquery.Document, base *url.URL) [][]doc.Block {
	var items [][]doc.Block
	seen := map[string]bool{}
	gq.Find("li.blurb, li.work, li.bookmark, li.reading, dl.subscription dt, table.subscription tr").Each(func(_ int, it *goquery.Selection) {
		a := it.Find(`h4.heading a[href^="/works/"], a[href^="/works/"], a[href^="/series/"]`).First()
		href, _ := a.Attr("href")
		if href == "" || seen[href] {
			return
		}
		seen[href] = true
		in := doc.Inline{{Text: text(a), Style: doc.Bold, Link: link(d, openHref(resolve(base, href)), text(a))}}
		if by := text(it.Find(`a[rel="author"]`).First()); by != "" {
			in = append(in, plain("  by "+by, doc.Italic))
		}
		blocks := []doc.Block{para(in...)}
		if sum := text(it.Find("blockquote.summary").First()); sum != "" {
			blocks = append(blocks, para(plain(sum, 0)))
		}
		if stats := text(it.Find("dl.stats").First()); stats != "" {
			blocks = append(blocks, para(plain(stats, doc.Italic)))
		}
		items = append(items, blocks)
	})
	return items
}

func ao3ListDoc(ctx context.Context, env Env, page, title string) (*doc.Document, error) {
	u, err := url.Parse(page)
	if err != nil || !ao3Hosts[strings.ToLower(u.Host)] {
		return nil, errors.New("not an AO3 list address")
	}
	gq, base, err := ao3Page(ctx, env.Fetcher, page, true)
	if err != nil {
		return nil, err
	}
	if title == "" {
		title = "AO3"
	}
	d := &doc.Document{Title: title, URL: page, Origin: "live", Lang: "en"}
	items := ao3Blurbs(d, gq, base)
	if len(items) == 0 {
		d.Blocks = []doc.Block{para(plain("Nothing here.", doc.Italic))}
	} else {
		d.Blocks = []doc.Block{doc.List{Ordered: true, Items: items}}
	}
	if next, ok := gq.Find("ol.pagination li.next a").First().Attr("href"); ok && next != "" {
		d.Next = "w5f:fiction/ao3/list?" + url.Values{"u": {resolve(base, next)}, "t": {title}}.Encode()
		d.Blocks = append(d.Blocks, para(doc.Span{Text: "more ›", Link: link(d, d.Next, "more")}))
	}
	return d, nil
}
