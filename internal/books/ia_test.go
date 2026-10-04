package books

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"w5f/internal/doc"
	"w5f/internal/fetch"
	"w5f/internal/store"
)

// The Archive, played by a fixture: two collections, a public text with an
// EPUB and its scans' full text, and a lending-library text.
func iaFixture(t *testing.T) (Env, *[]string) {
	t.Helper()
	t.Setenv("W5F_BOOKS", t.TempDir())
	epub := filepath.Join(t.TempDir(), "b.epub")
	writeEPUB(t, epub)
	epubBytes, _ := os.ReadFile(epub)
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/advancedsearch.php":
			q := r.URL.Query().Get("q")
			queries = append(queries, q+" | "+r.URL.Query().Get("sort[]")+" | page "+r.URL.Query().Get("page"))
			switch {
			case strings.HasPrefix(q, "mediatype:collection"):
				fmt.Fprint(w, `{"response":{"numFound":40,"docs":[{"identifier":"americana","title":"American Libraries","mediatype":"collection"}]}}`)
			case r.URL.Query().Get("rows") == "1":
				fmt.Fprint(w, `{"response":{"numFound":120,"docs":[{"identifier":"shadowbook","title":"Shadow","mediatype":"texts"}]}}`)
			default:
				fmt.Fprint(w, `{"response":{"numFound":120,"docs":[{"identifier":"shadowbook","title":"The Shadow of the Torturer","creator":["Wolfe, Gene"],"year":"1980","downloads":4321,"mediatype":"texts"}]}}`)
			}
		case "/metadata/americana":
			fmt.Fprint(w, `{"metadata":{"title":"American Libraries","mediatype":"collection","description":"<p>Books from <b>American</b> libraries.</p>","collection":["texts"]}}`)
		case "/metadata/shadowbook":
			fmt.Fprint(w, `{"metadata":{"identifier":"shadowbook","title":"The Shadow of the Torturer","creator":"Wolfe, Gene","date":"1980","language":"eng","subject":["Fantasy; Dying Earth"],"collection":["americana","texts"],"mediatype":"texts"},
"files":[{"name":"shadow_djvu.txt","format":"DjVuTXT","size":"2048"},{"name":"shadow.pdf","format":"Text PDF","size":"1048576"},{"name":"shadow.epub","format":"EPUB"},{"name":"secret.epub","private":"true"},{"name":"shadow.jpg"}]}`)
		case "/metadata/borrowed":
			fmt.Fprint(w, `{"metadata":{"title":"A Borrowed Book","mediatype":"texts","access-restricted-item":"true"},"files":[{"name":"b.pdf","private":"true"}]}`)
		case "/download/shadowbook/shadow.epub":
			w.Write(epubBytes)
		default:
			t.Errorf("unexpected request %s", r.URL)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	old := IABase
	IABase = srv.URL
	t.Cleanup(func() { IABase = old })
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	f := fetch.New(t.TempDir(), "test")
	f.HostGap = 0
	return Env{Fetcher: f, DB: db}, &queries
}

func hasLink(links []string, want string) bool {
	for _, l := range links {
		if l == want {
			return true
		}
	}
	return false
}

