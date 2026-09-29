// Package store keeps W5F's local state in SQLite (pure Go, no CGO): feeds,
// feed items and their read/starred state. User-authored data (notes,
// clippings) will live in plain files; this database can always be rebuilt
// from sources and the HTTP cache.
package store

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// DB wraps the SQLite handle.
type DB struct {
	sql  *sql.DB
	path string // database file (":memory:" in some tests)
}

var (
	mu      sync.Mutex
	shared  *DB
	openErr error
)

// DataDir returns W5F's data directory (XDG on Linux).
func DataDir() string {
	if runtime.GOOS == "linux" || runtime.GOOS == "freebsd" || runtime.GOOS == "openbsd" {
		if x := os.Getenv("XDG_DATA_HOME"); x != "" {
			return filepath.Join(x, "w5f")
		}
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, ".local", "share", "w5f")
		}
	}
	if d, err := os.UserConfigDir(); err == nil {
		return filepath.Join(d, "w5f")
	}
	return "w5f-data"
}

// Default opens (once) the database in the data directory.
func Default() (*DB, error) {
	mu.Lock()
	defer mu.Unlock()
	if shared != nil || openErr != nil {
		return shared, openErr
	}
	shared, openErr = Open(filepath.Join(DataDir(), "w5f.db"))
	return shared, openErr
}

// SetDefault replaces the shared database (tests).
func SetDefault(db *DB) {
	mu.Lock()
	defer mu.Unlock()
	shared, openErr = db, nil
}

const schema = `
CREATE TABLE IF NOT EXISTS feeds (
  id          TEXT PRIMARY KEY,
  url         TEXT NOT NULL DEFAULT '',  -- the address that worked last
  title       TEXT NOT NULL DEFAULT '',
  last_sync   INTEGER NOT NULL DEFAULT 0,
  last_ok     INTEGER NOT NULL DEFAULT 0,
  last_error  TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS items (
  id        INTEGER PRIMARY KEY AUTOINCREMENT,
  feed_id   TEXT NOT NULL,
  guid      TEXT NOT NULL,
  url       TEXT NOT NULL DEFAULT '',
  title     TEXT NOT NULL DEFAULT '',
  author    TEXT NOT NULL DEFAULT '',
  published INTEGER NOT NULL DEFAULT 0,
  summary   TEXT NOT NULL DEFAULT '',
  content   TEXT NOT NULL DEFAULT '',
  added     INTEGER NOT NULL DEFAULT 0,
  read      INTEGER NOT NULL DEFAULT 0,
  starred   INTEGER NOT NULL DEFAULT 0,
  UNIQUE(feed_id, guid)
);
CREATE INDEX IF NOT EXISTS items_feed ON items(feed_id, published DESC);
CREATE INDEX IF NOT EXISTS items_unread ON items(read, published DESC);
CREATE TABLE IF NOT EXISTS kv (k TEXT PRIMARY KEY, v TEXT NOT NULL);
`

// schemaExtras are table definitions registered by other files of this package.
var schemaExtras []string

// Open opens or creates a database file (":memory:" for tests).
func Open(path string) (*DB, error) {
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
	}
	dsn := path
	if path != ":memory:" {
		dsn = "file:" + filepath.ToSlash(path) + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	}
	s, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	s.SetMaxOpenConns(1) // one writer; keeps W5F's footprint small
	for _, sc := range append([]string{schema}, schemaExtras...) {
		if _, err := s.Exec(sc); err != nil {
			s.Close()
			return nil, err
		}
	}
	return &DB{sql: s, path: path}, nil
}

// Close closes the database.
func (db *DB) Close() error { return db.sql.Close() }

// FeedState is the sync status of one feed.
type FeedState struct {
	ID       string
	URL      string
	Title    string
	LastSync time.Time
	LastOK   time.Time
	Error    string
}

// Feed returns the stored state of a feed (zero value if unknown).
func (db *DB) Feed(id string) FeedState {
	var fs FeedState
	var ls, lo int64
	err := db.sql.QueryRow(`SELECT id,url,title,last_sync,last_ok,last_error FROM feeds WHERE id=?`, id).
		Scan(&fs.ID, &fs.URL, &fs.Title, &ls, &lo, &fs.Error)
	if err != nil {
		return FeedState{ID: id}
	}
	fs.LastSync, fs.LastOK = unix(ls), unix(lo)
	return fs
}

// SetFeed records a sync attempt.
func (db *DB) SetFeed(fs FeedState) error {
	_, err := db.sql.Exec(`INSERT INTO feeds(id,url,title,last_sync,last_ok,last_error) VALUES(?,?,?,?,?,?)
	  ON CONFLICT(id) DO UPDATE SET url=excluded.url, title=CASE WHEN excluded.title='' THEN feeds.title ELSE excluded.title END,
	  last_sync=excluded.last_sync, last_ok=CASE WHEN excluded.last_ok=0 THEN feeds.last_ok ELSE excluded.last_ok END,
	  last_error=excluded.last_error`,
		fs.ID, fs.URL, fs.Title, fs.LastSync.Unix(), zeroUnix(fs.LastOK), fs.Error)
	return err
}

// Item is one feed entry.
type Item struct {
	ID        int64
	FeedID    string
	GUID      string
	URL       string
	Title     string
	Author    string
	Published time.Time
	Summary   string
	Content   string
	Read      bool
	Starred   bool
}

