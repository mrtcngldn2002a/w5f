package books

import (
	"context"
	"crypto/md5"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"w5f/internal/doc"
	"w5f/internal/fetch"
	"w5f/internal/libgen"
	"w5f/internal/store"
)

func TestLibgenRoutesDownloadAndReuse(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("W5F_BOOKS", t.TempDir())
	payload := []byte("public domain fixture text")
	hash := fmt.Sprintf("%x", md5.Sum(payload))
	downloads := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/index.php":
			fmt.Fprintf(w, `<table><tr><td><a href="edition.php?id=1">Test book</a></td><td>Writer</td><td>Press</td><td>1900</td><td>English</td><td>1</td><td>26 B</td><td>txt</td><td><a href="file.php?id=1">1</a><a href="ads.php?md5=%s">Get</a></td></tr></table>`, hash)
		case "/json.php":
			if r.URL.Query().Get("object") == "f" {
				fmt.Fprintf(w, `{"1":{"md5":%q,"extension":"txt","editions":{"1":{"e_id":"1"}}}}`, hash)
			} else {
				fmt.Fprint(w, `{"1":{"title":"Test book","author":"Writer"}}`)
			}
		case "/ads.php":
			fmt.Fprintf(w, `<a href="get.php?md5=%s&amp;key=1">GET</a>`, hash)
		case "/get.php":
			downloads++
			w.Write(payload)
		default:
			t.Errorf("unexpected request %s", r.URL)
		}
	}))
	defer srv.Close()
	os.MkdirAll(filepath.Dir(libgen.ConfigPath()), 0755)
	os.WriteFile(libgen.ConfigPath(), []byte(fmt.Sprintf(`{"search_mirrors":[%q],"download_mirrors":[%q]}`, srv.URL, srv.URL)), 0600)
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	f := fetch.New("", "test")
	f.HostGap = 0
	env := Env{Fetcher: f, DB: db, LoadFile: func(p string) (*doc.Document, error) {
		b, e := os.ReadFile(p)
		return &doc.Document{Blocks: []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: string(b)}}}}}, e
	}}
	ctx := context.Background()
	search, err := Route(ctx, "w5f:books/libgen?q=Test", env)
	if err != nil {
		t.Fatal(err)
	}
	if len(search.Links) != 1 || !strings.Contains(search.Links[0].Href, "/item?md5="+hash) {
		t.Fatalf("%+v", search.Links)
	}
	item, err := Route(ctx, search.Links[0].Href, env)
	if err != nil {
		t.Fatal(err)
	}
	get := ""
	for _, l := range item.Links {
		if strings.HasPrefix(l.Href, "w5f:books/get/") {
			get = l.Href
		}
	}
	if get == "" {
		t.Fatal("no download action")
	}
	for i := 0; i < 2; i++ {
		d, err := Route(ctx, get, env)
		if err != nil {
			t.Fatal(err)
		}
		if d.Catalog != "BK·LG·"+hash {
			t.Fatalf("bad catalog: %s", d.Catalog)
		}
	}
	if downloads != 1 {
		t.Fatalf("downloaded %d times", downloads)
	}
	b, ok := db.BookBySource("libgen:" + hash)
	if !ok || b.Author != "Writer" || b.Title != "Test book" {
		t.Fatalf("%+v", b)
	}
}
