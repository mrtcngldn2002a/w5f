package fiction

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"w5f/internal/fetch"
)

func TestWordPressTableOfContents(t *testing.T) {
	srv := fixtureServer(t, map[string]string{
		"/table-of-contents/":      "wp-toc.html",
		"/2012/06/30/plague-12-2/": "wp-chapter.html",
	})
	f := fetch.New("", "test")
	f.HostGap = 0
	ctx := context.Background()
	a := wordPress{}
	// The fixture's links point to parahumans.wordpress.com; the test server
	// stands in for that host.
	wpHostAlias = map[string]string{"parahumans.wordpress.com": strings.TrimPrefix(srv.URL, "http://")}
	defer func() { wpHostAlias = nil }()
	s, err := a.Serial(ctx, f, srv.URL+"/table-of-contents/")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Chapters) < 150 || s.Title == "" || strings.EqualFold(s.Title, "Table of Contents") {
		t.Fatalf("toc: %q %d", s.Title, len(s.Chapters))
	}
	var ch Chapter
	for _, c := range s.Chapters {
		if strings.HasSuffix(c.URL, "/2012/06/30/plague-12-2/") {
			ch = c
		}
	}
	if ch.URL == "" {
		t.Fatal("plague 12.2 not in the contents")
	}
	d, err := a.Chapter(ctx, f, ch)
	if err != nil {
		t.Fatal(err)
	}
	txt := flat(d)
	if strings.Contains(txt, "Next Chapter") || strings.Contains(txt, "Last Chapter") || len(txt) < 2000 {
		t.Errorf("chapter text (navigation rows must go):\n%.500s", txt)
	}
}

func TestWordPressSiteRootUsesThePostList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/":
			fmt.Fprint(w, `<html><body><main><article><h1>Latest: Chapter 3</h1><p>news</p><a href="/a">a</a><a href="/b">b</a><a href="/c">c</a><a href="/d">d</a><a href="/e">e</a><a href="/f">f</a></article></main></body></html>`)
		case r.URL.Path == "/wp-json/wp/v2/posts" && r.URL.Query().Get("page") == "1":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `[{"date":"2016-01-01T00:00:00","link":"%[1]s/prologue/","title":{"rendered":"Prologue"}},
{"date":"2016-01-02T00:00:00","link":"%[1]s/chapter-1/","title":{"rendered":"Chapter 1: Dark Satanic Mills &#8217;"}}]`, "http://"+r.Host)
		default:
			w.WriteHeader(400)
			fmt.Fprint(w, `{"code":"rest_post_invalid_page_number"}`)
		}
	}))
	defer srv.Close()
	f := fetch.New("", "test")
	f.HostGap = 0
	s, err := wordPress{}.Serial(context.Background(), f, srv.URL+"/")
	if err != nil || len(s.Chapters) != 2 || s.Chapters[0].Title != "Prologue" || s.Chapters[1].Title != "Chapter 1: Dark Satanic Mills ’" ||
		s.Chapters[1].Published.Day() != 2 {
		t.Fatalf("post list: %v %+v", err, s)
	}
}

func TestWordPressFollowsNextLinks(t *testing.T) {
	var srv *httptest.Server
	page := func(n int) string {
		next := ""
		if n < 3 {
			next = fmt.Sprintf(`<p><a href="%s/c%d/">Next Chapter</a></p>`, srv.URL, n+1)
		}
		return fmt.Sprintf(`<html><body><article><h1 class="entry-title">Part %d</h1><div class="entry-content">
<p>Text of part %d, long enough to be a real paragraph of a web serial chapter.</p>%s</div></article></body></html>`, n, n, next)
	}
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var n int
		if _, err := fmt.Sscanf(r.URL.Path, "/c%d/", &n); err != nil {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, page(n))
	}))
	defer srv.Close()
	f := fetch.New("", "test")
	f.HostGap = 0
	a := wordPress{}
	s, err := a.SerialFrom(context.Background(), f, srv.URL+"/c1/", nil)
	if err != nil || len(s.Chapters) != 3 || s.Chapters[2].Title != "Part 3" {
		t.Fatalf("walk: %v %+v", err, s)
	}
	// A refresh continues from the last known chapter.
	s2, err := a.SerialFrom(context.Background(), f, srv.URL+"/c1/", s.Chapters[:2])
	if err != nil || len(s2.Chapters) != 3 {
		t.Fatalf("refresh walk: %v %+v", err, s2)
	}
}
