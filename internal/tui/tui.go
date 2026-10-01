// Package tui is the Bubble Tea front end: the reader screen and its chrome.
package tui

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"w5f/internal/books"
	"w5f/internal/browser"
	"w5f/internal/catalog"
	"w5f/internal/crom"
	"w5f/internal/dict"
	"w5f/internal/doc"
	"w5f/internal/fiction"
	"w5f/internal/index"
	"w5f/internal/personal"
	"w5f/internal/reddit"
	"w5f/internal/render"
	"w5f/internal/sitecat"
	"w5f/internal/smallweb"
	"w5f/internal/source"
	"w5f/internal/store"
	"w5f/internal/theme"
)

const maxMeasure = 72 // reading column width (see 02 Dizayn Planı §4)

type mode uint8

const (
	modeRead mode = iota
	modeHints
	modeTOC
	modeHelp
	modeGoto
	modeSecret // hidden input for the Reddit session cookie
	modeDict   // pop-up dictionary over the page
	modeFind   // "/" search prompt
	modeNote   // pop-up note box
	modeClip   // paragraph selection for a clipping
)

// Session storage hooks (replaced in tests so they never touch real config).
var (
	saveSession      = reddit.SaveSession
	deleteSession    = reddit.DeleteSession
	saveAO3Session   = fiction.SaveAO3Session
	deleteAO3Session = fiction.DeleteAO3Session
)

// page is one entry in the navigation history.
type page struct {
	target string
	doc    *doc.Document
	open   map[int]bool
	layout *render.Layout
	offset int
	focus  int // 1-based index into layout.Focus, 0 = none
}

// Model is the root Bubble Tea model.
type Model struct {
	version string
	theme   theme.Theme
	width   int
	height  int

	cur     *page
	back    []*page
	forward []*page

	mode      mode
	hintBuf   string
	gotoBuf   string
	secretBuf string
	secretAO3 bool       // the hidden input is for the AO3 session, not Reddit's
	form      *formState // a page's form (RegisterForm) instead of a login
	findBuf   string
	dict      dictState
	note      noteState
	clip      clipState
	hints     map[int]int // label -> focus index
	tocCursor int

	loading string
	status  string
	sideOff bool // the side menu hidden (\)
}

// New creates the model; target may be empty for the welcome page.
func New(target, version string) Model {
	m := Model{version: version, theme: theme.Default, width: 80, height: 24}
	m.loadPrefs()
	if target == "" {
		m.cur = newPage("w5f:welcome", welcomeDoc(version))
		// Bubble Tea renders once before the first WindowSizeMsg arrives, so
		// the page needs a layout for the default size right away.
		m.relayout()
		m.focusFirstVisible()
	} else if d := m.localDoc(target); d != nil {
		// A page the reader makes itself (w5f:cabinet, w5f:themes …).
		m.cur = newPage(target, d)
		m.relayout()
		m.focusFirstVisible()
	} else {
		m.loading = target
	}
	return m
}

func newPage(target string, d *doc.Document) *page {
	return &page{target: target, doc: d, open: map[int]bool{}}
}

type loadedMsg struct {
	target  string
	doc     *doc.Document
	err     error
	replace bool   // reload: replace current page instead of pushing history
	gen     uint64 // load generation; results of superseded loads are dropped
}

var (
	loadMu     sync.Mutex
	loadCancel context.CancelFunc
	loadGen    atomic.Uint64
)

// load starts loading a page; a new load cancels the one still running.
func load(target string, replace bool) tea.Cmd {
	ctx, cancel := context.WithCancel(context.Background())
	loadMu.Lock()
	if loadCancel != nil {
		loadCancel()
	}
	loadCancel = cancel
	gen := loadGen.Add(1)
	loadMu.Unlock()
	return tea.Batch(func() tea.Msg {
		d, err := source.Load(ctx, target, source.Options{Reload: replace})
		return loadedMsg{target: target, doc: d, err: err, replace: replace, gen: gen}
	}, progressTick())
}

// progressMsg redraws the status line while a long load (a full site
// search) reports its progress.
type progressMsg struct{}

func progressTick() tea.Cmd {
	return tea.Tick(250*time.Millisecond, func(time.Time) tea.Msg { return progressMsg{} })
}

func cancelLoad() {
	loadMu.Lock()
	if loadCancel != nil {
		loadCancel()
	}
	loadMu.Unlock()
}

// catalogLoading is the status text while a site-catalog page loads.
func catalogLoading(href string) string {
	switch {
	case !strings.HasPrefix(href, "w5f:catalog/"):
		return ""
	case strings.HasPrefix(href, "w5f:catalog/check"), strings.HasPrefix(href, "w5f:catalog/add"), strings.HasSuffix(href, "/recheck"):
		return "inspecting the site"
	case strings.Contains(href, "/get?"):
		return "downloading the book into your library"
	case strings.Contains(href, "?q="):
		return "searching the site (all result pages)"
	}
	return ""
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	if m.loading != "" {
		return load(m.loading, false)
	}
	return nil
}

func (m *Model) textWidth() int {
	w := m.width - 4
	if w > maxMeasure {
		w = maxMeasure
	}
	if m.sideShown() {
		w = min(m.pageWidth()-4, wideMeasure)
	}
	if w < 20 {
		w = 20
	}
	return w
}

func (m *Model) bodyHeight() int {
	h := m.height - 2
	if h < 1 {
		h = 1
	}
	return h
}

// relayout re-renders the current page, keeping the anchor line in place.
func (m *Model) relayout() {
	p := m.cur
	if p == nil {
		return
	}
	p.layout = render.Render(p.doc, render.Options{Width: m.textWidth(), Open: p.open})
	if p.focus > len(p.layout.Focus) {
		p.focus = 0
	}
	m.clamp()
}

