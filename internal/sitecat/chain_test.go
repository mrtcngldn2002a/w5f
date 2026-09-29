package sitecat

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A site whose OpenSearch results are built with JavaScript but whose plain
// form works: the chain must fall through to the form.
func TestProbeTriesNextCandidate(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><head><link rel="search" type="application/opensearchdescription+xml" href="/osd.xml"></head>
<body><form action="/s"><input type="search" name="q"></form></body></html>`)
	})
	mux.HandleFunc("/osd.xml", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<OpenSearchDescription><Url type="text/html" template="`+srv.URL+`/app?q={searchTerms}"/></OpenSearchDescription>`)
	})
	mux.HandleFunc("/app", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><body><div id="app"></div><noscript>Enable JavaScript</noscript></body></html>`)
	})
	mux.HandleFunc("/s", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><body><ul class="r"><li><a href="/b/1">Dracula</a> Stoker</li><li><a href="/b/2">Dracula's Guest</a> Stoker</li><li><a href="/b/3">Dracula (abridged)</a> Stoker</li></ul></body></html>`)
	})
	rep, err := Probe(context.Background(), testFetcher(), srv.URL, "dracula")
	if err != nil || !rep.CanAdd {
		t.Fatalf("probe: %v %+v", err, rep)
	}
	if rep.Profile.Search.Kind != "form" || len(rep.Findings) < 2 {
		t.Fatalf("profile %+v findings %+v", rep.Profile.Search, rep.Findings)
	}
	if rep.Findings[0].OK || !strings.Contains(rep.Findings[0].Text, "JavaScript") || !rep.Findings[1].OK {
		t.Errorf("findings: %+v", rep.Findings)
	}
}

func TestDiscoverAllListsEveryMethod(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mux.HandleFunc("/osd.xml", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<OpenSearchDescription><Url type="text/html" template="`+srv.URL+`/os?q={searchTerms}"/></OpenSearchDescription>`)
	})
	body := []byte(`<html><head><link rel="search" type="application/opensearchdescription+xml" href="/osd.xml"></head>
<body><form action="/s"><input type="search" name="q"></form></body></html>`)
	cs := discoverAll(context.Background(), testFetcher(), body, srv.URL+"/")
	if len(cs) != 2 || cs[0].Spec.Kind != "opensearch" || cs[1].Spec.Kind != "form" {
		t.Errorf("candidates: %+v", cs)
	}
}

func TestDirectDownload(t *testing.T) {
	cases := map[string]string{
		"https://x.example/files/a.pdf":              "pdf",
		"https://x.example/link.php?file=123-a5.pdf": "pdf",
		"https://x.example/get/Book%20One.epub":      "epub",
		"https://x.example/book/1":                   "",
		"https://x.example/archive.zip":              "",
	}
	for in, want := range cases {
		d, ok := directDownload(in)
		if ok != (want != "") || d.Format != want {
			t.Errorf("directDownload(%q) = %+v %v, want %q", in, d, ok, want)
		}
	}
}
