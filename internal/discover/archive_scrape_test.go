package discover

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRandomArchiveCursorIsAnIdentifierToStartFrom(t *testing.T) {
	for range 50 {
		raw, err := base64.StdEncoding.DecodeString(randomArchiveCursor())
		if err != nil {
			t.Fatal(err)
		}
		var c []map[string]string
		if err := json.Unmarshal(raw, &c); err != nil || len(c) != 1 || len(c[0]["identifier"]) != 2 {
			t.Fatalf("cursor %q: %v", raw, err)
		}
	}
}

func TestArchivePickUsesTheScrapeAPIFirst(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.URL.Query().Get("sorts") != "identifier" || r.URL.Query().Get("cursor") == "" || r.URL.Query().Get("count") != "100" {
			http.Error(w, "bad scrape query", http.StatusBadRequest)
			return
		}
		fmt.Fprint(w, `{"items":[{"identifier":"a1","title":"One","year":1930},{"identifier":"a2","title":"Two","year":1931}],"count":2,"total":9}`)
	}))
	defer srv.Close()
	swap(t, &archiveAPI, srv.URL)
	got := map[string]bool{}
	for range 40 {
		d, err := archivePick(context.Background(), testFetcher(), "collection:x")
		if err != nil {
			t.Fatal(err)
		}
		got[d.Identifier] = true
	}
	if !got["a1"] || !got["a2"] {
		t.Errorf("a random item of the hundred: %v", got)
	}
	for _, p := range paths {
		if p != "/services/search/v1/scrape" {
			t.Errorf("only the scrape API is asked while it answers: %s", p)
		}
	}
}

// advancedsearch failed with backend errors for hours on 2026-10-12 and the
// scrape API did not: the other way round, the offset search still serves.
func TestArchivePickFallsBackToTheOffsetSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/services/") {
			fmt.Fprint(w, `{"error":"scrape is down"}`)
			return
		}
		fmt.Fprint(w, `{"response":{"numFound":28411,"start":0,"docs":[{"identifier":"viaoffset","title":"Via offset","year":1933}]}}`)
	}))
	defer srv.Close()
	swap(t, &archiveAPI, srv.URL)
	d, err := archivePick(context.Background(), testFetcher(), "collection:x")
	if err != nil || d.Identifier != "viaoffset" {
		t.Fatalf("%+v %v", d, err)
	}
}

func TestArchivePickWhenBothFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"error":"[BACKEND_ERROR] Invalid or no response from Elasticsearch"}`)
	}))
	defer srv.Close()
	swap(t, &archiveAPI, srv.URL)
	_, err := archivePick(context.Background(), testFetcher(), "collection:x")
	if err == nil || !strings.Contains(err.Error(), "scrape API is failing") || !strings.Contains(err.Error(), "search is failing") {
		t.Errorf("both errors are reported: %v", err)
	}
}

func TestArchivePickWithACursorPastTheEnd(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/services/") {
			calls++
			if calls < 3 {
				fmt.Fprint(w, `{"items":[],"count":0,"total":5}`) // past the last identifier
				return
			}
			fmt.Fprint(w, `{"items":[{"identifier":"late","title":"Late","year":1900}],"count":1,"total":5}`)
			return
		}
		http.Error(w, "should not be asked", http.StatusInternalServerError)
	}))
	defer srv.Close()
	swap(t, &archiveAPI, srv.URL)
	d, err := archivePick(context.Background(), testFetcher(), "collection:x")
	if err != nil || d.Identifier != "late" {
		t.Fatalf("%+v %v", d, err)
	}
}

func TestArchivePickTriesAnotherCursorWhenOneIsRefused(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			http.Error(w, `{"error":"Bad cursor","errorType":"InvalidArgumentException"}`, http.StatusBadRequest)
			return
		}
		fmt.Fprint(w, `{"items":[{"identifier":"second","title":"Second","year":1900}],"count":1,"total":1}`)
	}))
	defer srv.Close()
	swap(t, &archiveAPI, srv.URL)
	d, err := archivePick(context.Background(), testFetcher(), "collection:x")
	if err != nil || d.Identifier != "second" || calls != 2 {
		t.Fatalf("%+v after %d calls: %v", d, calls, err)
	}
}

func TestArchiveTextFileIsTheLargestDjVuText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"result":[{"name":"a_hocr_searchtext.txt.gz","format":"OCR Search Text","size":"90000000"},
{"name":"a_supplement_djvu.txt","format":"DjVuTXT","size":"4872"},{"name":"a_djvu.txt","format":"DjVuTXT","size":"190848"}]}`)
	}))
	defer srv.Close()
	swap(t, &archiveAPI, srv.URL)
	name, err := archiveTextFile(context.Background(), testFetcher(), "a")
	if err != nil || name != "a_djvu.txt" {
		t.Errorf("%q %v", name, err)
	}
}