func TestInternetArchiveTexts(t *testing.T) {
	env, queries := iaFixture(t)
	ctx := context.Background()
	hrefs := func(t *testing.T, target string) ([]string, string) {
		t.Helper()
		d, err := Route(ctx, target, env)
		if err != nil {
			t.Fatal(err)
		}
		var hs []string
		for _, l := range d.Links {
			hs = append(hs, l.Href)
		}
		return hs, d.Title + "\n" + flat(d) + "\nnext=" + d.Next + " prev=" + d.Prev
	}

	// The Stacks lead here.
	links, _ := hrefs(t, "w5f:books")
	if !hasLink(links, "w5f:books/ia") {
		t.Errorf("The Stacks have no way to the Archive: %v", links)
	}

	// All texts: public ones only, most read first; the collections lead.
	*queries = nil
	links, page := hrefs(t, "w5f:books/ia")
	if len(*queries) != 2 || !strings.HasPrefix((*queries)[0], "mediatype:texts AND -collection:inlibrary") ||
		!strings.Contains((*queries)[0], "| downloads desc | page 1") || !strings.HasPrefix((*queries)[1], "mediatype:collection AND collection:(texts)") {
		t.Errorf("queries: %q", *queries)
	}
	for _, want := range []string{"w5f:books/ia?c=americana", "w5f:books/ia/item?id=shadowbook", "w5f:books/ia?page=2",
		"w5f:books/ia?show=coll", "w5f:books/ia?lang=tr", "w5f:books/ia?y=1800", "w5f:books/ia?sort=new", "w5f:books/ia/random"} {
		if !hasLink(links, want) {
			t.Errorf("no link %s in %v", want, links)
		}
	}
	for _, want := range []string{"Wolfe, Gene · 1980 · 4321 reads", "all 40 collections in here", "next=w5f:books/ia?page=2"} {
		if !strings.Contains(page, want) {
			t.Errorf("no %q in\n%s", want, page)
		}
	}

	root, _ := Route(ctx, "w5f:books/ia", env)
	var heads []string
	for _, b := range root.Blocks {
		if h, ok := b.(doc.Heading); ok {
			heads = append(heads, h.Text.PlainText())
		}
	}
	if strings.Join(heads, " / ") != "Collections / Texts — 1–1 of 120" {
		t.Errorf("headings: %q", heads)
	}

	// A collection, in Turkish, of the 1800s, newest added, page 2: its
	// name and description from its record, the filters in the query.
	*queries = nil
	links, page = hrefs(t, "w5f:books/ia?c=americana&lang=tr&y=1800&sort=new&page=2")
	if len(*queries) != 1 || !strings.Contains((*queries)[0], "collection:(americana)") ||
		!strings.Contains((*queries)[0], "language:(Turkish OR tur OR tr)") || !strings.Contains((*queries)[0], "year:[1800 TO 1899]") ||
		!strings.Contains((*queries)[0], "| addeddate desc | page 2") {
		t.Errorf("queries: %q", *queries)
	}
	if !strings.HasPrefix(page, "American Libraries\n") || !strings.Contains(page, "Books from American libraries.") ||
		!strings.Contains(page, "prev=w5f:books/ia?c=americana&lang=tr&sort=new&y=1800") {
		t.Errorf("collection page:\n%s", page)
	}
	if !hasLink(links, "w5f:books/ia?c=americana&sort=new&y=1800") { // the language let go
		t.Errorf("links: %v", links)
	}

	// Field syntax passes through; plain words lose the Archive's operators.
	*queries = nil
	hrefs(t, `w5f:books/ia?q=subject%3A"alchemy"`)
	hrefs(t, "w5f:books/ia?q="+"magic+-+(occult)")
	if !strings.Contains((*queries)[0], `AND (subject:"alchemy")`) || !strings.Contains((*queries)[1], "AND (magic occult)") {
		t.Errorf("queries: %q", *queries)
	}
	if _, err := Route(ctx, "w5f:books/ia?c=a)b", env); err == nil {
		t.Error("a collection name with Lucene in it was taken")
	}

	// The item: EPUB first, the private file left out; its subjects,
	// collections and author lead to more.
	links, page = hrefs(t, "w5f:books/ia/item?id=shadowbook")
	if strings.Contains(page, "secret") || !strings.Contains(page, "EPUB — shadow.epub — EPUB\nPDF — shadow.pdf — Text PDF (1.0 MB)\nTXT — full text") {
		t.Errorf("item:\n%s", page)
	}
	for _, want := range []string{`w5f:books/ia?q=subject%3A%22Fantasy%22`, `w5f:books/ia?q=subject%3A%22Dying+Earth%22`,
		"w5f:books/ia?c=americana", `w5f:books/ia?q=creator%3A%22Wolfe%2C+Gene%22`, "w5f:books/ia/get?f=shadow.epub&id=shadowbook"} {
		if !hasLink(links, want) {
			t.Errorf("no link %s in %v", want, links)
		}
	}

	// Read it: kept in the library, opened at once; the item then says so.
	d, err := Route(ctx, "w5f:books/ia/get?f=shadow.epub&id=shadowbook", env)
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "The Shadow of the Torturer" || !strings.HasPrefix(d.Ref, "book:") {
		t.Errorf("opened: %q %q", d.Title, d.Ref)
	}
	if b, ok := env.DB.BookBySource("ia:shadowbook/shadow.epub"); !ok || b.Author != "Gene Wolfe" {
		t.Errorf("in the library: %+v %v", b, ok)
	}
	if _, page = hrefs(t, "w5f:books/ia/item?id=shadowbook"); !strings.Contains(page, "✓ in your library — open it") {
		t.Errorf("item after reading:\n%s", page)
	}

	// A lending-library text says where it can be read instead.
	lend, err := Route(ctx, "w5f:books/ia/item?id=borrowed", env)
	if err != nil {
		t.Fatal(err)
	}
	if n, ok := lend.Blocks[len(lend.Blocks)-3].(doc.Notice); !ok || !strings.Contains(n.Text, "borrowed on the Internet Archive's site") {
		t.Errorf("lending item: %+v", lend.Blocks)
	}
	if _, err := Route(ctx, "w5f:books/ia/get?f=b.pdf&id=borrowed", env); err == nil || !strings.Contains(err.Error(), "lending-library") {
		t.Errorf("a lending-library download: %v", err)
	}

	// A random text: counted, then one row of it.
	*queries = nil
	if _, page = hrefs(t, "w5f:books/ia/random?c=americana"); !strings.HasPrefix(page, "The Shadow of the Torturer") {
		t.Errorf("random:\n%s", page)
	}
	if len(*queries) != 2 || !strings.Contains((*queries)[1], "identifier asc") {
		t.Errorf("random queries: %q", *queries)
	}
}
