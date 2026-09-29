package store

import (
	"database/sql"
	"errors"
	"time"
)

const booksSchema = `
CREATE TABLE IF NOT EXISTS books (
  id        INTEGER PRIMARY KEY AUTOINCREMENT,
  path      TEXT NOT NULL UNIQUE,
  format    TEXT NOT NULL DEFAULT '',
  title     TEXT NOT NULL DEFAULT '',
  author    TEXT NOT NULL DEFAULT '',
  lang      TEXT NOT NULL DEFAULT '',
  source    TEXT NOT NULL DEFAULT '',   -- e.g. "gutenberg:84", "se:h-p-lovecraft/short-fiction"
  added     INTEGER NOT NULL DEFAULT 0,
  opened    INTEGER NOT NULL DEFAULT 0,
  chapter   INTEGER NOT NULL DEFAULT 0,
  pos       REAL    NOT NULL DEFAULT 0, -- fraction of the chapter read (0..1)
  chapters  INTEGER NOT NULL DEFAULT 0,
  missing   INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS books_opened ON books(opened DESC);
`

func init() { schemaExtras = append(schemaExtras, booksSchema) }

// Book is a file in the local library.
type Book struct {
	ID       int64
	Path     string
	Format   string
	Title    string
	Author   string
	Lang     string
	Source   string
	Added    time.Time
	Opened   time.Time
	Chapter  int
	Pos      float64
	Chapters int
}

// UpsertBook adds a book or refreshes its metadata (keeping reading state).
func (db *DB) UpsertBook(b Book) (int64, error) {
	_, err := db.sql.Exec(`INSERT INTO books(path,format,title,author,lang,source,added,chapters) VALUES(?,?,?,?,?,?,?,?)
	  ON CONFLICT(path) DO UPDATE SET format=excluded.format, title=excluded.title, author=excluded.author,
	  lang=excluded.lang, source=CASE WHEN excluded.source='' THEN books.source ELSE excluded.source END,
	  chapters=CASE WHEN excluded.chapters=0 THEN books.chapters ELSE excluded.chapters END, missing=0`,
		b.Path, b.Format, b.Title, b.Author, b.Lang, b.Source, time.Now().Unix(), b.Chapters)
	if err != nil {
		return 0, err
	}
	var id int64
	err = db.sql.QueryRow(`SELECT id FROM books WHERE path=?`, b.Path).Scan(&id)
	return id, err
}

// Book returns one book.
func (db *DB) Book(id int64) (Book, error) {
	row := db.sql.QueryRow(`SELECT id,path,format,title,author,lang,source,added,opened,chapter,pos,chapters FROM books WHERE id=?`, id)
	b, err := scanBook(row)
	if errors.Is(err, sql.ErrNoRows) {
		return b, errors.New("book not found")
	}
	return b, err
}

// BookBySource finds a book downloaded from a catalog.
func (db *DB) BookBySource(src string) (Book, bool) {
	row := db.sql.QueryRow(`SELECT id,path,format,title,author,lang,source,added,opened,chapter,pos,chapters FROM books WHERE source=? AND missing=0`, src)
	b, err := scanBook(row)
	return b, err == nil
}

// Books lists the library. order: "recent" (last opened), "author", "added".
func (db *DB) Books(order string, limit int) ([]Book, error) {
	q := `SELECT id,path,format,title,author,lang,source,added,opened,chapter,pos,chapters FROM books WHERE missing=0`
	switch order {
	case "recent":
		q += ` AND opened>0 ORDER BY opened DESC`
	case "added":
		q += ` ORDER BY added DESC`
	default:
		q += ` ORDER BY author COLLATE NOCASE, title COLLATE NOCASE`
	}
	if limit > 0 {
		q += ` LIMIT ?`
	} else {
		limit = -1
		q += ` LIMIT ?`
	}
	rows, err := db.sql.Query(q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Book
	for rows.Next() {
		b, err := scanBook(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// MarkMissingExcept flags books whose files were not seen in a scan.
func (db *DB) MarkMissingExcept(seen map[string]bool) error {
	rows, err := db.sql.Query(`SELECT path FROM books WHERE missing=0`)
	if err != nil {
		return err
	}
	var gone []string
	for rows.Next() {
		var p string
		if rows.Scan(&p) == nil && !seen[p] {
			gone = append(gone, p)
		}
	}
	rows.Close()
	for _, p := range gone {
		if _, err := db.sql.Exec(`UPDATE books SET missing=1 WHERE path=?`, p); err != nil {
			return err
		}
	}
	return nil
}

// SaveProgress records where a book was left.
func (db *DB) SaveProgress(id int64, chapter int, pos float64) error {
	_, err := db.sql.Exec(`UPDATE books SET chapter=?, pos=?, opened=? WHERE id=?`, chapter, pos, time.Now().Unix(), id)
	return err
}

func scanBook(s scanner) (Book, error) {
	var b Book
	var added, opened int64
	err := s.Scan(&b.ID, &b.Path, &b.Format, &b.Title, &b.Author, &b.Lang, &b.Source, &added, &opened, &b.Chapter, &b.Pos, &b.Chapters)
	b.Added, b.Opened = unix(added), unix(opened)
	return b, err
}
