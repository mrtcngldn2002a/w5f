package sitecat

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"w5f/internal/doc"
	"w5f/internal/store"
)

func flat(d *doc.Document) string {
	var b strings.Builder
	doc.ReplaceBlocks(d.Blocks, func(x doc.Block) ([]doc.Block, bool) {
		switch x := x.(type) {
		case doc.Paragraph:
			b.WriteString(x.Text.PlainText() + "\n")
		case doc.Notice:
			b.WriteString("[N] " + x.Text + "\n")
		}
		return nil, false
	})
	return b.String()
}

func TestAddSearchGetEndToEnd(t *testing.T) {
	lib := t.TempDir()
	t.Setenv("W5F_BOOKS", lib)
	mux := http.NewServeMux()
	site := fakeSite(t) // search pages from search_test.go
	defer site.Close()
	mux.Handle("/", site.Config.Handler)
	mux.HandleFunc("/book/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<html><body><h1>Dracula</h1><a href="%s.pdf">PDF</a></body></html>`, r.URL.Path)
	})
	mux.HandleFunc("/book/0.pdf", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("%PDF-1.4 dracula")) })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	opened := ""
	env := Env{Fetcher: testFetcher(), DB: db, Path: filepath.Join(t.TempDir(), "catalogs.toml"),
		OpenBook: func(ctx context.Context, b store.Book) (*doc.Document, error) {
			opened = b.Path
			return &doc.Document{Title: b.Title}, nil
		}}
	ctx := context.Background()
	q := url.Values{"url": {srv.URL + "/"}, "w": {"dracula"}}.Encode()

	check, err := Route(ctx, "w5f:catalog/check?"+q, env)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(flat(check), "✓ search") || !strings.Contains(flat(check), "add this catalog") {
		t.Fatalf("check page:\n%s", flat(check))
	}
	if _, err := Route(ctx, "w5f:catalog/add?"+q, env); err != nil {
		t.Fatal(err)
	}
	ps, _ := LoadAll(env.Path)
	if len(ps) != 1 {
		t.Fatalf("profiles = %d", len(ps))
	}
	res, err := Route(ctx, "w5f:catalog/"+ps[0].ID+"?q=dracula", env)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(flat(res), "50 results from 10 pages") {
		t.Errorf("results page:\n%s", flat(res))
	}
	item, err := Route(ctx, "w5f:catalog/"+ps[0].ID+"/item?"+url.Values{"u": {srv.URL + "/book/0"}, "t": {"Dracula volume 0"}}.Encode(), env)
	if err != nil || !strings.Contains(flat(item), "PDF") {
		t.Fatalf("item page: %v\n%s", err, flat(item))
	}
	var get string
	for _, l := range item.Links {
		if strings.Contains(l.Href, "/get?") {
			get = l.Href
		}
	}
	if _, err := Route(ctx, get, env); err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(opened) != lib || filepath.Ext(opened) != ".pdf" {
		t.Errorf("opened %q", opened)
	}
}

func testEnv(t *testing.T) (Env, *store.DB) {
	t.Helper()
	t.Setenv("W5F_BOOKS", t.TempDir())
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	return Env{Fetcher: testFetcher(), DB: db, Path: filepath.Join(t.TempDir(), "catalogs.toml"),
		OpenBook: func(ctx context.Context, b store.Book) (*doc.Document, error) {
			return &doc.Document{Title: b.Title}, nil
		}}, db
}
