package books

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"w5f/internal/store"
)

// AddFile moves a downloaded file into the library and records it. For EPUB
// files the title, author and language are read from the book itself.
func AddFile(db *store.DB, tmpPath, format, title, author, src string) (store.Book, error) {
	format = strings.ToLower(strings.TrimPrefix(format, "."))
	b := store.Book{Format: format, Title: strings.TrimSpace(title), Author: strings.TrimSpace(author), Source: src}
	if chaptered(tmpPath) {
		r, err := Open(tmpPath)
		switch {
		case err == nil:
			m := r.Info()
			if m.Title != "" {
				b.Title = m.Title
			}
			if m.Author != "" {
				b.Author = m.Author
			}
			b.Lang, b.Chapters = m.Lang, len(r.Contents())
			r.Close()
		case format == "epub":
			return store.Book{}, fmt.Errorf("downloaded file is not a readable EPUB: %w", err)
		}
	}
	if b.Title == "" {
		b.Title = "Untitled"
	}
	dir := LibraryDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return store.Book{}, err
	}
	b.Path = uniquePath(filepath.Join(dir, FileName(b.Author, b.Title, "."+format)))
	if err := os.Rename(tmpPath, b.Path); err != nil {
		return store.Book{}, err
	}
	id, err := db.UpsertBook(b)
	if err != nil {
		return store.Book{}, err
	}
	return db.Book(id)
}

// uniquePath appends " (2)", " (3)"… when a file already exists.
func uniquePath(p string) string {
	if _, err := os.Stat(p); os.IsNotExist(err) {
		return p
	}
	ext := p[len(p)-len(bookExt(p)):] // "Book.fb2.zip" → "Book (2).fb2.zip"
	base := strings.TrimSuffix(p, ext)
	for i := 2; ; i++ {
		c := fmt.Sprintf("%s (%d)%s", base, i, ext)
		if _, err := os.Stat(c); os.IsNotExist(err) {
			return c
		}
	}
}
