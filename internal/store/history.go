package store

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

const historySchema = `
CREATE TABLE IF NOT EXISTS history (
  target  TEXT PRIMARY KEY,
  title   TEXT NOT NULL,
  kind    TEXT NOT NULL,
  catalog TEXT NOT NULL DEFAULT '',
  first   INTEGER NOT NULL,
  last    INTEGER NOT NULL,
  opens   INTEGER NOT NULL DEFAULT 1,
  pos     REAL NOT NULL DEFAULT 0
);`

func init() { schemaExtras = append(schemaExtras, historySchema) }

// Visit is one entry of the reading history.
type Visit struct {
	Target, Title, Kind, Catalog string
	First, Last                  time.Time
	Opens                        int
	Pos                          float64 // scroll position 0–1 when last left
}

// Visit records that a content page was opened. Each visit is also
// appended to history.log next to the database, so the reading history
// survives a deleted or corrupt database (RestoreHistory).
func (db *DB) Visit(target, title, kind, catalog string) error {
	now := stamp()
	if err := db.visitAt(target, title, kind, catalog, now); err != nil {
		return err
	}
	return db.logVisit(logEntry{Target: target, Title: title, Kind: kind, Catalog: catalog, At: now})
}

func (db *DB) visitAt(target, title, kind, catalog string, at int64) error {
	_, err := db.sql.Exec(`INSERT INTO history(target,title,kind,catalog,first,last,opens) VALUES(?,?,?,?,?,?,1)
	  ON CONFLICT(target) DO UPDATE SET title=excluded.title, kind=excluded.kind, catalog=excluded.catalog,
	    last=excluded.last, opens=opens+1`, target, title, kind, catalog, at, at)
	return err
}

// SavePos remembers how far a page was read.
func (db *DB) SavePos(target string, pos float64) error {
	_, err := db.sql.Exec(`UPDATE history SET pos=? WHERE target=?`, pos, target)
	return err
}

// Pos is how far a page was read when last left (0 when unknown).
func (db *DB) Pos(target string) float64 {
	var p float64
	_ = db.sql.QueryRow(`SELECT pos FROM history WHERE target=?`, target).Scan(&p)
	return p
}

type logEntry struct {
	Target  string `json:"t"`
	Title   string `json:"ti"`
	Kind    string `json:"k"`
	Catalog string `json:"c,omitempty"`
	At      int64  `json:"at"`
}

var logMu sync.Mutex

func (db *DB) logPath() string {
	if db.path == "" || db.path == ":memory:" {
		return ""
	}
	return filepath.Join(filepath.Dir(db.path), "history.log")
}

func (db *DB) logVisit(e logEntry) error {
	p := db.logPath()
	if p == "" {
		return nil
	}
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	logMu.Lock()
	defer logMu.Unlock()
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// RestoreHistory refills an empty history table from history.log (after the
// database was deleted); it returns the number of pages restored.
func (db *DB) RestoreHistory() (int, error) {
	var n int
	if err := db.sql.QueryRow(`SELECT count(*) FROM history`).Scan(&n); err != nil || n > 0 {
		return 0, err
	}
	p := db.logPath()
	if p == "" {
		return 0, nil
	}
	f, err := os.Open(p)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		var e logEntry
		if json.Unmarshal(sc.Bytes(), &e) != nil || e.Target == "" {
			continue // a torn last line after a crash
		}
		if err := db.visitAt(e.Target, e.Title, e.Kind, e.Catalog, e.At); err != nil {
			return 0, err
		}
	}
	if err := sc.Err(); err != nil {
		return 0, err
	}
	err = db.sql.QueryRow(`SELECT count(*) FROM history`).Scan(&n)
	return n, err
}

// History lists visits, newest first; limit ≤ 0 lists all.
func (db *DB) History(limit, offset int) ([]Visit, error) {
	if limit <= 0 {
		limit = -1
	}
	return db.visits(`SELECT target,title,kind,catalog,first,last,opens,pos FROM history
	  ORDER BY last DESC, rowid DESC LIMIT ? OFFSET ?`, limit, offset)
}

// Unfinished lists pages left part-way (books are tracked by the library).
func (db *DB) Unfinished(limit int) ([]Visit, error) {
	return db.visits(`SELECT target,title,kind,catalog,first,last,opens,pos FROM history
	  WHERE pos > 0.05 AND pos < 0.95 AND kind <> 'book' ORDER BY last DESC, rowid DESC LIMIT ?`, limit)
}

func (db *DB) visits(q string, args ...any) ([]Visit, error) {
	rows, err := db.sql.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Visit
	for rows.Next() {
		var v Visit
		var first, last int64
		if err := rows.Scan(&v.Target, &v.Title, &v.Kind, &v.Catalog, &first, &last, &v.Opens, &v.Pos); err != nil {
			return nil, err
		}
		v.First, v.Last = time.Unix(0, first), time.Unix(0, last)
		out = append(out, v)
	}
	return out, rows.Err()
}

var lastStamp atomic.Int64

// stamp is a strictly increasing nanosecond time: the Windows clock can
// return the same value for visits in quick succession.
func stamp() int64 {
	for {
		now, prev := time.Now().UnixNano(), lastStamp.Load()
		if now <= prev {
			now = prev + 1
		}
		if lastStamp.CompareAndSwap(prev, now) {
			return now
		}
	}
}
