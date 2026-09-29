package index

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"w5f/internal/books"
	"w5f/internal/catalog"
	"w5f/internal/doc"
	"w5f/internal/personal"
	"w5f/internal/store"
)

// Progress is a short status line while books are indexed in the background.
var Progress atomic.Value

var bookMu sync.Mutex

// Books indexes library books whose chapters are not indexed yet, one
// chapter per document, through books.Open. load opens the single-page
// books (text, HTML, Markdown); nil skips them. Only one indexer runs at a time.
func Books(db *store.DB, load func(path string) (*doc.Document, error)) (int, error) {
	if !bookMu.TryLock() {
		return 0, nil
	}
	defer bookMu.Unlock()
	defer Progress.Store("")
	bs, err := db.Books("", 0)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, b := range bs {
		prefix := fmt.Sprintf("w5f:book/%d/ch/", b.ID)
		// A chapter opened (and indexed) before this pass does not make the
		// book complete; compare with its chapter count.
		if n := db.CountDocs(prefix); n > 0 && n >= max(b.Chapters, 1) {
			continue
		}
		cat := catalog.Book(b.Source, b.ID)
		r, err := books.Open(b.Path)
		switch {
		case err == nil:
			chs := r.Contents()
			for i := range chs {
				Progress.Store(fmt.Sprintf("indexing %s · ch %d/%d", b.Title, i+1, len(chs)))
				cd, err := r.ChapterDoc(i, func(ch int) string { return fmt.Sprintf("%s%d", prefix, ch) })
				if err != nil {
					continue
				}
				title := b.Title
				if chs[i].Title != "" && chs[i].Title != b.Title {
					title += " · " + chs[i].Title
				} else if cd.Title != "" && cd.Title != b.Title {
					title += " · " + cd.Title
				}
				if err := put(db, fmt.Sprintf("%s%d", prefix, i), "book", title, cat, Text(cd)); err != nil {
					r.Close()
					return n, err
				}
			}
			r.Close()
			n++
		case errors.Is(err, books.ErrUnsupported) && textBook(b.Path):
			if load == nil {
				continue
			}
			d, err := load(b.Path)
			if err != nil {
				continue
			}
			if err := put(db, prefix+"0", "book", b.Title, cat, Text(d)); err != nil {
				return n, err
			}
			n++
		}
	}
	return n, nil
}

// textBook reports single-page text books (loaded like any file).
func textBook(p string) bool {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".txt", ".md", ".html", ".htm":
		return true
	}
	return false
}

var noteKinds = map[string]string{"Notes": "note", "Clippings": "clip", "Saved": "saved"}

// NoteFile indexes one file of the notes folder (notes, clippings, saved
// pages; the queue is not indexed).
func NoteFile(db *store.DB, dir, path string) error {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return err
	}
	kind := noteKinds[strings.Split(filepath.ToSlash(rel), "/")[0]]
	if kind == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	front, body := personal.SplitFront(string(data))
	title := front["title"]
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		if kind == "clip" {
			title = "Clippings " + title
		}
	}
	return put(db, personal.FileURL(path), kind, title, front["catalog"], capText(body))
}

// Notes indexes every Markdown file of the notes folder.
func Notes(db *store.DB, dir string) (int, error) {
	n := 0
	for sub := range noteKinds {
		err := filepath.WalkDir(filepath.Join(dir, sub), func(p string, de fs.DirEntry, err error) error {
			if err != nil || de.IsDir() || !strings.EqualFold(filepath.Ext(p), ".md") {
				return nil
			}
			if err := NoteFile(db, dir, p); err != nil {
				return err
			}
			n++
			return nil
		})
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

// Report counts what a rebuild indexed.
type Report struct{ Pages, Feeds, Books, Notes, Skipped int }

// Rebuild recreates the index: history pages (loaded by load, normally
// from the cache), feed items, library books and the notes folder.
func Rebuild(ctx context.Context, db *store.DB, notesDir string, load func(target string) (*doc.Document, error)) (Report, error) {
	var r Report
	if err := db.ClearIndex(); err != nil {
		return r, err
	}
	// After the database was deleted: history from history.log, books from
	// the library folder (feed items come back with the next sync).
	if _, err := db.RestoreHistory(); err != nil {
		return r, err
	}
	if _, err := books.Scan(db); err != nil {
		return r, err
	}
	vs, err := db.History(0, 0)
	if err != nil {
		return r, err
	}
	for _, v := range vs {
		if err := ctx.Err(); err != nil {
			return r, err
		}
		if v.Kind == "book" || v.Kind == "feed" {
			continue // indexed from the library and the feed store below
		}
		d, err := load(v.Target)
		if err != nil {
			r.Skipped++
			continue
		}
		if err := Page(db, v.Target, d); err != nil {
			return r, err
		}
		r.Pages++
	}
	if r.Feeds, err = Feeds(db); err != nil {
		return r, err
	}
	if r.Books, err = Books(db, load); err != nil {
		return r, err
	}
	r.Notes, err = Notes(db, notesDir)
	return r, err
}
