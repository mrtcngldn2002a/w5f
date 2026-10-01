package tui

import (
	"fmt"

	"w5f/internal/discover"
	"w5f/internal/doc"
	"w5f/internal/store"
)

// welcomeDoc is shown when w5f starts without a target. It doubles as a
// hands-on tour of the reader, and lists what was left half-read.
func welcomeDoc(version string) *doc.Document {
	d := &doc.Document{
		Title:  "W5F // ARCHIVE NODE",
		Origin: "local",
		Meta:   []doc.KV{{Key: "version", Value: version}},
		Links: []doc.Link{
			{Href: "https://scp-wiki.wikidot.com/scp-173", Text: "SCP-173"},
			{Href: "https://wanderers-library.wikidot.com/six-etchings-in-the-basalt-of-olympus-mons", Text: "Six Etchings"},
			{Href: "https://backrooms-wiki.wikidot.com/level-0", Text: "Level 0"},
			{Href: "w5f:feeds", Text: "Periodicals"},
			{Href: "w5f:books", Text: "Library"},
			{Href: "w5f:queue", Text: "Reading queue"},
			{Href: "w5f:notes", Text: "Notes & clippings"},
			{Href: "w5f:history", Text: "History"},
			{Href: "w5f:fiction", Text: "Internet Fiction"},
			{Href: "w5f:packet", Text: "Daily Packet"},
			{Href: "w5f:discover/random", Text: "Deep random"},
			{Href: "w5f:smallweb", Text: "Small Web"},
			{Href: "w5f:worlds", Text: "Archived worlds"},
			{Href: "w5f:comics", Text: "Comics"},
			{Href: "w5f:usenet", Text: "Usenet"},
		},
	}
	fiction := "serials, forum stories, Reddit series — follow them for new chapters"
	packet := "today's Daily Packet — press p"
	if db, err := store.Default(); err == nil {
		if n, _ := db.NewChapterTotal(); n > 0 {
			fiction = fmt.Sprintf("%d new chapters in what you follow", n)
		}
		packet = discover.Welcome(db)
	}
	d.Blocks = []doc.Block{
		doc.Paragraph{Text: doc.Inline{
			{Text: "A reading terminal for the textual internet and a personal archive. "},
			{Text: "Milestone M6: discovery — deep random (x), the Daily Packet (p), Gemini and Gopher, archived worlds.", Style: doc.Italic},
		}},
	}
	d.Blocks = append(d.Blocks, continueSection(d)...)
	d.Blocks = append(d.Blocks,
		doc.Heading{Level: 2, Text: doc.Inline{{Text: "Try it"}}},
		doc.List{Items: [][]doc.Block{
			{doc.Paragraph{Text: doc.Inline{{Text: "↑ ↓", Style: doc.Bold}, {Text: " move from link to link; when the next one is off screen the page scrolls instead."}}}},
			{doc.Paragraph{Text: doc.Inline{{Text: "→", Style: doc.Bold}, {Text: " or "}, {Text: "enter", Style: doc.Bold}, {Text: " opens the selected link or section, "}, {Text: "←", Style: doc.Bold}, {Text: " goes back — as many times as you like."}}}},
			{doc.Paragraph{Text: doc.Inline{{Text: "/", Style: doc.Bold}, {Text: " searches everything you have read; "}, {Text: "a", Style: doc.Bold}, {Text: " queues a page, "}, {Text: "n", Style: doc.Bold}, {Text: " writes a note, "}, {Text: "y", Style: doc.Bold}, {Text: " clips paragraphs; "}, {Text: "?", Style: doc.Bold}, {Text: " shows every key."}}}},
		}},
		doc.Heading{Level: 2, Text: doc.Inline{{Text: "Start reading"}}},
		doc.List{Items: [][]doc.Block{
			{doc.Paragraph{Text: doc.Inline{{Text: "Periodicals — ", Style: doc.Bold}, {Text: "your shelves of magazines and blogs", Link: 4}}}},
			{doc.Paragraph{Text: doc.Inline{{Text: "Library — ", Style: doc.Bold}, {Text: "your books, Project Gutenberg, Standard Ebooks", Link: 5}}}},
			{doc.Paragraph{Text: doc.Inline{{Text: "Internet Fiction — ", Style: doc.Bold}, {Text: fiction, Link: 9}}}},
			{doc.Paragraph{Text: doc.Inline{{Text: "Comics — ", Style: doc.Bold}, {Text: "your CBZ library and the series Suwayomi follows", Link: 14}}}},
			{doc.Paragraph{Text: doc.Inline{{Text: "Usenet — ", Style: doc.Bold}, {Text: "the text newsgroups you read", Link: 15}}}},
			{doc.Paragraph{Text: doc.Inline{{Text: "Discovery — ", Style: doc.Bold}, {Text: packet, Link: 10}, {Text: " · "}, {Text: "deep random (x)", Link: 11},
				{Text: " · "}, {Text: "Small Web", Link: 12}, {Text: " · "}, {Text: "archived worlds", Link: 13}}}},
			{doc.Paragraph{Text: doc.Inline{{Text: "SCP Foundation — ", Style: doc.Bold}, {Text: "SCP-173", Link: 1}}}},
			{doc.Paragraph{Text: doc.Inline{{Text: "Wanderers' Library — ", Style: doc.Bold}, {Text: "Six Etchings in the Basalt of Olympus Mons", Link: 2}}}},
			{doc.Paragraph{Text: doc.Inline{{Text: "The Backrooms — ", Style: doc.Bold}, {Text: "Level 0", Link: 3}}}},
		}},
		doc.Heading{Level: 2, Text: doc.Inline{{Text: "Your archive"}}},
		doc.List{Items: [][]doc.Block{
			{doc.Paragraph{Text: doc.Inline{{Text: "Reading queue", Link: 6}, {Text: " — pages you want to read (press a)", Style: doc.Italic}}}},
			{doc.Paragraph{Text: doc.Inline{{Text: "Notes & clippings", Link: 7}, {Text: " — Markdown files Obsidian can open", Style: doc.Italic}}}},
			{doc.Paragraph{Text: doc.Inline{{Text: "History", Link: 8}, {Text: " — everything you have read (H)", Style: doc.Italic}}}},
		}},
		doc.Collapsible{ID: 1, Show: "About collapsible sections", Hide: "Hide", Blocks: []doc.Block{
			doc.Paragraph{Text: doc.Inline{{Text: "Folded sections start closed, exactly as their authors intended. No spoilers leak before you choose to open them. Press enter on the label to toggle; "}, {Text: "+", Style: doc.Bold}, {Text: " and "}, {Text: "-", Style: doc.Bold}, {Text: " expand or fold every section on the page."}}},
		}},
		doc.Rule{},
		doc.Paragraph{Text: doc.Inline{{Text: "Open any file or URL from the shell: w5f https://… or w5f page.html", Style: doc.Code}}},
	)
	d.Collapsibles = 1
	return d
}

// continueSection lists pages and books left half-read, newest first.
func continueSection(d *doc.Document) []doc.Block {
	db, err := store.Default()
	if err != nil {
		return nil
	}
	var items [][]doc.Block
	add := func(href, title, sub string) {
		d.Links = append(d.Links, doc.Link{Href: href, Text: title})
		items = append(items, []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: title, Link: len(d.Links)}, {Text: "  " + sub, Style: doc.Italic}}}})
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
	if len(items) == 0 {
		return nil
	}
	return []doc.Block{doc.Heading{Level: 2, Text: doc.Inline{{Text: "Continue reading"}}}, doc.List{Items: items}}
}
