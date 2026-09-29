package books

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"w5f/internal/doc"
	"w5f/internal/fetch"
	"w5f/internal/store"
)

func writeEPUB(t *testing.T, path string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	add := func(name, body string) {
		w, _ := zw.Create(name)
		w.Write([]byte(body))
	}
	add("META-INF/container.xml", `<?xml version="1.0"?><container xmlns="urn:oasis:names:tc:opendocument:xmlns:container" version="1.0">
<rootfiles><rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`)
	add("OEBPS/content.opf", `<?xml version="1.0"?><package xmlns="http://www.idpf.org/2007/opf" version="3.0">
<metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>The Shadow of the Torturer</dc:title><dc:creator>Gene Wolfe</dc:creator><dc:language>en</dc:language></metadata>
<manifest>
 <item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>
 <item id="cover" href="text/cover.xhtml" media-type="application/xhtml+xml"/>
 <item id="c1" href="text/ch1.xhtml" media-type="application/xhtml+xml"/>
 <item id="c2" href="text/ch%202.xhtml" media-type="application/xhtml+xml"/>
</manifest>
<spine><itemref idref="cover"/><itemref idref="c1"/><itemref idref="c2"/></spine></package>`)
	add("OEBPS/nav.xhtml", `<html xmlns="http://www.w3.org/1999/xhtml"><body><nav epub:type="toc"><ol>
<li><a href="text/ch1.xhtml">Resurrection and Death</a></li><li><a href="text/ch%202.xhtml#s">Severian</a></li></ol></nav></body></html>`)
	add("OEBPS/text/cover.xhtml", `<html xmlns="http://www.w3.org/1999/xhtml"><body><img src="cover.jpg" alt=""/></body></html>`)
	add("OEBPS/text/ch1.xhtml", `<html xmlns="http://www.w3.org/1999/xhtml" lang="en"><body><header><h2>Resurrection and Death</h2></header>
<p>It is possible I already had some presentiment of my future. See <a href="ch%202.xhtml#s">the next part</a>.</p></body></html>`)
	add("OEBPS/text/ch 2.xhtml", `<html xmlns="http://www.w3.org/1999/xhtml"><body><h2 id="s">Severian</h2><p>`+strings.Repeat("The locked and rusted gate. ", 40)+`</p></body></html>`)
	zw.Close()
	f.Close()
}

func TestOpenEPUBAndChapterLinks(t *testing.T) {
	p := filepath.Join(t.TempDir(), "b.epub")
	writeEPUB(t, p)
	e, err := OpenEPUB(p)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if e.Title != "The Shadow of the Torturer" || e.Author != "Gene Wolfe" || len(e.Chapters) != 3 {
		t.Fatalf("meta: %q %q %d", e.Title, e.Author, len(e.Chapters))
	}
	if e.Chapters[1].Title != "Resurrection and Death" || e.Chapters[2].Title != "Severian" {
		t.Errorf("toc titles: %+v", e.Chapters)
	}
	d, err := e.ChapterDoc(1, func(ch int) string { return "w5f:book/7/ch/" + strconv.Itoa(ch) })
	if err != nil {
		t.Fatal(err)
	}
	if h, ok := d.Blocks[0].(doc.Heading); !ok || h.Text.PlainText() != "Resurrection and Death" {
		t.Errorf("<header> heading lost: %+v", d.Blocks[0])
	}
	if len(d.Links) != 1 || d.Links[0].Href != "w5f:book/7/ch/2" {
		t.Errorf("chapter link not rewritten: %+v", d.Links)
	}
}

func TestLibraryRoutesAndProgress(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("W5F_BOOKS", dir)
	writeEPUB(t, filepath.Join(dir, "Gene Wolfe - Shadow.epub"))
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("plain text"), 0o644)
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	env := Env{Fetcher: fetch.New("", "test"), DB: db}
	ctx := context.Background()

	home, err := Route(ctx, "w5f:books", env)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(flat(home), "The Shadow of the Torturer") {
		t.Fatalf("library home:\n%s", flat(home))
	}
	books, _ := db.Books("author", 0)
	var id int64
	for _, b := range books {
		if b.Format == "epub" {
			id = b.ID
		}
	}
	// First open skips the image-only cover.
	d, err := Route(ctx, "w5f:book/"+itoa(id), env)
	if err != nil {
		t.Fatal(err)
	}
	if d.Ref != "book:"+itoa(id)+":1" || d.Next == "" || d.Prev == "" {
		t.Errorf("first open: ref=%q next=%q prev=%q", d.Ref, d.Next, d.Prev)
	}
	// Leave chapter 2 halfway; reopening resumes there.
	SaveFromRef(db, "book:"+itoa(id)+":2", 0.5)
	d, _ = Route(ctx, "w5f:book/"+itoa(id), env)
	if d.Ref != "book:"+itoa(id)+":2" || d.Resume != 0.5 || d.Next != "" {
		t.Errorf("resume: ref=%q resume=%v next=%q", d.Ref, d.Resume, d.Next)
	}
	toc, _ := Route(ctx, "w5f:book/"+itoa(id)+"/toc", env)
	if !strings.Contains(flat(toc), "Severian  ◀ you are here") {
		t.Errorf("toc:\n%s", flat(toc))
	}
}

func flat(d *doc.Document) string {
	var b strings.Builder
	doc.ReplaceBlocks(d.Blocks, func(x doc.Block) ([]doc.Block, bool) {
		if p, ok := x.(doc.Paragraph); ok {
			b.WriteString(p.Text.PlainText() + "\n")
		}
		return nil, false
	})
	return b.String()
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
