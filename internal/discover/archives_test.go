package discover

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"w5f/internal/doc"
	"w5f/internal/fetch"
)

func testFetcher() *fetch.Fetcher {
	f := fetch.New("", "test")
	f.HostGap = 0
	return f
}

func swap[T any](t *testing.T, v *T, to T) {
	old := *v
	*v = to
	t.Cleanup(func() { *v = old })
}

func TestIEPPicksAnArticleOfALetter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		letter := strings.Trim(r.URL.Path, "/")
		fmt.Fprintf(w, `<html><body><nav><a href="/a/">A</a><a href="/b/">B</a></nav>
<ul><li><a href="/%s-one/" title="%s One" rel="bookmark">One</a></li>
<li><a href="https://elsewhere.example/x" rel="bookmark">off-site</a></li></ul></body></html>`, letter, strings.ToUpper(letter))
	}))
	defer srv.Close()
	swap(t, &iepBase, srv.URL+"/")
	target, title, err := iepPick(context.Background(), testFetcher())
	if err != nil || !strings.HasPrefix(target, srv.URL+"/") || !strings.HasSuffix(target, "-one/") || !strings.HasSuffix(title, " One") {
		t.Fatalf("iep: %q %q %v", target, title, err)
	}
}

func TestPerseusPicksAnEnglishReadingText(t *testing.T) {
	row := func(author, lang, href string) string {
		return fmt.Sprintf(`<tr class="trResults"><td class="tdAuthor" colspan="2">%s
			(%s) <a href="search?doc=x">search this work</a><ul class="subdoc"><li><a class="aResultsHeader" href="%s">part</a></li></ul></td></tr>`, author, lang, href)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><body><table class="tResults">`+
			row("Frank Frost Abbott. Commentary on Selected Letters of Cicero.", "English", "text?doc=comm")+
			row("Homer. Iliad. A. T. Murray.", "Greek", "text?doc=greek")+
			row("George A. Reisner. A History of the Giza Necropolis.", "English", "text?doc=Perseus%3atext%3a1999.04.0103")+
			row("Rerum Gestarum, volume 1 introduction. John C. Rolfe.", "English", "text?doc=Perseus%3atext%3a2007.01.0001")+
			row("Homer. Iliad. A. T. Murray.", "English", "text?doc=Perseus%3atext%3a1999.01.0134")+
			`</table></body></html>`)
	}))
	defer srv.Close()
	swap(t, &perseusCollection, srv.URL+"/hopper/collection?collection=x")
	for i := 0; i < 5; i++ {
		target, title, err := perseusPick(context.Background(), Env{Fetcher: testFetcher()})
		if err != nil || target != srv.URL+"/hopper/text?doc=Perseus%3atext%3a1999.01.0134" || title != "Homer. Iliad. A. T. Murray" {
			t.Fatalf("perseus: %q %q %v", target, title, err)
		}
	}
}

func TestBHLOpensTheFullTextOfACuriousBook(t *testing.T) {
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.Query().Get("q"))
		fmt.Fprint(w, `{"response":{"numFound":73,"start":0,"docs":[{"identifier":"traditionsofcadd41dors","title":"Traditions of the  Caddo","year":1905}]}}`)
	}))
	defer srv.Close()
	swap(t, &archiveAPI, srv.URL)
	d, err := bhlPick(context.Background(), Env{Fetcher: testFetcher()})
	if err != nil || d.Target != srv.URL+"/stream/traditionsofcadd41dors/traditionsofcadd41dors_djvu.txt" ||
		d.Why != "public domain/Biodiversity Heritage Library · Traditions of the Caddo (1905)" {
		t.Fatalf("bhl: %+v %v", d, err)
	}
	if len(queries) != 2 || !strings.Contains(queries[0], "collection:biodiversity") {
		t.Errorf("queries: %q", queries)
	}
}

func TestSmallWebDirectories(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/random/":
			fmt.Fprint(w, `<ol class="websites"><li class="websites__item"><p class="website__intro"><a href="https://blog.example/">Quiet Blog</a><br><q>Notes on old books.</q> <a href="/blog/x1/">More info</a></p>
<details><figure><figcaption><a href="https://blog.example/2026/post.html">A post</a></figcaption></figure></details></li></ol>`)
		case r.URL.Path == "/feed/":
			w.Header().Set("Content-Type", "application/atom+xml")
			fmt.Fprint(w, `<?xml version="1.0" encoding="utf-8"?><feed xmlns="http://www.w3.org/2005/Atom"><title>Kagi Small Web</title>
<entry><title>On gardens</title><link href="https://person.example/gardens"/><id>1</id><updated>2026-09-30T00:00:00Z</updated></entry></feed>`)
		case r.URL.Path == "/area51/":
			fmt.Fprint(w, `<html><head><title>Index of /area51</title></head><body><h1>Index of /area51</h1><a href="?C=N;O=D">Name</a><a href="/">Parent Directory</a><a href="1013/">1013/</a><a href="zone.71e.delayed.tmp">tmp</a></body></html>`)
		case r.URL.Path == "/area51/1013/":
			fmt.Fprint(w, `<html><head><title>Index of /area51/1013</title></head><body><p>This page or folder is old! A long museum notice.</p><a href="/area51/">Parent Directory</a><a href="notes.htm">notes.htm</a><a href="index.html">index.html</a></body></html>`)
		case r.URL.Path == "/area51/1013/index.html":
			fmt.Fprint(w, `<html><head><title>My X-Files Shrine</title></head><body><p>Welcome!</p></body></html>`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	swap(t, &oohRandom, srv.URL+"/random/")
	swap(t, &kagiFeed, srv.URL+"/feed/")
	swap(t, &oocitiesTop, srv.URL+"/")
	swap(t, &oocitiesHoods, []string{"area51"})
	f := testFetcher()

	d, err := oohDraw(context.Background(), f)
	if err != nil || d.Target != "https://blog.example/2026/post.html" || d.Why != "smallweb/ooh.directory · Quiet Blog — Notes on old books." {
		t.Errorf("ooh: %+v %v", d, err)
	}
	d, err = kagiDraw(context.Background(), f)
	if err != nil || d.Target != "https://person.example/gardens" || !strings.Contains(d.Why, "person.example — On gardens") {
		t.Errorf("kagi: %+v %v", d, err)
	}
	d, err = oocitiesDraw(context.Background(), f)
	if err != nil || d.Target != srv.URL+"/area51/1013/index.html" || !strings.Contains(d.Why, "area51 — My X-Files Shrine") {
		t.Errorf("geocities: %+v %v", d, err)
	}

	// The Small Web page opens each directory directly, saying where from.
	var loaded string
	env := Env{Fetcher: f, Load: func(_ context.Context, target string) (*doc.Document, error) {
		loaded = target
		return &doc.Document{Title: "page"}, nil
	}}
	page, err := Route(context.Background(), "w5f:discover/ooh", env)
	if err != nil || loaded != "https://blog.example/2026/post.html" || len(page.Blocks) == 0 {
		t.Fatalf("ooh route: %v %q", err, loaded)
	}
	if n, ok := page.Blocks[0].(doc.Notice); !ok || !strings.HasPrefix(n.Text, "ooh.directory random blog · Quiet Blog") {
		t.Errorf("notice: %+v", page.Blocks[0])
	}
	if _, err := Route(context.Background(), "w5f:discover/nothing", env); err == nil {
		t.Error("an unknown directory is an error")
	}
}
