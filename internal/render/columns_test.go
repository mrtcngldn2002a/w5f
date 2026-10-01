package render

import (
	"strings"
	"testing"

	"w5f/internal/doc"
)

// Columns stand side by side when there is room, their links in column
// order; on a narrow page they follow each other.
func TestColumns(t *testing.T) {
	d := &doc.Document{Links: []doc.Link{{Href: "a"}, {Href: "b"}, {Href: "c"}}}
	d.Blocks = []doc.Block{doc.Columns{Cols: [][]doc.Block{
		{doc.Heading{Level: 2, Text: doc.Inline{{Text: "Left"}}}, doc.Paragraph{Text: doc.Inline{{Text: "one", Link: 1}}}, doc.Paragraph{Text: doc.Inline{{Text: "two", Link: 2}}}},
		{doc.Heading{Level: 2, Text: doc.Inline{{Text: "Right"}}}, doc.Paragraph{Text: doc.Inline{{Text: "three", Link: 3}}}},
	}}}
	l := Render(d, Options{Width: 100})
	if !strings.HasPrefix(l.Lines[0].Text(), "LEFT") || !strings.Contains(l.Lines[0].Text(), "RIGHT") {
		t.Fatalf("not side by side:\n%s", text(l))
	}
	if i := strings.Index(l.Lines[0].Text(), "RIGHT"); i != 52 {
		t.Errorf("right column at %d, want 52 (48 + the gap)", i)
	}
	if len(l.Focus) != 3 || l.Focus[0].Link != 1 || l.Focus[2].Link != 3 || l.Focus[2].Line != 2 {
		t.Errorf("focus: %+v", l.Focus)
	}
	for _, ln := range l.Lines {
		for _, s := range ln.Segs {
			if s.Link == 3 && s.Focus != 3 {
				t.Errorf("the right column's link is focus %d", s.Focus)
			}
		}
	}
	if len(l.Headings) != 2 || l.Headings[1].Line != 0 {
		t.Errorf("headings: %+v", l.Headings)
	}
	n := Render(d, Options{Width: 60})
	if strings.Contains(n.Lines[0].Text(), "RIGHT") || !strings.Contains(text(n), "RIGHT") {
		t.Errorf("narrow page:\n%s", text(n))
	}
}
