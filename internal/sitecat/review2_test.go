package sitecat

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// Checking a site must never submit a form that is not a search form
// (a "request a book" or contact form).
func TestProbeDoesNotSubmitContactForms(t *testing.T) {
	var mu sync.Mutex
	var posts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			mu.Lock()
			posts = append(posts, r.URL.Path)
			mu.Unlock()
		}
		fmt.Fprint(w, `<html><body><form method="post" action="/request.php"><input name="yourname"><input name="youremail">
<input name="booktitle"><textarea name="msg"></textarea><input type="submit" value="Send request"></form></body></html>`)
	}))
	defer srv.Close()
	if _, err := Probe(context.Background(), testFetcher(), srv.URL, "history"); err != nil {
		t.Fatal(err)
	}
	if len(posts) != 0 {
		t.Errorf("the check submitted %v", posts)
	}
}

func TestNextFormOnlyFollowsPaging(t *testing.T) {
	news := page(`<form method="post" action="/subscribe"><input type="email" name="email"><button>→</button></form>`)
	if r := nextForm(news, "https://s.example/"); r != nil {
		t.Errorf("newsletter form taken as next page: %+v", r)
	}
	box := page(`<form action="/s"><input name="q"><button>→</button></form>`)
	if r := nextForm(box, "https://s.example/s?q=dracula"); r != nil {
		t.Errorf("search box taken as next page: %+v", r)
	}
	valued := page(`<form method="post" action="/find"><input type="hidden" name="q" value="dracula"><button name="page" value="2">Next</button></form>`)
	r := nextForm(valued, "https://s.example/find")
	if r == nil || r.Method != "POST" || r.Form.Get("page") != "2" || r.Form.Get("q") != "dracula" {
		t.Errorf("valued next button: %+v", r)
	}
}

func TestRecheckIntoIndexUsesDefaults(t *testing.T) {
	srv := archiveTree(t)
	defer srv.Close()
	crawlGap = 0
	defer func() { crawlGap = 1e9 }()
	env, db := testEnv(t)
	defer db.Close()
	old := `[[catalog]]
id = "lib"
name = "Library"
home = "` + srv.URL + `/library/"
[catalog.search]
kind = "none"
template = ""
max_pages = 25
max_results = 900
`
	if err := os.WriteFile(env.Path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Route(context.Background(), "w5f:catalog/lib/recheck", env); err != nil {
		t.Fatal(err)
	}
	ps, _ := LoadAll(env.Path)
	p := ps[0]
	if p.Search.Kind != "index" || p.Search.MaxPages != 10 || p.Search.MaxResults != 300 {
		t.Errorf("limits must follow the new kind: %+v", p.Search)
	}
	ix, err := loadIndex(p.Search.Index)
	if err != nil || len(ix.Files) != 4 || ix.Partial || filepath.Dir(p.Search.Index) != filepath.Join(filepath.Dir(env.Path), "catalogs") {
		t.Errorf("index after recheck: %v %+v", err, ix)
	}
}

func TestArchiveCapKeepsPreferredFormats(t *testing.T) {
	var b strings.Builder
	b.WriteString(`{"metadata":{},"files":[`)
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&b, `{"name":"scan%02d.pdf","size":"100"},`, i)
	}
	b.WriteString(`{"name":"book.epub","size":"100"}]}`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, b.String()) }))
	defer srv.Close()
	ArchiveBase = srv.URL
	defer func() { ArchiveBase = "https://archive.org" }()
	p := &Profile{Search: SearchSpec{Kind: "archive"}}
	p.ApplyDefaults()
	ds, err := itemDownloads(context.Background(), testFetcher(), p, srv.URL+"/details/big")
	if err != nil || len(ds) != 30 || ds[0].Format != "epub" {
		t.Errorf("err=%v n=%d first=%+v", err, len(ds), ds[0])
	}
}
