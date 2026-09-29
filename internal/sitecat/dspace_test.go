package sitecat

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDSpaceCatalog(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	api := srv.URL + "/server/api"
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><head><title>Open Repository</title></head><body><ds-app></ds-app></body></html>`)
	})
	mux.HandleFunc("/server/api", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"dspaceName":"Open Repository","dspaceVersion":"DSpace 8.2","_links":{}}`)
	})
	mux.HandleFunc("/server/api/discover/search/objects", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("dsoType") != "ITEM" {
			t.Errorf("dsoType = %q", q.Get("dsoType"))
		}
		if strings.HasPrefix(q.Get("query"), "dc.contributor.author:") && q.Get("query") != "dc.contributor.author:(Stoker)" {
			t.Errorf("author query = %q", q.Get("query"))
		}
		obj := func(id, title, author string) string {
			return `{"_embedded":{"indexableObject":{"uuid":"` + id + `","name":"` + title + `","type":"item","metadata":{"dc.title":[{"value":"` + title + `"}],"dc.contributor.author":[{"value":"` + author + `"}],"dc.date.issued":[{"value":"1897"}]}}}}`
		}
		if q.Get("page") == "1" {
			fmt.Fprint(w, `{"_embedded":{"searchResult":{"page":{"number":1,"size":20,"totalPages":2,"totalElements":2},"_embedded":{"objects":[`+obj("u2", "Dracula's Guest", "Stoker, Bram")+`]}}}}`)
			return
		}
		fmt.Fprint(w, `{"_embedded":{"searchResult":{"page":{"number":0,"size":20,"totalPages":2,"totalElements":2},"_embedded":{"objects":[`+obj("u1", "Dracula", "Stoker, Bram")+`]}}}}`)
	})
	mux.HandleFunc("/server/api/core/items/u1/bundles", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"_embedded":{"bundles":[{"name":"THUMBNAIL","_links":{"bitstreams":{"href":"`+api+`/core/bundles/t/bitstreams"}}},{"name":"ORIGINAL","_links":{"bitstreams":{"href":"`+api+`/core/bundles/o/bitstreams"}}}]}}`)
	})
	mux.HandleFunc("/server/api/core/bundles/o/bitstreams", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"_embedded":{"bitstreams":[{"uuid":"b1","name":"dracula.pdf","sizeBytes":2097152,"_links":{"content":{"href":"`+api+`/core/bitstreams/b1/content"}}},{"uuid":"b2","name":"license.txt.xml","sizeBytes":5,"_links":{"content":{"href":"`+api+`/core/bitstreams/b2/content"}}}]}}`)
	})
	f := testFetcher()
	rep, err := Probe(context.Background(), f, srv.URL, "dracula")
	if err != nil || !rep.CanAdd || rep.Profile.Search.Kind != "dspace" || rep.Profile.Search.API != api {
		t.Fatalf("probe: %v %+v", err, rep)
	}
	p := rep.Profile
	got, err := Search(context.Background(), f, &p, "dracula", nil)
	if err != nil || len(got.Results) != 2 || got.Pages != 2 {
		t.Fatalf("search: %v %+v", err, got)
	}
	if r := got.Results[0]; r.Title != "Dracula" || r.URL != srv.URL+"/items/u1" || r.Extra != "Stoker, Bram · 1897" {
		t.Errorf("first: %+v", r)
	}
	Search(context.Background(), f, &p, "author:Stoker", nil)
	ds, err := itemDownloads(context.Background(), f, &p, srv.URL+"/items/u1")
	if err != nil || len(ds) != 1 || ds[0].URL != api+"/core/bitstreams/b1/content" || ds[0].Format != "pdf" || !strings.Contains(ds[0].Label, "2.0 MB") {
		t.Errorf("downloads: %v %+v", err, ds)
	}
}
