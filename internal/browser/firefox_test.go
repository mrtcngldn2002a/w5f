package browser

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A Firefox cookie database as Firefox writes it (only the columns read).
func firefoxDB(t *testing.T, dir string, rows [][]any) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(filepath.Join(dir, "cookies.sqlite")))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE moz_cookies (id INTEGER PRIMARY KEY, name TEXT, value TEXT, host TEXT, path TEXT,
		expiry INTEGER, lastAccessed INTEGER, creationTime INTEGER)`); err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if _, err := db.Exec(`INSERT INTO moz_cookies(name, value, host, path, expiry, lastAccessed, creationTime) VALUES(?,?,?,'/',?,?,0)`, r...); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFirefoxProfilesIni(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{"Profiles/old.default", "Profiles/new.default-release"} {
		os.MkdirAll(filepath.Join(root, filepath.FromSlash(p)), 0o755)
		os.WriteFile(filepath.Join(root, filepath.FromSlash(p), "cookies.sqlite"), nil, 0o644)
	}
	ini := "[Profile1]\nName=old\nIsRelative=1\nPath=Profiles/old.default\nDefault=1\n\n" +
		"[Profile0]\nName=default-release\nIsRelative=1\nPath=Profiles/new.default-release\n\n" +
		"[Install4F96D1932A9F858E]\nDefault=Profiles/new.default-release\nLocked=1\n"
	os.WriteFile(filepath.Join(root, "profiles.ini"), []byte(ini), 0o644)
	// The installation's own default wins over the older Default=1.
	if p := profileIn(root); filepath.Base(p) != "new.default-release" {
		t.Errorf("profile: %q", p)
	}
	os.WriteFile(filepath.Join(root, "profiles.ini"), []byte(ini[:len(ini)-len("[Install4F96D1932A9F858E]\nDefault=Profiles/new.default-release\nLocked=1\n")]), 0o644)
	if p := profileIn(root); filepath.Base(p) != "old.default" {
		t.Errorf("Default=1: %q", p)
	}
	if profileIn(t.TempDir()) != "" {
		t.Error("no profiles.ini, no profile")
	}
}

func TestFirefoxCookies(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	firefoxDB(t, dir, [][]any{
		{"reddit_session", "old-value", ".reddit.com", now.Add(time.Hour).Unix(), 1},
		{"reddit_session", "new-value", ".reddit.com", now.Add(time.Hour).UnixMilli(), 2}, // newer Firefox: milliseconds
		{"_otwarchive_session", "gone", "archiveofourown.org", now.Add(-time.Hour).Unix(), 3},
		{"reddit_session", "not-reddit", ".notreddit.com", now.Add(time.Hour).Unix(), 4},
	})
	c, err := readFirefoxCookies(filepath.Join(dir, "cookies.sqlite"), "reddit.com", []string{"reddit_session"}, now)
	if err != nil || c["reddit_session"] != "new-value" {
		t.Errorf("reddit: %v %v", c, err)
	}
	if _, err := readFirefoxCookies(filepath.Join(dir, "cookies.sqlite"), "archiveofourown.org", []string{"_otwarchive_session"}, now); !errors.Is(err, ErrNoCookie) {
		t.Errorf("an expired cookie is no cookie: %v", err)
	}
	// Through a copy, as W5F reads it while Firefox runs.
	t.Setenv("W5F_FIREFOX_PROFILE", dir)
	if _, which, err := SessionCookies("firefox", "reddit.com", []string{"reddit_session"}); err != nil || which != "Firefox" {
		t.Errorf("session: %q %v", which, err)
	}
	if _, _, err := SessionCookies("opera", "reddit.com", []string{"reddit_session"}); err == nil {
		t.Error("an unknown browser")
	}
}
