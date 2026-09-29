package sitecat

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"w5f/internal/fetch"
)

func siteOf(t *testing.T, home string, body string) *site {
	u, _ := url.Parse(home)
	return newSite(context.Background(), testFetcher(), &fetch.Response{URL: u, Body: []byte(body)})
}

func TestPlatformHints(t *testing.T) {
	cases := []struct{ home, body, want string }{
		{"https://journals.example/index.php/folk/index", `<meta name="generator" content="Open Journal Systems 3.3.0.8">`,
			"https://journals.example/index.php/folk/search/search?query={q}"},
		{"https://eprints.example/", `<meta name="generator" content="EPrints 3.4">`,
			"https://eprints.example/cgi/search/simple?q={q}"},
		{"https://wiki.example/wiki/Main_Page", `<meta name="generator" content="MediaWiki 1.41"><link rel="EditURI" type="application/rsd+xml" href="https://wiki.example/w/api.php?action=rsd">`,
			"https://wiki.example/w/index.php?search={q}&title=Special:Search&fulltext=1&ns0=1&ns6=1"},
		{"https://occultblog.example/", `<meta content='blogger' name='generator'/>`,
			"https://occultblog.example/search?q={q}"},
	}
	for _, c := range cases {
		cs := detectHints(context.Background(), siteOf(t, c.home, "<html><head>"+c.body+"</head><body></body></html>"))
		if len(cs) != 1 || cs[0].Spec.Kind != "form" || cs[0].Spec.Template != c.want {
			t.Errorf("%s: %+v", c.home, cs)
		}
	}
	if cs := detectHints(context.Background(), siteOf(t, "https://plain.example/", "<html></html>")); len(cs) != 0 {
		t.Errorf("no generator, no hints: %+v", cs)
	}
}

func TestCalibreOPDSPath(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mux.HandleFunc("/opds", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<feed xmlns="http://www.w3.org/2005/Atom"><link rel="search" type="application/atom+xml" href="/opds/search/{searchTerms}"/></feed>`)
	})
	s := siteOf(t, srv.URL+"/", `<html><head><title>Calibre-Web | Books</title></head><body></body></html>`)
	cs := detectOPDSPath(context.Background(), s)
	if len(cs) != 1 || cs[0].Spec.Kind != "opds" || cs[0].Spec.Template != srv.URL+"/opds/search/{q}" {
		t.Errorf("candidates: %+v", cs)
	}
	if cs := detectOPDSPath(context.Background(), siteOf(t, srv.URL+"/", "<html><title>Blog</title></html>")); len(cs) != 0 {
		t.Error("sites without calibre/opds markers are not probed")
	}
}

func TestFindInOJSGalleyAndQueryFiles(t *testing.T) {
	body := page(`<a class="obj_galley_link pdf" href="/index.php/folk/article/view/12/34">PDF</a>
<a href="/link.php?file=20170553-a5.pdf">Download (A5)</a>`)
	ds := FindIn(body, "https://journals.example/index.php/folk/article/view/12", DefaultPrefer)
	if len(ds) != 2 || ds[0].URL != "https://journals.example/index.php/folk/article/download/12/34" || ds[0].Format != "pdf" ||
		ds[1].URL != "https://journals.example/link.php?file=20170553-a5.pdf" {
		t.Errorf("downloads: %+v", ds)
	}
}
