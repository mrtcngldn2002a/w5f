package store

import (
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// ClearHistory empties the reading history at the owner's word (2026-10-01).
// The log is not deleted but put aside beside the database under a dated
// name, so nothing read is lost for good; that name is returned ("" when
// there was no log). Book and serial progress live elsewhere and stay.
func (db *DB) ClearHistory(now time.Time) (string, error) {
	logMu.Lock()
	defer logMu.Unlock()
	kept := ""
	if p := db.logPath(); p != "" {
		if _, err := os.Stat(p); err == nil {
			kept = filepath.Join(filepath.Dir(p), "history-"+now.Format("20060102-150405")+".log")
			if err := os.Rename(p, kept); err != nil {
				return "", err
			}
		}
	}
	_, err := db.sql.Exec(`DELETE FROM history`)
	return kept, err
}

// SetAside takes a page off the desk until it is opened again (stamped
// like the visits, so an opening right after still counts as later).
func (db *DB) SetAside(target string) error {
	return db.Set("aside:"+target, strconv.FormatInt(stamp(), 10))
}

// Aside reports whether a page last opened at last has been set aside
// since.
func (db *DB) Aside(target string, last time.Time) bool {
	n, err := strconv.ParseInt(db.Get("aside:"+target), 10, 64)
	return err == nil && n >= last.UnixNano()
}

// DeleteDocs takes from the search index every document whose target
// starts with prefix (a removed book's chapters) or is it (a removed note).
func (db *DB) DeleteDocs(prefix string) error {
	tx, err := db.sql.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM docs_fts WHERE rowid IN (SELECT id FROM docs WHERE substr(target,1,?)=?)`, len(prefix), prefix); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM docs WHERE substr(target,1,?)=?`, len(prefix), prefix); err != nil {
		return err
	}
	return tx.Commit()
}

// ForgetBook takes a removed book out of the library.
func (db *DB) ForgetBook(id int64) error {
	_, err := db.sql.Exec(`DELETE FROM books WHERE id=?`, id)
	return err
}

// ForgetComic takes a removed comic out of the comics library.
func (db *DB) ForgetComic(id int64) error {
	_, err := db.sql.Exec(`DELETE FROM comics WHERE id=?`, id)
	return err
}
