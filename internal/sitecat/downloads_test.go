package sitecat

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"w5f/internal/fetch"
)

func testFetcher() *fetch.Fetcher {
	f := fetch.New("", "test")
	f.HostGap = 0
	return f
}

func TestFindInFormatsAndOrder(t *testing.T) {
	body := page(`<a href="/files/dracula.pdf">PDF</a>
<a href="/files/dracula.epub?dl=1">EPUB</a>
<a href="/get/77" type="application/x-mobipocket-ebook">Kindle</a>
<a href="/files/archive.zip">ZIP</a>
<a href="/dl/88">Download plain text</a>
<a href="#top">top</a>`)
	ds := FindIn(body, "https://s.example/book/1", DefaultPrefer)
	var formats []string
	for _, d := range ds {
		formats = append(formats, d.Format)
	}
	if fmt.Sprint(formats) != "[epub pdf txt mobi]" {
		t.Errorf("formats = %v", formats)
	}
	if ds[0].URL != "https://s.example/files/dracula.epub?dl=1" {
		t.Errorf("url = %s", ds[0].URL)
	}
}

func TestFindDownloadsFollowsDownloadPage(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mux.HandleFunc("/book/1", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><body><a href="/book/1/download">Download</a></body></html>`)
	})
	mux.HandleFunc("/book/1/download", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><body><a href="/f/1.epub">EPUB</a></body></html>`)
	})
	ds, err := FindDownloads(context.Background(), testFetcher(), srv.URL+"/book/1", DefaultPrefer)
	if err != nil || len(ds) != 1 || ds[0].Format != "epub" {
		t.Fatalf("ds=%+v err=%v", ds, err)
	}
}

func TestFetchFileInterstitialMagicAndErrors(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mux.HandleFunc("/f/a.pdf", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("source") != "download" {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<html><head><meta http-equiv="refresh" content="0; url=/f/a.pdf?source=download"></head></html>`)
			return
		}
		w.Write([]byte("%PDF-1.7 body"))
	})
	mux.HandleFunc("/f/page.epub", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body>Please log in</body></html>`)
	})
	mux.HandleFunc("/f/locked.epub", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(403) })
	mux.HandleFunc("/f/mislabel.epub", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("%PDF-1.4 x")) })
	dir := t.TempDir()
	f := testFetcher()
	ctx := context.Background()

	p, format, err := FetchFile(ctx, f, Download{URL: srv.URL + "/f/a.pdf", Format: "pdf"}, dir)
	if err != nil || format != "pdf" {
		t.Fatalf("interstitial: %v %s", err, format)
	}
	if b, _ := os.ReadFile(p); string(b[:4]) != "%PDF" {
		t.Error("wrong content")
	}
	if _, _, err := FetchFile(ctx, f, Download{URL: srv.URL + "/f/page.epub", Format: "epub"}, dir); err == nil {
		t.Error("an HTML page must not be accepted as a book")
	}
	if _, _, err := FetchFile(ctx, f, Download{URL: srv.URL + "/f/locked.epub", Format: "epub"}, dir); !errors.Is(err, ErrRefused) {
		t.Errorf("403 err = %v", err)
	}
	if _, format, err := FetchFile(ctx, f, Download{URL: srv.URL + "/f/mislabel.epub", Format: "epub"}, dir); err != nil || format != "pdf" {
		t.Errorf("magic bytes should win: %s %v", format, err)
	}
}
