package sitecat

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"w5f/internal/doc"
	"w5f/internal/store"
)

// A site with a "Popular" sidebar on every page and a plain "No results"
// page for unknown words.
func sidebarSite() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		side := `<div class="side"><ul class="pop"><li><a href="/p/1">Popular one book</a></li><li><a href="/p/2">Popular two book</a></li><li><a href="/p/3">Popular three book</a></li></ul></div>`
		switch {
		case r.URL.Path == "/":
			fmt.Fprint(w, `<html><body><form action="/s"><input type="search" name="q"></form>`+side+`</body></html>`)
		case r.URL.Query().Get("q") == "dracula":
			fmt.Fprint(w, `<html><body><div class="main"><ul class="res"><li class="r"><a href="/b/1">Dracula</a> Stoker 1897</li><li class="r"><a href="/b/2">Dracula's Guest</a> Stoker 1914</li>
<li class="r"><a href="/b/3">Dracula (abridged)</a> Stoker</li><li class="r"><a href="/b/4">Dracula: a play</a> Deane</li></ul></div>`+side+`</body></html>`)
		default:
			fmt.Fprint(w, `<html><body><div class="main"><p>No results.</p></div>`+side+`</body></html>`)
		}
	}))
}

func TestNoResultPageDoesNotRelearnSidebar(t *testing.T) {
	srv := sidebarSite()
	defer srv.Close()
	f := testFetcher()
	rep, err := Probe(context.Background(), f, srv.URL, "dracula")
	if err != nil || !rep.CanAdd {
		t.Fatalf("probe: %v %+v", err, rep)
	}
	p := rep.Profile
	if got, _ := Search(context.Background(), f, &p, "zzqx", nil); len(got.Results) != 0 {
		t.Errorf("no-result search showed %d sidebar items", len(got.Results))
	}
	if got, _ := Search(context.Background(), f, &p, "dracula", nil); len(got.Results) != 4 {
		t.Errorf("after a no-result search: %d results", len(got.Results))
	}
}

func TestSearchCancelledMidwayReturnsCancellation(t *testing.T) {
	site := fakeSite(t)
	defer site.Close()
	ctx, cancel := context.WithCancel(context.Background())
	// The owner presses esc while page 2 is loading.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			cancel()
			<-r.Context().Done()
			return
		}
		site.Config.Handler.ServeHTTP(w, r)
	}))
	defer srv.Close()
	f := testFetcher()
	rep, _ := Probe(context.Background(), f, srv.URL+"/", "dracula")
	p := rep.Profile
	_, err := Search(ctx, f, &p, "dracula", nil)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v", err)
	}
}

func TestPathTemplates(t *testing.T) {
	if got := cleanTemplate("https://x.example/search/{searchTerms}?page={startPage?}"); got != "https://x.example/search/{q}" {
		t.Errorf("cleanTemplate = %q", got)
	}
	u, _ := BuildURL(Profile{Search: SearchSpec{Template: "https://x.example/search/{q}?lang=en"}}, Query{Words: "Bram Stoker"})
	if u != "https://x.example/search/Bram%20Stoker?lang=en" {
		t.Errorf("BuildURL = %q", u)
	}
}

func TestOpenSearchPrefersHTMLAndAtomMeansOPDS(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mux.HandleFunc("/both.xml", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<OpenSearchDescription><Url type="application/atom+xml" template="`+srv.URL+`/opds/s?q={searchTerms}"/><Url type="text/html" template="`+srv.URL+`/s?q={searchTerms}"/></OpenSearchDescription>`)
	})
	mux.HandleFunc("/atom.xml", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<OpenSearchDescription><Url type="application/atom+xml;profile=opds-catalog" template="`+srv.URL+`/opds/s?q={searchTerms}"/></OpenSearchDescription>`)
	})
	ctx, f := context.Background(), testFetcher()
	osd := func(h string) []byte {
		return []byte(`<html><head><link rel="search" type="application/opensearchdescription+xml" href="` + h + `"></head><body></body></html>`)
	}
	if spec, _ := DiscoverSearch(ctx, f, osd("/both.xml"), srv.URL+"/"); spec.Kind != "opensearch" || spec.Template != srv.URL+"/s?q={q}" {
		t.Errorf("both: %+v", spec)
	}
	if spec, _ := DiscoverSearch(ctx, f, osd("/atom.xml"), srv.URL+"/"); spec.Kind != "opds" || spec.Template != srv.URL+"/opds/s?q={q}" {
		t.Errorf("atom only: %+v", spec)
	}
}

