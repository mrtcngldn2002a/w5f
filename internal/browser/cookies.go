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
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// ProfileDir is the Chromium profile cookies are read from (W5F_CHROMIUM_PROFILE
// overrides): on Linux Chromium's, else Chrome's, default profile; on a Mac
// Chrome's, else Chromium's, Brave's or Edge's.
func ProfileDir() string {
	if p := os.Getenv("W5F_CHROMIUM_PROFILE"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	rels := []string{".config/chromium/Default", ".config/google-chrome/Default"}
	if runtime.GOOS == "darwin" {
		rels = []string{"Library/Application Support/Google/Chrome/Default", "Library/Application Support/Chromium/Default",
			"Library/Application Support/BraveSoftware/Brave-Browser/Default", "Library/Application Support/Microsoft Edge/Default"}
	}
	for _, rel := range rels {
		if p := filepath.Join(home, filepath.FromSlash(rel)); cookieFile(p) != "" {
			return p
		}
	}
	return filepath.Join(home, filepath.FromSlash(rels[0]))
}

// cookieFile is a profile's cookie database: Network/Cookies in newer
// Chromes, Cookies beside the profile in older ones ("" when neither).
func cookieFile(profile string) string {
	for _, p := range []string{filepath.Join(profile, "Network", "Cookies"), filepath.Join(profile, "Cookies")} {
		if fileExists(p) {
			return p
		}
	}
	return ""
}

// ChromeName is the browser a Chromium profile belongs to, for messages.
func ChromeName(profile string) string {
	switch p := filepath.ToSlash(profile); {
	case strings.Contains(p, "BraveSoftware"):
		return "Brave"
	case strings.Contains(p, "Microsoft Edge"):
		return "Edge"
	case strings.Contains(p, "google-chrome"), strings.Contains(p, "Google/Chrome"):
		return "Chrome"
	}
	return "Chromium"
}

// safeStorage is the keychain item that holds a Mac Chromium's cookie
// password: its service and account.
func safeStorage(profile string) (service, account string) {
	switch ChromeName(profile) {
	case "Brave":
		return "Brave Safe Storage", "Brave"
	case "Edge":
		return "Microsoft Edge Safe Storage", "Microsoft Edge"
	case "Chromium":
		return "Chromium Safe Storage", "Chromium"
	}
	return "Chrome Safe Storage", "Chrome"
}

// keychainPassword asks macOS for a keychain item's password; macOS asks
// the owner first ("security wants to use your confidential information"),
// and Always Allow spares the question next time.
var keychainPassword = func(service, account string) (string, error) {
	out, err := exec.Command("/usr/bin/security", "find-generic-password", "-w", "-s", service, "-a", account).Output()
	if err != nil {
		return "", fmt.Errorf("macOS did not give W5F the key to the browser's cookies (%s: choose Allow when the keychain asks, or paste the cookie instead)", service)
	}
	return strings.TrimRight(string(out), "\r\n"), nil
}

// macKey is the key a Mac Chromium encrypts cookies with ("v10"): its
// keychain password stretched with the same salt as on Linux.
func macKey(password string) []byte {
	k, _ := pbkdf2.Key(sha1.New, password, []byte("saltysalt"), 1003, 16)
	return k
}

// ErrNoCookie means the browser has no such (unexpired) cookie: not signed
// in there, or the browser has not written it to disk yet.
var ErrNoCookie = errors.New("no such cookie")

// Cookies reads the named cookies a site set in Chromium (the domain and
// its subdomains), newest first wins; expired ones are skipped. Only what
// is asked for is decrypted; nothing is logged. On a Mac the key comes from
// the keychain, asked for only when a cookie needs it.
func Cookies(profile, domain string, names []string) (map[string]string, error) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		return nil, errors.New("cookies are taken from Chrome on Linux and macOS only (on Windows Chrome and Edge lock them): sign in with Firefox and use firefox (or browser) instead, or paste the cookie")
	}
	src := cookieFile(profile)
	if src == "" {
		return nil, fmt.Errorf("no %s profile at %s (open it once and sign in)", ChromeName(profile), profile)
	}
	key := func() ([]byte, error) { return linuxKey(), nil }
	if runtime.GOOS == "darwin" {
		key = func() ([]byte, error) {
			pw, err := keychainPassword(safeStorage(profile))
			if err != nil {
				return nil, err
			}
			return macKey(pw), nil
		}
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
	return readCookies(filepath.Join(tmp, "Cookies"), domain, names, key)
}

// readCookies reads a copy of Chromium's cookie database; key gives the
// key its values are encrypted with ("v10"), when one is first needed.
func readCookies(path, domain string, names []string, key func() ([]byte, error)) (map[string]string, error) {
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
	var k []byte
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
			if k == nil && bytes.HasPrefix(enc, []byte("v10")) {
				if k, err = key(); err != nil {
					return nil, err
				}
			}
			if value, err = decrypt(enc, host, version, k); err != nil {
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

// decrypt opens a value Chromium encrypted on Linux (without a keyring) or
// on a Mac, with key. Databases from version 24 on put a SHA-256 of the host
// before the value.
func decrypt(enc []byte, host string, version int, key []byte) (string, error) {
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
	block, err := aes.NewCipher(key)
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
