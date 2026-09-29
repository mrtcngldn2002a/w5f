package store

import (
	"database/sql"
	"errors"
	"time"
)

const serialsSchema = `
CREATE TABLE IF NOT EXISTS serials (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  kind        TEXT NOT NULL,
  url         TEXT NOT NULL UNIQUE,
  title       TEXT NOT NULL DEFAULT '',
  author      TEXT NOT NULL DEFAULT '',
  summary     TEXT NOT NULL DEFAULT '',
  followed    INTEGER NOT NULL DEFAULT 0,
  checked     INTEGER NOT NULL DEFAULT 0,
  check_error TEXT NOT NULL DEFAULT '',
  opened      INTEGER NOT NULL DEFAULT 0,
  chapter     INTEGER NOT NULL DEFAULT 0,
  pos         REAL    NOT NULL DEFAULT 0,
  seen        INTEGER NOT NULL DEFAULT 0,  -- chapters known when last looked at
  added       INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS serial_chapters (
  serial_id INTEGER NOT NULL,
  n         INTEGER NOT NULL,
  title     TEXT NOT NULL DEFAULT '',
  url       TEXT NOT NULL,
  published INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(serial_id, n)
);
CREATE UNIQUE INDEX IF NOT EXISTS serial_chapters_url ON serial_chapters(serial_id, url);
`

func init() { schemaExtras = append(schemaExtras, serialsSchema) }

// Serial is a web serial, forum story or Reddit series read in W5F.
type Serial struct {
	ID                       int64
	Kind, URL, Title, Author string
	Summary                  string
	Followed                 bool
	Checked, Opened          time.Time
	CheckError               string
	Chapter                  int
	Pos                      float64
	Seen, Chapters           int // Chapters is the number of known chapters
}

// SerialChapter is one chapter of a serial, in reading order.
type SerialChapter struct {
	N         int
	Title     string
	URL       string
	Published time.Time
}

const serialCols = `id,kind,url,title,author,summary,followed,checked,check_error,opened,chapter,pos,seen,
  (SELECT count(*) FROM serial_chapters c WHERE c.serial_id=serials.id)`

func scanSerial(s scanner) (Serial, error) {
	var x Serial
	var followed int
	var checked, opened int64
	err := s.Scan(&x.ID, &x.Kind, &x.URL, &x.Title, &x.Author, &x.Summary, &followed, &checked, &x.CheckError,
		&opened, &x.Chapter, &x.Pos, &x.Seen, &x.Chapters)
	x.Followed, x.Checked, x.Opened = followed != 0, unix(checked), unix(opened)
	return x, err
}

// UpsertSerial adds a serial or refreshes its metadata by URL, keeping the
// follow state, progress and seen count.
func (db *DB) UpsertSerial(s Serial) (int64, error) {
	_, err := db.sql.Exec(`INSERT INTO serials(kind,url,title,author,summary,added) VALUES(?,?,?,?,?,?)
	  ON CONFLICT(url) DO UPDATE SET kind=excluded.kind,
	  title=CASE WHEN excluded.title='' THEN serials.title ELSE excluded.title END,
	  author=CASE WHEN excluded.author='' THEN serials.author ELSE excluded.author END,
	  summary=CASE WHEN excluded.summary='' THEN serials.summary ELSE excluded.summary END`,
		s.Kind, s.URL, s.Title, s.Author, s.Summary, time.Now().Unix())
	if err != nil {
		return 0, err
	}
	var id int64
	err = db.sql.QueryRow(`SELECT id FROM serials WHERE url=?`, s.URL).Scan(&id)
	return id, err
}