func TestFetchFileRejectsWrongMagic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		fmt.Fprint(w, "Rate limited, try again later.")
	}))
	defer srv.Close()
	if _, _, err := FetchFile(context.Background(), testFetcher(), Download{URL: srv.URL + "/a.pdf", Format: "pdf"}, t.TempDir()); err == nil {
		t.Error("a text body must not be saved as a PDF")
	}
}

func TestBotWallIgnoresOrdinaryRecaptcha(t *testing.T) {
	body := `<html><body><h1>Small Press Library</h1><p>` + strings.Repeat("We publish old weird fiction. ", 40) +
		`</p><form class="contact"><div class="g-recaptcha"></div></form><script src="https://www.google.com/recaptcha/api.js"></script></body></html>`
	if w := botWall([]byte(body)); w != "" {
		t.Errorf("ordinary page reported as walled: %s", w)
	}
	if w := botWall([]byte(`<html><head><title>Just a moment...</title></head><body><div id="cf-challenge"></div></body></html>`)); w == "" {
		t.Error("challenge page not recognised")
	}
}

func TestDownloadHopToAFileIsNotFetched(t *testing.T) {
	served := 0
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mux.HandleFunc("/book/1", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><body><a href="/get/5">Download</a></body></html>`)
	})
	mux.HandleFunc("/get/5", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/epub+zip")
		w.Write([]byte("PK\x03\x04"))
		for i := 0; i < 128; i++ {
			n, err := w.Write(make([]byte, 64<<10))
			served += n
			if err != nil {
				return
			}
		}
	})
	ds, err := FindDownloads(context.Background(), testFetcher(), srv.URL+"/book/1", DefaultPrefer)
	if err != nil || len(ds) != 1 || ds[0].Format != "epub" || ds[0].URL != srv.URL+"/get/5" {
		t.Fatalf("ds=%+v err=%v", ds, err)
	}
	if served >= 8<<20 {
		t.Errorf("the whole file (%d bytes) was read during discovery", served)
	}
}

func TestGetCleansUpAndRefetchesDeletedBook(t *testing.T) {
	lib := t.TempDir()
	t.Setenv("W5F_BOOKS", lib)
	good := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if good {
			w.Write([]byte("%PDF-1.4 book"))
			return
		}
		w.Write([]byte("PK\x03\x04 not really an epub"))
	}))
	defer srv.Close()
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	env := Env{Fetcher: testFetcher(), DB: db, Path: filepath.Join(t.TempDir(), "catalogs.toml"),
		OpenBook: func(ctx context.Context, b store.Book) (*doc.Document, error) {
			return &doc.Document{Title: b.Title}, nil
		}}
	ps := []Profile{{ID: "t", Name: "T", Home: srv.URL}}
	ps[0].ApplyDefaults()
	if err := SaveAll(env.Path, ps); err != nil {
		t.Fatal(err)
	}
	get := func(file, format, src string) error {
		_, err := Route(context.Background(), "w5f:catalog/t/get?u="+srv.URL+file+"&f="+format+"&t=Book&src="+src, env)
		return err
	}
	good = false
	if err := get("/bad.epub", "epub", "https://s/1"); err == nil {
		t.Error("a broken EPUB should fail")
	}
	if left, _ := filepath.Glob(filepath.Join(lib, ".download-*")); len(left) != 0 {
		t.Errorf("temp files left behind: %v", left)
	}
	good = true
	if err := get("/a.pdf", "pdf", "https://s/2"); err != nil {
		t.Fatal(err)
	}
	b, _ := db.BookBySource("site:t:https://s/2")
	os.Remove(b.Path)
	if err := get("/a.pdf", "pdf", "https://s/2"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(b.Path); err != nil {
		t.Errorf("deleted book was not fetched again: %v", err)
	}
}

// Anubis and similar proof-of-work walls are bot checks, not "JavaScript
// sites"; the check must say so and stop.
func TestBotWallRecognisesProofOfWorkPages(t *testing.T) {
	page := `<html><head><title>Making sure you're not a bot!</title><script id="anubis_challenge" type="application/json">{}</script></head><body><h1>Making sure you're not a bot!</h1></body></html>`
	if w := botWall([]byte(page)); !strings.Contains(w, "bot check") {
		t.Errorf("botWall = %q", w)
	}
}