func (m *Model) clamp() {
	p := m.cur
	max := len(p.layout.Lines) - m.bodyHeight()
	if max < 0 {
		max = 0
	}
	if p.offset > max {
		p.offset = max
	}
	if p.offset < 0 {
		p.offset = 0
	}
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.relayout()
		return m, nil
	case progressMsg:
		if m.loading != "" {
			return m, progressTick()
		}
		return m, nil
	case loadedMsg:
		if errors.Is(msg.err, context.Canceled) || (msg.gen != 0 && msg.gen != loadGen.Load()) {
			return m, nil
		}
		m.loading = ""
		if msg.err != nil {
			m.status = "error: " + source.Friendly(msg.err)
			if m.cur == nil {
				m.cur = newPage("w5f:welcome", welcomeDoc(m.version))
				m.relayout()
			}
			return m, nil
		}
		target := msg.target
		if strings.HasPrefix(target, "w5f:") && msg.doc.URL != "" {
			target = msg.doc.URL // e.g. a random page: remember where we landed
		}
		np := newPage(target, msg.doc)
		if !msg.replace {
			m.leavePage()
		}
		if msg.replace && m.cur != nil {
			np.offset = m.cur.offset
			np.open = m.cur.open
			np.focus = m.cur.focus // + on a counter, pressed again and again
		} else if m.cur != nil {
			m.back = append(m.back, m.cur)
			m.forward = nil
		}
		m.cur = np
		m.status = ""
		// Modes tied to the old page's layout end with it.
		if m.mode == modeClip || m.mode == modeHints || m.mode == modeTOC {
			m.mode = modeRead
		}
		m.relayout()
		if np.layout != nil && np.focus > len(np.layout.Focus) {
			np.focus = len(np.layout.Focus)
		}
		if !msg.replace {
			resume := np.doc.Resume
			if resume == 0 {
				resume = historyPos(target, np.doc)
			}
			if resume > 0 {
				np.offset = int(resume * float64(len(np.layout.Lines)))
				m.clamp()
			}
			m.focusFirstVisible()
		}
		return m, record(target, np.doc)
	case recordedMsg:
		if msg.err != nil && m.status == "" {
			m.status = "error: search index: " + msg.err.Error()
		}
		return m, nil
	case tea.PasteMsg:
		switch m.mode {
		case modeGoto:
			m.gotoBuf += strings.TrimSpace(msg.Content)
		case modeSecret:
			m.secretBuf += strings.TrimSpace(msg.Content)
		case modeFind:
			m.findBuf += strings.TrimSpace(msg.Content)
		case modeNote:
			m.note.insert(msg.Content)
		case modeDict:
			m.dict.query += strings.TrimSpace(msg.Content)
			m.refreshSuggestions()
		}
		return m, nil
	case dictInstalledMsg:
		m.loading = ""
		if msg.err != nil {
			m.status = "error: dictionary install failed (" + msg.err.Error() + ")"
		} else {
			m.status = "Dictionary installed: " + msg.title + ". Press d to look up a word."
		}
		return m, nil
	case tea.KeyPressMsg:
		return m.key(msg)
	}
	return m, nil
}

