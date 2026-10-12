package discover

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestPrintDrawsAWindowOntoAnArchiveItem(t *testing.T) {
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.Query().Get("q"))
		fmt.Fprint(w, `{"response":{"numFound":28411,"start":0,"docs":[{"identifier":"weird-tales-v-22-n-01-1933-07","title":"Weird Tales v22 n01 [1933-07]","year":1933}]}}`)
	}))
	defer srv.Close()
	swap(t, &archiveAPI, srv.URL)
	for range 120 {
		d, err := printFamily{}.Draw(context.Background(), Env{Fetcher: testFetcher()})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(d.Target, "w5f:discover/window?") || !strings.HasPrefix(d.Why, "print/") ||
			!strings.HasSuffix(d.Why, "Weird Tales v22 n01 [1933-07] (1933)") {
			t.Fatalf("draw: %+v", d)
		}
		q, _ := url.ParseQuery(strings.TrimPrefix(d.Target, "w5f:discover/window?"))
		if q.Get("u") != srv.URL+"/download/weird-tales-v-22-n-01-1933-07/weird-tales-v-22-n-01-1933-07_djvu.txt" {
			t.Fatalf("source text %q", q.Get("u"))
		}
	}
	seen := map[string]bool{}
	for _, q := range queries {
		seen[strings.SplitN(strings.TrimPrefix(q, "collection:"), " ", 2)[0]] = true
	}
	for _, c := range []string{"pulpmagazinearchive", "zines", "magazine_rack", "newspapers"} {
		if !seen[c] {
			t.Errorf("collection %s never drawn from in 120 draws: %v", c, seen)
		}
	}
}

func TestPrintFailsWhenTheArchiveHasNothing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"response":{"numFound":0,"start":0,"docs":[]}}`)
	}))
	defer srv.Close()
	swap(t, &archiveAPI, srv.URL)
	if _, err := (printFamily{}).Draw(context.Background(), Env{Fetcher: testFetcher()}); err == nil || !strings.Contains(err.Error(), "nothing found") {
		t.Errorf("empty archive: %v", err)
	}
}

func TestArchiveSearchReportsAFailingBackend(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"error":"[BACKEND_ERROR] Invalid or no response from Elasticsearch"}`)
	}))
	defer srv.Close()
	swap(t, &archiveAPI, srv.URL)
	_, _, err := archiveSearch(context.Background(), testFetcher(), "x", 1, 1)
	if err == nil || !strings.Contains(err.Error(), "failing") {
		t.Errorf("a failing backend is an error, not an empty result: %v", err)
	}
	if _, err := (printFamily{}).Draw(context.Background(), Env{Fetcher: testFetcher()}); err == nil || strings.Contains(err.Error(), "nothing found") {
		t.Errorf("print: %v", err)
	}
}
