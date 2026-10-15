package render

import (
	"strings"
	"testing"

	"w5f/internal/doc"
)

func TestBookIndentsFollowingParagraphs(t *testing.T) {
	p := func(s string) doc.Block { return doc.Paragraph{Text: doc.Inline{{Text: s}}} }
	d := &doc.Document{Blocks: []doc.Block{
		doc.Heading{Level: 2, Text: doc.Inline{{Text: "One"}}},
		p("First paragraph."), p("Second paragraph."), doc.Rule{}, p("After the break."), p("And on."),
	}}
	got := text(Render(d, Options{Width: 40, Book: true}))
	want := "ONE\n\nFirst paragraph.\n   Second paragraph.\n\n" + strings.Repeat(" ", 16) + "·  ·  ·\n\nAfter the break.\n   And on.\n"
	if got != want {
		t.Errorf("book layout:\n%s\nwant:\n%s", got, want)
	}
	plain := text(Render(d, Options{Width: 40}))
	if !strings.Contains(plain, "First paragraph.\n\nSecond paragraph.") {
		t.Errorf("web layout should keep the blank line:\n%s", plain)
	}
}
