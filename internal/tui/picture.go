package tui

import (
	"context"
	"fmt"
	"image/color"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"w5f/internal/picture"
	"w5f/internal/render"
	"w5f/internal/source"
)

// Pictures in the reader (P4, the owner, 2026-10-30): a plate's pictures
// are drawn in its page in half blocks, in one tone of the theme, dithered;
// z shows the picture on screen large (in sixel where the terminal has it,
// the laptop's xterm with decGraphicsID), and o there opens it in its own
// colours in the full-screen viewer.

// picCacheSize is how many decoded pictures are kept (a megabyte at most
// each, in grey).
const picCacheSize = 12

// picStore keeps the decoded pictures, shared by the model's copies.
type picStore struct {
	mu     sync.Mutex
	pics   map[string]*picEntry
	order  []string        // oldest first
	wanted map[string]bool // asked for by a layout, not yet fetched
}

type picEntry struct {
	pic     *picture.Picture
	err     error
	loading bool
}

func newPicStore() *picStore {
	return &picStore{pics: map[string]*picEntry{}, wanted: map[string]bool{}}
}

// get is a decoded picture; ok is false when it has not been fetched (and
// then it is wanted), pic nil when it could not be.
func (s *picStore) get(src string) (pic *picture.Picture, ok bool) {
	if s == nil {
		return nil, true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.pics[src]
	if e == nil {
		s.wanted[src] = true
		return nil, false
	}
	if e.loading {
		return nil, false
	}
	return e.pic, true
}

// fetches are the commands that fetch the wanted pictures.
func (s *picStore) fetches() tea.Cmd {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var cmds []tea.Cmd
	for src := range s.wanted {
		s.pics[src] = &picEntry{loading: true}
		cmds = append(cmds, fetchPicture(src))
	}
	clear(s.wanted)
	return tea.Batch(cmds...)
}

// put keeps a fetched picture, dropping the oldest beyond picCacheSize.
func (s *picStore) put(src string, pic *picture.Picture, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pics[src] = &picEntry{pic: pic, err: err}
	s.order = append(s.order, src)
	for len(s.order) > picCacheSize {
		delete(s.pics, s.order[0])
		s.order = s.order[1:]
	}
}

// fetchPicture fetches and decodes a picture; the store is told by the
// model, which keeps it.
var fetchPicture = func(src string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		b, err := source.Picture(ctx, src)
		var pic *picture.Picture
		if err == nil {
			pic, err = picture.Decode(b)
		}
		return pictureLoaded{src, pic, err}
	}
}

type pictureLoaded struct {
	src string
	pic *picture.Picture
	err error
}

// gfx is what the terminal draws: sixel, and its cell size in pixels.
type gfx struct {
	sixel        bool
	cellW, cellH int
}

// gfxQuery asks the terminal what it is (DA1: 4 is sixel) and how large its
// window is in pixels (XTSMGRAPHICS), for the size of its cells.
const gfxQuery = "\x1b[c\x1b[?2;1;0S"

// sixelMax is the most xterm draws in sixel on a side (its maxGraphicSize).
const sixelMax = 1000

// gfxEvent reads the terminal's answers to gfxQuery.
func (m *Model) gfxEvent(msg tea.Msg) bool {
	switch e := msg.(type) {
	case uv.PrimaryDeviceAttributesEvent:
		for _, a := range e {
			if a == 4 {
				m.gfx.sixel = true
			}
		}
		return true
	case uv.UnknownCsiEvent:
		// CSI ? 2 ; 0 ; width ; height S: the graphics area, the window.
		s := string(e)
		if !strings.HasSuffix(s, "S") || !strings.Contains(s, "?2;0;") {
			return false
		}
		f := strings.Split(strings.TrimSuffix(s[strings.Index(s, "?2;0;")+5:], "S"), ";")
		if len(f) == 2 && m.width > 0 && m.height > 0 {
			w, e1 := strconv.Atoi(f[0])
			h, e2 := strconv.Atoi(f[1])
			if e1 == nil && e2 == nil && w > 0 && h > 0 {
				// xterm answers at most its largest sixel (1000x1000): a
				// side cut to that says nothing, and cells are twice as
				// high as wide.
				cw, ch := w/m.width, h/m.height
				switch {
				case w >= sixelMax && h >= sixelMax:
					cw, ch = 0, 0
				case w >= sixelMax:
					cw = ch / 2
				case h >= sixelMax:
					ch = cw * 2
				}
				m.gfx.cellW, m.gfx.cellH = cw, ch
			}
		}
		return true
	}
	return false
}

// cell is the terminal's cell size in pixels (8x16, the laptop's Terminus,
// when it did not say).
func (g gfx) cell() (int, int) {
	if g.cellW < 4 || g.cellH < 8 {
		return 8, 16
	}
	return g.cellW, g.cellH
}

