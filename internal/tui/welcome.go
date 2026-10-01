package tui

import (
	"fmt"
	"time"

	"w5f/internal/discover"
	"w5f/internal/doc"
	"w5f/internal/store"
	"w5f/internal/ultan"
)

// welcomeDoc is the Reading Room of Ultan's library, shown when w5f starts
// without a target (chosen with the owner, 2026-10-01): what lies open on
// the desk and what is new on the shelves on the left, the day and Ultan's
// note on the right; the rooms themselves are in the side menu.
func welcomeDoc(version string) *doc.Document {
	d := &doc.Document{
		Title:  "The Reading Room",
		Origin: "local",
		Meta:   []doc.KV{{Key: "version", Value: version}},
	}
	link := func(href, text string) int {
		d.Links = append(d.Links, doc.Link{Href: href, Text: text})
		return len(d.Links)
	}
	db, _ := store.Default()
	now := time.Now()

	left := []doc.Block{doc.Heading{Level: 2, Text: doc.Inline{{Text: "On the desk"}}}}
	if items := deskItems(db, link); len(items) > 0 {
		left = append(left, doc.List{Items: items})
	} else {
		left = append(left, doc.Paragraph{Text: doc.Inline{{Text: "Nothing lies open on the desk.", Style: doc.Italic}}})
	}
	left = append(left, doc.Heading{Level: 2, Text: doc.Inline{{Text: "New on the shelves"}}})
	if items := shelfItems(db, link); len(items) > 0 {
		left = append(left, doc.List{Items: items})
	} else {
		left = append(left, doc.Paragraph{Text: doc.Inline{{Text: "Nothing new has come in.", Style: doc.Italic}}})
	}

	right := []doc.Block{doc.Heading{Level: 2, Text: doc.Inline{{Text: "Today"}}}}
	var today [][]doc.Block
	if db != nil {
		for _, t := range discover.Today(db) {
			today = append(today, []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: t.Label + " · ", Style: doc.Bold}, {Text: t.Text, Link: link(t.Href, t.Text)}}}})
		}
	}
	today = append(today, []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: "Draw · ", Style: doc.Bold},
		{Text: "a tarot card", Link: link("w5f:discover/tarot", "tarot")}, {Text: " (T) · "},
		{Text: "an I Ching cast", Link: link("w5f:discover/iching", "I Ching")}, {Text: " (I) · "},
		{Text: "a random page", Link: link("w5f:discover/random", "deep random")}, {Text: " (x)"}}}})
	right = append(right, doc.List{Items: today})
	right = append(right, doc.Heading{Level: 2, Text: doc.Inline{{Text: "Ultan's note"}}})
	right = append(right, ultan.Desk(db, now).Blocks(link)...)

	d.Blocks = []doc.Block{doc.Columns{Cols: [][]doc.Block{left, right}}}

	// The rooms, for a window too narrow for the side menu (their keys
	// work everywhere).
	rooms := doc.Inline{{Text: "Rooms  ", Style: doc.Bold}}
	for i, r := range roomList() {
		if i > 0 {
			rooms = append(rooms, doc.Span{Text: " · "})
		}
		rooms = append(rooms, doc.Span{Text: r.key + " "}, doc.Span{Text: r.label, Link: link(r.target, r.label)})
	}
	d.Blocks = append(d.Blocks, doc.Rule{}, doc.Collapsible{ID: 1, Show: "First time here?", Hide: "Hide", Blocks: []doc.Block{
		doc.Paragraph{Text: doc.Inline{{Text: "A reading terminal for the textual internet and a personal archive: Ultan's library, of which you hold the only card."}}},
		doc.List{Items: [][]doc.Block{
			{doc.Paragraph{Text: doc.Inline{{Text: "↑ ↓", Style: doc.Bold}, {Text: " move from link to link; when the next one is off screen the page scrolls instead."}}}},
			{doc.Paragraph{Text: doc.Inline{{Text: "→", Style: doc.Bold}, {Text: " or "}, {Text: "enter", Style: doc.Bold}, {Text: " opens the selected link or section, "}, {Text: "←", Style: doc.Bold}, {Text: " goes back — as many times as you like."}}}},
			{doc.Paragraph{Text: doc.Inline{{Text: "1 … 9, 0", Style: doc.Bold}, {Text: " open the rooms; "}, {Text: `\`, Style: doc.Bold}, {Text: " hides the side menu; "}, {Text: "g → theme", Style: doc.Bold}, {Text: " changes the colours."}}}},
			{doc.Paragraph{Text: doc.Inline{{Text: "/", Style: doc.Bold}, {Text: " searches everything you have read; "}, {Text: "a", Style: doc.Bold}, {Text: " queues a page, "}, {Text: "n", Style: doc.Bold}, {Text: " writes a note, "}, {Text: "y", Style: doc.Bold}, {Text: " clips paragraphs; "}, {Text: "?", Style: doc.Bold}, {Text: " shows every key."}}}},
			{doc.Paragraph{Text: doc.Inline{{Text: "Folded sections like this one start closed; enter opens them, "}, {Text: "+", Style: doc.Bold}, {Text: " and "}, {Text: "-", Style: doc.Bold}, {Text: " open or fold them all."}}}},
		}},
		doc.Paragraph{Text: rooms},
		doc.Paragraph{Text: doc.Inline{{Text: "Open any file or URL from the shell: w5f https://… or w5f page.html", Style: doc.Code}}},
	}})
	d.Collapsibles = 1
	return d
}

// deskItems are the pages and books left half-read, newest first.
func deskItems(db *store.DB, link func(href, text string) int) [][]doc.Block {
	if db == nil {
		return nil
	}
	var items [][]doc.Block
	add := func(href, title, sub string) {
		items = append(items, []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: title, Link: link(href, title)}, {Text: "  " + sub, Style: doc.Italic}}}})
	}
	vs, _ := db.Unfinished(5)
	for _, v := range vs {
		sub := fmt.Sprintf("%d%%", int(v.Pos*100+0.5))
		if v.Catalog != "" {
			sub += " · " + v.Catalog
		}
		add(v.Target, v.Title, sub)
	}
	bs, _ := db.Books("recent", 3)
	for _, b := range bs {
		if b.Chapters > 0 && (b.Chapter < b.Chapters-1 || b.Pos < 0.95) {
			add(fmt.Sprintf("w5f:book/%d", b.ID), b.Title, fmt.Sprintf("chapter %d of %d", b.Chapter+1, b.Chapters))
		}
	}
	ss, _ := db.Serials(false)
	for i, s := range ss {
		if i == 3 || s.Opened.IsZero() {
			break
		}
		if s.Chapter < s.Chapters-1 || s.Pos < 0.95 {
			add(fmt.Sprintf("w5f:serial/%d/continue", s.ID), s.Title, fmt.Sprintf("chapter %d of %d", s.Chapter+1, s.Chapters))
		}
	}
	return items
}

// shelfItems is what has come in: new chapters of what is followed, the
// unread periodicals, the queue (all kept locally: no fetching here).
func shelfItems(db *store.DB, link func(href, text string) int) [][]doc.Block {
	if db == nil {
		return nil
	}
	var items [][]doc.Block
	add := func(href, title, sub string) {
		items = append(items, []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: title, Link: link(href, title)}, {Text: "  " + sub, Style: doc.Italic}}}})
	}
	if ss, err := db.Serials(true); err == nil {
		n := 0
		for _, s := range ss {
			if k := s.Chapters - s.Seen; s.Seen > 0 && k > 0 && n < 4 {
				add(fmt.Sprintf("w5f:serial/%d/continue", s.ID), s.Title, fmt.Sprintf("%d new %s", k, map[bool]string{true: "chapter", false: "chapters"}[k == 1]))
				n++
			}
		}
	}
	if cs, err := db.Counts(); err == nil {
		unread := 0
		for _, c := range cs {
			unread += c[0]
		}
		if unread > 0 {
			add("w5f:feeds", "The Periodical Gallery", fmt.Sprintf("%d unread", unread))
		}
	}
	if left := queueLeft(); left > 0 {
		add("w5f:queue", "The Lectern", fmt.Sprintf("%d %s in your queue", left, map[bool]string{true: "page", false: "pages"}[left == 1]))
	}
	return items
}
