package browser

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// encrypt does what Chromium does on Linux without a keyring (v10), with
// the host's hash in front from database version 24 on.
func encrypt(value, host string, version int) []byte {
	plain := []byte(value)
	if version >= 24 {
		h := sha256.Sum256([]byte(host))
		plain = append(h[:], plain...)
	}
	pad := aes.BlockSize - len(plain)%aes.BlockSize
	plain = append(plain, bytes.Repeat([]byte{byte(pad)}, pad)...)
	block, _ := aes.NewCipher(linuxKey())
	out := make([]byte, len(plain))
	cipher.NewCBCEncrypter(block, bytes.Repeat([]byte{' '}, aes.BlockSize)).CryptBlocks(out, plain)
	return append([]byte("v10"), out...)
}

func cookieDB(t *testing.T, version int) string {
	t.Helper()
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

func TestChromiumCookies(t *testing.T) {
	for _, version := range []int{23, 24} {
		p := cookieDB(t, version)
		got, err := readCookies(p, "reddit.com", []string{"reddit_session"})
		if err != nil || len(got) != 1 || got["reddit_session"] != "NEWER-SESSION" {
			t.Errorf("v%d reddit: %v %v", version, got, err)
		}
		got, err = readCookies(p, "archiveofourown.org", []string{"_otwarchive_session", "remember_user_token"})
		if err != nil || len(got) != 1 || got["_otwarchive_session"] != "AO3-SESSION" {
			t.Errorf("v%d ao3 (the expired token left out): %v %v", version, got, err)
		}
		if got, _ := readCookies(p, "plain.example", []string{"sid"}); got["sid"] != "PLAIN" {
			t.Errorf("an unencrypted cookie: %v", got)
		}
		if _, err := readCookies(p, "example.org", []string{"sid"}); !errors.Is(err, ErrNoCookie) {
			t.Errorf("no cookie: %v", err)
		}
	}
	// A keyring-kept key (v11) is refused with a clear message.
	p := cookieDB(t, 24)
	db, _ := sql.Open("sqlite", p)
	db.Exec(`UPDATE cookies SET encrypted_value = ? WHERE name = '_otwarchive_session'`, append([]byte("v11"), make([]byte, 32)...))
	db.Close()
	if _, err := readCookies(p, "archiveofourown.org", []string{"_otwarchive_session"}); err == nil || !strings.Contains(err.Error(), "keyring") {
		t.Errorf("v11: %v", err)
	}
}

func TestOpenTakesOnlyWebPages(t *testing.T) {
	for _, bad := range []string{"file:///etc/passwd", "w5f:usenet", "javascript:alert(1)", "-–no-sandbox", "https://"} {
		if err := Open(bad); err == nil || !strings.Contains(err.Error(), "only web pages") {
			t.Errorf("%q: %v", bad, err)
		}
	}
}
