package sitecat

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// An old site: POST search, results paged by a "Next" button form.
func postSite(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			fmt.Fprint(w, `<html><body>
<form action="/login" method="post"><input name="user"><input type="password" name="pw"></form>
<form action="/news" method="post"><input type="email" name="mail"></form>
<form action="/find.asp" method="post"><input name="query"><input type="hidden" name="section" value="books"></form>
</body></html>`)
			return
		}
		r.ParseForm()
		if r.PostForm.Get("section") != "books" || r.PostForm.Get("query") == "" {
			http.Error(w, "bad form", 400)
			return
		}
		page := r.PostForm.Get("page")
		if page == "" {
			page = "1"
		}
		fmt.Fprintf(w, `<html><body><ul class="hits">`)
		for i := 0; i < 3; i++ {
			fmt.Fprintf(w, `<li><a href="/book/%s-%d">%s book number %s-%d</a></li>`, page, i, r.PostForm.Get("query"), page, i)
		}
		fmt.Fprint(w, `</ul>`)
		if page == "1" {
			fmt.Fprintf(w, `<form action="/find.asp" method="post"><input type="hidden" name="query" value="%s"><input type="hidden" name="section" value="books"><input type="hidden" name="page" value="2"><input type="submit" value="Next"></form>`, r.PostForm.Get("query"))
		}
		fmt.Fprint(w, `</body></html>`)
	}))
}

func TestPostFormSearchAcrossPages(t *testing.T) {
	srv := postSite(t)
	defer srv.Close()
	f := testFetcher()
	rep, err := Probe(context.Background(), f, srv.URL, "dracula")
	if err != nil || !rep.CanAdd {
		t.Fatalf("probe: %v %+v", err, rep)
	}
	sp := rep.Profile.Search
	if sp.Kind != "post" || sp.Method != "POST" || sp.Template != srv.URL+"/find.asp" || sp.Body != "query={q}&section=books" {
		t.Fatalf("spec: %+v", sp)
	}
	p := rep.Profile
	got, err := Search(context.Background(), f, &p, "dracula", nil)
	if err != nil || len(got.Results) != 6 || got.Pages != 2 {
		t.Errorf("search: %v results=%d pages=%d", err, len(got.Results), got.Pages)
	}
}

func TestNextFormGET(t *testing.T) {
	body := page(`<form action="/s"><input type="hidden" name="q" value="x"><input type="hidden" name="p" value="3"><button type="submit">Next ›</button></form>`)
	r := nextForm(body, "https://s.example/s?q=x&p=2")
	if r == nil || r.Method != "GET" || r.URL != "https://s.example/s?p=3&q=x" {
		t.Errorf("next = %+v", r)
	}
	if nextForm(page(`<form action="/s"><input type="submit" value="Search"></form>`), "https://s.example/") != nil {
		t.Error("a plain search button is not a next page")
	}
}
