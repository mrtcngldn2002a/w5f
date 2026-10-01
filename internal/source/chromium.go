package source

import (
	"errors"
	"fmt"
	"strings"

	"w5f/internal/browser"
	"w5f/internal/fiction"
	"w5f/internal/reddit"
)

// ImportSession takes a site's login cookie from the browser the owner
// signed in with and keeps it as W5F's session for that site (site:
// "reddit" or "ao3"; from: "browser" for the first browser that has it,
// Chromium first, or "chromium", "firefox"). It says what it did; cookie
// values are never shown.
func ImportSession(site, from string) (string, error) {
	if from == "browser" {
		from = ""
	}
	switch site {
	case "reddit":
		c, which, err := browser.SessionCookies(from, "reddit.com", []string{"reddit_session"})
		if err != nil {
			return "", sessionHint("Reddit", "https://old.reddit.com/login", err)
		}
		if err := reddit.SaveSession(c["reddit_session"]); err != nil {
			return "", err
		}
		return "Reddit connected with the session " + which + " has (stored only on this computer).", nil
	case "ao3":
		names := fiction.AO3CookieNames()
		c, which, err := browser.SessionCookies(from, "archiveofourown.org", names)
		if err != nil {
			return "", sessionHint("AO3", "https://archiveofourown.org/users/login", err)
		}
		var parts []string
		for _, n := range names {
			if v, ok := c[n]; ok {
				parts = append(parts, n+"="+v)
			}
		}
		if err := fiction.SaveAO3Session(strings.Join(parts, "; ")); err != nil {
			return "", err
		}
		return "AO3 connected with the session " + which + " has (stored only on this computer).", nil
	}
	return "", fmt.Errorf("W5F takes sessions for reddit and ao3, not %q", site)
}

// SessionFrom reads "reddit-login browser" (or chromium, firefox): the
// site and the browser, ok when it is such a command.
func SessionFrom(s string) (site, from string, ok bool) {
	f := strings.Fields(strings.ToLower(s))
	if len(f) != 2 || (f[0] != "reddit-login" && f[0] != "ao3-login") {
		return "", "", false
	}
	switch f[1] {
	case "browser", "chromium", "firefox":
		return strings.TrimSuffix(f[0], "-login"), f[1], true
	}
	return "", "", false
}

func sessionHint(site, login string, err error) error {
	if errors.Is(err, browser.ErrNoCookie) {
		return fmt.Errorf("not signed in to %s in Chromium or Firefox: g → browser %s, sign in, then try again (a browser writes new cookies to disk within about half a minute)", site, login)
	}
	return err
}
