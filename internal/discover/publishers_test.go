package discover

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"w5f/internal/fetch"
)

func TestBritannicaSelectsReadableArticles(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/browse/Philosophy-Religion":
			fmt.Fprint(w, `<nav><a href="/topic/navigation">Nav</a></nav><a href="/quiz/not-an-article">Quiz</a><a href="https://elsewhere.test/topic/offsite">Offsite</a><a href="/topic/Hermeticism">Hermeticism</a>`)
		case "/topic/Hermeticism":
			http.Redirect(w, r, "/art/Hermeticism", http.StatusFound)
		case "/art/Hermeticism":
			fmt.Fprintf(w, `<title>Hermeticism | Britannica</title><article><h1>Hermeticism</h1><p>%s</p></article>`, longText(30))
		default:
			t.Errorf("selected non-article %s", r.URL)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	f := fetch.New("", "test")
	f.HostGap = 0
	target, title, err := britannicaPick(context.Background(), f, srv.URL+"/browse/Philosophy-Religion")
	if err != nil || title != "Hermeticism" || target != srv.URL+"/art/Hermeticism" {
		t.Fatalf("%q %q %v", target, title, err)
	}
}

func TestDiscoverySourcesAndFamilyFallback(t *testing.T) {
	names := map[string]bool{}
	for _, s := range esotericSites {
		names[s.name] = true
	}
	if !names["Sacred Texts"] || !names["Hermetic Library"] {
		t.Fatal("missing esoteric publishers")
	}
	found := false
	for _, s := range encyclopedias {
		if s.name == "Britannica" {
			found = true
		}
	}
	if !found {
		t.Fatal("missing Britannica")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/blocked":
			w.WriteHeader(403)
			fmt.Fprint(w, `<title>Just a moment...</title>`)
		case "/random":
			http.Redirect(w, r, "/article", 302)
		default:
			fmt.Fprint(w, "<title>Article</title><p>Readable content</p>")
		}
	}))
	defer srv.Close()
	f := fetch.New("", "test")
	f.HostGap = 0
	old := encyclopedias
	defer func() { encyclopedias = old }()
	encyclopedias = []site{{"Blocked", srv.URL + "/blocked"}, {"Working", srv.URL + "/random"}}
	for i := 0; i < 10; i++ {
		d, err := (encyclopedic{}).Draw(context.Background(), Env{Fetcher: f})
		if err != nil || !strings.HasPrefix(d.Why, "encyclopedic/Working") {
			t.Fatalf("family fallback: %+v %v", d, err)
		}
	}
}

func TestLongIndexIsNotReturnedAsReadingMatter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/world.htm":
			fmt.Fprint(w, `<a href="/book/index.htm">Book</a>`)
		case "/book/index.htm":
			fmt.Fprintf(w, `<title>Contents</title><p>%s</p><a href="chapter.htm">Chapter</a>`, longText(50))
		case "/book/chapter.htm":
			fmt.Fprintf(w, `<title>A real text</title><p>%s</p>`, longText(50))
		}
	}))
	defer srv.Close()
	f := fetch.New("", "test")
	f.HostGap = 0
	u, title, err := descend(context.Background(), f, srv.URL+"/world.htm", 4)
	if err != nil || !strings.HasSuffix(u, "/chapter.htm") || title != "A real text" {
		t.Fatalf("%s %s %v", u, title, err)
	}
}

func TestSacredTextsStaysInSelectedReligion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/world.htm":
			fmt.Fprint(w, `<a href="./time/index.htm">Sidebar</a><a href="bud/index.htm">Buddhism</a><a href="cdshop/index.htm">Shop</a><a href="time/timeline.htm">Timeline</a>`)
		case "/bud/index.htm":
			fmt.Fprint(w, `<a href="/time/timeline.htm">Navigation</a><a href="book/index.htm">Book</a>`)
		case "/bud/book/index.htm":
			fmt.Fprintf(w, `<p>%s</p><a href="chapter.htm">Chapter</a>`, longText(60))
		case "/bud/book/chapter.htm":
			fmt.Fprintf(w, `<title>The chapter</title><p>%s</p>`, longText(60))
		default:
			t.Errorf("picked sidebar or escaped religion: %s", r.URL)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	f := fetch.New("", "test")
	f.HostGap = 0
	u, title, err := sacredTextsPick(context.Background(), f, srv.URL+"/world.htm")
	if err != nil || title != "The chapter" || u != srv.URL+"/bud/book/chapter.htm" {
		t.Fatalf("%s %s %v", u, title, err)
	}
}
