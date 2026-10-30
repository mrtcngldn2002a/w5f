package tui

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"w5f/internal/doc"
	"w5f/internal/picture"
)

func testPicture(t *testing.T) *picture.Picture {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 200, 100))
	for y := range 100 {
		for x := range 200 {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y * 2), 0, 255})
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	p, err := picture.Decode(b.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func platePage() *doc.Document {
	return &doc.Document{Title: "The Bedroom", URL: "w5f:discover/plate?src=aic&id=1", Pictures: true,
		Blocks: []doc.Block{doc.Image{Src: "https://example.org/bedroom.jpg", Caption: "The Bedroom, 1889"},
			doc.Paragraph{Text: doc.Inline{{Text: "Van Gogh's room in Arles."}}}}}
}

// A plate's picture is fetched when its page is laid out, then drawn in
// the page in half blocks; z shows it large, esc goes back.
func TestPlatePictureDrawnAndShownLarge(t *testing.T) {
	var fetched []string
	pic := testPicture(t)
	defer func(f func(string) tea.Cmd) { fetchPicture = f }(fetchPicture)
	fetchPicture = func(src string) tea.Cmd {
		fetched = append(fetched, src)
		return func() tea.Msg { return pictureLoaded{src, pic, nil} }
	}
	m := sized(New("", "test"))
	next, cmd := m.Update(loadedMsg{target: "w5f:discover/plate?src=aic&id=1", doc: platePage()})
	m = next.(Model)
	if len(fetched) != 1 || cmd == nil {
		t.Fatalf("fetched %v", fetched)
	}
	next, _ = m.Update(pictureLoaded{"https://example.org/bedroom.jpg", pic, nil})
	m = next.(Model)
	view := m.View().Content
	if n := strings.Count(view, "▀"); n < 100 {
		t.Fatalf("%d half blocks on screen", n)
	}
	if !strings.Contains(view, "The Bedroom, 1889") || !strings.Contains(view, "Van Gogh") {
		t.Fatal("caption or text missing")
	}
	if bar := m.bottomBar(); !strings.Contains(bar, "z") || !strings.Contains(bar, "picture") {
		t.Fatalf("the bar does not say z: %q", bar)
	}
	m = press(m, "z")
	if m.mode != modePicture || !strings.Contains(m.View().Content, "1/1 · The Bedroom, 1889") {
		t.Fatalf("mode %v:\n%s", m.mode, m.View().Content)
	}
	m = press(m, "esc")
	if m.mode != modeRead {
		t.Fatal("esc did not close the picture")
	}
}

// In a terminal with sixel, the large view is blank where the picture goes
// and the picture comes as a sixel at its place.
func TestSixelLargeView(t *testing.T) {
	pic := testPicture(t)
	defer func(f func(string) tea.Cmd) { fetchPicture = f }(fetchPicture)
	fetchPicture = func(src string) tea.Cmd { return func() tea.Msg { return pictureLoaded{src, pic, nil} } }
	m := sized(New("", "test"))
	next, _ := m.Update(uv.PrimaryDeviceAttributesEvent{64, 1, 2, 4, 6})
	m = next.(Model)
	if !m.gfx.sixel {
		t.Fatal("DA1 with 4 is sixel")
	}
	next, _ = m.Update(uv.UnknownCsiEvent("\x1b[?2;0;720;480S")) // 90x30 cells
	m = next.(Model)
	if m.gfx.cellW != 8 || m.gfx.cellH != 16 {
		t.Fatalf("cell %dx%d", m.gfx.cellW, m.gfx.cellH)
	}
	m = open(m, "w5f:discover/plate?src=aic&id=1", platePage())
	next, _ = m.Update(pictureLoaded{"https://example.org/bedroom.jpg", pic, nil})
	m = next.(Model)
	m = press(m, "z")
	if strings.Count(m.View().Content, "▀") != 0 {
		t.Fatal("half blocks under the sixel")
	}
	s := m.sixel()
	if !strings.HasPrefix(s, "\x1b7\x1b[2;") || !strings.Contains(s, "\x1bP0;1;0q") || !strings.HasSuffix(s, "\x1b\\"+"\x1b8") {
		t.Fatalf("sixel %q … %q", s[:min(len(s), 40)], s[max(len(s)-12, 0):])
	}
}

// Elsewhere a picture is a line; z still shows it.
func TestPictureOnAnyPage(t *testing.T) {
	pic := testPicture(t)
	defer func(f func(string) tea.Cmd) { fetchPicture = f }(fetchPicture)
	var fetched int
	fetchPicture = func(src string) tea.Cmd {
		fetched++
		return func() tea.Msg { return pictureLoaded{src, pic, nil} }
	}
	d := platePage()
	d.Pictures = false
	m := open(sized(New("", "test")), "https://example.org/page", d)
	if fetched != 0 || strings.Contains(m.View().Content, "▀") {
		t.Fatal("a page's picture drawn without being asked for")
	}
	next, cmd := m.Update(tea.KeyPressMsg{Code: 'z', Text: "z"})
	m = next.(Model)
	if fetched != 1 || cmd == nil {
		t.Fatalf("z fetched %d", fetched)
	}
	next, _ = m.Update(pictureLoaded{"https://example.org/bedroom.jpg", pic, nil})
	m = next.(Model)
	if strings.Count(m.View().Content, "▀") < 100 {
		t.Fatal("the large view has no picture")
	}
}
