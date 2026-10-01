package fiction

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"w5f/internal/fetch"
)

// The AO3 session is the owner's own AO3 login cookie, pasted in by the
// owner (w5f ao3-login or g → ao3-login). It is stored only on this machine,
// readable by the owner alone, and sent only to archiveofourown.org.

// ao3Hosts are the hosts the session may be sent to (a variable for tests).
var ao3Hosts = map[string]bool{"archiveofourown.org": true, "www.archiveofourown.org": true}

// ao3Cookies are the cookie names kept from a paste.
var ao3Cookies = []string{"_otwarchive_session", "remember_user_token"}

// AO3CookieNames are AO3's login cookies, in the order they are kept.
func AO3CookieNames() []string { return append([]string{}, ao3Cookies...) }

// AO3SessionPath is where the cookie is kept.
func AO3SessionPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "w5f", "ao3-session")
}

// cleanAO3Cookie keeps AO3's login cookies from a paste: a bare session
// value, or a Cookie header copied from the browser.
func cleanAO3Cookie(v string) string {
	v = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(v), "Cookie:"))
	if v == "" {
		return ""
	}
	if !strings.Contains(v, "=") {
		if strings.ContainsAny(v, " \t\r\n;") {
			return ""
		}
		return ao3Cookies[0] + "=" + v
	}
	var keep []string
	for _, name := range ao3Cookies {
		for _, part := range strings.Split(v, ";") {
			part = strings.TrimSpace(part)
			if strings.HasPrefix(part, name+"=") && len(part) > len(name)+1 {
				keep = append(keep, part)
				break
			}
		}
	}
	return strings.Join(keep, "; ")
}

// LoadAO3Session returns the stored cookies, or "".
func LoadAO3Session() string {
	if v := os.Getenv("W5F_AO3_SESSION"); strings.TrimSpace(v) != "" {
		return cleanAO3Cookie(v)
	}
	p := AO3SessionPath()
	if p == "" {
		return ""
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return cleanAO3Cookie(string(b))
}

// SaveAO3Session stores the cookies with owner-only permissions.
func SaveAO3Session(v string) error {
	v = cleanAO3Cookie(v)
	if v == "" {
		return errors.New("no AO3 session cookie found in what was pasted")
	}
	p := AO3SessionPath()
	if p == "" {
		return errors.New("no config directory")
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(v+"\n"), 0o600)
}

// DeleteAO3Session removes the stored cookies (log out).
func DeleteAO3Session() error {
	p := AO3SessionPath()
	if p == "" {
		return nil
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// errUnavailable: AO3 did not answer (Cloudflare 52x, 5xx, time-outs).
type errUnavailable struct{ detail string }

func (e errUnavailable) Error() string {
	return "AO3 isn't answering W5F right now (" + e.detail + "). AO3 often refuses readers that are not web browsers; " +
		"try again later, or download the work in a browser (Download → EPUB) — W5F imports AO3 books from your Downloads folder"
}

// ao3RetryWait is the pause before the one retry.
var ao3RetryWait = 3 * time.Second

// ao3Page fetches an AO3 page with the owner's session (when connected),
// retrying once when AO3 does not answer.
func ao3Page(ctx context.Context, f *fetch.Fetcher, raw string, fresh bool) (*goquery.Document, *url.URL, error) {
	body, final, err := ao3Get(ctx, f, raw, fresh)
	if err != nil {
		return nil, nil, err
	}
	gq, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	return gq, final, nil
}

// ao3Get fetches raw bytes (pages and downloads) from AO3.
func ao3Get(ctx context.Context, f *fetch.Fetcher, raw string, fresh bool) ([]byte, *url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, nil, err
	}
	// Use the same browser-compatible transport and cookie session for menus,
	// fandom works, chapters, and downloads. Default Go TLS can get refused
	// on work listings even when AO3's cached navigation pages are available.
	f, err = f.ForCatalog()
	if err != nil {
		return nil, nil, err
	}
	opts := fetch.Options{Revalidate: fresh}
	if s := LoadAO3Session(); s != "" && ao3Hosts[strings.ToLower(u.Host)] {
		opts.Header = http.Header{}
		opts.Header.Set("Cookie", s)
	}
	for attempt := 0; ; attempt++ {
		resp, err := f.Get(ctx, u, opts)
		if err == nil {
			if r := fetch.ChallengeReason(resp.Body); r != "" {
				return nil, nil, errVerification{"AO3", r}
			}
			final := resp.URL
			if final == nil {
				final = u
			}
			return resp.Body, final, nil
		}
		var ce *fetch.ChallengeError
		if errors.As(err, &ce) {
			return nil, nil, errVerification{"AO3", ce.Reason}
		}
		detail := ""
		var he *fetch.HTTPError
		switch {
		case errors.As(err, &he) && he.Status >= 500:
			detail = fmt.Sprintf("HTTP %d", he.Status)
		case errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "Timeout"):
			detail = "timed out"
		default:
			return nil, nil, fmt.Errorf("AO3: %w", err)
		}
		if attempt == 1 || ctx.Err() != nil || detail == "timed out" {
			return nil, nil, errUnavailable{detail}
		}
		select {
		case <-ctx.Done():
			return nil, nil, errUnavailable{detail}
		case <-time.After(ao3RetryWait):
		}
		opts.Revalidate = true
	}
}
