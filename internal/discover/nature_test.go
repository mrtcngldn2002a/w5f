package discover

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEmbeddedBHLList(t *testing.T) {
	bhlOnce.Do(loadBHL)
	if len(bhlItems[1]) < 200 || len(bhlItems[2]) < 2000 {
		t.Fatalf("the embedded list is thin: tier 1 %d, tier 2 %d", len(bhlItems[1]), len(bhlItems[2]))
	}
	for tier := 1; tier <= 2; tier++ {
		for _, it := range bhlItems[tier] {
			if it.id <= 0 || it.title == "" || it.year > 1923 || it.tier != tier {
				t.Fatalf("bad item %+v", it)
			}
		}
	}
	for range 50 {
		if it, ok := pickBHL(); !ok || it.id == 0 {
			t.Fatal("pickBHL")
		}
	}
}

// bhlServer lists the pages of any book: leaves 1-3 and the last 3 are short
// (covers and plates), the rest alternate between a readable page and noise.
func bhlServer(t *testing.T) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("list-type") == "2" {
			prefix := r.URL.Query().Get("prefix") // ocr/item-000123/
			fmt.Fprint(w, `<?xml version="1.0"?><ListBucketResult>`)
			for i := 1; i <= 20; i++ {
				size := 2400
				if i <= 3 || i > 17 {
					size = 90
				}
				fmt.Fprintf(w, `<Contents><Key>%sitem-x-%08d-%04d.txt</Key><Size>%d</Size></Contents>`, prefix, 1000+i, i, size)
			}
			fmt.Fprint(w, `</ListBucketResult>`)
			return
		}
		if strings.HasSuffix(r.URL.Path, "0007.txt") || strings.HasSuffix(r.URL.Path, "0009.txt") {
			fmt.Fprint(w, strings.Repeat("Biri A94a b znartattas 8i sat 44 hét 8 MLLAKULTTONT TET 1 VAYTAT jat papa ", 8))
			return
		}
		fmt.Fprint(w, strings.Repeat(goodText+" ", 4))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestBHLPageIsAReadablePageOfABook(t *testing.T) {
	srv := bhlServer(t)
	swap(t, &bhlBucket, srv.URL)
	for range 30 {
		target, title, err := bhlPage(context.Background(), Env{Fetcher: testFetcher()})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(target, srv.URL+"/ocr/item-") || !strings.HasSuffix(target, ".txt") {
			t.Fatalf("target %q", target)
		}
		leaf := target[strings.LastIndex(target, "-")+1 : len(target)-4]
		if leaf <= "0003" || leaf > "0017" || leaf == "0007" || leaf == "0009" {
			t.Fatalf("leaf %s should be a readable page inside the book", leaf)
		}
		if !strings.Contains(title, ", leaf ") || title == "" {
			t.Fatalf("title %q", title)
		}
	}
}

func TestBHLPageGivesUpOnABookOfNoise(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("list-type") == "2" {
			fmt.Fprint(w, `<ListBucketResult></ListBucketResult>`) // a book with no OCR of its own
			return
		}
	}))
	defer srv.Close()
	swap(t, &bhlBucket, srv.URL)
	if _, _, err := bhlPage(context.Background(), Env{Fetcher: testFetcher()}); err == nil {
		t.Error("no pages: an error is expected")
	}
}

func TestPSMPicksAnArticle(t *testing.T) {
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/wiki/") {
			fmt.Fprint(w, "ok")
			return
		}
		n++
		path := "/wiki/Popular_Science_Monthly/Volume_30/April_1887/The_History_of_a_Delusion"
		if n%2 == 1 {
			path = "/wiki/Popular_Science_Monthly/Volume_30/April_1887/Notes" // skipped
		}
		http.Redirect(w, r, path, http.StatusFound)
	}))
	defer srv.Close()
	swap(t, &psmRandom, srv.URL+"/random/")
	target, title, err := psmPick(context.Background(), Env{Fetcher: testFetcher()})
	if err != nil || !strings.HasSuffix(target, "/The_History_of_a_Delusion") {
		t.Fatalf("%q %v", target, err)
	}
	if title != "The History of a Delusion (April 1887)" {
		t.Errorf("title %q", title)
	}
}

func TestNatureFamily(t *testing.T) {
	srv := bhlServer(t)
	swap(t, &bhlBucket, srv.URL)
	old := natureSources
	t.Cleanup(func() { natureSources = old })
	natureSources = natureSources[:1] // BHL alone: the Wikisource side needs the network
	d, err := nature{}.Draw(context.Background(), Env{Fetcher: testFetcher()})
	if err != nil || !strings.HasPrefix(d.Why, "nature/Biodiversity Heritage Library · ") || !strings.HasPrefix(d.Target, srv.URL) {
		t.Fatalf("draw: %+v %v", d, err)
	}
}
