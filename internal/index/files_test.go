package index

import (
	"archive/zip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"w5f/internal/doc"
	"w5f/internal/personal"
	"w5f/internal/store"
)

func writeEPUB(t *testing.T, path string) {
	t.Helper()
	f, _ := os.Create(path)
	zw := zip.NewWriter(f)
	add := func(name, body string) { w, _ := zw.Create(name); w.Write([]byte(body)) }
	add("META-INF/container.xml", `<?xml version="1.0"?><container xmlns="urn:oasis:names:tc:opendocument:xmlns:container" version="1.0"><rootfiles><rootfile full-path="c.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`)
	add("c.opf", `<?xml version="1.0"?><package xmlns="http://www.idpf.org/2007/opf" version="3.0"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>Carmilla</dc:title><dc:creator>Le Fanu</dc:creator></metadata>
<manifest><item id="a" href="a.xhtml" media-type="application/xhtml+xml"/><item id="b" href="b.xhtml" media-type="application/xhtml+xml"/></manifest><spine><itemref idref="a"/><itemref idref="b"/></spine></package>`)
	add("a.xhtml", `<html xmlns="http://www.w3.org/1999/xhtml"><body><h2>Prologue</h2><p>A vampire story.</p></body></html>`)
	add("b.xhtml", `<html xmlns="http://www.w3.org/1999/xhtml"><body><h2>Chapter I</h2><p>An early fright in Styria.</p></body></html>`)
	zw.Close()
	f.Close()
}

func TestBooksAndNotesIndexing(t *testing.T) {
	db := tmpDB(t)
	p := filepath.Join(t.TempDir(), "Le Fanu - Carmilla.epub")
	writeEPUB(t, p)
	id, _ := db.UpsertBook(store.Book{Path: p, Format: "epub", Title: "Carmilla", Author: "Le Fanu", Source: "gutenberg:10007", Chapters: 2})
	n, err := Books(db, nil)
	if err != nil || n != 1 {
		t.Fatalf("Books: %v %d", err, n)
	}
	hits, _ := Search(db, "styria", "", 30, 0)
	if len(hits) != 1 || hits[0].Kind != "book" || hits[0].Catalog != "BK·GUT·10007" || hits[0].Target != "w5f:book/"+itoa(id)+"/ch/1" {
		t.Errorf("book hit: %+v", hits)
	}
	if n, _ := Books(db, nil); n != 0 {
		t.Error("book indexed twice")
	}
	dir := t.TempDir()
	t.Setenv("W5F_NOTES", dir)
	np, _ := personal.AppendNote(personal.Source{Title: "SCP-173", URL: "https://scp-wiki.wikidot.com/scp-173", Catalog: "FIC·SCP·173"}, "Compare with the weeping angels.", now())
	personal.AppendClipping(personal.Source{Title: "X", URL: "https://x"}, []string{"angels in the archive"}, now())
	os.WriteFile(personal.QueuePath(), []byte("# Reading queue\nangels queue\n"), 0o644)
	n, err = Notes(db, dir)
	if err != nil || n != 2 {
		t.Fatalf("Notes: %v %d", err, n)
	}
	hits, _ = Search(db, "angels", "notes", 30, 0)
	if len(hits) != 2 {
		t.Fatalf("notes hits: %+v", hits)
	}
	for _, h := range hits {
		if h.Kind == "note" && (h.Target != personal.FileURL(np) || h.Catalog != "FIC·SCP·173" || h.Title != "SCP-173") {
			t.Errorf("note hit: %+v", h)
		}
	}
}

// Deleting the database loses no personal data: a new database is rebuilt
// from history pages (via load), feeds, books and the notes folder.
func TestRebuildAfterDatabaseLoss(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("W5F_NOTES", dir)
	t.Setenv("W5F_BOOKS", t.TempDir())
	personal.AppendNote(personal.Source{Title: "Note", URL: "https://n"}, "the lighthouse keeper", now())
	db := tmpDB(t) // "new" database after the old one was deleted
	db.Visit("https://example.org/a", "A", "web", "")
	db.Visit("https://example.org/gone", "Gone", "web", "")
	load := func(target string) (*doc.Document, error) {
		if target == "https://example.org/a" {
			return textDoc("A", "the lighthouse at dusk"), nil
		}
		return nil, errors.New("not in the cache")
	}
	r, err := Rebuild(context.Background(), db, dir, load)
	if err != nil || r.Pages != 1 || r.Skipped != 1 || r.Notes != 1 {
		t.Fatalf("rebuild: %v %+v", err, r)
	}
	if hits, _ := Search(db, "lighthouse", "", 30, 0); len(hits) != 2 {
		t.Errorf("after rebuild: %+v", hits)
	}
}
