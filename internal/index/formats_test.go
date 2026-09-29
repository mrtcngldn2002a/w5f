package index

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"w5f/internal/books/fixtures"
	"w5f/internal/store"
)

func TestBooksIndexMOBIAndFB2(t *testing.T) {
	db := tmpDB(t)
	dir := t.TempDir()
	raw := []byte("<html><body><h1>One</h1><p>The lighthouse keeper wrote a letter.</p></body></html>")
	mobi := fixtures.MOBI{Records: fixtures.Records(raw, fixtures.PalmDOC), TextLength: len(raw), Compression: 2,
		Encoding: 65001, Version: 6, Title: "Keeper"}.Build()
	fb2 := fixtures.FB2(`<description><title-info><book-title>Вий</book-title></title-info></description>`+
		`<body><section><title><p>Глава</p></title><p>Панночка и маяк.</p></section></body>`, "utf-8")
	for name, data := range map[string][]byte{"keeper.mobi": mobi, "viy.fb2": fb2} {
		p := filepath.Join(dir, name)
		os.WriteFile(p, data, 0o644)
		db.UpsertBook(store.Book{Path: p, Format: strings.TrimPrefix(filepath.Ext(name), "."), Title: name, Chapters: 1})
	}
	if n, err := Books(db, nil); err != nil || n != 2 {
		t.Fatalf("Books: %v %d", err, n)
	}
	for _, q := range []string{"lighthouse", "маяк"} {
		if hits, _ := Search(db, q, "book", 30, 0); len(hits) != 1 {
			t.Errorf("%q: %+v", q, hits)
		}
	}
}
