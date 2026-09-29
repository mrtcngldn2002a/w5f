package reddit

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// The Reddit session is the user's own reddit_session cookie, pasted in by the
// user via `w5f reddit-login`. It is stored only on this machine, readable by
// the user alone, and sent only to old.reddit.com.

// SessionPath is where the cookie is kept.
func SessionPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "w5f", "reddit-session")
}

// LoadSession returns the stored cookie value, or "".
func LoadSession() string {
	if v := strings.TrimSpace(os.Getenv("W5F_REDDIT_SESSION")); v != "" {
		return cleanCookie(v)
	}
	p := SessionPath()
	if p == "" {
		return ""
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return cleanCookie(string(b))
}

// SaveSession stores the cookie value with owner-only permissions.
func SaveSession(v string) error {
	v = cleanCookie(v)
	if v == "" {
		return errors.New("empty session value")
	}
	p := SessionPath()
	if p == "" {
		return errors.New("no config directory")
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(v+"\n"), 0o600)
}

// DeleteSession removes the stored cookie (log out).
func DeleteSession() error {
	p := SessionPath()
	if p == "" {
		return nil
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// cleanCookie accepts either the bare value or "reddit_session=value; …".
func cleanCookie(v string) string {
	v = strings.TrimSpace(v)
	for _, part := range strings.Split(v, ";") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "reddit_session=") {
			return strings.TrimPrefix(part, "reddit_session=")
		}
	}
	if strings.ContainsAny(v, " \t\r\n;=") {
		return ""
	}
	return v
}
