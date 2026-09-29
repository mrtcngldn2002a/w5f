package sitecat

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func archiveFixture(t *testing.T) *httptest.Server {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	mux.HandleFunc("/details/folklore", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><head><title>Folklore : Internet Archive</title></head><body><app-root></app-root></body></html>`)
	})
	mux.HandleFunc("/advancedsearch.php", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		for _, must := range []string{"mediatype:texts", "-collection:inlibrary", "-collection:printdisabled", "-collection:lendinglibrary", "collection:(folklore)"} {
			if !strings.Contains(q, must) {
				t.Errorf("query %q lacks %q", q, must)
			}
		}
		if r.URL.Query().Get("page") == "2" {
			fmt.Fprint(w, `{"response":{"numFound":3,"start":2,"docs":[{"identifier":"c","title":"Third","creator":["A","B"]}]}}`)
			return
		}
		fmt.Fprint(w, `{"response":{"numFound":3,"start":0,"docs":[{"identifier":"dracula1897","title":"Dracula","creator":"Stoker, Bram","year":1897},{"identifier":"lent","title":"Lent Book"}]}}`)
	})
	mux.HandleFunc("/metadata/dracula1897", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"metadata":{"title":"Dracula"},"files":[
{"name":"dracula.pdf","format":"Text PDF","size":"2000000","source":"original"},
{"name":"dracula.epub","format":"EPUB","size":"300000","source":"derivative"},
{"name":"dracula_meta.xml","format":"Metadata","size":"900"},
{"name":"secret.pdf","format":"Text PDF","size":"10","private":"true"},
{"name":"sub dir/extra.txt","format":"DjVuTXT","size":"50000"}]}`)
	})
	mux.HandleFunc("/metadata/lent", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"metadata":{"title":"Lent Book","access-restricted-item":"true"},"files":[{"name":"lent.pdf","size":"1"}]}`)
	})
	mux.HandleFunc("/metadata/big", func(w http.ResponseWriter, r *http.Request) {
		var b strings.Builder
		b.WriteString(`{"metadata":{},"files":[`)
		for i := 0; i < 200; i++ {
			if i > 0 {
				b.WriteString(",")
			}
			fmt.Fprintf(&b, `{"name":"issue%03d.pdf","size":"100"}`, i)
		}
		b.WriteString(`]}`)
		fmt.Fprint(w, b.String())
	})
	return srv
}

func TestArchiveCatalog(t *testing.T) {
	srv := archiveFixture(t)
	defer srv.Close()
	ArchiveBase = srv.URL
	defer func() { ArchiveBase = "https://archive.org" }()
	f := testFetcher()
	rep, err := Probe(context.Background(), f, srv.URL+"/details/folklore", "dracula")
	if err != nil || !rep.CanAdd || rep.Profile.Search.Kind != "archive" || rep.Profile.Search.Collection != "folklore" {
		t.Fatalf("probe: %v %+v", err, rep)
	}
	p := rep.Profile
	got, err := Search(context.Background(), f, &p, "dracula", nil)
	if err != nil || len(got.Results) != 3 || got.Pages != 2 {
		t.Fatalf("search: %v %+v", err, got)
	}
	if r := got.Results[0]; r.Title != "Dracula" || r.URL != srv.URL+"/details/dracula1897" || !strings.Contains(r.Extra, "Stoker") || !strings.Contains(r.Extra, "1897") {
		t.Errorf("first: %+v", r)
	}
	if !strings.Contains(got.Results[2].Extra, "A; B") {
		t.Errorf("creator list: %+v", got.Results[2])
	}
	ds, err := itemDownloads(context.Background(), f, &p, srv.URL+"/details/dracula1897")
	if err != nil || len(ds) != 3 || ds[0].Format != "epub" || ds[0].URL != srv.URL+"/download/dracula1897/dracula.epub" {
		t.Fatalf("downloads: %v %+v", err, ds)
	}
	if ds[2].URL != srv.URL+"/download/dracula1897/sub%20dir/extra.txt" {
		t.Errorf("escaped path: %s", ds[2].URL)
	}
	_, err = itemDownloads(context.Background(), f, &p, srv.URL+"/details/lent")
	var nd *notDownloadable
	if !errors.As(err, &nd) || !strings.Contains(nd.Reason, "lending") {
		t.Errorf("lending item: %v", err)
	}
	ds, _ = itemDownloads(context.Background(), f, &p, srv.URL+"/details/big")
	if len(ds) != 30 {
		t.Errorf("file list not capped: %d", len(ds))
	}
}

func TestArchiveQueryFields(t *testing.T) {
	p := &Profile{}
	if q := archiveQuery(p, Query{Field: "author", Words: `Stoker (Bram) "x":y`}); !strings.HasPrefix(q, "creator:(Stoker Bram x y)") {
		t.Errorf("author: %q", q)
	}
	if q := archiveQuery(p, Query{Field: "title", Words: "Dracula"}); !strings.HasPrefix(q, "title:(Dracula)") {
		t.Errorf("title: %q", q)
	}
	if q := archiveQuery(p, Query{Words: "vampire"}); !strings.HasPrefix(q, "(title:(vampire) OR creator:(vampire) OR subject:(vampire))") {
		t.Errorf("general: %q", q)
	}
}
