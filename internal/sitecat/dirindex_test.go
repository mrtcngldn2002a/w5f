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
)

// A small book archive: Apache table listings, an nginx pre listing, a
// loop back to the root and links that leave the tree.
func archiveTree(t *testing.T) *httptest.Server {
	apache := func(title string, rows ...string) string {
		return `<html><head><title>Index of ` + title + `</title></head><body><h1>Index of ` + title + `</h1><table>
<tr><th><a href="?C=N;O=D">Name</a></th><th><a href="?C=S;O=A">Size</a></th></tr>
<tr><td><a href="../">Parent Directory</a></td><td>-</td></tr>` + strings.Join(rows, "") + `</table></body></html>`
	}
	row := func(href, size string) string {
		return `<tr><td><a href="` + href + `">` + href + `</a></td><td>` + size + `</td></tr>`
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			fmt.Fprint(w, `<html><body><a href="/library/">Library</a></body></html>`)
		case "/library/":
			fmt.Fprint(w, apache("/library", row("occult/", "-"), row("folklore/", "-"), row("Readme.html", "1K"),
				row("Lovecraft_-_Supernatural_Horror.pdf", "1.2M"), row("/elsewhere/", "-"), row("http://other.example/x.pdf", "1M")))
		case "/library/occult/":
			fmt.Fprint(w, apache("/library/occult", row("Agrippa%20-%20Üç%20Kitap.epub", "300K"), row("deeper/", "-")))
		case "/library/occult/deeper/":
			fmt.Fprint(w, apache("/library/occult/deeper", row("/library/", "-"), row("Grimoire.djvu", "5M")))
		case "/library/folklore/":
			fmt.Fprint(w, `<html><head><title>Index of /library/folklore/</title></head><body><h1>Index of /library/folklore/</h1><hr><pre><a href="../">../</a>
<a href="Turkish_Tales.txt">Turkish_Tales.txt</a>                  12-Mar-2019 10:21              48213
<a href="notes.docx">notes.docx</a>                         12-Mar-2019 10:21              1000
</pre><hr></body></html>`)
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestReadListing(t *testing.T) {
	root, _ := url.Parse("https://b.example/library/")
	body := []byte(`<html><head><title>Index of /library</title></head><body><table>
<tr><td><a href="../">Parent Directory</a></td></tr>
<tr><td><a href="?C=M;O=A">Last modified</a></td></tr>
<tr><td><a href="sub/">sub/</a></td><td>-</td></tr>
<tr><td><a href="Book.epub">Book.epub</a></td><td>2019-03-12 10:21</td><td>1.2M</td></tr></table></body></html>`)
	folders, files, ok := readListing(body, "https://b.example/library/", root)
	if !ok || len(folders) != 1 || folders[0] != "https://b.example/library/sub/" || len(files) != 1 || files[0].Size != "1.2M" {
		t.Errorf("ok=%v folders=%v files=%+v", ok, folders, files)
	}
	if _, _, ok := readListing([]byte(`<html><title>Shop</title><a href="/a">a</a></html>`), "https://b.example/", root); ok {
		t.Error("an ordinary page is not a listing")
	}
}

func TestCrawlStaysInsideAndStopsAtLimits(t *testing.T) {
	srv := archiveTree(t)
	defer srv.Close()
	crawlGap = 0
	defer func() { crawlGap = 1e9 }()
	ix, err := crawl(context.Background(), testFetcher(), srv.URL+"/library/", 150, 5, nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range ix.Files {
		names = append(names, f.Path[strings.LastIndex(f.Path, "/")+1:])
	}
	if ix.Folders != 4 || len(ix.Files) != 4 || ix.Partial {
		t.Errorf("folders=%d partial=%v files=%v", ix.Folders, ix.Partial, names)
	}
	ix, _ = crawl(context.Background(), testFetcher(), srv.URL+"/library/", 2, 5, nil)
	if !ix.Partial || ix.Folders != 2 {
		t.Errorf("limit: folders=%d partial=%v", ix.Folders, ix.Partial)
	}
	ix, _ = crawl(context.Background(), testFetcher(), srv.URL+"/library/", 150, 1, nil)
	for _, f := range ix.Files {
		if strings.Contains(f.Path, "/deeper/") {
			t.Errorf("depth limit ignored: %s", f.Path)
		}
	}
}

func TestIndexCatalogEndToEnd(t *testing.T) {
	srv := archiveTree(t)
	defer srv.Close()
	crawlGap = 0
	defer func() { crawlGap = 1e9 }()
	env, db := testEnv(t)
	defer db.Close()
	ctx := context.Background()
	add := "w5f:catalog/add?" + url.Values{"url": {srv.URL + "/library/"}, "w": {"lovecraft"}}.Encode()
	if _, err := Route(ctx, add, env); err != nil {
		t.Fatal(err)
	}
	ps, _ := LoadAll(env.Path)
	if len(ps) != 1 || ps[0].Search.Kind != "index" || filepath.Dir(ps[0].Search.Index) != filepath.Join(filepath.Dir(env.Path), "catalogs") {
		t.Fatalf("profile: %+v", ps)
	}
	p := ps[0]
	for q, want := range map[string]string{"uc kitap": "Agrippa - Üç Kitap", "turkish tales": "Turkish Tales", "grimoire": "Grimoire", "SUPERNATURAL horror": "Lovecraft - Supernatural Horror"} {
		got, err := Search(ctx, env.Fetcher, &p, q, nil)
		if err != nil || len(got.Results) != 1 || got.Results[0].Title != want {
			t.Errorf("%q: %v %+v", q, err, got)
		}
	}
	d, err := Route(ctx, "w5f:catalog/"+p.ID, env)
	if err != nil || !strings.Contains(flat(d), "occult/") || !strings.Contains(flat(d), "4 book files") {
		t.Errorf("catalog page: %v\n%s", err, flat(d))
	}
	d, err = Route(ctx, "w5f:catalog/"+p.ID+"?"+url.Values{"dir": {srv.URL + "/library/occult/"}}.Encode(), env)
	if err != nil || !strings.Contains(flat(d), "Agrippa - Üç Kitap.epub") {
		t.Errorf("folder page: %v\n%s", err, flat(d))
	}
	if _, err := Route(ctx, "w5f:catalog/"+p.ID+"?"+url.Values{"dir": {"https://other.example/"}}.Encode(), env); err == nil {
		t.Error("browsing outside the catalog root must be refused")
	}
}
