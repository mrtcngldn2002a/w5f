package sitecat

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// fakeSite serves 10 result pages of 5 books each for "dracula"; page 11+
// repeats page 10 (a common site behaviour).
func fakeSite(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			fmt.Fprint(w, `<html><head><title>Fake Library - Home</title></head><body>
<form action="/search" method="get"><input type="search" name="q">
<select name="in"><option value="all">All</option><option value="author">Author</option><option value="title">Title</option></select>
<input type="hidden" name="lang" value="en"></form></body></html>`)
		case "/search":
			p, _ := strconv.Atoi(r.URL.Query().Get("page"))
			if p < 1 {
				p = 1
			}
			if p > 10 {
				p = 10
			}
			var b strings.Builder
			b.WriteString(`<html><body><ul class="results">`)
			for i := 0; i < 5; i++ {
				n := (p-1)*5 + i
				fmt.Fprintf(&b, `<li class="hit"><a href="/book/%d">Dracula volume %d</a> by Bram Stoker</li>`, n, n)
			}
			b.WriteString(`</ul>`)
			fmt.Fprintf(&b, `<a href="/search?q=%s&in=%s&lang=en&page=%d">Next ›</a></body></html>`,
				r.URL.Query().Get("q"), r.URL.Query().Get("in"), p+1)
			fmt.Fprint(w, b.String())
		}
	}))
}

func TestParseQueryAndBuildURL(t *testing.T) {
	if q := ParseQuery("author:Bram Stoker"); q.Field != "author" || q.Words != "Bram Stoker" {
		t.Errorf("%+v", q)
	}
	if q := ParseQuery("Dracula"); q.Field != "" || q.Words != "Dracula" {
		t.Errorf("%+v", q)
	}
	p := Profile{Search: SearchSpec{Template: "https://s.example/find?q={q}"}}
	u, ignored := BuildURL(p, ParseQuery("title:Dracula's Guest"))
	if u != "https://s.example/find?q=Dracula%27s+Guest" || !ignored {
		t.Errorf("no fields: %s %v", u, ignored)
	}
	p.Search.Fields = map[string]string{"title": "https://s.example/find?q={q}&in=title"}
	if u, ignored := BuildURL(p, ParseQuery("title:Dracula")); u != "https://s.example/find?q=Dracula&in=title" || ignored {
		t.Errorf("field: %s %v", u, ignored)
	}
}

func TestFullSearchAcrossPages(t *testing.T) {
	srv := fakeSite(t)
	defer srv.Close()
	f := testFetcher()
	rep, err := Probe(context.Background(), f, srv.URL+"/", "dracula")
	if err != nil || !rep.CanAdd {
		t.Fatalf("probe: %v %+v", err, rep)
	}
	p := rep.Profile
	if p.Search.Fields["author"] == "" || p.Search.Fields["title"] == "" {
		t.Errorf("fields not learned: %+v", p.Search)
	}
	pages := 0
	got, err := Search(context.Background(), f, &p, "dracula", func(page, found int) { pages = page })
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Results) != 50 || got.Pages != 10 || pages != 10 {
		t.Errorf("results=%d pages=%d progress=%d", len(got.Results), got.Pages, pages)
	}
	// Stops on the repeated last page rather than looping to max_pages.
	p.Search.MaxPages = 30
	got, _ = Search(context.Background(), f, &p, "dracula", nil)
	if len(got.Results) != 50 || got.Pages > 11 || got.Capped {
		t.Errorf("repeat stop: results=%d pages=%d capped=%v", len(got.Results), got.Pages, got.Capped)
	}
	// Caps.
	p.Search.MaxPages = 3
	got, _ = Search(context.Background(), f, &p, "dracula", nil)
	if len(got.Results) != 15 || !got.Capped {
		t.Errorf("page cap: %d capped=%v", len(got.Results), got.Capped)
	}
	p.Search.MaxPages, p.Search.MaxResults = 10, 12
	got, _ = Search(context.Background(), f, &p, "dracula", nil)
	if len(got.Results) != 12 || !got.Capped {
		t.Errorf("result cap: %d", len(got.Results))
	}
	// Field routing.
	p.Search.MaxResults = 300
	got, _ = Search(context.Background(), f, &p, "author:Stoker", nil)
	if got.FieldIgnored || len(got.Results) == 0 {
		t.Errorf("author field: %+v", got)
	}
}
