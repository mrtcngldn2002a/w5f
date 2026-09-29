package sitecat

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestDiscoverSearchVariants(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mux.HandleFunc("/osd.xml", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<?xml version="1.0"?><OpenSearchDescription xmlns="http://a9.com/-/spec/opensearch/1.1/">
<Url type="text/html" template="`+srv.URL+`/s?query={searchTerms}&amp;page={startPage?}"/></OpenSearchDescription>`)
	})
	mux.HandleFunc("/opds", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<?xml version="1.0"?><feed xmlns="http://www.w3.org/2005/Atom"><title>Catalog</title>
<link rel="search" type="application/opensearchdescription+xml" href="/osd.xml"/></feed>`)
	})
	ctx := context.Background()
	f := testFetcher()
	cases := []struct{ name, body, kind, tmpl string }{
		{"opds", `<head><link rel="alternate" type="application/atom+xml;profile=opds-catalog" href="/opds"></head>`, "opds", srv.URL + "/s?query={q}"},
		{"opensearch", `<head><link rel="search" type="application/opensearchdescription+xml" href="/osd.xml"></head>`, "opensearch", srv.URL + "/s?query={q}"},
		{"form", `<nav><form action="/find"><input name="term"><input type="hidden" name="x" value="1"></form></nav>`, "form", srv.URL + "/find?term={q}&x=1"},
		{"wordpress", `<head><meta name="generator" content="WordPress 6.6"></head>`, "wordpress", srv.URL + "/?s={q}"},
		{"post-only", `<form action="/p" method="post"><input type="search" name="q"></form>`, "post", srv.URL + "/p"},
	}
	for _, c := range cases {
		spec, desc := DiscoverSearch(ctx, f, []byte("<html>"+c.body+"<body></body></html>"), srv.URL+"/")
		if spec.Kind != c.kind || spec.Template != c.tmpl {
			t.Errorf("%s: %+v (%s)", c.name, spec, desc)
		}
	}
}

func TestProbeBrowseOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><head><title>Small Shelf</title></head><body><ul class="books">
<li><a href="/b/1">The King in Yellow</a> Chambers</li><li><a href="/b/2">The Great God Pan</a> Machen</li>
<li><a href="/b/3">The House on the Borderland</a> Hodgson</li><li><a href="/b/4">The Willows</a> Blackwood</li>
<li><a href="/b/5">The Night Land</a> Hodgson</li></ul></body></html>`)
	}))
	defer srv.Close()
	rep, err := Probe(context.Background(), testFetcher(), srv.URL, "")
	if err != nil || !rep.CanAdd || rep.Profile.Search.Kind != "browse" || len(rep.Sample) != 5 {
		t.Fatalf("browse-only: %v %+v", err, rep)
	}
}

func TestProbeReportsJavaScriptSite(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><head><title>JS Books</title></head><body><form action="/search"><input name="q"></form>
<div id="app"></div><noscript>Please enable JavaScript</noscript><script src="/bundle.js"></script></body></html>`)
	}))
	defer srv.Close()
	rep, err := Probe(context.Background(), testFetcher(), srv.URL, "history")
	if err != nil {
		t.Fatal(err)
	}
	if rep.CanAdd {
		t.Error("a JavaScript-only site must not be addable")
	}
	text := ""
	for _, fi := range rep.Findings {
		text += fi.Text + "\n"
	}
	if !strings.Contains(text, "JavaScript") {
		t.Errorf("findings:\n%s", text)
	}
}

