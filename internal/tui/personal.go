package tui

import (
	"context"
	"net/url"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"w5f/internal/catalog"
	"w5f/internal/doc"
	"w5f/internal/index"
	"w5f/internal/personal"
	"w5f/internal/source"
	"w5f/internal/store"
)

// recordedMsg reports the outcome of recording a page (history + index).
type recordedMsg struct{ err error }

// historyPos is where a content page was left, for "Continue reading"
// (0 when it was finished, barely started or never opened).
func historyPos(target string, d *doc.Document) float64 {
	kind, key := index.Kind(target, d)
	if kind == "" || kind == "book" { // books resume from their own progress
		return 0
	}
	db, err := store.Default()
	if err != nil {
		return 0
	}
	if p := db.Pos(key); p > 0.05 && p < 0.95 {
		return p
	}
	return 0
}

// navigationW5F reports w5f: addresses that only show something (safe from
// notes and saved pages); actions such as queue/remove or downloads are not.
func navigationW5F(href string) bool {
	for _, p := range []string{"w5f:book/", "w5f:item/", "w5f:find?", "w5f:history"} {
		if strings.HasPrefix(href, p) {
			return true
		}
	}
	if strings.HasPrefix(href, "w5f:serial/") && !strings.HasSuffix(href, "/follow") && !strings.HasSuffix(href, "/refresh") &&
		!strings.HasPrefix(href, "w5f:serial/open") {
		return true
	}
	switch href {
	case "w5f:queue", "w5f:notes", "w5f:books", "w5f:feeds", "w5f:welcome", "w5f:catalogs", "w5f:fiction", "w5f:following",
		"w5f:ledger", "w5f:cabinet":
		return true
	}
	return false
}

// loadForIndex opens non-EPUB books for the background book indexer.
var loadForIndex = func(p string) (*doc.Document, error) {
	return source.Load(context.Background(), p, source.Options{})
}

// record adds an opened content page to the reading history and the search
// index, off the UI goroutine. The Library and Periodicals pages also start
// background indexing of new books and feed items.
func record(target string, d *doc.Document) tea.Cmd {
	return func() tea.Msg {
		db, err := store.Default()
		if err != nil {
			return recordedMsg{err}
		}
		if kind, key := index.Kind(target, d); kind != "" {
			if err := db.Visit(key, d.Title, kind, catalog.Number(target, d)); err != nil {
				return recordedMsg{err}
			}
			if err := index.Page(db, target, d); err != nil {
				return recordedMsg{err}
			}
		}
		switch {
		case target == "w5f:books" || strings.HasPrefix(d.Ref, "book:"):
			go index.Books(db, loadForIndex)
		case strings.HasPrefix(target, "w5f:feeds"):
			go index.Feeds(db)
		}
		return recordedMsg{}
	}
}

// indexFile adds a just-written note, clipping or saved page to the index.
func indexFile(path string) tea.Cmd {
	return func() tea.Msg {
		db, err := store.Default()
		if err != nil {
			return recordedMsg{err}
		}
		return recordedMsg{index.NoteFile(db, personal.Dir(), path)}
	}
}

// pageSource describes the current page for notes, clippings and the queue.
// Web addresses are kept (they open in Obsidian too); W5F's own records use
// their w5f: address.
func pageSource(p *page) personal.Source {
	kind, key := index.Kind(p.target, p.doc)
	u := p.doc.URL
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		u = key
		if u == "" {
			u = p.target
		}
	}
	title := strings.TrimSpace(p.doc.Title)
	if title == "" {
		title = u
	}
	if kind == "" {
		kind = "page"
	}
	return personal.Source{Title: title, URL: u, Catalog: catalog.Number(p.target, p.doc), Kind: kind}
}

// queueAdd adds a page to "This week" and returns the status line.
func queueAdd(s personal.Source) string {
	q, err := personal.LoadQueue()
	if err != nil {
		return "error: " + err.Error()
	}
	if !q.Add(personal.Entry{Title: s.Title, URL: s.URL, Catalog: s.Catalog}, personal.ThisWeek) {
		return "already in the queue"
	}
	if err := q.Save(); err != nil {
		return "error: " + err.Error()
	}
	return "added to the queue (This week) — g → queue"
}

// --- search prompt (/) ---

func (m Model) findKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		m.mode = modeRead
	case "enter":
		m.mode = modeRead
		q := strings.TrimSpace(m.findBuf)
		if q == "" {
			return m, nil
		}
		m.loading = "searching your archive"
		return m, load("w5f:find?"+url.Values{"q": {q}}.Encode(), false)
	case "backspace":
		if r := []rune(m.findBuf); len(r) > 0 {
			m.findBuf = string(r[:len(r)-1])
		}
	case "ctrl+u":
		m.findBuf = ""
	default:
		if k.Text != "" {
			m.findBuf += k.Text
		}
	}
	return m, nil
}

// savePage writes the Markdown copy of the current page.
func (m *Model) savePage() tea.Cmd {
	path, err := personal.SavePage(pageSource(m.cur), m.cur.doc, time.Now())
	if err != nil {
		m.status = "error: " + err.Error()
		return nil
	}
	m.status = "saved to " + personal.Rel(path)
	return indexFile(path)
}

// progressText is the background indexer's status line.
func progressText() string {
	s, _ := index.Progress.Load().(string)
	return s
}
