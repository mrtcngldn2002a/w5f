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
