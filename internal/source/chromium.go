package source

import (
	"errors"
	"fmt"
	"strings"

	"w5f/internal/browser"
	"w5f/internal/fiction"
	"w5f/internal/reddit"
)

// ImportSession takes a site's login cookie from Chromium, where the owner
// signed in, and keeps it as W5F's session for that site (site: "reddit"
// or "ao3"). It says what it did; cookie values are never shown.
func ImportSession(site string) (string, error) {
	switch site {
	case "reddit":
		c, err := browser.Cookies(browser.ProfileDir(), "reddit.com", []string{"reddit_session"})
		if err != nil {
			return "", sessionHint("Reddit", "https://old.reddit.com/login", err)
		}
		if err := reddit.SaveSession(c["reddit_session"]); err != nil {
			return "", err
		}
		return "Reddit connected with the session Chromium has (stored only on this computer).", nil
	case "ao3":
		names := fiction.AO3CookieNames()
		c, err := browser.Cookies(browser.ProfileDir(), "archiveofourown.org", names)
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
		return "AO3 connected with the session Chromium has (stored only on this computer).", nil
	}
	return "", fmt.Errorf("W5F takes sessions for reddit and ao3, not %q", site)
}

func sessionHint(site, login string, err error) error {
	if errors.Is(err, browser.ErrNoCookie) {
		return fmt.Errorf("Chromium is not signed in to %s: g → chromium %s, sign in, then try again (Chromium writes new cookies to disk within about half a minute)", site, login)
	}
	return err
}
