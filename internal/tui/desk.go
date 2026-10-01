package tui

import (
	"fmt"
	"net/url"
	"strings"

	"w5f/internal/doc"
	"w5f/internal/store"
)

// deskEntry is something left open on the desk.
type deskEntry struct{ target, title, sub string }

// deskEntries are the pages and books left half-read, newest first: at
// most pages unfinished web pages (≤ 0: all), the three latest books and
// serials; whatever was set aside since it was last opened stays off.
func deskEntries(db *store.DB, pages int) []deskEntry {
	if db == nil {
		return nil
	}
	var out []deskEntry
	vs, _ := db.Unfinished(pages)
	for _, v := range vs {
		sub := fmt.Sprintf("%d%%", int(v.Pos*100+0.5))
		if v.Catalog != "" {
			sub += " · " + v.Catalog
		}
		out = append(out, deskEntry{v.Target, v.Title, sub})
	}
	bs, _ := db.Books("recent", 3)
	for _, b := range bs {
		t := fmt.Sprintf("w5f:book/%d", b.ID)
		if b.Chapters > 0 && (b.Chapter < b.Chapters-1 || b.Pos < 0.95) && !db.Aside(t, b.Opened) {
			out = append(out, deskEntry{t, b.Title, fmt.Sprintf("chapter %d of %d", b.Chapter+1, b.Chapters)})
		}
	}
	ss, _ := db.Serials(false)
	for i, s := range ss {
		if i == 3 || s.Opened.IsZero() {
			break
		}
		t := fmt.Sprintf("w5f:serial/%d/continue", s.ID)
		if (s.Chapter < s.Chapters-1 || s.Pos < 0.95) && !db.Aside(t, s.Opened) {
			out = append(out, deskEntry{t, s.Title, fmt.Sprintf("chapter %d of %d", s.Chapter+1, s.Chapters)})
		}
	}
	return out
}

// deskDoc is the desk itself (w5f:desk): everything left open, each with
// "set aside"; w5f:desk/aside?u= sets one aside, w5f:desk/clear asks
// before setting them all aside and ?sure=yes does it. Nothing is lost:
// what is set aside stays in the history and comes back to the desk when
// it is opened again.
func deskDoc(target string) *doc.Document {
	d := &doc.Document{Title: "The desk", URL: "w5f:desk", Origin: "local", Lang: "en"}
	link := func(href, text string) int {
		d.Links = append(d.Links, doc.Link{Href: href, Text: text})
		return len(d.Links)
	}
	db, _ := store.Default()
	u, _ := url.Parse(target)
	var note string
	if u != nil && db != nil {
		q := u.Query()
		switch strings.TrimPrefix(u.Opaque, "desk/") {
		case "aside":
			if t := q.Get("u"); t != "" && db.SetAside(t) == nil {
				note = "Set aside: it comes back when you open it again."
			}
		case "clear":
			es := deskEntries(db, 0)
			if q.Get("sure") != "yes" {
				d.Title, d.URL = "Clear the desk?", "w5f:desk/clear"
				if len(es) == 0 {
					d.Blocks = []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: "The desk is already clear. "}, {Text: "back to the Reading Room", Link: link("w5f:welcome", "Reading Room")}}}}
					return d
				}
				d.Blocks = []doc.Block{
					doc.Paragraph{Text: doc.Inline{{Text: fmt.Sprintf("All %d left open will be set aside.", len(es))}}},
					doc.Paragraph{Text: doc.Inline{{Text: "They stay in the history, books and serials keep their place, and each comes back to the desk when it is opened again.", Style: doc.Italic}}},
					doc.Paragraph{Text: doc.Inline{{Text: "No, leave it", Link: link("w5f:desk", "leave")}, {Text: " · "}, {Text: "Yes, clear the desk", Style: doc.Bold, Link: link("w5f:desk/clear?sure=yes", "clear")}}}, // "No" first: the selection starts there
				}
				return d
			}
			n := 0
			for _, e := range es {
				if db.SetAside(e.target) == nil {
					n++
				}
			}
			note = fmt.Sprintf("The desk is clear: %d set aside.", n)
		}
	}
	if note != "" {
		d.Blocks = append(d.Blocks, doc.Notice{Kind: "info", Text: note})
	}
	es := deskEntries(db, 0)
	if len(es) == 0 {
		d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: "Nothing lies open on the desk.", Style: doc.Italic}}})
		return d
	}
	var items [][]doc.Block
	for _, e := range es {
		items = append(items, []doc.Block{doc.Paragraph{Text: doc.Inline{
			{Text: e.title, Link: link(e.target, e.title)}, {Text: "  " + e.sub, Style: doc.Italic}, {Text: "  "},
			{Text: "set aside", Style: doc.Italic, Link: link("w5f:desk/aside?"+url.Values{"u": {e.target}}.Encode(), "set aside")}}}})
	}
	d.Blocks = append(d.Blocks, doc.List{Items: items}, doc.Rule{},
		doc.Paragraph{Text: doc.Inline{{Text: "set everything aside…", Style: doc.Italic, Link: link("w5f:desk/clear", "clear the desk")}}},
		doc.Paragraph{Text: doc.Inline{{Text: "What is set aside stays in the history and comes back here when you open it again.", Style: doc.Italic}}})
	return d
}
