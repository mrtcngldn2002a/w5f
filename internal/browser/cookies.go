package browser

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/sha1"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// ProfileDir is the Chromium profile cookies are read from (W5F_CHROMIUM_PROFILE
// overrides): Chromium's, else Chrome's, default profile.
func ProfileDir() string {
	if p := os.Getenv("W5F_CHROMIUM_PROFILE"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	for _, rel := range []string{".config/chromium/Default", ".config/google-chrome/Default"} {
		if p := filepath.Join(home, rel); fileExists(filepath.Join(p, "Cookies")) {
			return p
		}
	}
	return filepath.Join(home, ".config", "chromium", "Default")
}

// ErrNoCookie means Chromium has no such (unexpired) cookie: not signed in
// there, or Chromium has not written it to disk yet.
var ErrNoCookie = errors.New("no such cookie in Chromium")

// Cookies reads the named cookies a site set in Chromium (the domain and
// its subdomains), newest first wins; expired ones are skipped. Only what
// is asked for is decrypted; nothing is logged.
func Cookies(profile, domain string, names []string) (map[string]string, error) {
	if runtime.GOOS != "linux" {
		return nil, errors.New("taking cookies from Chromium works on Linux only: sign in with Firefox and use firefox (or browser) instead, or paste the cookie")
	}
	src := filepath.Join(profile, "Cookies")
	if !fileExists(src) {
		return nil, fmt.Errorf("no Chromium profile at %s (open Chromium once and sign in)", profile)
	}
	// Chromium keeps the database open: read a copy.
	tmp, err := os.MkdirTemp("", "w5f-cookies-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	if err := copyFile(src, filepath.Join(tmp, "Cookies")); err != nil {
		return nil, err
	}
	return readCookies(filepath.Join(tmp, "Cookies"), domain, names)
}

// readCookies reads a copy of Chromium's cookie database.
func readCookies(path, domain string, names []string) (map[string]string, error) {
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?mode=ro")
	if err != nil {
		return nil, err
	}
	defer db.Close()
	version := 0
	var v string
	if db.QueryRow(`SELECT value FROM meta WHERE key = 'version'`).Scan(&v) == nil {
		version, _ = strconv.Atoi(v)
	}
	domain = strings.TrimPrefix(strings.ToLower(domain), ".")
	args := []any{domain, "." + domain, "%." + domain}
	ph := make([]string, len(names))
	for i, n := range names {
		ph[i] = "?"
		args = append(args, n)
	}
	rows, err := db.Query(`SELECT host_key, name, value, encrypted_value, expires_utc, has_expires FROM cookies
		WHERE (host_key = ? OR host_key = ? OR host_key LIKE ?) AND name IN (`+strings.Join(ph, ",")+`)
		ORDER BY last_access_utc DESC`, args...)
	if err != nil {
		return nil, fmt.Errorf("reading Chromium's cookies: %v", err)
	}
	defer rows.Close()
	now := chromeTime(time.Now())
	out := map[string]string{}
	for rows.Next() {
		var host, name, plain string
		var enc []byte
		var expires int64
		var hasExpires int
		if err := rows.Scan(&host, &name, &plain, &enc, &expires, &hasExpires); err != nil {
			return nil, err
		}
		if _, done := out[name]; done || hasExpires == 1 && expires < now {
			continue
		}
		value := plain
		if len(enc) > 0 {
			if value, err = decrypt(enc, host, version); err != nil {
				return nil, err
			}
		}
		if value != "" {
			out[name] = value
		}
	}
	if len(out) == 0 {
		return nil, ErrNoCookie
	}
	return out, rows.Err()
}

// chromeTime is microseconds since 1601-01-01, Chromium's clock.
func chromeTime(t time.Time) int64 { return t.UnixMicro() + 11644473600*1_000_000 }

// linuxKey is what Chromium uses when no keyring holds its secret ("v10").
func linuxKey() []byte {
	k, _ := pbkdf2.Key(sha1.New, "peanuts", []byte("saltysalt"), 1, 16)
	return k
}

// decrypt opens a value Chromium encrypted on Linux. Databases from version
// 24 on put a SHA-256 of the host before the value.
func decrypt(enc []byte, host string, version int) (string, error) {
	switch {
	case bytes.HasPrefix(enc, []byte("v11")):
		return "", errors.New("Chromium keeps its cookie key in a keyring here, which W5F does not read; paste the cookie instead")
	case !bytes.HasPrefix(enc, []byte("v10")):
		return "", errors.New("this Chromium encrypts its cookies in a way W5F does not know; paste the cookie instead")
	}
	data := enc[3:]
	if len(data) == 0 || len(data)%aes.BlockSize != 0 {
		return "", errors.New("a damaged cookie in Chromium's database")
	}
	block, err := aes.NewCipher(linuxKey())
	if err != nil {
		return "", err
	}
	out := make([]byte, len(data))
	cipher.NewCBCDecrypter(block, bytes.Repeat([]byte{' '}, aes.BlockSize)).CryptBlocks(out, data)
	pad := int(out[len(out)-1])
	if pad < 1 || pad > aes.BlockSize || pad > len(out) {
		return "", errors.New("a cookie Chromium's key does not open")
	}
	out = out[:len(out)-pad]
	if version >= 24 {
		if len(out) < 32 {
			return "", errors.New("a damaged cookie in Chromium's database")
		}
		out = out[32:]
	}
	return string(out), nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