// tones are the theme's tone for pictures: the darker and the lighter of
// the page's ground and the chrome's ink (amber on the amber theme).
func (m Model) tones() (dark, light color.Color) {
	a, b := m.theme.Page.BG, m.theme.Chrome.FG
	if luma(a) > luma(b) {
		a, b = b, a
	}
	return a, b
}

func luma(c color.Color) uint32 {
	r, g, b, _ := c.RGBA()
	return 299*r + 587*g + 114*b
}

// pictureSize is the layout's question of how large a picture is drawn: on
// a page that draws its pictures, as wide as the column and at most half a
// screen high, so its label shows under it (z shows it large); elsewhere
// not at all.
func (m *Model) pictureSize(p *page) func(string, int) (int, int) {
	if p.doc == nil || !p.doc.Pictures || m.pics == nil {
		return nil
	}
	maxRows := max(m.bodyHeight()/2, 4)
	return func(src string, width int) (int, int) {
		pic, ok := m.pics.get(src)
		if !ok || pic == nil {
			return 0, 0
		}
		return pic.Fit(width, maxRows)
	}
}

// drawPictures sets the half-block lines of the pictures the layout drew.
func (m *Model) drawPictures(p *page) {
	p.picLines = nil
	for _, pp := range p.layout.Pictures {
		if pp.Rows == 0 {
			continue
		}
		pic, ok := m.pics.get(pp.Src)
		if !ok || pic == nil {
			continue
		}
		if p.picLines == nil {
			p.picLines = map[int]string{}
		}
		dark, light := m.tones()
		for i, row := range pic.HalfBlock(pp.Cols, pp.Rows, dark, light) {
			p.picLines[pp.Line+i] = strings.Repeat(" ", pp.Indent) + row
		}
	}
}

// pictureArrived keeps a fetched picture and lays the page out again.
func (m Model) pictureArrived(msg pictureLoaded) (tea.Model, tea.Cmd) {
	if m.pics == nil {
		return m, nil
	}
	m.pics.put(msg.src, msg.pic, msg.err)
	if m.cur != nil {
		m.relayout()
	}
	if m.mode == modePicture {
		if pp, ok := m.viewedPicture(); ok && pp.Src == msg.src {
			if msg.err != nil {
				m.status = "the picture did not open: " + msg.err.Error()
			}
			return m, m.sixelLater()
		}
	}
	return m, nil
}

// --- z: the picture large.

// pictureOnScreen is the first picture on screen, or the next one below it
// (or the last one above), as an index into the layout's pictures.
func (m Model) pictureOnScreen() int {
	p := m.cur
	if p == nil || p.layout == nil || len(p.layout.Pictures) == 0 {
		return -1
	}
	for i, pp := range p.layout.Pictures {
		if pp.Line+max(pp.Rows, 1) > p.offset {
			return i
		}
	}
	return len(p.layout.Pictures) - 1
}

func (m Model) viewedPicture() (render.Picture, bool) {
	if m.cur == nil || m.cur.layout == nil || m.picView < 0 || m.picView >= len(m.cur.layout.Pictures) {
		return render.Picture{}, false
	}
	return m.cur.layout.Pictures[m.picView], true
}

// openPicture shows the picture on screen large.
func (m Model) openPicture() (tea.Model, tea.Cmd) {
	i := m.pictureOnScreen()
	if i < 0 {
		m.status = "no picture on this page"
		return m, nil
	}
	pp := m.cur.layout.Pictures[i]
	if pp.Src == "" {
		m.status = "this picture has no address"
		return m, nil
	}
	m.mode, m.picView = modePicture, i
	m.pics.get(pp.Src) // fetch it if it is not here
	return m, tea.Batch(m.pics.fetches(), m.sixelLater())
}

// sixelMsg draws the picture in sixel once the screen under it is blank.
type sixelMsg struct{ gen int }

// sixelLater waits a frame, so the picture lands on the blank screen and
// is not drawn over.
func (m *Model) sixelLater() tea.Cmd {
	if !m.gfx.sixel {
		return nil
	}
	m.sixelGen++
	gen := m.sixelGen
	return tea.Tick(80*time.Millisecond, func(time.Time) tea.Msg { return sixelMsg{gen} })
}

// pictureBox is the room the picture has in the large view: the body less
// a line for its caption, in cells.
func (m Model) pictureBox() (cols, rows int) {
	return max(m.width-2, 10), max(m.bodyHeight()-2, 4)
}

