package render

import (
	"strings"
	"testing"

	"w5f/internal/doc"
)

func TestParasRecordTextBlocks(t *testing.T) {
	p := func(s string) doc.Paragraph { return doc.Paragraph{Text: doc.Inline{{Text: s}}} }
	d := &doc.Document{Title: "T", Blocks: []doc.Block{
		doc.Heading{Level: 2, Text: doc.Inline{{Text: "Section"}}},
		p(strings.Repeat("long paragraph words ", 12)),
		doc.List{Items: [][]doc.Block{{p("first item")}, {p("second item")}}},
		doc.Quote{Blocks: []doc.Block{p("quoted text")}},
		doc.Collapsible{ID: 1, Show: "closed", Blocks: []doc.Block{p("hidden")}},
	}}
	l := Render(d, Options{Width: 40})
	var texts []string
	last := -1
	for _, pr := range l.Paras {
		texts = append(texts, pr.Text)
		if pr.Start <= last || pr.End <= pr.Start || pr.End > len(l.Lines) {
			t.Errorf("bad range %+v (last end %d)", pr, last)
		}
		last = pr.End - 1
		if !strings.Contains(strings.ToLower(l.Lines[pr.Start].Text()), strings.ToLower(strings.Fields(pr.Text)[0])) { // H2 is upper-cased
			t.Errorf("range %+v does not start at its text: %q", pr, l.Lines[pr.Start].Text())
		}
	}
	want := "Section|" + strings.TrimSpace(strings.Repeat("long paragraph words ", 12)) + "|first item|second item|quoted text"
	if strings.Join(texts, "|") != want {
		t.Errorf("paras = %q", strings.Join(texts, "|"))
	}
	if l.Paras[1].End-l.Paras[1].Start < 5 {
		t.Errorf("wrapped paragraph should span several lines: %+v", l.Paras[1])
	}
}
