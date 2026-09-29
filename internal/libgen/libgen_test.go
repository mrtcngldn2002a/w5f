package libgen

import (
	"context"
	"crypto/md5"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"w5f/internal/fetch"
)

func row(hash, title, ext string) string {
	return `<tr><td><a href="edition.php?id=1">` + title + `</a></td><td>Bram Stoker</td><td>Example</td><td>1897 January</td><td>English</td><td>100</td><td>10 kB</td><td>` + ext + `</td><td><a href="file.php?id=1">file</a><a href="ads.php?md5=` + hash + `">get</a></td></tr>`
}
func testClient(ms ...string) *Client {
	f := fetch.New("", "test")
	f.HostGap = 0
	return &Client{F: f, SearchMirrors: ms, DownloadMirrors: ms}
}

func TestSearchFallbackFiltersAndPaging(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `<title>Welcome to nginx!</title>`) }))
	defer bad.Close()
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != UserAgent {
			t.Error("browser User-Agent missing or duplicated")
		}
		if r.URL.Query().Get("req") != "Dracula" || r.URL.Query().Get("page") != "1" || r.URL.Query().Get("sortmode") != "DESC" {
			t.Errorf("query: %s", r.URL.RawQuery)
		}
		fmt.Fprint(w, `<table>`+row(strings.Repeat("a", 32), "Dracula &amp; Stories", "epub")+row(strings.Repeat("b", 32), "Dracula PDF", "pdf")+`</table><a href="?req=Dracula&amp;page=2">2</a>`)
	}))
	defer good.Close()
	o, err := ParseQuery("Dracula ext:.EPUB lang:english year:1897 sort:-year")
	if err != nil {
		t.Fatal(err)
	}
	rs, err := testClient(bad.URL, good.URL).Search(context.Background(), o)
	if err != nil || len(rs.Books) != 1 || !rs.More {
		t.Fatalf("%+v %v", rs, err)
	}
	if rs.Books[0].Title != "Dracula & Stories" || rs.Mirror != good.URL {
		t.Fatalf("%+v", rs)
	}
	o, _ = ParseQuery(`title:Dracula author:"Bram Stoker" publisher:"Example Press" limit:50`)
	if o.Query != "Dracula Bram Stoker" || o.Author != "Bram Stoker" || o.Publisher != "Example Press" || o.Results != 50 {
		t.Fatalf("%+v", o)
	}
}

func TestParserRejectsUnexpectedPage(t *testing.T) {
	u, _ := url.Parse("https://libgen.li/index.php?req=x")
	if _, _, err := ParseResults([]byte(`<h1>Access denied</h1>`), u); err == nil {
		t.Fatal("wall reported as no results")
	}
	if bs, _, err := ParseResults([]byte(`<p>0 files found</p>`), u); err != nil || len(bs) != 0 {
		t.Fatalf("%v %v", bs, err)
	}
}

func TestDetailsDownloadRefererSessionFreshKeyAndFallback(t *testing.T) {
	payload := []byte("Example book for the download test")
	hash := fmt.Sprintf("%x", md5.Sum(payload))
	landings := 0
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "unavailable", 503) }))
	defer bad.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json.php":
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Query().Get("object") == "f" {
				fmt.Fprintf(w, `{"1":{"md5":%q,"extension":"txt","filesize":"34","editions":{"9":{"e_id":"9"}}}}`, hash)
			} else {
				fmt.Fprint(w, `{"9":{"title":"Book","author":"Writer","year":"1900","add":{"1":{"name_en":"Language","value":"English"}}}}`)
			}
		case "/ads.php":
			landings++
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "yes", Path: "/"})
			fmt.Fprintf(w, `<a href="get.php?md5=%s&amp;key=%d">GET</a>`, hash, landings)
		case "/get.php":
			cookie, _ := r.Cookie("session")
			if cookie == nil || cookie.Value != "yes" || !strings.Contains(r.Referer(), "/ads.php?md5="+hash) || r.URL.Query().Get("key") != fmt.Sprint(landings) {
				http.Error(w, "missing session/referer/fresh key", 403)
				return
			}
			w.Header().Set("Content-Type", "text/plain")
			w.Write(payload)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	c := testClient(bad.URL, srv.URL)
	c.F.CacheDir = t.TempDir()
	b, err := c.Details(context.Background(), hash)
	if err != nil || b.Title != "Book" || b.Language != "English" {
		t.Fatalf("%+v %v", b, err)
	}
	if _, err := c.Resolve(context.Background(), hash); err != nil {
		t.Fatal(err)
	}
	p, err := c.Download(context.Background(), b, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(p)
	if string(data) != string(payload) || landings != 2 {
		t.Fatalf("payload or fresh key failed, landings=%d", landings)
	}
}

func TestDownloadRemovesCorruptAndTruncatedFiles(t *testing.T) {
	for _, truncated := range []bool{false, true} {
		t.Run(fmt.Sprint(truncated), func(t *testing.T) {
			hash := strings.Repeat("a", 32)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/ads.php" {
					fmt.Fprintf(w, `<a href="get.php?md5=%s&amp;key=x">GET</a>`, hash)
					return
				}
				if truncated {
					w.Header().Set("Content-Length", "1000")
				}
				fmt.Fprint(w, "broken")
			}))
			defer srv.Close()
			dir := t.TempDir()
			_, err := testClient(srv.URL).Download(context.Background(), Book{MD5: hash, Extension: "txt"}, dir)
			if err == nil {
				t.Fatal("corrupt file accepted")
			}
			files, _ := os.ReadDir(dir)
			if len(files) != 0 {
				t.Fatal("partial file left behind")
			}
		})
	}
}

func TestOfflineAndCancellation(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits++ }))
	defer srv.Close()
	c := testClient(srv.URL)
	c.F.Offline = true
	if _, err := c.Download(context.Background(), Book{MD5: strings.Repeat("a", 32), Extension: "pdf"}, t.TempDir()); err == nil {
		t.Fatal("offline download accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Search(ctx, Options{Query: "x"}); err != context.Canceled {
		t.Fatalf("%v", err)
	}
	if hits != 0 {
		t.Fatal("unexpected network request")
	}
}
