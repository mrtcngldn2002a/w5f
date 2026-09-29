package tui

import (
	"strings"

	"w5f/internal/doc"
	"w5f/internal/fiction"
	"w5f/internal/store"
)

// w5fPage reports a page W5F built itself: its address is a w5f: one, or
// it was reached from a site address but W5F gave it a w5f: address
// (serials opened from Royal Road or AO3 links, AO3's menus). Converters
// of web pages never set a w5f: address, so their links stay untrusted.
func w5fPage(p *page) bool {
	return strings.HasPrefix(p.target, "w5f:") || (p.doc != nil && strings.HasPrefix(p.doc.URL, "w5f:"))
}

// fromNetwork reports pages that came from the network — web pages, Gemini
// capsules, Gopher holes: their w5f: links are never opened.
func fromNetwork(target string) bool {
	l := strings.ToLower(target)
	for _, p := range []string{"http://", "https://", "gemini://", "gopher://"} {
		if strings.HasPrefix(l, p) {
			return true
		}
	}
	return false
}

// followKey follows or unfollows the serial (or Reddit series) of a page.
func followKey(d *doc.Document) string {
	db, err := store.Default()
	if err != nil {
		return err.Error()
	}
	id, ok := fiction.SerialRef(d.Ref)
	if !ok {
		if id, ok = fiction.SerialOfPage(db, d); !ok {
			return "F follows serials and Reddit series — this page is neither"
		}
	}
	on, err := fiction.ToggleFollow(db, id)
	if err != nil {
		return err.Error()
	}
	if on {
		return "following ✓ — new chapters show on the Internet Fiction page"
	}
	return "unfollowed"
}
