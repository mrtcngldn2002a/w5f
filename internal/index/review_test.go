package index

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"w5f/internal/doc"
	"w5f/internal/store"
)

// A download lands on a chapter page, which is indexed first; the
// background pass must still index the rest of the book.
func TestBookPartlyIndexedGetsCompleted(t *testing.T) {
	db := tmpDB(t)
	p := filepath.Join(t.TempDir(), "Le Fanu - Carmilla.epub")
	writeEPUB(t, p)
	id, _ := db.UpsertBook(store.Book{Path: p, Format: "epub", Title: "Carmilla", Author: "Le Fanu", Chapters: 2})
	Page(db, "w5f:book/"+itoa(id)+"/ch/0", &doc.Document{Title: "Carmilla", Ref: "book:" + itoa(id) + ":0",
		Blocks: []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: "A vampire story."}}}}})
	if n, err := Books(db, nil); err != nil || n != 1 {
		t.Fatalf("Books: %v %d", err, n)
	}
	if hits, _ := Search(db, "styria", "", 30, 0); len(hits) != 1 {
		t.Errorf("chapter 2 not indexed: %+v", hits)
	}
}

// Deleting w5f.db: the reading history comes back from history.log, the
// library from a scan, and reindex restores page search.
func TestRebuildAfterDeletingTheDatabaseFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("W5F_NOTES", filepath.Join(dir, "notes"))
	t.Setenv("W5F_BOOKS", filepath.Join(dir, "books"))
	path := filepath.Join(dir, "w5f.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	db.Visit("https://example.org/a", "A", "web", "")
	db.Close()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	db, err = store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	load := func(target string) (*doc.Document, error) { return textDoc("A", "the lighthouse at dusk"), nil }
	r, err := Rebuild(context.Background(), db, filepath.Join(dir, "notes"), load)
	if err != nil || r.Pages != 1 {
		t.Fatalf("rebuild: %v %+v", err, r)
	}
	if hits, _ := Search(db, "lighthouse", "", 30, 0); len(hits) != 1 {
		t.Errorf("page search not restored: %+v", hits)
	}
}
