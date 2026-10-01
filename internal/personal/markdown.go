package personal

import (
	"regexp"
	"strconv"
	"strings"

	"w5f/internal/doc"
)

var mdEscaper = strings.NewReplacer(`\`, `\\`, "*", `\*`, "_", `\_`, "[", `\[`, "]", `\]`, "`", "\\`")

func mdEscape(s string) string { return mdEscaper.Replace(s) }

var reUnescape = regexp.MustCompile("\\\\([\\\\*_\\[\\]`|])")

func mdUnescape(s string) string { return reUnescape.ReplaceAllString(s, "$1") }

// ToMarkdown renders a document as Markdown; the title is the only level-1
// heading, so document headings start at level 2.
func ToMarkdown(d *doc.Document) string {
	var b strings.Builder
	if d.Title != "" {
		b.WriteString("# " + d.Title + "\n\n")
	}
	writeBlocks(&b, d, d.Blocks, "")
	return strings.TrimRight(b.String(), "\n") + "\n"
}

func blankLine(b *strings.Builder, prefix string) {
	b.WriteString(strings.TrimRight(prefix, " ") + "\n")
}

func writeBlocks(b *strings.Builder, d *doc.Document, bs []doc.Block, prefix string) {
	for _, x := range bs {
		switch x := x.(type) {
		case doc.Paragraph:
			b.WriteString(prefix + inlineMD(d, x.Text) + "\n")
		case doc.Heading:
			b.WriteString(prefix + strings.Repeat("#", min(max(x.Level, 2), 6)) + " " + mdEscape(strings.TrimSpace(x.Text.PlainText())) + "\n")
		case doc.Quote:
			writeBlocks(b, d, x.Blocks, prefix+"> ")
		case doc.List:
			for i, it := range x.Items {
				marker := "- "
				if x.Ordered {
					marker = strconv.Itoa(i+1) + ". "
				}
				var sub strings.Builder
				writeBlocks(&sub, d, it, "")
				first := true
				for _, l := range strings.Split(strings.TrimRight(sub.String(), "\n"), "\n") {
					if strings.TrimSpace(l) == "" {
						continue
					}
					if first {
						b.WriteString(prefix + marker + l + "\n")
						first = false
					} else {
						b.WriteString(prefix + strings.Repeat(" ", len(marker)) + l + "\n")
					}
				}
			}
		case doc.Table:
			for i, r := range x.Rows {
				var cells []string
				for _, c := range r {
					cells = append(cells, strings.ReplaceAll(inlineMD(d, c), "|", `\|`))
				}
				b.WriteString(prefix + "| " + strings.Join(cells, " | ") + " |\n")
				if i == 0 {
					b.WriteString(prefix + "|" + strings.Repeat(" --- |", len(r)) + "\n")
				}
			}
		case doc.Collapsible:
			b.WriteString(prefix + "**" + mdEscape(x.Show) + "**\n")
			blankLine(b, prefix)
			writeBlocks(b, d, x.Blocks, prefix)
			continue
		case doc.Columns:
			for _, col := range x.Cols {
				writeBlocks(b, d, col, prefix)
			}
			continue
		case doc.Image:
			b.WriteString(prefix + "![" + mdEscape(x.Alt) + "](" + x.Src + ")\n")
		case doc.Rule:
			b.WriteString(prefix + "---\n")
		case doc.Pre:
			b.WriteString(prefix + "```\n")
			for _, l := range strings.Split(x.Text, "\n") {
				b.WriteString(prefix + l + "\n")
			}
			b.WriteString(prefix + "```\n")
		case doc.Notice:
			b.WriteString(prefix + "> " + mdEscape(x.Text) + "\n")
		case doc.Footnotes:
			for _, n := range x.Notes {
				b.WriteString(prefix + "[" + n.Label + "]: " + inlineMD(d, n.Text) + "\n")
			}
		default:
			continue
		}
		blankLine(b, prefix)
	}
}

func inlineMD(d *doc.Document, in doc.Inline) string {
	var b strings.Builder
	for _, s := range in {
		if s.Break {
			b.WriteString("  \n")
			continue
		}
		t := mdEscape(s.Text)
		switch {
		case s.Style&doc.Code != 0:
			t = "`" + s.Text + "`"
		case s.Style&doc.Bold != 0:
			t = "**" + t + "**"
		case s.Style&doc.Italic != 0:
			t = "*" + t + "*"
		case s.Style&doc.Strike != 0:
			t = "~~" + t + "~~"
		}
		if s.Link > 0 && s.Link <= len(d.Links) {
			t = mdLink(s.Text, d.Links[s.Link-1].Href)
		}
		b.WriteString(t)
	}
	return b.String()
}

