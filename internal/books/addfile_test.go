package books

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"w5f/internal/store"
)

func TestAddFileMovesAndRecords(t *testing.T) {
	lib := t.TempDir()
	t.Setenv("W5F_BOOKS", lib)
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// EPUB: metadata from the file wins over the given title.
	tmp := filepath.Join(lib, ".download-1.epub")
	writeEPUB(t, tmp)
	b, err := AddFile(db, tmp, "epub", "ignored", "", "site:x:https://x/1")
	if err != nil {
		t.Fatal(err)
	}
	if b.Title != "The Shadow of the Torturer" || b.Author != "Gene Wolfe" || b.Chapters != 3 {
		t.Errorf("epub meta: %+v", b)
	}
	if filepath.Base(b.Path) != "Gene Wolfe - The Shadow of the Torturer.epub" {
		t.Errorf("path = %s", b.Path)
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Error("temp file should have been moved")
	}
	if got, ok := db.BookBySource("site:x:https://x/1"); !ok || got.ID != b.ID {
		t.Error("book not findable by source")
	}

	// Non-EPUB with the same name gets a unique path.
	tmp2 := filepath.Join(lib, ".download-2.pdf")
	os.WriteFile(tmp2, []byte("%PDF-1.4"), 0o644)
	p, err := AddFile(db, tmp2, "pdf", "Dracula", "Bram Stoker", "site:x:https://x/2")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(p.Path) != "Bram Stoker - Dracula.pdf" || p.Format != "pdf" {
		t.Errorf("pdf: %+v", p)
	}
	tmp3 := filepath.Join(lib, ".download-3.pdf")
	os.WriteFile(tmp3, []byte("%PDF-1.4"), 0o644)
	p2, _ := AddFile(db, tmp3, "pdf", "Dracula", "Bram Stoker", "site:x:https://x/3")
	if filepath.Base(p2.Path) != "Bram Stoker - Dracula (2).pdf" {
		t.Errorf("unique path = %s", p2.Path)
	}
}

func TestHomeListsSiteCatalogs(t *testing.T) {
	t.Setenv("W5F_BOOKS", t.TempDir())
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	env := Env{DB: db, Catalogs: func() []CatalogLink { return []CatalogLink{{ID: "fadedpage-com", Name: "Faded Page"}} }}
	d, err := Route(context.Background(), "w5f:books", env)
	if err != nil {
		t.Fatal(err)
	}
	var hrefs []string
	for _, l := range d.Links {
		hrefs = append(hrefs, l.Href)
	}
	all := strings.Join(hrefs, " ")
	if !strings.Contains(all, "w5f:catalog/fadedpage-com") || !strings.Contains(all, "w5f:catalogs") {
		t.Errorf("links: %s", all)
	}
}
