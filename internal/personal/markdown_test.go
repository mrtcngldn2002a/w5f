package personal

import (
	"strings"
	"testing"

	"w5f/internal/doc"
)

func TestMarkdownRoundTrip(t *testing.T) {
	d := &doc.Document{Title: "Page", Links: []doc.Link{{Href: "https://example.org/a b", Text: "a link"}}}
	d.Blocks = []doc.Block{
		doc.Heading{Level: 2, Text: doc.Inline{{Text: "Section"}}},
		doc.Paragraph{Text: doc.Inline{{Text: "Plain "}, {Text: "bold", Style: doc.Bold}, {Text: " and "}, {Text: "a link", Link: 1}, {Text: " with *stars*."}}},
		doc.List{Items: [][]doc.Block{{doc.Paragraph{Text: doc.Inline{{Text: "one"}}}}, {doc.Paragraph{Text: doc.Inline{{Text: "two"}}}}}},
		doc.Quote{Blocks: []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: "quoted"}}}}},
		doc.Table{Header: true, Rows: [][]doc.Inline{{{{Text: "h1"}}, {{Text: "h2"}}}, {{{Text: "a|b"}}, {{Text: "c"}}}}},
		doc.Pre{Text: "code"},
		doc.Image{Src: "https://example.org/i.png", Alt: "pic"},
		doc.Collapsible{ID: 1, Show: "More", Blocks: []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: "inside"}}}}},
	}
	md := ToMarkdown(d)
	for _, want := range []string{"# Page\n", "## Section\n", "Plain **bold** and [a link](<https://example.org/a b>) with \\*stars\\*.",
		"- one\n- two\n", "> quoted\n", "| h1 | h2 |\n| --- | --- |\n| a\\|b | c |", "```\ncode\n```", "![pic](https://example.org/i.png)", "**More**\n\ninside"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q:\n%s", want, md)
		}
	}
	back := ReadMarkdown([]byte("---\ntitle: \"My note\"\ntags: [w5f]\n---\n# My note\n\n## 2026-09-28 10:21\nSee [SCP-173](https://scp-wiki.wikidot.com/scp-173) now.\n\n> quoted\n>\n> more\n\n- [ ] [Queue item](<https://x/y z>) · FIC·SCP·1\n- [x] done one\n"), "file:///n.md")
	if back.Title != "My note" || back.Origin != "file" {
		t.Errorf("title/origin: %+v", back)
	}
	var kinds []string
	for _, b := range back.Blocks {
		kinds = append(kinds, strings.TrimPrefix(fmtType(b), "doc."))
	}
	if strings.Join(kinds, ",") != "Heading,Paragraph,Quote,List" {
		t.Errorf("blocks: %v", kinds)
	}
	if len(back.Links) != 2 || back.Links[0].Href != "https://scp-wiki.wikidot.com/scp-173" || back.Links[1].Href != "https://x/y z" {
		t.Errorf("links: %+v", back.Links)
	}
	q := back.Blocks[2].(doc.Quote)
	if len(q.Blocks) != 2 {
		t.Errorf("quote paragraphs: %+v", q)
	}
	l := back.Blocks[3].(doc.List)
	if l.Items[1][0].(doc.Paragraph).Text.PlainText() != "☑ done one" {
		t.Errorf("checked item: %q", l.Items[1][0].(doc.Paragraph).Text.PlainText())
	}
}

func fmtType(b doc.Block) string {
	switch b.(type) {
	case doc.Heading:
		return "doc.Heading"
	case doc.Paragraph:
		return "doc.Paragraph"
	case doc.Quote:
		return "doc.Quote"
	case doc.List:
		return "doc.List"
	}
	return "other"
}
