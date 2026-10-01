package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"w5f/internal/doc"
	"w5f/internal/personal"
	"w5f/internal/store"
	"w5f/internal/theme"
	"w5f/internal/ultan"
)

// The side menu (chosen with the owner, 2026-10-01): on a wide screen the
// library's rooms stay at hand on the left and the page takes the rest,
// left-aligned, up to wideMeasure columns. \ hides it while reading.
const (
	sideWidth   = 24
	sideMin     = 110 // narrower terminals get the centred column alone
	wideMeasure = 112
)

// room is a place in the side menu, opened by its key.
type room struct {
	key, label, target string
	also               []string // other addresses that belong to the room
}

// The rooms of Ultan's library (named with the owner, 2026-10-01).
var rooms = []room{
	{"1", "The Reading Room", "w5f:welcome", nil},
	{"2", "Periodical Gallery", "w5f:feeds", []string{"w5f:feed", "w5f:item/"}},
	{"3", "The Stacks", "w5f:books", []string{"w5f:book/", "w5f:catalog"}},
	{"4", "The Serial Hall", "w5f:fiction", []string{"w5f:serial/", "w5f:following"}},
	{"5", "The Picture Vault", "w5f:comics", nil},
	{"6", "The Gaming Table", "w5f:solo", nil},
	{"7", "The Newsroom", "w5f:usenet", nil},
	{"8", "Curiosity Cabinet", "w5f:cabinet", []string{"w5f:packet", "w5f:discover/", "w5f:smallweb", "w5f:worlds", "w5f:random", "w5f:almanac"}},
	{"9", "The Lectern", "w5f:queue", nil},
	{"0", "The Scriptorium", "w5f:notes", nil},
	{"H", "The Register", "w5f:history", nil},
	{"L", "Ultan's Ledger", "w5f:ledger", nil},
}

// roomList is the rooms in order.
func roomList() []room { return rooms }

// roomFor is the room a page belongs to, or nil.
func roomFor(target string) *room {
	for i := range rooms {
		r := &rooms[i]
		if target == r.target || (r.target != "w5f:welcome" && strings.HasPrefix(target, r.target)) {
			return r
		}
		for _, p := range r.also {
			if strings.HasPrefix(target, p) {
				return r
			}
		}
	}
	return nil
}

// sideShown reports whether the side menu is drawn.
func (m Model) sideShown() bool { return !m.sideOff && m.width >= sideMin }

// pageWidth is the width the page has to itself.
func (m Model) pageWidth() int {
	if m.sideShown() {
		return m.width - sideWidth - 1
	}
	return m.width
}