// UpsertItem inserts a new item or refreshes its text; read/starred state is
// kept. It reports whether the item was new.
func (db *DB) UpsertItem(it Item) (bool, error) {
	res, err := db.sql.Exec(`INSERT INTO items(feed_id,guid,url,title,author,published,summary,content,added,read)
	  VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(feed_id,guid) DO NOTHING`,
		it.FeedID, it.GUID, it.URL, it.Title, it.Author, zeroUnix(it.Published), it.Summary, it.Content, time.Now().Unix(), b2i(it.Read))
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return true, nil
	}
	_, err = db.sql.Exec(`UPDATE items SET url=?, title=?, author=?, summary=?, content=CASE WHEN length(?)>length(content) THEN ? ELSE content END
	  WHERE feed_id=? AND guid=?`, it.URL, it.Title, it.Author, it.Summary, it.Content, it.Content, it.FeedID, it.GUID)
	return false, err
}

// Prune keeps at most keep items per feed, never deleting starred or unread
// ones younger than a month.
func (db *DB) Prune(feedID string, keep int) error {
	_, err := db.sql.Exec(`DELETE FROM items WHERE feed_id=? AND starred=0 AND id NOT IN (
	    SELECT id FROM items WHERE feed_id=? ORDER BY published DESC, id DESC LIMIT ?)
	  AND (read=1 OR added < ?)`, feedID, feedID, keep, time.Now().AddDate(0, -1, 0).Unix())
	return err
}

// Query selects items.
type Query struct {
	Feeds   []string // empty = all
	Unread  bool
	Starred bool
	Limit   int
	Offset  int
}

// Items returns items matching q, newest first.
func (db *DB) Items(q Query) ([]Item, error) {
	var where []string
	var args []any
	if len(q.Feeds) > 0 {
		where = append(where, "feed_id IN ("+strings.TrimSuffix(strings.Repeat("?,", len(q.Feeds)), ",")+")")
		for _, f := range q.Feeds {
			args = append(args, f)
		}
	}
	if q.Unread {
		where = append(where, "read=0")
	}
	if q.Starred {
		where = append(where, "starred=1")
	}
	sqlq := `SELECT id,feed_id,guid,url,title,author,published,summary,'',read,starred FROM items`
	if len(where) > 0 {
		sqlq += " WHERE " + strings.Join(where, " AND ")
	}
	if q.Limit <= 0 {
		q.Limit = 100
	}
	sqlq += " ORDER BY published DESC, id DESC LIMIT ? OFFSET ?"
	args = append(args, q.Limit, q.Offset)
	rows, err := db.sql.Query(sqlq, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Item
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// Item returns one item with its full content.
func (db *DB) Item(id int64) (Item, error) {
	row := db.sql.QueryRow(`SELECT id,feed_id,guid,url,title,author,published,summary,content,read,starred FROM items WHERE id=?`, id)
	it, err := scanItem(row)
	if errors.Is(err, sql.ErrNoRows) {
		return it, errors.New("item not found")
	}
	return it, err
}

// Counts returns unread and total item counts per feed.
func (db *DB) Counts() (map[string][2]int, error) {
	rows, err := db.sql.Query(`SELECT feed_id, SUM(read=0), COUNT(*) FROM items GROUP BY feed_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][2]int{}
	for rows.Next() {
		var id string
		var unread, total int
		if err := rows.Scan(&id, &unread, &total); err != nil {
			return nil, err
		}
		out[id] = [2]int{unread, total}
	}
	return out, rows.Err()
}

// StarredCount returns the number of starred items.
func (db *DB) StarredCount() int {
	var n int
	_ = db.sql.QueryRow(`SELECT COUNT(*) FROM items WHERE starred=1`).Scan(&n)
	return n
}

// SetRead marks an item read or unread.
func (db *DB) SetRead(id int64, read bool) error {
	_, err := db.sql.Exec(`UPDATE items SET read=? WHERE id=?`, b2i(read), id)
	return err
}

// MarkAllRead marks every item of the given feeds read.
func (db *DB) MarkAllRead(feeds []string) error {
	if len(feeds) == 0 {
		return nil
	}
	args := make([]any, len(feeds))
	for i, f := range feeds {
		args[i] = f
	}
	_, err := db.sql.Exec(`UPDATE items SET read=1 WHERE feed_id IN (`+strings.TrimSuffix(strings.Repeat("?,", len(feeds)), ",")+`)`, args...)
	return err
}

// ToggleStar flips the starred flag and returns the new state.
func (db *DB) ToggleStar(id int64) (bool, error) {
	if _, err := db.sql.Exec(`UPDATE items SET starred=1-starred WHERE id=?`, id); err != nil {
		return false, err
	}
	var s int
	err := db.sql.QueryRow(`SELECT starred FROM items WHERE id=?`, id).Scan(&s)
	return s == 1, err
}

// Get/Set store small key-value settings (e.g. last sync time).
func (db *DB) Get(k string) string {
	var v string
	_ = db.sql.QueryRow(`SELECT v FROM kv WHERE k=?`, k).Scan(&v)
	return v
}

func (db *DB) Set(k, v string) error {
	_, err := db.sql.Exec(`INSERT INTO kv(k,v) VALUES(?,?) ON CONFLICT(k) DO UPDATE SET v=excluded.v`, k, v)
	return err
}

type scanner interface{ Scan(...any) error }

func scanItem(s scanner) (Item, error) {
	var it Item
	var pub int64
	var read, star int
	err := s.Scan(&it.ID, &it.FeedID, &it.GUID, &it.URL, &it.Title, &it.Author, &pub, &it.Summary, &it.Content, &read, &star)
	it.Published, it.Read, it.Starred = unix(pub), read == 1, star == 1
	return it, err
}

func unix(s int64) time.Time {
	if s == 0 {
		return time.Time{}
	}
	return time.Unix(s, 0)
}

func zeroUnix(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}
