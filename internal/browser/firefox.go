package browser

import (
	"bufio"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Firefox keeps its cookies unencrypted (cookies.sqlite) on every system,
// so a session signed in there can be taken on Windows and macOS too, where
// Chromium's cookies are locked (chosen with the owner, 2026-10-02).

// firefoxRoots are the folders that hold Firefox's profiles.ini.
func firefoxRoots() []string {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "windows":
		return []string{filepath.Join(os.Getenv("APPDATA"), "Mozilla", "Firefox")}
	case "darwin":
		return []string{filepath.Join(home, "Library", "Application Support", "Firefox")}
	}
	cfg := os.Getenv("XDG_CONFIG_HOME")
	if cfg == "" {
		cfg = filepath.Join(home, ".config")
	}
	return []string{
		filepath.Join(home, ".mozilla", "firefox"),
		filepath.Join(cfg, "mozilla", "firefox"),
		filepath.Join(home, "snap", "firefox", "common", ".mozilla", "firefox"),
		filepath.Join(home, ".var", "app", "org.mozilla.firefox", ".mozilla", "firefox"),
	}
}

// FirefoxProfile is the profile cookies are read from (W5F_FIREFOX_PROFILE
// overrides): the default profile of the first Firefox found, else "".
func FirefoxProfile() string {
	if p := os.Getenv("W5F_FIREFOX_PROFILE"); p != "" {
		return p
	}
	for _, root := range firefoxRoots() {
		if p := profileIn(root); p != "" {
			return p
		}
	}
	return ""
}

// profileIn reads root/profiles.ini: the profile an installation uses
// ([Install…] Default=), else the one marked Default=1, else the first.
func profileIn(root string) string {
	fh, err := os.Open(filepath.Join(root, "profiles.ini"))
	if err != nil {
		return ""
	}
	defer fh.Close()
	type section struct {
		name string
		kv   map[string]string
	}
	var secs []section
	sc := bufio.NewScanner(fh)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]"):
			secs = append(secs, section{line[1 : len(line)-1], map[string]string{}})
		case len(secs) > 0:
			if k, v, ok := strings.Cut(line, "="); ok {
				secs[len(secs)-1].kv[strings.TrimSpace(k)] = strings.TrimSpace(v)
			}
		}
	}
	resolve := func(path string, relative bool) string {
		if path == "" {
			return ""
		}
		if relative {
			path = filepath.Join(root, filepath.FromSlash(path))
		}
		if fileExists(filepath.Join(path, "cookies.sqlite")) {
			return path
		}
		return ""
	}
	for _, s := range secs {
		if strings.HasPrefix(s.name, "Install") {
			if p := resolve(s.kv["Default"], true); p != "" {
				return p
			}
		}
	}
	var first string
	for _, s := range secs {
		if !strings.HasPrefix(s.name, "Profile") {
			continue
		}
		p := resolve(s.kv["Path"], s.kv["IsRelative"] != "0")
		if p == "" {
			continue
		}
		if s.kv["Default"] == "1" {
			return p
		}
		if first == "" {
			first = p
		}
	}
	return first
}

// clock is the time cookies expire by (fixed in tests).
var clock = time.Now

// FirefoxCookies reads the named cookies a site set in a Firefox profile
// (the domain and its subdomains), most recently used first; expired ones
// are skipped. Firefox may be running: a copy of the database (and of its
// write-ahead log, which holds the newest cookies) is read.
func FirefoxCookies(profile, domain string, names []string) (map[string]string, error) {
	if profile == "" || !fileExists(filepath.Join(profile, "cookies.sqlite")) {
		return nil, errors.New("no Firefox profile found (open Firefox once and sign in)")
	}
	tmp, err := os.MkdirTemp("", "w5f-cookies-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	dst := filepath.Join(tmp, "cookies.sqlite")
	if err := copyFile(filepath.Join(profile, "cookies.sqlite"), dst); err != nil {
		return nil, err
	}
	if wal := filepath.Join(profile, "cookies.sqlite-wal"); fileExists(wal) {
		if err := copyFile(wal, dst+"-wal"); err != nil {
			return nil, err
		}
	}
	return readFirefoxCookies(dst, domain, names, clock())
}

func readFirefoxCookies(path, domain string, names []string, now time.Time) (map[string]string, error) {
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path))
	if err != nil {
		return nil, err
	}
	defer db.Close()
	domain = strings.TrimPrefix(strings.ToLower(domain), ".")
	args := []any{domain, "." + domain, "%." + domain}
	ph := make([]string, len(names))
	for i, n := range names {
		ph[i] = "?"
		args = append(args, n)
	}
	rows, err := db.Query(`SELECT name, value, expiry FROM moz_cookies
		WHERE (host = ? OR host = ? OR host LIKE ?) AND name IN (`+strings.Join(ph, ",")+`)
		ORDER BY lastAccessed DESC`, args...)
	if err != nil {
		return nil, fmt.Errorf("reading Firefox's cookies: %v", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var name, value string
		var expiry int64
		if err := rows.Scan(&name, &value, &expiry); err != nil {
			return nil, err
		}
		// Seconds since 1970; newer Firefox versions write milliseconds.
		exp := time.Unix(expiry, 0)
		if expiry > 1e11 {
			exp = time.UnixMilli(expiry)
		}
		if _, done := out[name]; done || value == "" || (expiry > 0 && exp.Before(now)) {
			continue
		}
		out[name] = value
	}
	if len(out) == 0 {
		return nil, ErrNoCookie
	}
	return out, rows.Err()
}

// SessionCookies takes a site's cookies from the browser the owner signed
// in with: "chromium" (Linux), "firefox", or "" for the first that has
// them, Chromium first (the laptop's way). It says which one had them.
func SessionCookies(from, domain string, names []string) (map[string]string, string, error) {
	var tried []string
	var firstErr error
	try := func(name string, read func() (map[string]string, error)) map[string]string {
		c, err := read()
		if err == nil {
			return c
		}
		tried = append(tried, name)
		if firstErr == nil || errors.Is(firstErr, ErrNoCookie) {
			firstErr = err
		}
		return nil
	}
	if from == "" || from == "chromium" {
		if runtime.GOOS == "linux" || from == "chromium" {
			if c := try("Chromium", func() (map[string]string, error) { return Cookies(ProfileDir(), domain, names) }); c != nil {
				return c, "Chromium", nil
			}
		}
	}
	if from == "" || from == "firefox" {
		if c := try("Firefox", func() (map[string]string, error) { return FirefoxCookies(FirefoxProfile(), domain, names) }); c != nil {
			return c, "Firefox", nil
		}
	}
	if from != "" && from != "chromium" && from != "firefox" {
		return nil, "", fmt.Errorf("sessions are taken from chromium or firefox, not %q", from)
	}
	if len(tried) > 1 || errors.Is(firstErr, ErrNoCookie) {
		return nil, "", fmt.Errorf("%w (looked in %s)", ErrNoCookie, strings.Join(tried, " and "))
	}
	return nil, "", firstErr
}
