package browser

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// encrypt does what Chromium does on Linux without a keyring (v10), with
// the host's hash in front from database version 24 on.
func encrypt(value, host string, version int) []byte {
	return encryptWith(linuxKey(), value, host, version)
}

// encryptWith is the same with another key (a Mac's, from its keychain).
func encryptWith(key []byte, value, host string, version int) []byte {
	plain := []byte(value)
	if version >= 24 {
		h := sha256.Sum256([]byte(host))
		plain = append(h[:], plain...)
	}
	pad := aes.BlockSize - len(plain)%aes.BlockSize
	plain = append(plain, bytes.Repeat([]byte{byte(pad)}, pad)...)
	block, _ := aes.NewCipher(key)
	out := make([]byte, len(plain))
	cipher.NewCBCEncrypter(block, bytes.Repeat([]byte{' '}, aes.BlockSize)).CryptBlocks(out, plain)
	return append([]byte("v10"), out...)
}

func cookieDB(t *testing.T, version int) string {
	t.Helper()
	return cookieDBWith(t, version, linuxKey())
}

func cookieDBWith(t *testing.T, version int, key []byte) string {
	t.Helper()
	encrypt := func(value, host string, version int) []byte { return encryptWith(key, value, host, version) }
	p := filepath.Join(t.TempDir(), "Cookies")
	db, err := sql.Open("sqlite", p)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, q := range []string{
		`CREATE TABLE meta (key TEXT, value TEXT)`,
		`CREATE TABLE cookies (host_key TEXT, name TEXT, value TEXT, encrypted_value BLOB, expires_utc INTEGER, has_expires INTEGER, last_access_utc INTEGER)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	db.Exec(`INSERT INTO meta VALUES ('version', ?)`, version)
	future, past := chromeTime(time.Now().Add(24*time.Hour)), chromeTime(time.Now().Add(-time.Hour))
	add := func(host, name, value string, enc []byte, expires int64, access int64) {
		if _, err := db.Exec(`INSERT INTO cookies VALUES (?,?,?,?,?,1,?)`, host, name, value, enc, expires, access); err != nil {
			t.Fatal(err)
		}
	}
	add(".reddit.com", "reddit_session", "", encrypt("NEWER-SESSION", ".reddit.com", version), future, 20)
	add("www.reddit.com", "reddit_session", "", encrypt("OLDER-SESSION", "www.reddit.com", version), future, 10)
	add(".reddit.com", "token_v2", "", encrypt("not asked for", ".reddit.com", version), future, 30)
	add("archiveofourown.org", "_otwarchive_session", "", encrypt("AO3-SESSION", "archiveofourown.org", version), future, 5)
	add("archiveofourown.org", "remember_user_token", "", encrypt("EXPIRED", "archiveofourown.org", version), past, 6)
	add(".notreddit.com", "reddit_session", "", encrypt("WRONG-SITE", ".notreddit.com", version), future, 40)
	add(".plain.example", "sid", "PLAIN", nil, future, 1)
	return p
}

func linux() ([]byte, error) { return linuxKey(), nil }

func TestChromiumCookies(t *testing.T) {
	for _, version := range []int{23, 24} {
		p := cookieDB(t, version)
		got, err := readCookies(p, "reddit.com", []string{"reddit_session"}, linux)
		if err != nil || len(got) != 1 || got["reddit_session"] != "NEWER-SESSION" {
			t.Errorf("v%d reddit: %v %v", version, got, err)
		}
		got, err = readCookies(p, "archiveofourown.org", []string{"_otwarchive_session", "remember_user_token"}, linux)
		if err != nil || len(got) != 1 || got["_otwarchive_session"] != "AO3-SESSION" {
			t.Errorf("v%d ao3 (the expired token left out): %v %v", version, got, err)
		}
		if got, _ := readCookies(p, "plain.example", []string{"sid"}, linux); got["sid"] != "PLAIN" {
			t.Errorf("an unencrypted cookie: %v", got)
		}
		if _, err := readCookies(p, "example.org", []string{"sid"}, linux); !errors.Is(err, ErrNoCookie) {
			t.Errorf("no cookie: %v", err)
		}
	}
	// A keyring-kept key (v11) is refused with a clear message.
	p := cookieDB(t, 24)
	db, _ := sql.Open("sqlite", p)
	db.Exec(`UPDATE cookies SET encrypted_value = ? WHERE name = '_otwarchive_session'`, append([]byte("v11"), make([]byte, 32)...))
	db.Close()
	if _, err := readCookies(p, "archiveofourown.org", []string{"_otwarchive_session"}, linux); err == nil || !strings.Contains(err.Error(), "keyring") {
		t.Errorf("v11: %v", err)
	}
}

// On a Mac the key is the keychain's password stretched 1003 times; it is
// asked for once, and only when an encrypted cookie is wanted.
func TestMacChromeCookies(t *testing.T) {
	key := macKey("Q2hyb21lIFNhZmUgU3RvcmFnZQ==")
	if bytes.Equal(key, linuxKey()) || len(key) != 16 {
		t.Fatalf("mac key: %x", key)
	}
	asked := 0
	mac := func() ([]byte, error) { asked++; return key, nil }
	p := cookieDBWith(t, 24, key)
	if got, _ := readCookies(p, "plain.example", []string{"sid"}, mac); got["sid"] != "PLAIN" || asked != 0 {
		t.Errorf("an unencrypted cookie asked the keychain: %v, asked %d", got, asked)
	}
	got, err := readCookies(p, "reddit.com", []string{"reddit_session"}, mac)
	if err != nil || got["reddit_session"] != "NEWER-SESSION" || asked != 1 {
		t.Errorf("reddit: %v %v, asked %d", got, err, asked)
	}
	if _, err := readCookies(p, "reddit.com", []string{"reddit_session"}, linux); err == nil {
		t.Error("Linux's key opened a Mac's cookie")
	}
	denied := func() ([]byte, error) { return nil, errors.New("macOS did not give W5F the key") }
	if _, err := readCookies(p, "reddit.com", []string{"reddit_session"}, denied); err == nil || !strings.Contains(err.Error(), "did not give") {
		t.Errorf("a refused keychain: %v", err)
	}
}

// Which keychain item each Mac browser keeps its password in, and where
// newer Chromes keep the database (Network/Cookies).
func TestChromeProfiles(t *testing.T) {
	for profile, want := range map[string][3]string{
		"/Users/u/Library/Application Support/Google/Chrome/Default":               {"Chrome", "Chrome Safe Storage", "Chrome"},
		"/Users/u/Library/Application Support/Chromium/Default":                    {"Chromium", "Chromium Safe Storage", "Chromium"},
		"/Users/u/Library/Application Support/BraveSoftware/Brave-Browser/Default": {"Brave", "Brave Safe Storage", "Brave"},
		"/Users/u/Library/Application Support/Microsoft Edge/Default":              {"Edge", "Microsoft Edge Safe Storage", "Microsoft Edge"},
		"/home/u/.config/google-chrome/Default":                                    {"Chrome", "Chrome Safe Storage", "Chrome"},
	} {
		service, account := safeStorage(profile)
		if got := [3]string{ChromeName(profile), service, account}; got != want {
			t.Errorf("%s: %v, want %v", profile, got, want)
		}
	}
	dir := t.TempDir()
	if cookieFile(dir) != "" {
		t.Error("an empty profile has cookies")
	}
	os.WriteFile(filepath.Join(dir, "Cookies"), nil, 0o600)
	if cookieFile(dir) != filepath.Join(dir, "Cookies") {
		t.Errorf("older Chrome: %q", cookieFile(dir))
	}
	os.MkdirAll(filepath.Join(dir, "Network"), 0o700)
	os.WriteFile(filepath.Join(dir, "Network", "Cookies"), nil, 0o600)
	if cookieFile(dir) != filepath.Join(dir, "Network", "Cookies") {
		t.Errorf("newer Chrome: %q", cookieFile(dir))
	}
}

func TestOpenTakesOnlyWebPages(t *testing.T) {
	for _, bad := range []string{"file:///etc/passwd", "w5f:usenet", "javascript:alert(1)", "-–no-sandbox", "https://"} {
		if err := Open(bad); err == nil || !strings.Contains(err.Error(), "only web pages") {
			t.Errorf("%q: %v", bad, err)
		}
	}
}
