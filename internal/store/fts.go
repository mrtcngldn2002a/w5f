package store

import (
	"strings"
	"time"
)

const ftsSchema = `
CREATE TABLE IF NOT EXISTS docs (
  id      INTEGER PRIMARY KEY,
  target  TEXT NOT NULL UNIQUE,
  kind    TEXT NOT NULL,
  title   TEXT NOT NULL,
  catalog TEXT NOT NULL DEFAULT '',
  text    TEXT NOT NULL,
  updated INTEGER NOT NULL
);
CREATE VIRTUAL TABLE IF NOT EXISTS docs_fts USING fts5(title, body, tokenize='unicode61');
CREATE VIRTUAL TABLE IF NOT EXISTS docs_vocab USING fts5vocab(docs_fts, row);
`

func init() { schemaExtras = append(schemaExtras, ftsSchema) }

// IndexDoc is one searchable document. Title and Text are shown; the folded
// forms are what the full-text index matches.
type IndexDoc struct {
	ID        int64
	Target    string
	Kind      string
	Title     string
	Catalog   string
	Text      string
	Updated   time.Time
	FoldTitle string
	FoldText  string
}

// PutDoc adds a document to the index or replaces the one with its target.
func (db *DB) PutDoc(d IndexDoc) error {
	tx, err := db.sql.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id int64
	err = tx.QueryRow(`INSERT INTO docs(target,kind,title,catalog,text,updated) VALUES(?,?,?,?,?,?)
	  ON CONFLICT(target) DO UPDATE SET kind=excluded.kind, title=excluded.title, catalog=excluded.catalog,
	    text=excluded.text, updated=excluded.updated
	  RETURNING id`, d.Target, d.Kind, d.Title, d.Catalog, d.Text, d.Updated.Unix()).Scan(&id)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM docs_fts WHERE rowid=?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO docs_fts(rowid,title,body) VALUES(?,?,?)`, id, d.FoldTitle, d.FoldText); err != nil {
		return err
	}
	return tx.Commit()
}

// FindDocs runs an FTS5 query (over folded text), best matches first.
func (db *DB) FindDocs(match string, kinds []string, limit, offset int) ([]IndexDoc, error) {
	q := `SELECT d.id,d.target,d.kind,d.title,d.catalog,d.text,d.updated
	  FROM docs_fts JOIN docs d ON d.id=docs_fts.rowid WHERE docs_fts MATCH ?`
	args := []any{match}
	if len(kinds) > 0 {
		q += ` AND d.kind IN (` + strings.TrimSuffix(strings.Repeat("?,", len(kinds)), ",") + `)`
		for _, k := range kinds {
			args = append(args, k)
		}
	}
	q += ` ORDER BY bm25(docs_fts, 5.0, 1.0), d.updated DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)
	rows, err := db.sql.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []IndexDoc
	for rows.Next() {
		var d IndexDoc
		var up int64
		if err := rows.Scan(&d.ID, &d.Target, &d.Kind, &d.Title, &d.Catalog, &d.Text, &up); err != nil {
			return nil, err
		}
		d.Updated = time.Unix(up, 0)
		out = append(out, d)
	}
	return out, rows.Err()
}

// CountDocs counts indexed documents whose target starts with prefix.
func (db *DB) CountDocs(prefix string) int {
	var n int
	_ = db.sql.QueryRow(`SELECT count(*) FROM docs WHERE substr(target,1,?)=?`, len(prefix), prefix).Scan(&n)
	return n
}

// ClearIndex empties the full-text index (before a rebuild).
func (db *DB) ClearIndex() error {
	if _, err := db.sql.Exec(`DELETE FROM docs`); err != nil {
		return err
	}
	_, err := db.sql.Exec(`DELETE FROM docs_fts`)
	return err
}

// ItemsToIndex lists feed items that are not in the index yet, newest first.
func (db *DB) ItemsToIndex(limit int) ([]Item, error) {
	rows, err := db.sql.Query(`SELECT id,feed_id,url,title,author,published,summary,content FROM items
	  WHERE ('w5f:item/'||id) NOT IN (SELECT target FROM docs) ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Item
	for rows.Next() {
		var it Item
		var pub int64
		if err := rows.Scan(&it.ID, &it.FeedID, &it.URL, &it.Title, &it.Author, &pub, &it.Summary, &it.Content); err != nil {
			return nil, err
		}
		if pub > 0 {
			it.Published = time.Unix(pub, 0)
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// DocFreq tells in how many indexed documents each (folded) word occurs,
// and how many documents there are.
func (db *DB) DocFreq(words []string) (map[string]int, int, error) {
	var total int
	if err := db.sql.QueryRow(`SELECT count(*) FROM docs`).Scan(&total); err != nil {
		return nil, 0, err
	}
	out := map[string]int{}
	for len(words) > 0 {
		part := words[:min(len(words), 200)]
		words = words[len(part):]
		args := make([]any, len(part))
		for i, w := range part {
			args[i] = w
		}
		rows, err := db.sql.Query(`SELECT term, doc FROM docs_vocab WHERE term IN (`+
			strings.TrimSuffix(strings.Repeat("?,", len(part)), ",")+`)`, args...)
		if err != nil {
			return nil, 0, err
		}
		for rows.Next() {
			var t string
			var n int
			if err := rows.Scan(&t, &n); err != nil {
				rows.Close()
				return nil, 0, err
			}
			out[t] = n
		}
		if err := rows.Close(); err != nil {
			return nil, 0, err
		}
	}
	return out, total, nil
}

// ReadDoc is an indexed page that is also in the reading history.
type ReadDoc struct {
	IndexDoc
	Last time.Time // when it was last opened
}

// FindRead runs an FTS5 query over the pages of the reading history last
// opened before a time, leaving one target out; best matches first.
func (db *DB) FindRead(match, except string, before time.Time, limit int) ([]ReadDoc, error) {
	rows, err := db.sql.Query(`SELECT d.id,d.target,d.kind,d.title,d.catalog,d.text,d.updated,h.last
	  FROM docs_fts JOIN docs d ON d.id=docs_fts.rowid JOIN history h ON h.target=d.target
	  WHERE docs_fts MATCH ? AND d.target <> ? AND h.last < ?
	  ORDER BY bm25(docs_fts, 5.0, 1.0) LIMIT ?`, match, except, before.UnixNano(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ReadDoc
	for rows.Next() {
		var d ReadDoc
		var up, last int64
		if err := rows.Scan(&d.ID, &d.Target, &d.Kind, &d.Title, &d.Catalog, &d.Text, &up, &last); err != nil {
			return nil, err
		}
		d.Updated, d.Last = time.Unix(up, 0), time.Unix(0, last)
		out = append(out, d)
	}
	return out, rows.Err()
}

// Doc is the indexed document of a target.
func (db *DB) Doc(target string) (IndexDoc, bool) {
	var d IndexDoc
	var up int64
	err := db.sql.QueryRow(`SELECT id,target,kind,title,catalog,text,updated FROM docs WHERE target=?`, target).
		Scan(&d.ID, &d.Target, &d.Kind, &d.Title, &d.Catalog, &d.Text, &up)
	d.Updated = time.Unix(up, 0)
	return d, err == nil
}