// Serial returns one serial.
func (db *DB) Serial(id int64) (Serial, error) {
	s, err := scanSerial(db.sql.QueryRow(`SELECT `+serialCols+` FROM serials WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return s, errors.New("serial not found")
	}
	return s, err
}

// SerialByURL finds a serial by its canonical address.
func (db *DB) SerialByURL(u string) (Serial, bool) {
	s, err := scanSerial(db.sql.QueryRow(`SELECT `+serialCols+` FROM serials WHERE url=?`, u))
	return s, err == nil
}

// Serials lists serials, most recently opened (then added) first.
func (db *DB) Serials(followedOnly bool) ([]Serial, error) {
	q := `SELECT ` + serialCols + ` FROM serials`
	if followedOnly {
		q += ` WHERE followed=1`
	}
	rows, err := db.sql.Query(q + ` ORDER BY opened DESC, added DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Serial
	for rows.Next() {
		s, err := scanSerial(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// MergeChapters brings the chapter list up to date: known URLs get their
// title and date refreshed, new URLs are appended in the given order, and
// chapters no longer listed are kept (so progress survives). It returns the
// number of chapters appended; an empty list is refused.
func (db *DB) MergeChapters(id int64, chs []SerialChapter) (int, error) {
	if len(chs) == 0 {
		return 0, errors.New("no chapters found")
	}
	tx, err := db.sql.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	known := map[string]bool{}
	next := 0
	rows, err := tx.Query(`SELECT n,url FROM serial_chapters WHERE serial_id=?`, id)
	if err != nil {
		return 0, err
	}
	for rows.Next() {
		var n int
		var u string
		if rows.Scan(&n, &u) == nil {
			known[u] = true
			next = max(next, n+1)
		}
	}
	rows.Close()
	added := 0
	for _, c := range chs {
		if c.URL == "" {
			continue
		}
		if known[c.URL] {
			if _, err := tx.Exec(`UPDATE serial_chapters SET title=CASE WHEN ?='' THEN title ELSE ? END,
			  published=CASE WHEN ?=0 THEN published ELSE ? END WHERE serial_id=? AND url=?`,
				c.Title, c.Title, c.Published.Unix(), c.Published.Unix(), id, c.URL); err != nil {
				return 0, err
			}
			continue
		}
		var pub int64
		if !c.Published.IsZero() {
			pub = c.Published.Unix()
		}
		if _, err := tx.Exec(`INSERT INTO serial_chapters(serial_id,n,title,url,published) VALUES(?,?,?,?,?)`,
			id, next, c.Title, c.URL, pub); err != nil {
			return 0, err
		}
		known[c.URL] = true
		next++
		added++
	}
	return added, tx.Commit()
}

// SerialChapters lists a serial's chapters in reading order.
func (db *DB) SerialChapters(id int64) ([]SerialChapter, error) {
	rows, err := db.sql.Query(`SELECT n,title,url,published FROM serial_chapters WHERE serial_id=? ORDER BY n`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SerialChapter
	for rows.Next() {
		var c SerialChapter
		var pub int64
		if err := rows.Scan(&c.N, &c.Title, &c.URL, &pub); err != nil {
			return nil, err
		}
		c.Published = unix(pub)
		out = append(out, c)
	}
	return out, rows.Err()
}

// SerialByChapterURL finds the serial and chapter number of a chapter address.
func (db *DB) SerialByChapterURL(u string) (id int64, n int, ok bool) {
	err := db.sql.QueryRow(`SELECT serial_id,n FROM serial_chapters WHERE url=? LIMIT 1`, u).Scan(&id, &n)
	return id, n, err == nil
}

// SetFollowed turns following on or off.
func (db *DB) SetFollowed(id int64, on bool) error {
	v := 0
	if on {
		v = 1
	}
	_, err := db.sql.Exec(`UPDATE serials SET followed=? WHERE id=?`, v, id)
	return err
}

// MarkSeen records that every known chapter has been seen.
func (db *DB) MarkSeen(id int64) error {
	_, err := db.sql.Exec(`UPDATE serials SET seen=(SELECT count(*) FROM serial_chapters WHERE serial_id=?) WHERE id=?`, id, id)
	return err
}

// SaveSerialProgress records where a serial was left.
func (db *DB) SaveSerialProgress(id int64, chapter int, pos float64) error {
	_, err := db.sql.Exec(`UPDATE serials SET chapter=?, pos=?, opened=? WHERE id=?`, chapter, pos, time.Now().Unix(), id)
	return err
}

// SetChecked records an update check and its error ("" = fine).
func (db *DB) SetChecked(id int64, errText string) error {
	_, err := db.sql.Exec(`UPDATE serials SET checked=?, check_error=? WHERE id=?`, time.Now().Unix(), errText, id)
	return err
}

// NewChapterTotal counts chapters of followed serials not seen yet.
func (db *DB) NewChapterTotal() (int, error) {
	var n sql.NullInt64
	err := db.sql.QueryRow(`SELECT sum(max(0, (SELECT count(*) FROM serial_chapters c WHERE c.serial_id=serials.id) - seen))
	  FROM serials WHERE followed=1`).Scan(&n)
	return int(n.Int64), err
}
