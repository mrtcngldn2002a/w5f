package store

import (
	"database/sql"
	"errors"
	"time"
)

const comicsSchema = `
CREATE TABLE IF NOT EXISTS comics (
  id       INTEGER PRIMARY KEY AUTOINCREMENT,
  path     TEXT NOT NULL UNIQUE,
  series   TEXT NOT NULL DEFAULT '',
  number   REAL NOT NULL DEFAULT 0,
  title    TEXT NOT NULL DEFAULT '',
  pages    INTEGER NOT NULL DEFAULT 0,
  rtl      INTEGER NOT NULL DEFAULT 0,
  added    INTEGER NOT NULL DEFAULT 0,
  opened   INTEGER NOT NULL DEFAULT 0,
  page     INTEGER NOT NULL DEFAULT 0,  -- last page shown (0-based)
  finished INTEGER NOT NULL DEFAULT 0,
  missing  INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS comics_series ON comics(series, number);
`

func init() { schemaExtras = append(schemaExtras, comicsSchema) }

// Comic is one issue or chapter file in the local comics library.
type Comic struct {
	ID       int64
	Path     string
	Series   string
	Number   float64
	Title    string
	Pages    int
	RTL      bool
	Added    time.Time
	Opened   time.Time
	Page     int
	Finished bool
}

const comicCols = `id,path,series,number,title,pages,rtl,added,opened,page,finished`

func scanComic(s scanner) (Comic, error) {
	var c Comic
	var rtl, fin int
	var added, opened int64
	err := s.Scan(&c.ID, &c.Path, &c.Series, &c.Number, &c.Title, &c.Pages, &rtl, &added, &opened, &c.Page, &fin)
	c.RTL, c.Finished = rtl != 0, fin != 0
	c.Added, c.Opened = time.Unix(added, 0), time.Unix(opened, 0)
	if opened == 0 {
		c.Opened = time.Time{}
	}
	return c, err
}

// UpsertComic adds a comic or refreshes its metadata, keeping reading state.
func (db *DB) UpsertComic(c Comic) (int64, error) {
	_, err := db.sql.Exec(`INSERT INTO comics(path,series,number,title,pages,rtl,added) VALUES(?,?,?,?,?,?,?)
	  ON CONFLICT(path) DO UPDATE SET series=excluded.series, number=excluded.number, title=excluded.title,
	  pages=excluded.pages, rtl=excluded.rtl, missing=0`,
		c.Path, c.Series, c.Number, c.Title, c.Pages, b2i(c.RTL), time.Now().Unix())
	if err != nil {
		return 0, err
	}
	var id int64
	err = db.sql.QueryRow(`SELECT id FROM comics WHERE path=?`, c.Path).Scan(&id)
	return id, err
}

// Comic returns one comic.
func (db *DB) Comic(id int64) (Comic, error) {
	c, err := scanComic(db.sql.QueryRow(`SELECT `+comicCols+` FROM comics WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return c, errors.New("comic not found")
	}
	return c, err
}

// ComicSeries is a series in the local library with its read state.
type ComicSeries struct {
	Name   string
	Issues int
	Unread int
	Opened time.Time
}

// ComicSeriesList lists local series by name.
func (db *DB) ComicSeriesList() ([]ComicSeries, error) {
	rows, err := db.sql.Query(`SELECT series, count(*), sum(CASE WHEN finished=0 THEN 1 ELSE 0 END), max(opened)
	  FROM comics WHERE missing=0 GROUP BY series ORDER BY series COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ComicSeries
	for rows.Next() {
		var s ComicSeries
		var opened int64
		if err := rows.Scan(&s.Name, &s.Issues, &s.Unread, &opened); err != nil {
			return nil, err
		}
		if opened > 0 {
			s.Opened = time.Unix(opened, 0)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ComicsInSeries lists a series' issues in reading order.
func (db *DB) ComicsInSeries(series string) ([]Comic, error) {
	return db.comics(`SELECT `+comicCols+` FROM comics WHERE missing=0 AND series=? ORDER BY number, path`, series)
}

// RecentComics lists the comics opened last (unfinished first).
func (db *DB) RecentComics(limit int) ([]Comic, error) {
	return db.comics(`SELECT `+comicCols+` FROM comics WHERE missing=0 AND opened>0 ORDER BY finished, opened DESC LIMIT ?`, limit)
}

func (db *DB) comics(q string, args ...any) ([]Comic, error) {
	rows, err := db.sql.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Comic
	for rows.Next() {
		c, err := scanComic(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SaveComicPage records the page shown; the last page marks the comic read.
func (db *DB) SaveComicPage(id int64, page, pages int) error {
	fin := 0
	if pages > 0 && page >= pages-1 {
		fin = 1
	}
	_, err := db.sql.Exec(`UPDATE comics SET page=?, opened=?, finished=CASE WHEN ?=1 THEN 1 ELSE finished END WHERE id=?`,
		page, time.Now().Unix(), fin, id)
	return err
}

// SetComicFinished marks a comic read or unread.
func (db *DB) SetComicFinished(id int64, finished bool) error {
	_, err := db.sql.Exec(`UPDATE comics SET finished=? WHERE id=?`, b2i(finished), id)
	return err
}

// MarkComicsMissingExcept flags comics whose files were not seen in a scan.
func (db *DB) MarkComicsMissingExcept(seen map[string]bool) error {
	rows, err := db.sql.Query(`SELECT path FROM comics WHERE missing=0`)
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
		if _, err := db.sql.Exec(`UPDATE comics SET missing=1 WHERE path=?`, p); err != nil {
			return err
		}
	}
	return nil
}

// ComicByPath finds a comic by its file.
func (db *DB) ComicByPath(path string) (Comic, bool) {
	c, err := scanComic(db.sql.QueryRow(`SELECT `+comicCols+` FROM comics WHERE path=? AND missing=0`, path))
	return c, err == nil
}