func (m Model) key(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := k.String()
	if s == "ctrl+c" {
		m.leavePage()
		return m, tea.Quit
	}
	if s == "esc" && m.loading != "" && m.cur != nil && m.mode != modeGoto && m.mode != modeSecret && m.mode != modeDict && m.mode != modeFind && m.mode != modeNote && m.mode != modeClip {
		cancelLoad()
		m.loading, m.status = "", "cancelled"
		return m, nil
	}
	if m.cur == nil {
		if s == "q" {
			return m, tea.Quit
		}
		return m, nil
	}
	switch m.mode {
	case modeHints:
		return m.hintKey(s)
	case modeTOC:
		return m.tocKey(s)
	case modeHelp:
		m.mode = modeRead
		return m, nil
	case modeGoto:
		return m.gotoKey(k)
	case modeSecret:
		return m.secretKey(k)
	case modeDict:
		return m.dictKey(k)
	case modeFind:
		return m.findKey(k)
	case modeNote:
		return m.noteKey(k)
	case modeClip:
		return m.clipKey(k)
	}
	m.status = ""
	p := m.cur
	switch s {
	case "q":
		m.leavePage()
		return m, tea.Quit
	case "]":
		if p.doc.Next != "" {
			return m.follow(p.doc.Next)
		}
		m.status = "this is the last part"
		return m, nil
	case "[":
		if p.doc.Prev != "" {
			return m.follow(p.doc.Prev)
		}
		m.status = "this is the first part"
		return m, nil
	case "down":
		m.lynxMove(1)
		return m, nil
	case "up":
		m.lynxMove(-1)
		return m, nil
	case "right":
		return m.activate(p.focus)
	case "j":
		p.offset++
	case "k":
		p.offset--
	case " ", "space", "pgdown", "ctrl+d":
		p.offset += m.bodyHeight() - 2
	case "b", "pgup", "ctrl+u":
		p.offset -= m.bodyHeight() - 2
	case "g":
		m.mode, m.gotoBuf = modeGoto, ""
		return m, nil
	case "r", "R":
		host := ""
		if u, err := url.Parse(p.doc.URL); err == nil {
			host = u.Host
		}
		pr := crom.PresetForHost(host)
		m.loading = pr.Label
		return m, load("w5f:random/"+pr.Name, false)
	case "home":
		p.offset = 0
	case "G", "end":
		p.offset = len(p.layout.Lines)
	case "tab":
		m.moveFocus(1)
		return m, nil
	case "shift+tab":
		m.moveFocus(-1)
		return m, nil
	case "enter":
		if p.focus == 0 && smallweb.IsInputPage(p.doc.URL) {
			m.mode, m.gotoBuf = modeGoto, "? " // type the answer right away
			return m, nil
		}
		return m.activate(p.focus)
	case "esc":
		p.focus = 0
	case "f":
		m.startHints()
		return m, nil
	case "h", "left", "backspace":
		m.goBack()
		return m, m.refreshLocal()
	case "l", "shift+right":
		m.goForward()
		return m, m.refreshLocal()
	case "t":
		if strings.HasPrefix(p.doc.Ref, "book:") {
			id := strings.SplitN(strings.TrimPrefix(p.doc.Ref, "book:"), ":", 2)[0]
			return m.follow("w5f:book/" + id + "/toc")
		}
		if id, ok := fiction.SerialRef(p.doc.Ref); ok {
			return m.follow(fmt.Sprintf("w5f:serial/%d/toc", id))
		}
		if len(p.layout.Headings) == 0 {
			m.status = "no headings on this page"
			return m, nil
		}
		m.mode, m.tocCursor = modeTOC, 0
		return m, nil
	case "?":
		m.mode = modeHelp
		return m, nil
	case "ctrl+r":
		if strings.HasPrefix(p.target, "w5f:") {
			return m, nil
		}
		m.loading = p.target
		return m, load(p.target, true)
	case "1", "2", "3", "4", "5", "6", "7", "8", "9", "0", "L":
		return m.openRoom(s)
	case `\`:
		return m.toggleSide()
	case "T", "I":
		// A tarot card, or an I Ching hexagram, drawn now.
		target := "w5f:discover/tarot"
		if s == "I" {
			target = "w5f:discover/iching"
		}
		m.loading = target
		return m, load(target, false)
	case "B":
		// This page (or the selected link) in the browser.
		target := p.target
		if f := m.focused(); f != nil && f.Kind == render.FocusLink && strings.HasPrefix(p.doc.Links[f.Link-1].Href, "http") {
			target = p.doc.Links[f.Link-1].Href
		} else if !strings.HasPrefix(target, "http") {
			target = p.doc.URL
		}
		if err := browser.Open(target); err != nil {
			m.status = "error: " + err.Error()
		} else {
			m.status = "Opened in " + browser.Name() + ": " + target
		}
		return m, nil
	case "o":
		if f := m.focused(); f != nil && f.Kind == render.FocusLink {
			m.status = p.doc.Links[f.Link-1].Href
		} else if strings.HasPrefix(p.target, "http") {
			m.status = p.target // the site address, also for pages W5F built
		} else {
			m.status = p.doc.URL
		}
		return m, nil
	case "d":
		m.openDict()
		return m, nil
	case "n":
		m.openNote()
		return m, nil
	case "y":
		m.startClip()
		return m, nil
	case "/":
		m.mode, m.findBuf = modeFind, ""
		return m, nil
	case "a":
		m.status = queueAdd(pageSource(p))
		return m, nil
	case "A":
		f := m.focused()
		if f == nil || f.Kind != render.FocusLink {
			m.status = "select a link first (↑↓), then press A"
			return m, nil
		}
		l := p.doc.Links[f.Link-1]
		title := strings.TrimSpace(l.Text)
		if title == "" {
			title = l.Href
		}
		m.status = queueAdd(personal.Source{Title: title, URL: l.Href, Catalog: catalog.Number(l.Href, nil), Kind: "link"})
		return m, nil
	case "s":
		return m, m.savePage()
	case "H":
		m.loading = "history"
		return m, load("w5f:history", false)
	case "*":
		if id, ok := itemRef(p.doc); ok {
			if db, err := store.Default(); err == nil {
				if on, err := db.ToggleStar(id); err == nil {
					m.status = map[bool]string{true: "* starred", false: "star removed"}[on]
				}
			}
		} else {
			m.status = "only feed items can be starred (for now)"
		}
		return m, nil
	case "F":
		m.status = followKey(p.doc)
		return m, nil
	case "x", "X":
		m.loading = "deep random"
		return m, load("w5f:discover/random", false)
	case "p", "P":
		m.loading = "the Daily Packet"
		return m, load("w5f:packet", false)
	case "m":
		if id, ok := itemRef(p.doc); ok {
			if db, err := store.Default(); err == nil {
				if it, err := db.Item(id); err == nil {
					_ = db.SetRead(id, !it.Read)
					m.status = map[bool]string{true: "marked unread", false: "marked read"}[it.Read]
				}
			}
		}
		return m, nil
	case "+", "=":
		m.setAllFolds(true)
		return m, nil
	case "-":
		m.setAllFolds(false)
		return m, nil
	}
	m.clamp()
	return m, nil
}

// visibleFocus returns the first (dir > 0) or last (dir < 0) focusable whose
// line is on screen, or 0.
func (m *Model) visibleFocus(dir int) int {
	n := len(m.cur.layout.Focus)
	for i := 1; i <= n; i++ {
		j := i
		if dir < 0 {
			j = n - i + 1
		}
		if m.onScreen(j) {
			return j
		}
	}
	return 0
}

func (m *Model) focusFirstVisible() {
	if m.cur != nil && m.cur.layout != nil {
		m.cur.focus = m.visibleFocus(1)
	}
}

// lynxMove implements Lynx-style arrow navigation: up/down jump between links
// and sections. When the next one is not on screen the page scrolls instead,
// so text between links is never skipped.
func (m *Model) lynxMove(dir int) {
	p := m.cur
	n := len(p.layout.Focus)
	cand := 0
	if p.focus > 0 && m.onScreen(p.focus) {
		cand = p.focus + dir
	} else {
		cand = m.visibleFocus(dir)
	}
	if cand >= 1 && cand <= n && m.onScreen(cand) {
		p.focus = cand
		return
	}
	// Nothing further on this screen: scroll one page in that direction.
	before := p.offset
	p.offset += dir * (m.bodyHeight() - 2)
	m.clamp()
	if p.offset == before {
		if dir > 0 {
			m.status = "end of page"
		} else {
			m.status = "top of page"
		}
		return
	}
	switch {
	case cand >= 1 && cand <= n && m.onScreen(cand):
		p.focus = cand
	case p.focus > 0 && m.onScreen(p.focus):
		// keep the current focus if it is still visible
	default:
		p.focus = m.visibleFocus(dir)
	}
}

func (m *Model) focused() *render.Focusable {
	p := m.cur
	if p.focus < 1 || p.focus > len(p.layout.Focus) {
		return nil
	}
	return &p.layout.Focus[p.focus-1]
}

// moveFocus moves to the next/previous focusable, preferring ones on screen.
func (m *Model) moveFocus(dir int) {
	p := m.cur
	n := len(p.layout.Focus)
	if n == 0 {
		m.status = "nothing to focus on this page"
		return
	}
	top, bottom := p.offset, p.offset+m.bodyHeight()-1
	next := 0
	if p.focus == 0 || !m.onScreen(p.focus) {
		// Start from the first (or last) focusable visible on screen.
		for i := 1; i <= n; i++ {
			j := i
			if dir < 0 {
				j = n - i + 1
			}
			if l := p.layout.Focus[j-1].Line; l >= top && l <= bottom {
				next = j
				break
			}
		}
		if next == 0 {
			for i := 1; i <= n; i++ {
				j := i
				if dir < 0 {
					j = n - i + 1
				}
				l := p.layout.Focus[j-1].Line
				if (dir > 0 && l > bottom) || (dir < 0 && l < top) {
					next = j
					break
				}
			}
		}
	} else {
		next = p.focus + dir
	}
	if next < 1 || next > n {
		return
	}
	p.focus = next
	m.reveal(p.layout.Focus[next-1].Line)
}

func (m *Model) onScreen(fi int) bool {
	l := m.cur.layout.Focus[fi-1].Line
	return l >= m.cur.offset && l < m.cur.offset+m.bodyHeight()
}

func (m *Model) reveal(line int) {
	p := m.cur
	h := m.bodyHeight()
	if line < p.offset {
		p.offset = line
	} else if line >= p.offset+h {
		p.offset = line - h + 3
	}
	m.clamp()
}

func (m Model) activate(fi int) (tea.Model, tea.Cmd) {
	p := m.cur
	if fi < 1 || fi > len(p.layout.Focus) {
		return m, nil
	}
	f := p.layout.Focus[fi-1]
	switch f.Kind {
	case render.FocusFold:
		screenRow := f.Line - p.offset
		p.open[f.Fold] = !m.isOpen(f.Fold)
		m.relayout()
		// Keep the toggled label on the same screen row and focused.
		for i, nf := range p.layout.Focus {
			if nf.Kind == render.FocusFold && nf.Fold == f.Fold {
				p.focus = i + 1
				p.offset = nf.Line - screenRow
				break
			}
		}
		m.clamp()
		return m, nil
	case render.FocusLink:
		return m.follow(p.doc.Links[f.Link-1].Href)
	}
	return m, nil
}

func (m *Model) isOpen(id int) bool {
	if v, ok := m.cur.open[id]; ok {
		return v
	}
	open := false
	var visit func([]doc.Block)
	visit = func(bs []doc.Block) {
		for _, b := range bs {
			switch b := b.(type) {
			case doc.Collapsible:
				if b.ID == id {
					open = b.Open
					return
				}
				visit(b.Blocks)
			case doc.Quote:
				visit(b.Blocks)
			case doc.Columns:
				for _, col := range b.Cols {
					visit(col)
				}
			case doc.List:
				for _, it := range b.Items {
					visit(it)
				}
			}
		}
	}
	visit(m.cur.doc.Blocks)
	return open
}

func (m *Model) setAllFolds(open bool) {
	for i := 1; i <= m.cur.doc.Collapsibles; i++ {
		m.cur.open[i] = open
	}
	m.relayout()
	if open {
		m.status = "all sections expanded"
	} else {
		m.status = "all sections folded"
	}
}

func (m Model) follow(href string) (tea.Model, tea.Cmd) {
	u, err := url.Parse(href)
	if err != nil {
		m.status = "bad link: " + href
		return m, nil
	}
	switch u.Scheme {
	case "http", "https", "gemini", "gopher":
		m.loading = href
		return m, load(href, false)
	case "w5f":
		if href == "w5f:themes" {
			return m.openThemes(false)
		}
		if d := m.localDoc(href); d != nil && (m.cur == nil || w5fPage(m.cur)) {
			return m, func() tea.Msg { return loadedMsg{target: href, doc: d} }
		}
		if name, ok := strings.CutPrefix(href, "w5f:theme/"); ok && m.cur != nil && m.cur.target == "w5f:themes" {
			return m.setTheme(name)
		}
		if m.cur != nil && !w5fPage(m.cur) && fromNetwork(m.cur.target) {
			m.status = "W5F addresses are not opened from web pages (use g to go there yourself)"
			return m, nil
		}
		if open, ok := formFor(href); ok {
			if m.cur != nil && !w5fPage(m.cur) {
				m.status = "W5F forms open only from W5F pages"
				return m, nil
			}
			return m.openForm(open, href)
		}
		// Notes, clippings and saved pages hold text from the web: their
		// links may navigate, but never run W5F actions.
		if m.cur != nil && !w5fPage(m.cur) && !navigationW5F(href) {
			m.status = "W5F actions are not run from files (use g to go there yourself)"
			return m, nil
		}
		m.loading = href
		if strings.Contains(href, "sync") {
			m.loading = "syncing feeds (this can take a minute)"
		}
		if strings.HasPrefix(href, "w5f:books/get/") {
			m.loading = "downloading the book into your library"
		}
		if t := catalogLoading(href); t != "" {
			m.loading = t
		}
		// The Gaming Table's own changes redraw it in place.
		if m.cur != nil && m.cur.doc.URL == "w5f:solo" && soloEdit(href) {
			return m, load(href, true)
		}
		return m, load(href, false)
	case "file":
		path := u.Path
		if len(path) > 2 && path[0] == '/' && path[2] == ':' { // /C:/... on Windows
			path = path[1:]
		}
		m.loading = path
		return m, load(path, false)
	}
	m.status = "cannot open " + u.Scheme + ": links yet — " + href
	return m, nil
}

// itemRef returns the feed item id a document shows, if any.
func itemRef(d *doc.Document) (int64, bool) {
	if d == nil || !strings.HasPrefix(d.Ref, "item:") {
		return 0, false
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(d.Ref, "item:"), 10, 64)
	return id, err == nil
}

// refreshLocal reloads Periodicals list pages (and the Reading Room and the
// desk) after navigating back to them, so read/star markers are current. Local pages are cheap to rebuild.
func (m *Model) refreshLocal() tea.Cmd {
	if m.cur != nil && m.cur.target == "w5f:books" {
		return load(m.cur.target, true)
	}
	if m.cur != nil && (m.cur.target == "w5f:welcome" || m.cur.target == "w5f:desk") {
		// The desk may have been cleared since.
		t := m.cur.target
		d := m.localDoc(t)
		return func() tea.Msg { return loadedMsg{target: t, doc: d, replace: true} }
	}
	if m.cur == nil || !strings.HasPrefix(m.cur.target, "w5f:feeds") || strings.Contains(m.cur.target, "sync") ||
		strings.Contains(m.cur.target, "/read/") {
		return nil
	}
	return load(m.cur.target, true)
}

// leavePage remembers how far the current page was read (books keep their
// chapter progress; every content page its history position).
func (m *Model) leavePage() {
	if m.cur == nil || m.cur.layout == nil {
		return
	}
	frac := 0.0
	if n := len(m.cur.layout.Lines); n > 0 {
		frac = float64(m.cur.offset) / float64(n)
		if m.cur.offset+m.bodyHeight() >= n {
			frac = 0.999 // read to the end
		}
	}
	db, err := store.Default()
	if err != nil {
		return
	}
	if strings.HasPrefix(m.cur.doc.Ref, "book:") {
		books.SaveFromRef(db, m.cur.doc.Ref, frac)
	}
	if strings.HasPrefix(m.cur.doc.Ref, "serial:") {
		fiction.SaveFromRef(db, m.cur.doc.Ref, frac)
	}
	// A page left at the top keeps its earlier position (a quick look must
	// not erase "Continue reading").
	if kind, key := index.Kind(m.cur.target, m.cur.doc); kind != "" && frac > 0 {
		_ = db.SavePos(key, frac)
	}
}

func (m *Model) goBack() {
	m.leavePage()
	if len(m.back) == 0 {
		m.status = "no earlier page"
		return
	}
	m.forward = append(m.forward, m.cur)
	m.cur = m.back[len(m.back)-1]
	m.back = m.back[:len(m.back)-1]
	m.relayout()
}

func (m *Model) goForward() {
	m.leavePage()
	if len(m.forward) == 0 {
		m.status = "no later page"
		return
	}
	m.back = append(m.back, m.cur)
	m.cur = m.forward[len(m.forward)-1]
	m.forward = m.forward[:len(m.forward)-1]
	m.relayout()
}

// --- go to address (g) ---

func (m Model) gotoKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		m.mode = modeRead
	case "enter":
		m.mode = modeRead
		switch strings.ToLower(strings.TrimSpace(m.gotoBuf)) {
		case "reddit-login", "reddit login":
			m.mode, m.secretBuf, m.secretAO3 = modeSecret, "", false
			return m, nil
		case "ao3-login", "ao3 login":
			m.mode, m.secretBuf, m.secretAO3 = modeSecret, "", true
			return m, nil
		case "ao3-logout", "ao3 logout":
			if err := deleteAO3Session(); err != nil {
				m.status = "error: " + err.Error()
			} else {
				m.status = "AO3 session removed from this computer."
			}
			return m, nil
		case "reddit-login browser", "ao3-login browser", "reddit-login chromium", "ao3-login chromium", "reddit-login firefox", "ao3-login firefox":
			site, from, _ := source.SessionFrom(m.gotoBuf)
			if msg, err := source.ImportSession(site, from); err != nil {
				m.status = "error: " + err.Error()
			} else {
				m.status = msg
			}
			return m, nil
		case "dict-install", "dict install":
			m.loading = "installing the English–Turkish dictionary (16 MB download)"
			return m, installDict(dict.DefaultURL)
		case "reddit-logout", "reddit logout":
			if err := deleteSession(); err != nil {
				m.status = "error: " + err.Error()
			} else {
				m.status = "Reddit session removed from this computer."
			}
			return m, nil
		}
		if low := strings.ToLower(strings.TrimSpace(m.gotoBuf)); low == "theme" || low == "themes" {
			m.mode = modeRead
			return m.openThemes(false)
		} else if name, ok := strings.CutPrefix(low, "theme "); ok {
			m.mode = modeRead
			return m.setTheme(name)
		}
		if word, ok := browserCommand(m.gotoBuf); ok {
			// g → browser [address] (or the older g → chromium): a page in
			// the browser (no address: this one).
			addr := strings.TrimSpace(strings.TrimSpace(m.gotoBuf)[len(word):])
			if addr == "" && m.cur != nil {
				addr = m.cur.target
				if !strings.HasPrefix(addr, "http") && m.cur.doc != nil {
					addr = m.cur.doc.URL
				}
			} else if addr != "" && !strings.Contains(addr, "://") {
				addr = "https://" + addr
			}
			if err := browser.Open(addr); err != nil {
				m.status = "error: " + err.Error()
			} else {
				m.status = "Opened in " + browser.Name() + ": " + addr
			}
			return m, nil
		}
		target := source.Resolve(m.gotoBuf)
		if m.cur != nil && m.cur.doc != nil {
			// "f words 1000-" etc. on an AO3 list: its typed filters.
			if t := fiction.FilterCommand(m.cur.doc.URL, m.gotoBuf); t != "" {
				target = t
			}
			// "? text" on a Gemini or Gopher page that asks for input.
			if t := smallweb.InputCommand(m.cur.doc.URL, m.gotoBuf); t != "" {
				target = t
			}
		}
		if target == "" {
			return m, nil
		}
		if low := strings.ToLower(strings.TrimSpace(m.gotoBuf)); low == "cabinet" || low == "reading room" || low == "home" {
			target = map[string]string{"cabinet": "w5f:cabinet", "reading room": "w5f:welcome", "home": "w5f:welcome"}[low]
		}
		if d := m.localDoc(target); d != nil {
			m.mode = modeRead
			return m, func() tea.Msg { return loadedMsg{target: target, doc: d} }
		}
		m.loading = target
		if t := catalogLoading(target); t != "" {
			m.loading = t
		}
		return m, load(target, false)
	case "backspace":
		if r := []rune(m.gotoBuf); len(r) > 0 {
			m.gotoBuf = string(r[:len(r)-1])
		}
	case "ctrl+u":
		m.gotoBuf = ""
	default:
		if k.Text != "" {
			m.gotoBuf += k.Text
		}
	}
	return m, nil
}

// --- hidden input for the Reddit session (g → reddit-login) ---

func (m Model) secretKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.form != nil {
		return m.formKey(k)
	}
	switch k.String() {
	case "esc":
		m.mode, m.secretBuf = modeRead, ""
		m.status = m.secretSite() + " login cancelled."
	case "enter":
		v := m.secretBuf
		m.mode, m.secretBuf = modeRead, ""
		if strings.TrimSpace(v) == "" {
			m.status = "Nothing entered; " + m.secretSite() + " login cancelled."
			return m, nil
		}
		if m.secretAO3 {
			if err := saveAO3Session(v); err != nil {
				m.status = "error: could not save the AO3 session (" + err.Error() + ")"
				return m, nil
			}
			m.status = "AO3 connected. g → fiction → My AO3 shows your bookmarks and subscriptions."
			return m, nil
		}
		if err := saveSession(v); err != nil {
			m.status = "error: could not save the Reddit session (" + err.Error() + ")"
			return m, nil
		}
		m.status = "Reddit connected. It stays connected on every start."
		if m.cur != nil && strings.Contains(m.cur.target, "reddit.com") {
			m.loading = m.cur.target
			return m, load(m.cur.target, true)
		}
	case "backspace":
		if r := []rune(m.secretBuf); len(r) > 0 {
			m.secretBuf = string(r[:len(r)-1])
		}
	case "ctrl+u":
		m.secretBuf = ""
	default:
		if k.Text != "" {
			m.secretBuf += k.Text
		}
	}
	return m, nil
}

// --- link hints (f) ---

func (m *Model) startHints() {
	p := m.cur
	m.hints = map[int]int{}
	label := 1
	for i := range p.layout.Focus {
		if m.onScreen(i + 1) {
			m.hints[label] = i + 1
			label++
		}
	}
	if len(m.hints) == 0 {
		m.status = "no links or sections on screen"
		return
	}
	m.mode, m.hintBuf = modeHints, ""
}

func (m Model) hintKey(s string) (tea.Model, tea.Cmd) {
	switch {
	case s == "esc" || s == "q":
		m.mode = modeRead
		return m, nil
	case s == "backspace":
		if len(m.hintBuf) > 0 {
			m.hintBuf = m.hintBuf[:len(m.hintBuf)-1]
		}
		return m, nil
	case s == "enter":
		n, _ := strconv.Atoi(m.hintBuf)
		m.mode = modeRead
		if fi, ok := m.hints[n]; ok {
			m.cur.focus = fi
			return m.activate(fi)
		}
		return m, nil
	case len(s) == 1 && s[0] >= '0' && s[0] <= '9':
		m.hintBuf += s
		n, _ := strconv.Atoi(m.hintBuf)
		// Activate immediately when no longer label could start with this prefix.
		if fi, ok := m.hints[n]; ok && n*10 > len(m.hints) {
			m.mode = modeRead
			m.cur.focus = fi
			return m.activate(fi)
		}
		return m, nil
	}
	return m, nil
}

// --- table of contents (t) ---

func (m Model) tocKey(s string) (tea.Model, tea.Cmd) {
	hs := m.cur.layout.Headings
	switch s {
	case "esc", "q", "t":
		m.mode = modeRead
	case "j", "down", "tab":
		if m.tocCursor < len(hs)-1 {
			m.tocCursor++
		}
	case "k", "up", "shift+tab":
		if m.tocCursor > 0 {
			m.tocCursor--
		}
	case "enter":
		m.cur.offset = hs[m.tocCursor].Line
		m.clamp()
		m.mode = modeRead
	}
	return m, nil
}

// --- view ---

// View implements tea.Model.
func (m Model) View() tea.View {
	if m.cur != nil && m.cur.layout == nil {
		// Defensive: never draw a page that has not been laid out yet.
		cp := *m.cur
		m.cur = &cp
		m.relayout()
	}
	var b strings.Builder
	b.WriteString(m.topBar())
	b.WriteByte('\n')
	body := m.bodyLines()
	var side []string
	if m.sideShown() {
		side = m.sideLines(m.bodyHeight())
	}
	for i := 0; i < m.bodyHeight(); i++ {
		if side != nil {
			if i < len(side) {
				b.WriteString(side[i])
			}
			b.WriteString(m.sideSeparator())
		}
		if i < len(body) {
			b.WriteString(body[i])
		}
		b.WriteByte('\n')
	}
	b.WriteString(m.bottomBar())
	v := tea.NewView(b.String())
	v.AltScreen = true
	v.BackgroundColor = m.theme.Page.BG
	v.ForegroundColor = m.theme.Page.FG
	title := "W5F"
	if m.cur != nil && m.cur.doc.Title != "" {
		title = m.cur.doc.Title + " — W5F"
	}
	v.WindowTitle = title
	return v
}

func (m Model) margin() string {
	if m.sideShown() {
		return "  " // the page starts beside the side menu
	}
	pad := (m.width - m.textWidth()) / 2
	if pad < 0 {
		pad = 0
	}
	return strings.Repeat(" ", pad)
}

func (m Model) bodyLines() []string {
	if m.cur == nil {
		return []string{m.margin() + "loading " + m.loading + " …"}
	}
	switch m.mode {
	case modeTOC:
		return m.tocLines()
	case modeHelp:
		return m.helpLines()
	case modeSecret:
		return m.secretLines()
	case modeDict:
		read := m
		read.mode = modeRead
		return m.overlayDict(read.bodyLines())
	case modeNote:
		read := m
		read.mode = modeRead
		return m.overlayNote(read.bodyLines())
	}
	p := m.cur
	margin := m.margin()
	var out []string
	end := p.offset + m.bodyHeight()
	if end > len(p.layout.Lines) {
		end = len(p.layout.Lines)
	}
	labelFor := map[int]int{}
	if m.mode == modeHints {
		for label, fi := range m.hints {
			labelFor[fi] = label
		}
	}
	selLo, selHi := -1, -1
	if lo, hi := m.clip.span(); m.mode == modeClip && hi < len(p.layout.Paras) {
		selLo, selHi = p.layout.Paras[lo].Start, p.layout.Paras[hi].End-1
	}
	for li, ln := range p.layout.Lines[p.offset:end] {
		var sb strings.Builder
		lead := margin
		for _, sg := range ln.Segs {
			if p.focus != 0 && sg.Focus == p.focus && len(margin) >= 2 {
				// A mark beside the selected line, whatever the colours.
				lead = margin[:len(margin)-2] + m.theme.Seg(render.Seg{Role: render.Title}, false).Render("»") + " "
				break
			}
		}
		sb.WriteString(lead)
		for _, sg := range ln.Segs {
			if label, ok := labelFor[sg.Focus]; ok && sg.Focus > 0 {
				sb.WriteString(m.theme.Hint().Render(strconv.Itoa(label)))
				delete(labelFor, sg.Focus)
			}
			sel := p.offset+li >= selLo && p.offset+li <= selHi
			sb.WriteString(m.theme.Seg(sg, sel || (sg.Focus != 0 && sg.Focus == p.focus)).Render(sg.Text))
		}
		out = append(out, ansi.Truncate(sb.String(), m.pageWidth(), ""))
	}
	return out
}

func (m Model) secretLines() []string {
	if m.form != nil {
		return m.formLines()
	}
	margin := m.margin()
	st := func(r render.Role) func(string) string {
		return func(s string) string { return m.theme.Seg(render.Seg{Role: r}, false).Render(s) }
	}
	title, body, dim := st(render.Title), st(render.Body), st(render.Dim)
	mask := strings.Repeat("•", len([]rune(m.secretBuf)))
	if len([]rune(mask)) > m.textWidth()-4 {
		mask = "…" + string([]rune(mask)[len([]rune(mask))-(m.textWidth()-5):])
	}
	head, cookie, path := "CONNECT REDDIT", "reddit_session", reddit.SessionPath()
	if m.secretAO3 {
		head, cookie, path = "CONNECT AO3", "_otwarchive_session", fiction.AO3SessionPath()
	}
	return []string{
		margin + title(head),
		"",
		margin + body("Paste (or type) the value of your "+cookie+" cookie,"),
		margin + body("then press enter. Input stays hidden."),
		"",
		margin + m.theme.Seg(render.Seg{Role: render.Fold}, false).Render("› ") + body(mask) + body("_"),
		"",
		margin + dim(fmt.Sprintf("%d characters · esc cancels · ctrl+u clears", len([]rune(m.secretBuf)))),
		margin + dim("Stored only on this computer: "+path),
		margin + dim("Signed in to "+m.secretSite()+" in Chromium or Firefox? esc, then g → "+strings.ToLower(m.secretSite())+"-login browser"),
	}
}

func (m Model) secretSite() string {
	if m.secretAO3 {
		return "AO3"
	}
	return "Reddit"
}

func (m Model) tocLines() []string {
	margin := m.margin()
	out := []string{margin + m.theme.Seg(render.Seg{Role: render.Title}, false).Render("CONTENTS"), ""}
	for i, h := range m.cur.layout.Headings {
		line := strings.Repeat("  ", h.Level-1) + h.Text
		out = append(out, margin+m.theme.Seg(render.Seg{Role: render.Body}, i == m.tocCursor).Render(line))
	}
	return out
}

func (m Model) helpLines() []string {
	margin := m.margin()
	keys := [][2]string{
		{"↑ / ↓", "previous / next link or section (scrolls when needed)"},
		{"→ / enter", "open link · fold/unfold section"},
		{"←", "back"}, {"l", "forward again"},
		{"space / b", "page down / up"}, {"home / end", "top / bottom"},
		{"g", "go to: address, scp-173, w <wikipedia>, scp <wiki search>, or any words to search the web"},
		{"r", "random page from this wiki (SCP by default)"},
		{"t", "table of contents (in a book: the book's chapters)"},
		{"] / [", "next / previous chapter (books) or page"}, {"f", "link hints: type the number"},
		{"d", "dictionary (English → Turkish) over the page · esc closes"},
		{"* / m", "star · mark read/unread (feed items)"},
		{"F", "follow / unfollow this serial or Reddit series (g → fiction, g → following)"},
		{"x / p", "deep random (eight families of sources) · today's Daily Packet"},
		{"enter / g → ? text", "answer a page that asks for input (g → smallweb, g → worlds)"},
		{"+ / -", "expand / fold all sections"}, {"o", "show link address"}, {"ctrl+r", "reload"},
		{"B", "open this page (or the selected link) in the browser · g → browser <address>"},
		{"T · I", "draw a tarot card · cast an I Ching hexagram (kept: the texts come once from sacred-texts)"},
		{"1 … 9, 0", "the rooms of the library: 1 Reading Room (home) · 2 Periodical Gallery (periodicals) · 3 The Stacks (books) · 4 The Serial Hall (internet fiction) · 5 The Picture Vault (comics) · 6 The Gaming Table (solo RPG) · 7 The Newsroom (Usenet) · 8 Curiosity Cabinet (discovery) · 9 The Lectern (queue) · 0 The Scriptorium (notes)"},
		{"H · L", "The Register (your history) · Ultan's Ledger (your reading, counted)"},
		{`\`, "hide / show the side menu (wide windows)"},
		{"g → theme", "choose a theme: amber, day, cold, night, green (g → theme day puts one on)"},
		{"g → reddit-login browser", "take your Reddit (or ao3-login browser: AO3) session from Chromium or Firefox, where you signed in"},
		{"/", "search everything you have read (feeds, wikis, web pages, books, notes)"},
		{"a / A", "add this page / the selected link to the reading queue (g → queue)"},
		{"n", "write a note about this page"}, {"y", "clip paragraphs (↑↓ choose, shift+↑↓ extend, enter save)"},
		{"s", "save a Markdown copy of this page"}, {"H", "history (also g → history, g → notes)"},
		{"j / k", "scroll one line (vim style)"}, {"?", "this help"}, {"q", "quit"},
	}
	out := []string{margin + m.theme.Seg(render.Seg{Role: render.Title}, false).Render("KEYS"), ""}
	for _, k := range keys {
		out = append(out, margin+m.theme.Seg(render.Seg{Role: render.Fold}, false).Render(fmt.Sprintf("%-18s", k[0]))+
			m.theme.Seg(render.Seg{Role: render.Body}, false).Render(k[1]))
	}
	out = append(out, "", margin+m.theme.Seg(render.Seg{Role: render.Dim}, false).Render("press any key to return"))
	return out
}

func (m Model) topBar() string {
	left := " W5F // ARCHIVE NODE "
	right := ""
	if m.cur != nil {
		lines := len(m.cur.layout.Lines)
		pct := 100
		if max := lines - m.bodyHeight(); max > 0 {
			pct = m.cur.offset * 100 / max
		}
		right = fmt.Sprintf(" %s  %3d%% ", strings.ToUpper(m.cur.doc.Origin), pct)
	}
	mid := ""
	if m.cur != nil {
		mid = m.cur.doc.Title
	}
	space := m.width - ansi.StringWidth(left) - ansi.StringWidth(right)
	if space < 0 {
		space = 0
	}
	mid = ansi.Truncate(mid, space-2, "…")
	pad := space - ansi.StringWidth(mid)
	return m.theme.Bar().Bold(true).Render(left) +
		m.theme.BarDim().Render(strings.Repeat(" ", pad/2)+mid+strings.Repeat(" ", pad-pad/2)) +
		m.theme.Bar().Render(right)
}

func (m Model) bottomBar() string {
	var text string
	switch {
	case m.loading != "":
		text = " loading " + m.loading + " …"
		if p, _ := sitecat.CurrentProgress.Load().(string); p != "" {
			text = " " + p + " …   esc cancels"
		}
	case m.mode == modeDict:
		text = " dictionary · type a word · enter: meaning · esc: back to reading"
	case m.mode == modeSecret:
		text = " reddit login · paste the cookie value · enter: save · esc: cancel"
	case m.mode == modeGoto:
		text = " go to › " + m.gotoBuf + "_   (address · scp-173 · w wikipedia · scp wikis · words = web search)"
	case m.mode == modeHints:
		text = " link › " + m.hintBuf + "_   (enter: open · esc: cancel)"
	case m.mode == modeTOC:
		text = " contents · j/k move · enter jump · esc close"
	case m.mode == modeClip:
		text = " clip · ↑↓ paragraph · shift+↑↓ extend · enter save · esc cancel"
	case m.mode == modeNote:
		text = " note · type · enter new line · ctrl+s save · esc discard"
	case m.mode == modeFind:
		text = " search › " + m.findBuf + "_   (words · \"exact phrase\" · enter: search everything you read · esc: cancel)"
	case m.status == "" && progressText() != "":
		text = " " + progressText()
	case m.status != "":
		text = " " + m.status
	default:
		return m.hintBar() // the keys of the room the reader is in
	}
	text = ansi.Truncate(text, m.width, "…")
	if pad := m.width - ansi.StringWidth(text); pad > 0 {
		text += strings.Repeat(" ", pad)
	}
	if m.status != "" && strings.HasPrefix(m.status, "error") {
		return m.theme.Bar().Foreground(m.theme.Chrome.Warn).Render(text)
	}
	return m.theme.BarDim().Render(text)
}

// soloEdit reports the Gaming Table's changes to its own lists.
func soloEdit(href string) bool {
	for _, p := range []string{"w5f:solo/character/", "w5f:solo/thread/", "w5f:solo/counter/", "w5f:solo/pick/"} {
		if strings.HasPrefix(href, p) {
			return true
		}
	}
	return false
}

// browserCommand reports g → browser [address], or the older chromium;
// "browser wars" (no address after it) stays a web search.
func browserCommand(buf string) (string, bool) {
	low := strings.ToLower(strings.TrimSpace(buf))
	for _, w := range []string{"browser", "chromium"} {
		if low == w {
			return w, true
		}
		if rest, ok := strings.CutPrefix(low, w+" "); ok {
			rest = strings.TrimSpace(rest)
			if !strings.Contains(rest, " ") && (strings.Contains(rest, ".") || strings.Contains(rest, "://")) {
				return w, true
			}
		}
	}
	return "", false
}
