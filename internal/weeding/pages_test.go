package weeding

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"w5f/internal/doc"
	"w5f/internal/personal"
	"w5f/internal/store"
)

func text(d *doc.Document) string {
	var b strings.Builder
	doc.ReplaceBlocks(d.Blocks, func(x doc.Block) ([]doc.Block, bool) {
		switch x := x.(type) {
		case doc.Paragraph:
			b.WriteString(x.Text.PlainText() + "\n")
		case doc.Heading:
			b.WriteString("# " + x.Text.PlainText() + "\n")
		case doc.Notice:
			b.WriteString("! " + x.Text + "\n")
		case doc.Table:
			for _, r := range x.Rows {
				for _, c := range r {
					b.WriteString(c.PlainText() + " | ")
				}
				b.WriteString("\n")
			}
		}
		return nil, false
	})
	return b.String()
}

func testEnv(t *testing.T) Env {
	t.Helper()
	base := t.TempDir()
	os.MkdirAll(filepath.Join(base, "data"), 0o755)
	db, err := store.Open(filepath.Join(base, "data", "w5f.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return Env{DB: db, CacheDir: filepath.Join(base, "cache"), DataDir: filepath.Join(base, "data"),
		NotesDir: filepath.Join(base, "Notes"), BooksDir: filepath.Join(base, "Books"), ComicsDir: filepath.Join(base, "Comics"),
		CacheLimit: 500 << 20}
}

func put(t *testing.T, p, body string) string {
	t.Helper()
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func route(t *testing.T, env Env, target string) (*doc.Document, error) {
	t.Helper()
	return Route(context.Background(), target, env)
}

func sureLinks(d *doc.Document) []string {
	var out []string
	for _, l := range d.Links {
		if strings.Contains(l.Href, "sure=yes") {
			out = append(out, l.Href)
		}
	}
	return out
}

// The room lists what is kept and what can go; nothing on it removes
// anything at once.
func TestRoom(t *testing.T) {
	env := testEnv(t)
	put(t, filepath.Join(env.CacheDir, "http", "aa", "x.body"), strings.Repeat("x", 2048))
	put(t, filepath.Join(env.CacheDir, "uv", "big"), strings.Repeat("x", 9000)) // another program's
	d, err := route(t, env, "w5f:weeding")
	if err != nil {
		t.Fatal(err)
	}
	s := text(d)
	for _, want := range []string{"What the library keeps", "The page cache (1 files) | 2 KB", "Empty the page cache…", "Remove a note…", "Remove a book…",
		"Remove a comic…", "Clear the history…", "kept under 500.0 MB", "— U."} {
		if !strings.Contains(s, want) {
			t.Errorf("room lacks %q:\n%s", want, s)
		}
	}
	if l := sureLinks(d); len(l) > 0 {
		t.Errorf("the room itself removes something: %v", l)
	}
}

func TestEmptyCacheAsksFirst(t *testing.T) {
	env := testEnv(t)
	put(t, filepath.Join(env.CacheDir, "http", "aa", "x.body"), "page")
	other := put(t, filepath.Join(env.CacheDir, "flaresolverr-install", "keep"), "theirs")
	d, _ := route(t, env, "w5f:weeding/cache")
	if d.Links[0].Href != "w5f:weeding" || !strings.Contains(text(d), "No, keep it · Yes, empty it") {
		t.Fatalf("asking: %+v\n%s", d.Links, text(d))
	}
	if n, _ := CacheSize(env.CacheDir); n == 0 {
		t.Fatal("asking emptied the cache")
	}
	d, _ = route(t, env, "w5f:weeding/cache?sure=yes")
	if !strings.Contains(text(d), "! The page cache is empty: 1 files") || d.URL != "w5f:weeding" {
		t.Errorf("emptied: %q\n%s", d.URL, text(d))
	}
	if _, err := os.Stat(other); err != nil {
		t.Error("another program's file went")
	}
}

func TestRemoveANote(t *testing.T) {
	env := testEnv(t)
	a := put(t, filepath.Join(env.NotesDir, "Notes", "SCP-173.md"), "# a note")
	b := put(t, filepath.Join(env.NotesDir, "Clippings", "2026", "10", "2026-10-02.md"), "> clip")
	queue := put(t, filepath.Join(env.NotesDir, "Queue.md"), "# queue")
	env.DB.PutDoc(store.IndexDoc{Target: personal.FileURL(a), Kind: "note", Title: "SCP-173", Text: "a note", Updated: time.Now()})

	d, _ := route(t, env, "w5f:weeding/notes")
	s := text(d)
	if !strings.Contains(s, "Notes/SCP-173.md") || !strings.Contains(s, "Clippings/2026/10/2026-10-02.md") || strings.Contains(s, "Queue.md") {
		t.Fatalf("notes:\n%s", s)
	}
	if l := sureLinks(d); len(l) > 0 {
		t.Errorf("a removal without asking: %v", l)
	}
	rel := "Notes/SCP-173.md"
	d, err := route(t, env, "w5f:weeding/note?"+url.Values{"f": {rel}}.Encode())
	if err != nil || d.Links[0].Href != "w5f:weeding/notes" || !strings.Contains(text(d), "deleted for good") {
		t.Fatalf("asking: %v %+v", err, d)
	}
	if _, err := os.Stat(a); err != nil {
		t.Fatal("asking removed the note")
	}
	d, err = route(t, env, "w5f:weeding/note?"+url.Values{"f": {rel}, "sure": {"yes"}}.Encode())
	if err != nil || !strings.Contains(text(d), "! Removed: Notes/SCP-173.md") || d.URL != "w5f:weeding/notes" {
		t.Fatalf("removed: %v\n%s", err, text(d))
	}
	if _, err := os.Stat(a); err == nil {
		t.Error("the note is still there")
	}
	if _, err := os.Stat(b); err != nil {
		t.Error("another note went with it")
	}
	if env.DB.CountDocs(personal.FileURL(a)) != 0 {
		t.Error("the search still finds it")
	}
	// Never outside the notes' own folders, never the queue.
	for _, bad := range []string{"Queue.md", "../Books/x.md", "Notes/../../outside.md", "Notes", "Notes/missing.md"} {
		if _, err := route(t, env, "w5f:weeding/note?"+url.Values{"f": {bad}, "sure": {"yes"}}.Encode()); err == nil {
			t.Errorf("%q was taken for a note", bad)
		}
	}
	if _, err := os.Stat(queue); err != nil {
		t.Error("the queue went")
	}
}

func TestRemoveABook(t *testing.T) {
	env := testEnv(t)
	p := put(t, filepath.Join(env.BooksDir, "Dracula.epub"), "epub")
	q := put(t, filepath.Join(env.BooksDir, "Carmilla.epub"), "epub")
	id, _ := env.DB.UpsertBook(store.Book{Path: p, Format: "epub", Title: "Dracula", Author: "Bram Stoker", Chapters: 2})
	other, _ := env.DB.UpsertBook(store.Book{Path: q, Format: "epub", Title: "Carmilla", Chapters: 2})
	env.DB.PutDoc(store.IndexDoc{Target: fmt.Sprintf("w5f:book/%d/ch/0", id), Kind: "book", Title: "Dracula", Text: "x", Updated: time.Now()})
	env.DB.PutDoc(store.IndexDoc{Target: fmt.Sprintf("w5f:book/%d/ch/0", other), Kind: "book", Title: "Carmilla", Text: "x", Updated: time.Now()})

	d, _ := route(t, env, "w5f:weeding/books")
	if l := sureLinks(d); len(l) > 0 {
		t.Errorf("a removal without asking: %v", l)
	}
	d, _ = route(t, env, fmt.Sprintf("w5f:weeding/book?id=%d", id))
	if d.Links[0].Href != "w5f:weeding/books" || !strings.Contains(text(d), "Dracula — Bram Stoker") {
		t.Fatalf("asking:\n%s", text(d))
	}
	d, err := route(t, env, fmt.Sprintf("w5f:weeding/book?id=%d&sure=yes", id))
	if err != nil || !strings.Contains(text(d), "! Removed: Dracula") {
		t.Fatalf("removed: %v\n%s", err, text(d))
	}
	if _, err := os.Stat(p); err == nil {
		t.Error("the file is still there")
	}
	if _, err := env.DB.Book(id); err == nil {
		t.Error("still in The Stacks")
	}
	if env.DB.CountDocs(fmt.Sprintf("w5f:book/%d/", id)) != 0 || env.DB.CountDocs(fmt.Sprintf("w5f:book/%d/", other)) != 1 {
		t.Error("the index")
	}
	if _, err := os.Stat(q); err != nil {
		t.Error("another book went")
	}
	// A book whose file lies outside the library folder is not deleted.
	outside := put(t, filepath.Join(t.TempDir(), "elsewhere.epub"), "epub")
	oid, _ := env.DB.UpsertBook(store.Book{Path: outside, Format: "epub", Title: "Elsewhere", Chapters: 1})
	if _, err := route(t, env, fmt.Sprintf("w5f:weeding/book?id=%d&sure=yes", oid)); err == nil {
		t.Error("a file outside the library was removed")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Error("the outside file went")
	}
}

func TestRemoveAComic(t *testing.T) {
	env := testEnv(t)
	cbz := put(t, filepath.Join(env.ComicsDir, "Local", "Gamma", "Chapter 1.cbz"), "zip")
	cover := put(t, filepath.Join(env.ComicsDir, "Local", "Gamma", "cover.jpg"), "jpg")
	folder := filepath.Join(env.ComicsDir, "Scans", "Issue 2")
	put(t, filepath.Join(folder, "01.jpg"), "1")
	put(t, filepath.Join(folder, "02.png"), "2")
	deeper := put(t, filepath.Join(folder, "extras", "keep.jpg"), "k")
	dl := put(t, filepath.Join(env.ComicsDir, "Suwayomi", "mangas", "Src", "Berserk", "Chapter 1.cbz"), "zip")
	id1, _ := env.DB.UpsertComic(store.Comic{Path: cbz, Series: "Gamma", Title: "Chapter 1", Pages: 3})
	id2, _ := env.DB.UpsertComic(store.Comic{Path: folder, Series: "Scans", Title: "Issue 2", Pages: 2})
	id3, _ := env.DB.UpsertComic(store.Comic{Path: dl, Series: "Berserk", Title: "Chapter 1", Pages: 3})
	thumbs := filepath.Join(env.ComicsDir, "Suwayomi", "thumbnails")
	put(t, filepath.Join(thumbs, "75.webp"), "w")
	tid, _ := env.DB.UpsertComic(store.Comic{Path: thumbs, Series: "Suwayomi", Title: "thumbnails", Pages: 1}) // an older scan's mistake
	rescanned := false
	env.RescanComics = func() { rescanned = true }

	d, _ := route(t, env, "w5f:weeding/comics")
	if s := text(d); !strings.Contains(s, "Berserk · Chapter 1") || !strings.Contains(s, "downloaded by Suwayomi") || strings.Contains(s, "thumbnails") || !rescanned {
		t.Fatalf("comics (rescanned %v):\n%s", rescanned, s)
	}
	if _, err := route(t, env, fmt.Sprintf("w5f:weeding/comic?id=%d&sure=yes", tid)); err == nil {
		t.Error("Suwayomi's thumbnails were taken for a comic")
	}
	if l := sureLinks(d); len(l) > 0 {
		t.Errorf("a removal without asking: %v", l)
	}
	if d, _ := route(t, env, fmt.Sprintf("w5f:weeding/comic?id=%d", id1)); d.Links[0].Href != "w5f:weeding/comics" {
		t.Error("asking")
	}
	if _, err := route(t, env, fmt.Sprintf("w5f:weeding/comic?id=%d&sure=yes", id1)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cbz); err == nil {
		t.Error("the CBZ is still there")
	}
	if _, err := os.Stat(cover); err != nil {
		t.Error("the series' cover went with the chapter")
	}
	if _, err := route(t, env, fmt.Sprintf("w5f:weeding/comic?id=%d&sure=yes", id2)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(folder, "01.jpg")); err == nil {
		t.Error("a page is still there")
	}
	if _, err := os.Stat(deeper); err != nil {
		t.Error("a folder below the comic went")
	}

	// Suwayomi's chapter: through Suwayomi, never by hand.
	var asked string
	env.RemoveSuwayomi = func(ctx context.Context, p string) error { asked = p; return errors.New("Suwayomi is not running") }
	if _, err := route(t, env, fmt.Sprintf("w5f:weeding/comic?id=%d&sure=yes", id3)); err == nil || asked != dl {
		t.Errorf("Suwayomi: %v %q", err, asked)
	}
	if _, err := os.Stat(dl); err != nil {
		t.Error("the downloaded chapter was deleted by hand")
	}
	if _, err := env.DB.Comic(id3); err != nil {
		t.Error("forgotten though Suwayomi did not remove it")
	}
}

func TestWithin(t *testing.T) {
	base := filepath.Join("C", "Archive", "Books")
	for p, want := range map[string]bool{
		filepath.Join(base, "a.epub"): true, filepath.Join(base, "x", "b.pdf"): true, base: false,
		filepath.Join(base, "..", "Notes", "n.md"): false, filepath.Join("C", "Archive", "Books2", "c.epub"): false, "": false,
	} {
		if got := within(base, p); got != want {
			t.Errorf("%s: %v", p, got)
		}
	}
}
