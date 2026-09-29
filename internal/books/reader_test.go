package books

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"w5f/internal/doc"
	"w5f/internal/fetch"
	"w5f/internal/store"
)

type fakeBook struct{}

func (fakeBook) Info() Meta { return Meta{Title: "Fake Book", Author: "A. Writer", Lang: "en"} }
func (fakeBook) Contents() []Chapter {
	return []Chapter{{Title: "One"}, {Title: "Two"}}
}
func (fakeBook) ChapterDoc(i int, link func(int) string) (*doc.Document, error) {
	return &doc.Document{Blocks: []doc.Block{doc.Paragraph{Text: doc.Inline{
		{Text: fmt.Sprintf("Text of chapter %d, long enough to count as reading matter.", i+1)}}}}}, nil
}
func (fakeBook) Close() error { return nil }

func hasNotice(d *doc.Document, s string) bool {
	for _, b := range d.Blocks {
		if n, ok := b.(doc.Notice); ok && strings.Contains(n.Text, s) {
			return true
		}
	}
	return false
}

func TestReaderInterfaceDrivesTheLibrary(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("W5F_BOOKS", dir)
	openers[".fake"] = func(string) (Reader, error) { return fakeBook{}, nil }
	openers[".drm"] = func(string) (Reader, error) { return nil, ErrDRM }
	defer delete(openers, ".fake")
	defer delete(openers, ".drm")
	os.WriteFile(filepath.Join(dir, "x.fake"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, "Someone - Locked.drm"), []byte("x"), 0o644)
	old := filepath.Join(dir, "old name.fake")
	os.WriteFile(old, []byte("x"), 0o644)
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// Scanned by an earlier version, when this format was "external".
	db.UpsertBook(store.Book{Path: old, Format: "fake", Title: "old name"})
	env := Env{Fetcher: fetch.New("", "test"), DB: db}
	ctx := context.Background()
	if _, err := Scan(db); err != nil {
		t.Fatal(err)
	}
	var fake, locked, refreshed store.Book
	bs, _ := db.Books("author", 0)
	for _, b := range bs {
		switch {
		case b.Path == old:
			refreshed = b
		case b.Format == "fake":
			fake = b
		case b.Format == "drm":
			locked = b
		}
	}
	if fake.Title != "Fake Book" || fake.Author != "A. Writer" || fake.Chapters != 2 {
		t.Fatalf("scanned meta: %+v", fake)
	}
	if refreshed.Title != "Fake Book" || refreshed.Chapters != 2 {
		t.Errorf("a book listed before its format was readable is re-read: %+v", refreshed)
	}
	if locked.Title != "Locked" || locked.Author != "Someone" {
		t.Fatalf("a book that cannot be opened is still listed by its file name: %+v", locked)
	}
	d, err := Route(ctx, "w5f:book/"+itoa(fake.ID), env)
	if err != nil || d.Ref != "book:"+itoa(fake.ID)+":0" || d.Next == "" || !strings.Contains(flat(d), "Text of chapter 1") {
		t.Fatalf("chapter: %v %+v", err, d)
	}
	toc, _ := Route(ctx, "w5f:book/"+itoa(fake.ID)+"/toc", env)
	if !strings.Contains(flat(toc), "One") || !strings.Contains(flat(toc), "Two") {
		t.Errorf("toc:\n%s", flat(toc))
	}
	d, err = Route(ctx, "w5f:book/"+itoa(locked.ID), env)
	if err != nil || !hasNotice(d, "DRM") || !strings.Contains(flat(d), "external viewer") {
		t.Errorf("DRM page: %v\n%s", err, flat(d))
	}
	if r, err := Open("book.xyz"); r != nil || !errors.Is(err, ErrUnsupported) {
		t.Errorf("unknown format: %v %v", r, err)
	}
	if bookExt(`C:\A\B.FB2.ZIP`) != ".fb2.zip" || bookExt("a/b.Mobi") != ".mobi" {
		t.Error("bookExt")
	}
	tmp := filepath.Join(dir, ".download-1.fake")
	os.WriteFile(tmp, []byte("x"), 0o644)
	b, err := AddFile(db, tmp, "fake", "given title", "", "site:x:1")
	if err != nil || b.Title != "Fake Book" || b.Chapters != 2 {
		t.Errorf("AddFile meta: %v %+v", err, b)
	}
	os.WriteFile(filepath.Join(dir, "Book.fb2.zip"), []byte("x"), 0o644)
	if p := uniquePath(filepath.Join(dir, "Book.fb2.zip")); filepath.Base(p) != "Book (2).fb2.zip" {
		t.Errorf("uniquePath keeps the double extension: %s", p)
	}
}