var (
	reMDLink   = regexp.MustCompile(`\[((?:\\.|[^\]\\])*)\]\((<[^>]*>|[^)\s]+)\)`)
	reListItem = regexp.MustCompile(`^\s*(?:[-*+]|\d+\.)\s+(?:\[([ xX])\]\s+)?(.*)$`)
)

// ReadMarkdown shows a Markdown file in the reader: frontmatter hidden,
// headings, quotes, lists and links kept.
func ReadMarkdown(data []byte, fileURL string) *doc.Document {
	front, body := SplitFront(string(data))
	d := &doc.Document{URL: fileURL, Title: front["title"], Origin: "file"}
	var para, quote []string
	var list [][]doc.Block
	flushPara := func() {
		if len(para) > 0 {
			d.Blocks = append(d.Blocks, doc.Paragraph{Text: readInline(d, strings.Join(para, " "))})
			para = nil
		}
	}
	flushQuote := func() {
		if len(quote) == 0 {
			return
		}
		var qb []doc.Block
		var cur []string
		for _, l := range append(quote, "") {
			if l == "" {
				if len(cur) > 0 {
					qb = append(qb, doc.Paragraph{Text: readInline(d, strings.Join(cur, " "))})
					cur = nil
				}
				continue
			}
			cur = append(cur, l)
		}
		d.Blocks = append(d.Blocks, doc.Quote{Blocks: qb})
		quote = nil
	}
	flushList := func() {
		if len(list) > 0 {
			d.Blocks = append(d.Blocks, doc.List{Items: list})
			list = nil
		}
	}
	flushAll := func() { flushPara(); flushQuote(); flushList() }
	var fence []string
	inFence := false
	for _, ln := range strings.Split(body, "\n") {
		t := strings.TrimRight(ln, " \t")
		// Code blocks are shown as they are; nothing inside becomes a link.
		if strings.HasPrefix(strings.TrimSpace(t), "```") {
			if inFence {
				d.Blocks = append(d.Blocks, doc.Pre{Text: strings.Join(fence, "\n")})
				fence, inFence = nil, false
			} else {
				flushAll()
				inFence = true
			}
			continue
		}
		if inFence {
			fence = append(fence, ln)
			continue
		}
		switch {
		case strings.TrimSpace(t) == "":
			flushAll()
		case strings.HasPrefix(t, "#"):
			flushAll()
			level := len(t) - len(strings.TrimLeft(t, "#"))
			text := strings.TrimSpace(t[level:])
			if level == 1 && (d.Title == "" || d.Title == mdUnescape(text)) {
				d.Title = mdUnescape(text)
				continue
			}
			d.Blocks = append(d.Blocks, doc.Heading{Level: min(level, 3), Text: readInline(d, text)})
		case strings.HasPrefix(t, ">"):
			flushPara()
			flushList()
			quote = append(quote, strings.TrimSpace(strings.TrimPrefix(t, ">")))
		case reListItem.MatchString(t):
			flushPara()
			flushQuote()
			m := reListItem.FindStringSubmatch(t)
			text := m[2]
			switch m[1] {
			case "x", "X":
				text = "☑ " + text
			case " ":
				text = "☐ " + text
			}
			list = append(list, []doc.Block{doc.Paragraph{Text: readInline(d, text)}})
		default:
			flushQuote()
			flushList()
			para = append(para, strings.TrimSpace(t))
		}
	}
	flushAll()
	if inFence {
		d.Blocks = append(d.Blocks, doc.Pre{Text: strings.Join(fence, "\n")})
	}
	return d
}

// readInline turns Markdown text into spans, keeping links; text inside
// `code spans` stays text.
func readInline(d *doc.Document, s string) doc.Inline {
	if parts := strings.Split(s, "`"); len(parts) >= 3 {
		var out doc.Inline
		for i, p := range parts {
			switch {
			case i%2 == 1 && i < len(parts)-1:
				if p != "" {
					out = append(out, doc.Span{Text: p, Style: doc.Code})
				}
			case i%2 == 1: // an unmatched backtick
				out = append(out, readInline(d, "`"+p)...)
			default:
				out = append(out, readInline(d, p)...)
			}
		}
		return out
	}
	var out doc.Inline
	plain := func(t string) {
		if t = mdUnescape(strings.ReplaceAll(t, "**", "")); t != "" {
			out = append(out, doc.Span{Text: t})
		}
	}
	last := 0
	for _, m := range reMDLink.FindAllStringSubmatchIndex(s, -1) {
		plain(s[last:m[0]])
		text := mdUnescape(s[m[2]:m[3]])
		href := strings.TrimSuffix(strings.TrimPrefix(s[m[4]:m[5]], "<"), ">")
		d.Links = append(d.Links, doc.Link{Href: href, Text: text})
		out = append(out, doc.Span{Text: text, Link: len(d.Links)})
		last = m[1]
	}
	plain(s[last:])
	return out
}
