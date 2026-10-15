package tui

import (
	"net/url"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"w5f/internal/doc"
)

// siteSearchHref is the line a page with a search box gets at its top. Web
// content cannot carry it (the converter drops w5f: links), and it only
// ever asks the page's own box.
const siteSearchHref = "w5f:site-search"

// withSiteSearch puts "→ search this site" above a web page that has a
// search box. The page's document is not changed: a copy carries the line.
func withSiteSearch(d *doc.Document) *doc.Document {
	if d == nil || len(d.Searches) == 0 || strings.HasPrefix(d.URL, "w5f:") {
		return d
	}
	c := *d
	c.Links = append(slices.Clip(d.Links), doc.Link{Href: siteSearchHref, Text: "search this site"})
	line := doc.Paragraph{Text: doc.Inline{{Text: "→ search this site", Style: doc.Italic, Link: len(c.Links)}}}
	c.Blocks = append([]doc.Block{line}, d.Blocks...)
	return &c
}

// withoutSiteSearch takes the line off again (a page saved to the Library
// keeps only what the site wrote).
func withoutSiteSearch(d *doc.Document) *doc.Document {
	n := len(d.Links)
	if n == 0 || d.Links[n-1].Href != siteSearchHref || len(d.Blocks) == 0 {
		return d
	}
	if p, ok := d.Blocks[0].(doc.Paragraph); !ok || len(p.Text) != 1 || p.Text[0].Link != n {
		return d
	}
	c := *d
	c.Links, c.Blocks = d.Links[:n-1:n-1], d.Blocks[1:]
	return &c
}

// openSiteSearch asks for the words and opens the site's results.
func (m Model) openSiteSearch() (tea.Model, tea.Cmd) {
	if m.cur == nil || len(m.cur.doc.Searches) == 0 {
		m.status = "this page has no search box"
		return m, nil
	}
	s := m.cur.doc.Searches[0]
	host := "the wikis" // w5f:search/wikis, set by the source layer
	if u, err := url.Parse(s.Action); err == nil && u.Hostname() != "" {
		host = strings.TrimPrefix(u.Hostname(), "www.")
	}
	label := s.Label
	if label == "" {
		label = "Words to search for"
	}
	f := &Form{
		Title:  "Search " + host,
		Intro:  []string{"The site's own search box. Its results open as a page; backspace comes back here."},
		Fields: []FormField{{Label: label}},
		Save: func(v []string) (string, string, error) {
			if v[0] == "" {
				return "", "nothing to search for", nil
			}
			return s.URL(v[0]), "", nil
		},
	}
	return m.openForm(func(string) (*Form, error) { return f, nil }, siteSearchHref)
}