func TestDiscoverFieldTemplates(t *testing.T) {
	ctx, f := context.Background(), testFetcher()
	base := "https://lib.example/"
	twoInputs := `<form action="/adv" role="search"><input type="search" name="q"><input name="author" placeholder="Author"><input name="title" placeholder="Title"></form>`
	spec, _ := DiscoverSearch(ctx, f, []byte("<html><body>"+twoInputs+"</body></html>"), base)
	if spec.Template != "https://lib.example/adv?q={q}" || spec.Fields["author"] != "https://lib.example/adv?author={q}" ||
		spec.Fields["title"] != "https://lib.example/adv?title={q}" {
		t.Errorf("two inputs: %+v", spec)
	}
	twoForms := `<form action="/search" class="search"><input type="search" name="q"></form>
<form action="/by-author"><input name="author"></form><form action="/by-title"><input name="booktitle"></form>`
	spec, _ = DiscoverSearch(ctx, f, []byte("<html><body>"+twoForms+"</body></html>"), base)
	if spec.Template != "https://lib.example/search?q={q}" || spec.Fields["author"] != "https://lib.example/by-author?author={q}" ||
		spec.Fields["title"] != "https://lib.example/by-title?booktitle={q}" {
		t.Errorf("two forms: %+v", spec)
	}
}

func TestProbeRejectsShortFrontPageList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><body><ul class="facets"><li><a href="/s?tags=mystery">Tags: mystery 951</a></li>
<li><a href="/s?lang=en">Language: English 8684</a></li><li><a href="/s?by=author">By Author Popularity</a></li></ul></body></html>`)
	}))
	defer srv.Close()
	rep, err := Probe(context.Background(), testFetcher(), srv.URL, "")
	if err != nil || rep.CanAdd {
		t.Fatalf("a three-link facet list must not make a catalog: %v %+v", err, rep)
	}
}

func TestProbeFollowsSearchPageLink(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><body><a href="/about">About us</a> <a href="/find.php">Find a book</a></body></html>`)
	})
	mux.HandleFunc("/find.php", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("title") == "" {
			fmt.Fprint(w, `<html><body><form id="searchform"><input type="text" name="title"><input type="text" name="author"></form></body></html>`)
			return
		}
		fmt.Fprint(w, `<html><body><ul class="r"><li><a href="/book/1">Dracula</a> Stoker</li><li><a href="/book/2">Dracula's Guest</a> Stoker</li><li><a href="/book/3">Dracula (abridged)</a> Stoker</li></ul></body></html>`)
	})
	rep, err := Probe(context.Background(), testFetcher(), srv.URL, "dracula")
	if err != nil || !rep.CanAdd {
		t.Fatalf("probe: %v %+v", err, rep)
	}
	sp := rep.Profile.Search
	if sp.Template != srv.URL+"/find.php?title={q}" || sp.Fields["author"] != srv.URL+"/find.php?author={q}" {
		t.Errorf("search spec: %+v", sp)
	}
}

func TestProbeExplainsJavaScriptWhenNoSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><head><title>App</title></head><body><app-root></app-root><noscript>Enable JavaScript</noscript><script src="/a.js"></script></body></html>`)
	}))
	defer srv.Close()
	rep, _ := Probe(context.Background(), testFetcher(), srv.URL, "")
	text := ""
	for _, fi := range rep.Findings {
		text += fi.Text + "\n"
	}
	if rep.CanAdd || !strings.Contains(text, "JavaScript") {
		t.Errorf("findings:\n%s", text)
	}
}

func TestSiteNamePrefersDeclaredName(t *testing.T) {
	u, _ := url.Parse("https://www.gutenberg.org/")
	body := []byte(`<html><head><title>Free eBooks | Project Gutenberg</title><meta property="og:site_name" content="Project Gutenberg"></head></html>`)
	if got := siteName(body, u); got != "Project Gutenberg" {
		t.Errorf("siteName = %q", got)
	}
	if got := siteName([]byte(`<title>Faded Page | Home</title>`), u); got != "Faded Page" {
		t.Errorf("title fallback = %q", got)
	}
}

func TestSortSelectIsNotAFieldChoice(t *testing.T) {
	form := `<form id="searchform"><input type="text" name="title"><input type="text" name="author">
<select name="sort"><option value="--">--</option><option value="author">Author</option><option value="title">Title</option></select></form>`
	spec, _ := DiscoverSearch(context.Background(), testFetcher(), []byte("<html><body>"+form+"</body></html>"), "https://lib.example/find")
	if spec.Template != "https://lib.example/find?title={q}" || spec.Fields["author"] != "https://lib.example/find?author={q}" {
		t.Errorf("spec: %+v", spec)
	}
}