// sixel is the picture drawn at its place in the large view.
func (m Model) sixel() string {
	pp, ok := m.viewedPicture()
	if !ok {
		return ""
	}
	pic, ok := m.pics.get(pp.Src)
	if !ok || pic == nil {
		return ""
	}
	cw, ch := m.gfx.cell()
	cols, rows := m.pictureBox()
	bw, bh := min(cols*cw, sixelMax), min(rows*ch, sixelMax)
	w, h := pic.Size()
	// The largest size of the picture's shape in the box.
	pw, ph := bw, bw*h/w
	if ph > bh {
		pw, ph = bh*w/h, bh
	}
	// Whole bands of six pixels, so nothing is left below the picture.
	ph -= ph % 6
	if pw < 8 || ph < 6 {
		return ""
	}
	col := max((m.width*cw-pw)/2/cw, 0) + 1
	dark, light := m.tones()
	row := 2 // under the top bar
	return "\x1b7" + fmt.Sprintf("\x1b[%d;%dH", row, col) + pic.Sixel(pw, ph, dark, light) + "\x1b8"
}

// pictureLines is the large view without sixel: the picture in half blocks,
// in the middle of the screen, its caption under it.
func (m Model) pictureLines() []string {
	pp, ok := m.viewedPicture()
	if !ok {
		return nil
	}
	cols, rows := m.pictureBox()
	pic, fetched := m.pics.get(pp.Src)
	var out []string
	switch {
	case !fetched:
		out = append(out, "", "  loading the picture …")
	case pic == nil:
		out = append(out, "", "  the picture did not open")
	case m.gfx.sixel:
		for range rows {
			out = append(out, "")
		}
	default:
		c, r := pic.Fit(cols, rows)
		pad := strings.Repeat(" ", max((m.width-c)/2, 0))
		dark, light := m.tones()
		for _, row := range pic.HalfBlock(c, r, dark, light) {
			out = append(out, pad+row)
		}
	}
	for len(out) < rows {
		out = append(out, "")
	}
	n := len(m.cur.layout.Pictures)
	cap := pp.Caption
	if cap == "" || cap == "image" {
		cap = m.cur.doc.Title
	}
	line := fmt.Sprintf("%d/%d · %s · o its own colours · ← → the others · esc back", m.picView+1, n, cap)
	out = append(out, "", " "+m.theme.Seg(render.Seg{Role: render.Dim}, false).Render(truncateCells(line, m.width-2)))
	return out
}

func truncateCells(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:max(n-1, 0)]) + "…"
}

// pictureKey: the keys of the large view.
func (m Model) pictureKey(s string) (tea.Model, tea.Cmd) {
	switch s {
	case "esc", "q", "z", "backspace":
		m.mode = modeRead
		if m.gfx.sixel {
			return m, tea.ClearScreen // the sixel stays where text does not cover it
		}
		return m, nil
	case "right", "l", "n", " ", "space", "down", "j":
		return m.turnPicture(1)
	case "left", "h", "p", "b", "up", "k":
		return m.turnPicture(-1)
	case "o", "enter":
		pp, ok := m.viewedPicture()
		if !ok {
			return m, nil
		}
		return m, ownColours(pp.Src)
	}
	return m, nil
}

func (m Model) turnPicture(dir int) (tea.Model, tea.Cmd) {
	n := len(m.cur.layout.Pictures)
	next := m.picView + dir
	if next < 0 || next >= n {
		return m, nil
	}
	m.picView = next
	m.pics.get(m.cur.layout.Pictures[next].Src)
	var clearCmd tea.Cmd
	if m.gfx.sixel {
		clearCmd = tea.ClearScreen
	}
	return m, tea.Sequence(clearCmd, tea.Batch(m.pics.fetches(), m.sixelLater()))
}

// ownColours opens the picture in its own colours in the full-screen
// viewer (w5f view: X11 on the laptop, else a browser tab), W5F waiting.
var ownColours = func(src string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		b, err := source.Picture(ctx, src)
		if err != nil {
			return ownColoursDone{err}
		}
		ext := ".jpg"
		switch http.DetectContentType(b) {
		case "image/png":
			ext = ".png"
		case "image/gif":
			ext = ".gif"
		case "image/webp":
			ext = ".webp"
		}
		dir, err := os.MkdirTemp("", "w5f-picture-")
		if err != nil {
			return ownColoursDone{err}
		}
		path := filepath.Join(dir, "picture"+ext)
		if err := os.WriteFile(path, b, 0o600); err != nil {
			os.RemoveAll(dir)
			return ownColoursDone{err}
		}
		self, err := os.Executable()
		if err != nil {
			os.RemoveAll(dir)
			return ownColoursDone{err}
		}
		return viewPicture{exec.Command(self, "view", path), dir}
	}
}

type viewPicture struct {
	cmd *exec.Cmd
	dir string
}

type ownColoursDone struct{ err error }

// runViewer hands the terminal to the viewer until it closes.
func (m Model) runViewer(v viewPicture) (tea.Model, tea.Cmd) {
	return m, tea.ExecProcess(v.cmd, func(err error) tea.Msg {
		os.RemoveAll(v.dir)
		return ownColoursDone{err}
	})
}
