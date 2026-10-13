package discover

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// archiveServer plays the Internet Archive: the scrape API gives two items,
// one of which has no text file; the text of the other is an issue.
func archiveServer(t *testing.T, queries *[]string) *httptest.Server {
	t.Helper()
	xml := columnsXML(3)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/services/search/v1/scrape":
			*queries = append(*queries, r.URL.Query().Get("q"))
			fmt.Fprint(w, `{"items":[{"identifier":"no-text-item","title":"A scan of pictures","year":1950},
{"identifier":"weird-tales-v-22-n-01-1933-07","title":"Weird Tales v22 n01 [1933-07]","year":1933}],"count":2,"total":28411}`)
		case r.URL.Path == "/metadata/weird-tales-v-22-n-01-1933-07/files":
			// The files keep the names they were uploaded with, not the identifier.
			fmt.Fprintf(w, `{"result":[{"name":"cover.jpg","format":"JPEG","size":"900"},
{"name":"Weird Tales v22 n01 (1933-07)_djvu.txt","format":"DjVuTXT","size":"600000"},
{"name":"Weird Tales v22 n01 (1933-07)_djvu.xml","format":"Djvu XML","size":"%d"},
{"name":"Weird Tales v22 n01 (1933-07)_supplement_djvu.txt","format":"DjVuTXT","size":"4000"}]}`, len(xml))
		case r.URL.Path == "/metadata/no-text-item/files":
			fmt.Fprint(w, `{"result":[{"name":"pictures.pdf","format":"Text PDF","size":"900"}]}`)
		case r.URL.Path == "/download/weird-tales-v-22-n-01-1933-07/Weird Tales v22 n01 (1933-07)_djvu.xml":
			http.ServeContent(w, r, "x_djvu.xml", time.Time{}, strings.NewReader(xml))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestPrintDrawsAWindowOntoAnArchiveItemThatHasText(t *testing.T) {
	var queries []string
	srv := archiveServer(t, &queries)
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
		if u, _ := url.Parse(q.Get("u")); u == nil || u.Path != "/download/weird-tales-v-22-n-01-1933-07/Weird Tales v22 n01 (1933-07)_djvu.xml" || q.Get("r") == "" {
			t.Fatalf("source %q, range %q: an item with no text must be passed over, the window cut from pages of the XML", q.Get("u"), q.Get("r"))
		}
		if page, err := Route(context.Background(), d.Target, Env{Fetcher: testFetcher()}); err != nil || len(page.Blocks) < 2 {
			t.Fatalf("the window opens: %v", err)
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

func TestPrintFailsWhenNoItemHasText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/services/search/v1/scrape" {
			fmt.Fprint(w, `{"items":[{"identifier":"x","title":"X","year":1900}],"count":1,"total":1}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	swap(t, &archiveAPI, srv.URL)
	if _, err := (printFamily{}).Draw(context.Background(), Env{Fetcher: testFetcher()}); err == nil || !strings.Contains(err.Error(), "x: ") {
		t.Errorf("items without text: %v", err)
	}
}

func TestPrintFailsWhenTheArchiveHasNothing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/services/search/v1/scrape" {
			fmt.Fprint(w, `{"items":[],"count":0,"total":0}`)
			return
		}
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

func TestPrintPassesOverAnItemWithTextButNoLayout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/services/search/v1/scrape":
			fmt.Fprint(w, `{"items":[{"identifier":"plain","title":"Plain","year":1900}],"count":1,"total":1}`)
		case "/metadata/plain/files":
			fmt.Fprint(w, `{"result":[{"name":"plain_djvu.txt","format":"DjVuTXT","size":"600000"}]}`)
		case "/download/plain/plain_djvu.txt":
			fmt.Fprint(w, issueText(150))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	swap(t, &archiveAPI, srv.URL)
	if _, err := (printFamily{}).Draw(context.Background(), Env{Fetcher: testFetcher()}); err == nil || !strings.Contains(err.Error(), "no layout file") {
		t.Errorf("the plain text reads across the columns, so the item is passed over: %v", err)
	}
}

func TestPrintMovesOnToAnotherItemWhenTheFileCannotBeHad(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/services/search/v1/scrape":
			fmt.Fprint(w, `{"items":[{"identifier":"x","title":"X","year":1900}],"count":1,"total":1}`)
		case "/metadata/x/files":
			fmt.Fprint(w, `{"result":[{"name":"x_djvu.xml","format":"Djvu XML","size":"9000000"}]}`)
		case "/download/x/x_djvu.xml":
			hits++
			http.Error(w, "node down", http.StatusServiceUnavailable)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	swap(t, &archiveAPI, srv.URL)
	_, err := (printFamily{}).Draw(context.Background(), Env{Fetcher: testFetcher()})
	if err == nil || !strings.Contains(err.Error(), "cannot be had") {
		t.Errorf("a file that cannot be had: %v", err)
	}
	// one place per item, four items for each of the three sources tried: not two places in each
	if hits > 12 || hits == 0 {
		t.Errorf("%d requests for the file", hits)
	}
}
