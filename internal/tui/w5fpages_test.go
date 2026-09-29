package tui

import (
	"testing"

	"w5f/internal/doc"
)

// Pages W5F builds itself (serials, AO3 menus) may be reached from a web
// address, but their w5f: links are W5F's own and must work. Plain web
// pages still never open w5f: addresses.
func TestW5FPagesFromWebAddressesKeepTheirLinks(t *testing.T) {
	m := sized(New("", "test"))
	m = open(m, "https://www.royalroad.com/fiction/21220", &doc.Document{Title: "Mother of Learning", URL: "w5f:serial/5"})
	if _, cmd := m.follow("w5f:serial/5/ch/0"); cmd == nil {
		t.Error("a serial page opened from its site address must follow its own links")
	}
	m = open(m, "https://archiveofourown.org/", &doc.Document{Title: "AO3", URL: "w5f:fiction/ao3/page?u=https%3A%2F%2Farchiveofourown.org%2F"})
	if _, cmd := m.follow("w5f:fiction/ao3/fandoms?u=x"); cmd == nil {
		t.Error("AO3 menu links must work")
	}
	m = open(m, "https://example.org/p", &doc.Document{Title: "Web", URL: "https://example.org/p"})
	if _, cmd := m.follow("w5f:books"); cmd != nil {
		t.Error("a web page must not open w5f: addresses")
	}
	for _, target := range []string{"gemini://capsule.example/", "gopher://hole.example/1/"} {
		m = open(m, target, &doc.Document{Title: "Small web", URL: target})
		if _, cmd := m.follow("w5f:queue/add?u=x"); cmd != nil {
			t.Errorf("%s: a capsule or gopher hole must not open w5f: addresses", target)
		}
		if _, cmd := m.follow("gemini://capsule.example/next"); cmd == nil {
			t.Errorf("%s: small web links must open", target)
		}
	}
}
