package fiction

import (
	"archive/zip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeAO3EPUB writes an EPUB whose preface names an AO3 work, as AO3's own
// downloads do.
func writeAO3EPUB(t *testing.T, path, title, work string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, _ := zw.CreateHeader(&zip.FileHeader{Name: "mimetype", Method: zip.Store})
	w.Write([]byte("application/epub+zip"))
	w, _ = zw.Create("META-INF/container.xml")
	w.Write([]byte(`<?xml version="1.0"?><container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="content.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`))
	w, _ = zw.Create("content.opf")
	w.Write([]byte(`<?xml version="1.0"?><package xmlns="http://www.idpf.org/2007/opf" version="2.0"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>` + title + `</dc:title><dc:creator>Writer</dc:creator><dc:language>en</dc:language></metadata><manifest><item id="p" href="preface.xhtml" media-type="application/xhtml+xml"/><item id="c1" href="c1.xhtml" media-type="application/xhtml+xml"/></manifest><spine><itemref idref="p"/><itemref idref="c1"/></spine></package>`))
	w, _ = zw.Create("preface.xhtml")
	pre := `<p>A book.</p>`
	if work != "" {
		pre = `<p>Posted originally on the <a href="http://archiveofourown.org/">Archive of Our Own</a> at <a href="http://archiveofourown.org/works/` + work + `">http://archiveofourown.org/works/` + work + `</a>.</p>`
	}
	w.Write([]byte(`<html xmlns="http://www.w3.org/1999/xhtml"><head><title>Preface</title></head><body>` + pre + `</body></html>`))
	w, _ = zw.Create("c1.xhtml")
	w.Write([]byte(`<html xmlns="http://www.w3.org/1999/xhtml"><head><title>One</title></head><body><h1>One</h1><p>The story itself, long enough to be read as a chapter in W5F.</p></body></html>`))
	zw.Close()
	f.Close()
}

func TestImportAO3Downloads(t *testing.T) {
	env := testEnv(t, ao3{})
	dl := t.TempDir()
	env.Downloads = dl
	t.Setenv("W5F_BOOKS", t.TempDir())
	writeAO3EPUB(t, filepath.Join(dl, "Test_Work.epub"), "Test Work", "555")
	writeAO3EPUB(t, filepath.Join(dl, "Other Book.epub"), "Other Book", "")
	if n, err := ImportAO3Downloads(env); err != nil || n != 1 {
		t.Fatalf("first import: %d %v", n, err)
	}
	if n, _ := ImportAO3Downloads(env); n != 0 {
		t.Errorf("second import of the same files: %d", n)
	}
	if _, err := os.Stat(filepath.Join(dl, "Test_Work.epub")); err != nil {
		t.Error("the downloaded file must stay where it is")
	}
	// Downloaded again after an update: the same book is replaced.
	later := time.Now().Add(time.Minute)
	writeAO3EPUB(t, filepath.Join(dl, "Test_Work (1).epub"), "Test Work", "555")
	os.Chtimes(filepath.Join(dl, "Test_Work (1).epub"), later, later)
	if n, _ := ImportAO3Downloads(env); n != 1 {
		t.Errorf("updated download: %d", n)
	}
	if bs, _ := env.DB.Books("author", 0); len(bs) != 1 || bs[0].Source != "ao3:555" {
		t.Errorf("library: %+v", bs)
	}
}

func TestAO3SaveWholeWorkAndMyPage(t *testing.T) {
	env := testEnv(t, ao3{})
	t.Setenv("W5F_BOOKS", t.TempDir())
	epub := filepath.Join(t.TempDir(), "w.epub")
	writeAO3EPUB(t, epub, "Test Work", "555")
	data, _ := os.ReadFile(epub)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/works/555":
			w.Write([]byte(`<html><body><h2 class="title">Test Work</h2><li class="download"><ul><li><a href="/downloads/555/Test_Work.epub?updated_at=1">EPUB</a></li></ul></li></body></html>`))
		case strings.HasPrefix(r.URL.Path, "/downloads/555/"):
			w.Write(data)
		case r.URL.Path == "/":
			w.Write([]byte(`<html><body><ul id="greeting"><li><a href="/users/reader1">Hi, reader1!</a></li></ul></body></html>`))
		case r.URL.Path == "/users/reader1/bookmarks":
			w.Write([]byte(`<html><body><ol class="bookmark index group"><li class="bookmark blurb group"><h4 class="heading"><a href="/works/777">Bookmarked Work</a> by <a rel="author" href="/users/x/pseuds/x">x</a></h4></li></ol><ol class="pagination"><li class="next"><a href="/users/reader1/bookmarks?page=2">Next</a></li></ol></body></html>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	oldBase, oldHosts := ao3Root, ao3Hosts
	ao3Root, ao3Hosts = srv.URL, map[string]bool{strings.TrimPrefix(srv.URL, "http://"): true}
	defer func() { ao3Root, ao3Hosts = oldBase, oldHosts }()
	env.Fetcher.HostGap = 0
	ctx := context.Background()

	d, err := Route(ctx, "w5f:fiction/ao3/save?u="+srv.URL+"/works/555", env)
	if err != nil || !strings.Contains(flat(d), "Posted originally on the Archive of Our Own") || d.Next == "" {
		t.Fatalf("save whole work: %v\n%s", err, flat(d))
	}
	if b, ok := env.DB.BookBySource("ao3:555"); !ok || b.Title != "Test Work" {
		t.Errorf("library: %+v", b)
	}

	t.Setenv("W5F_AO3_SESSION", "")
	d, _ = Route(ctx, "w5f:fiction/ao3/me", env)
	if !strings.Contains(flat(d), "ao3-login") {
		t.Errorf("not connected:\n%s", flat(d))
	}
	t.Setenv("W5F_AO3_SESSION", "_otwarchive_session=s")
	d, err = Route(ctx, "w5f:fiction/ao3/me", env)
	if err != nil || len(d.Meta) == 0 || !strings.Contains(d.Meta[0].Value, "reader1") || !strings.Contains(flat(d), "Bookmarks") {
		t.Fatalf("my AO3: %v\n%s", err, flat(d))
	}
	var bm string
	for _, l := range d.Links {
		if l.Text == "Bookmarks" {
			bm = l.Href
		}
	}
	d, err = Route(ctx, bm, env)
	if err != nil || !strings.Contains(flat(d), "Bookmarked Work") || d.Next == "" {
		t.Errorf("bookmarks: %v %q\n%s", err, d.Next, flat(d))
	}
}