// sideLines draws the side menu, n rows high, each sideWidth wide.
func (m Model) sideLines(n int) []string {
	c := m.theme.Chrome
	bg := lipgloss.NewStyle().Background(c.BG)
	head := bg.Foreground(c.Accent).Bold(true)
	key := bg.Foreground(c.Dim)
	label := bg.Foreground(c.FG)
	here := lipgloss.NewStyle().Background(m.theme.FocusBG).Foreground(m.theme.FocusFG).Bold(true)
	var cur *room
	if m.cur != nil {
		cur = roomFor(m.cur.target)
	}
	pad := func(s string, st lipgloss.Style) string {
		if w := ansi.StringWidth(s); w < sideWidth {
			s += strings.Repeat(" ", sideWidth-w)
		}
		return st.Render(ansi.Truncate(s, sideWidth, "…"))
	}
	row := func(k, text string) string {
		return key.Render("   "+k+"  ") + label.Render(text) + bg.Render(strings.Repeat(" ", max(0, sideWidth-6-ansi.StringWidth(text))))
	}
	var out []string
	add := func(s string) { out = append(out, s) }
	add(pad("", bg))
	section := func(title string, rs []room) {
		add(pad(" "+title, head))
		for _, r := range rs {
			if cur != nil && cur.key == r.key {
				add(pad(" » "+r.key+"  "+r.label, here))
				continue
			}
			add(row(r.key, r.label))
		}
		add(pad("", bg))
	}
	section("THE LIBRARY", rooms[:8])
	section("YOUR ARCHIVE", rooms[8:])
	add(pad(" TODAY", head))
	for _, l := range [][2]string{{"p", "Daily Packet"}, {"x", "Deep random"}, {"T", "A tarot card"}, {"I", "An I Ching cast"}} {
		add(row(l[0], l[1]))
	}
	for len(out) < n-2 {
		add(pad("", bg))
	}
	add(pad(`   \  hide this menu`, key))
	add(pad("   g → theme", key))
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// sideSeparator is the rule between the side menu and the page.
func (m Model) sideSeparator() string {
	return lipgloss.NewStyle().Foreground(m.theme.Chrome.Rule).Background(m.theme.Page.BG).Render("│")
}

// openRoom opens a room by its key.
func (m Model) openRoom(key string) (tea.Model, tea.Cmd) {
	for _, r := range rooms {
		if r.key != key {
			continue
		}
		if d := m.localDoc(r.target); d != nil {
			return m, func() tea.Msg { return loadedMsg{target: r.target, doc: d} }
		}
		m.loading = r.target
		return m, load(r.target, false)
	}
	return m, nil
}

// --- preferences ---

// loadPrefs reads the kept theme and side-menu choice.
func (m *Model) loadPrefs() {
	db, err := store.Default()
	if err != nil {
		return
	}
	if t, ok := theme.ByName(db.Get("ui:theme")); ok {
		m.theme = t
	}
	m.sideOff = db.Get("ui:sidebar") == "off"
}

func savePref(key, value string) {
	if db, err := store.Default(); err == nil {
		_ = db.Set(key, value)
	}
}

// toggleSide shows or hides the side menu, and keeps the choice.
func (m Model) toggleSide() (tea.Model, tea.Cmd) {
	if m.width < sideMin {
		m.status = "the side menu needs a wider window"
		return m, nil
	}
	m.sideOff = !m.sideOff
	savePref("ui:sidebar", map[bool]string{true: "off", false: "on"}[m.sideOff])
	m.relayout()
	return m, nil
}

// themesDoc lists the themes; following one puts it on.
func themesDoc(cur theme.Theme) *doc.Document {
	d := &doc.Document{Title: "Themes", Origin: "local", URL: "w5f:themes"}
	var items [][]doc.Block
	for _, t := range theme.Themes {
		d.Links = append(d.Links, doc.Link{Href: "w5f:theme/" + t.Name, Text: t.Name})
		in := doc.Inline{{Text: t.Name, Link: len(d.Links), Style: doc.Bold}, {Text: "  " + t.Label}}
		if t.Name == cur.Name {
			in = append(in, doc.Span{Text: "  · in use", Style: doc.Italic})
		}
		items = append(items, []doc.Block{doc.Paragraph{Text: in}})
	}
	d.Blocks = []doc.Block{
		doc.Paragraph{Text: doc.Inline{{Text: "Choose a theme; it is kept for next time. g → theme <name> does the same.", Style: doc.Italic}}},
		doc.List{Items: items},
	}
	return d
}

// openThemes shows the themes page.
func (m Model) openThemes(replace bool) (tea.Model, tea.Cmd) {
	d := themesDoc(m.theme)
	return m, func() tea.Msg { return loadedMsg{target: "w5f:themes", doc: d, replace: replace} }
}

// setTheme puts a theme on and keeps it.
func (m Model) setTheme(name string) (tea.Model, tea.Cmd) {
	t, ok := theme.ByName(strings.ToLower(strings.TrimSpace(name)))
	if !ok {
		var names []string
		for _, t := range theme.Themes {
			names = append(names, t.Name)
		}
		m.status = "no theme " + name + " (" + strings.Join(names, ", ") + ")"
		return m, nil
	}
	m.theme = t
	savePref("ui:theme", t.Name)
	m.status = "Theme: " + t.Label
	if m.cur != nil && m.cur.target == "w5f:themes" {
		return m.openThemes(true)
	}
	return m, nil
}

// localDoc builds the pages the reader makes itself, or nil.
func (m Model) localDoc(target string) *doc.Document {
	switch target {
	case "w5f:welcome":
		return welcomeDoc(m.version)
	case "w5f:themes":
		return themesDoc(m.theme)
	case "w5f:cabinet":
		return cabinetDoc()
	}
	return nil
}

// cabinetDoc is the Curiosity Cabinet: every door of discovery.
func cabinetDoc() *doc.Document {
	d := &doc.Document{Title: "The Curiosity Cabinet", URL: "w5f:cabinet", Origin: "local", Lang: "en"}
	link := func(href, text string) int {
		d.Links = append(d.Links, doc.Link{Href: href, Text: text})
		return len(d.Links)
	}
	var items [][]doc.Block
	for _, e := range []struct{ key, label, href, what string }{
		{"p", "The Daily Packet", "w5f:packet", "today's issue: periodicals, a weird world, an esoteric text, an old-internet relic, your queue"},
		{"x", "Deep random", "w5f:discover/random", "a page from one of eight families of sources, with Ultan's note on its shelf"},
		{"T", "A tarot card", "w5f:discover/tarot", "Waite's Pictorial Key, the card drawn upright or reversed"},
		{"I", "An I Ching cast", "w5f:discover/iching", "three coins six times, Legge's translation"},
		{"", "On this day", "w5f:almanac", "Chambers's Book of Days (1864) and the day's events"},
		{"", "The Small Web", "w5f:smallweb", "Gemini capsules and Gopher holes, Wiby and Marginalia"},
		{"", "Archived worlds", "w5f:worlds", "invented worlds: SCP, the Backrooms, the Wanderers' Library and more"},
	} {
		in := doc.Inline{{Text: e.label, Style: doc.Bold, Link: link(e.href, e.label)}}
		if e.key != "" {
			in = append(in, doc.Span{Text: "  (" + e.key + ")"})
		}
		in = append(in, doc.Span{Text: " — " + e.what, Style: doc.Italic})
		items = append(items, []doc.Block{doc.Paragraph{Text: in}})
	}
	d.Blocks = append(ultan.Note{Text: "Not everything here was meant to be found. That is what makes it worth the looking."}.Blocks(link), doc.List{Items: items})
	return d
}

// queueLeft is the number of pages waiting in the queue.
func queueLeft() int {
	q, err := personal.LoadQueue()
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range q.Entries() {
		if !e.Done {
			n++
		}
	}
	return n
}

// hint is a key and what it does, for the bottom bar.
type hint struct{ key, does string }

// roomHints are the keys that matter where the reader is (chosen with the
// owner, 2026-10-01: the bottom bar follows the room).
func (m Model) roomHints() []hint {
	reading := []hint{{"/", "search"}, {"a", "queue"}, {"n", "note"}, {"y", "clip"}, {"d", "dictionary"}, {"B", "Chromium"}}
	if m.cur == nil {
		return nil
	}
	t := m.cur.target
	if _, ok := itemRef(m.cur.doc); ok {
		return append([]hint{{"*", "star"}, {"m", "read / unread"}}, reading...)
	}
	switch {
	case strings.HasPrefix(t, "w5f:discover/tarot"):
		return []hint{{"T", "another card"}, {"I", "an I Ching cast"}, {"←", "back"}}
	case strings.HasPrefix(t, "w5f:discover/iching"):
		return []hint{{"I", "another cast"}, {"T", "a tarot card"}, {"←", "back"}}
	case strings.HasPrefix(t, "w5f:item/"):
		return append([]hint{{"*", "star"}, {"m", "read / unread"}}, reading...)
	case strings.HasPrefix(t, "w5f:book/"):
		return append([]hint{{"t", "chapters"}, {"] [", "chapter"}}, reading...)
	case strings.HasPrefix(t, "w5f:serial/"):
		return append([]hint{{"t", "chapters"}, {"] [", "chapter"}, {"F", "follow"}}, reading...)
	case strings.HasPrefix(t, "w5f:packet"):
		return []hint{{"] [", "through the issue"}, {"→", "open"}, {"x", "deep random"}, {"T", "tarot"}, {"I", "I Ching"}}
	}
	if r := roomFor(t); r != nil {
		switch r.key {
		case "1":
			return []hint{{"↑↓", "select"}, {"→", "open"}, {"1…0", "rooms"}, {"p", "packet"}, {"x", "random"}, {"T", "tarot"}, {"I", "I Ching"}, {`\`, "menu"}, {"g", "go"}}
		case "2":
			return []hint{{"→", "open"}, {"*", "star"}, {"m", "read / unread"}, {"g → sync", "fetch new"}, {"g → opml-import", "add feeds"}}
		case "3":
			return []hint{{"→", "open"}, {"g → gut", "Gutenberg"}, {"g → se", "Standard Ebooks"}, {"g → catalog-add", "a book site"}, {"/", "search"}}
		case "4":
			return []hint{{"→", "open"}, {"F", "follow"}, {"g → following", "followed"}, {"g → fiction", "this hall"}}
		case "5":
			return []hint{{"→", "open"}, {"B", "in Chromium"}, {"g → comics", "this vault"}}
		case "6":
			return []hint{{"g → roll", "2d6+1"}, {"g → ask", "likely <question>"}, {"g → spark", "words · tarot · iching · reading"}}
		case "7":
			return []hint{{"→", "open"}, {"g → usenet", "<word> finds groups"}}
		case "8":
			return []hint{{"p", "packet"}, {"x", "deep random"}, {"T", "tarot"}, {"I", "I Ching"}, {"→", "open"}}
		case "9":
			return []hint{{"→", "open"}, {"a", "queue a page anywhere"}, {"A", "queue the selected link"}}
		case "0":
			return []hint{{"→", "open"}, {"n", "write a note"}, {"y", "clip paragraphs"}}
		case "H":
			return []hint{{"→", "open"}, {"/", "search everything you read"}, {"L", "the ledger"}}
		case "L":
			return []hint{{"→", "open"}, {"H", "the register"}}
		}
	}
	return append([]hint{{"→", "open"}, {"←", "back"}}, reading...)
}

// hintBar draws the hints, keys bright and what they do dim, as many as
// fit in the width, ? help always last.
func (m Model) hintBar() string {
	hs := append(m.roomHints(), hint{"?", "help"}, hint{"q", "quit"})
	plain := func(hs []hint) int {
		w := 1
		for i, h := range hs {
			if i > 0 {
				w += 3
			}
			w += ansi.StringWidth(h.key) + 1 + ansi.StringWidth(h.does)
		}
		return w
	}
	for len(hs) > 2 && plain(hs) > m.width {
		hs = append(hs[:len(hs)-3], hs[len(hs)-2:]...) // drop the last room hint
	}
	key, dim := m.theme.Bar().Bold(true), m.theme.BarDim()
	var b strings.Builder
	b.WriteString(dim.Render(" "))
	for i, h := range hs {
		if i > 0 {
			b.WriteString(dim.Render(" · "))
		}
		b.WriteString(key.Render(h.key) + dim.Render(" "+h.does))
	}
	if pad := m.width - plain(hs); pad > 0 {
		b.WriteString(dim.Render(strings.Repeat(" ", pad)))
	}
	return ansi.Truncate(b.String(), m.width, "")
}
