package render

import (
	"strings"
	"testing"

	"w5f/internal/doc"
)

// A picture the reader draws takes its rows, centred in the column, its
// caption under it; one it does not draw is a line naming it. Both are in
// the layout's pictures, for z.
func TestPicturesTakeTheirRows(t *testing.T) {
	d := &doc.Document{Blocks: []doc.Block{
		doc.Paragraph{Text: doc.Inline{{Text: "Before."}}},
		doc.Image{Src: "a.jpg", Caption: "Plate I"},
		doc.Image{Src: "b.jpg", Alt: "a heron"},
	}}
	l := Render(d, Options{Width: 40, Picture: func(src string, width int) (int, int) {
		if src == "a.jpg" {
			return 20, 5
		}
		return 0, 0
	}})
	if len(l.Pictures) != 2 {
		t.Fatalf("pictures %+v", l.Pictures)
	}
	a, b := l.Pictures[0], l.Pictures[1]
	if a.Rows != 5 || a.Cols != 20 || a.Indent != 10 || a.Src != "a.jpg" {
		t.Fatalf("drawn picture %+v", a)
	}
	for i := a.Line; i < a.Line+a.Rows; i++ {
		if strings.TrimSpace(l.Lines[i].Text()) != "" {
			t.Fatalf("line %d of the picture is %q", i, l.Lines[i].Text())
		}
	}
	if got := l.Lines[a.Line+a.Rows].Text(); !strings.Contains(got, "Plate I") {
		t.Fatalf("caption line %q", got)
	}
	if b.Rows != 0 || !strings.Contains(l.Lines[b.Line].Text(), "▒▒ a heron") {
		t.Fatalf("named picture %+v: %q", b, l.Lines[b.Line].Text())
	}
}
