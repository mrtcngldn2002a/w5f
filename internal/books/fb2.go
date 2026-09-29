package books

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strconv"
	"strings"

	"golang.org/x/text/encoding/charmap"

	"w5f/internal/doc"
)

// fbNode is an FB2 element (or a text node when name is "").
type fbNode struct {
	name  string
	attrs map[string]string // local names: l:href → href
	text  string
	kids  []*fbNode
}

func (n *fbNode) child(name string) *fbNode {
	if n == nil {
		return nil
	}
	for _, k := range n.kids {
		if k.name == name {
			return k
		}
	}
	return nil
}

func (n *fbNode) children(name string) []*fbNode {
	if n == nil {
		return nil
	}
	var out []*fbNode
	for _, k := range n.kids {
		if k.name == name {
			out = append(out, k)
		}
	}
	return out
}

// plain is the element's text with whitespace collapsed.
func (n *fbNode) plain() string {
	if n == nil {
		return ""
	}
	var b strings.Builder
	var walk func(*fbNode)
	walk = func(x *fbNode) {
		if x.name == "" {
			b.WriteString(x.text)
			b.WriteByte(' ')
		}
		for _, k := range x.kids {
			walk(k)
		}
	}
	walk(n)
	return strings.Join(strings.Fields(b.String()), " ")
}

// titleText joins the paragraphs of a <title> with " — ".
func (n *fbNode) titleText() string {
	var parts []string
	for _, p := range n.children("p") {
		if t := p.plain(); t != "" {
			parts = append(parts, t)
		}
	}
	if len(parts) == 0 {
		return n.plain()
	}
	return strings.Join(parts, " — ")
}

func (n *fbNode) walk(f func(*fbNode)) {
	f(n)
	for _, k := range n.kids {
		k.walk(f)
	}
}

func fbCharset(label string, in io.Reader) (io.Reader, error) {
	switch strings.ToLower(label) {
	case "utf-8", "utf8":
		return in, nil
	case "windows-1251", "cp1251":
		return charmap.Windows1251.NewDecoder().Reader(in), nil
	case "windows-1252", "cp1252":
		return charmap.Windows1252.NewDecoder().Reader(in), nil
	case "koi8-r":
		return charmap.KOI8R.NewDecoder().Reader(in), nil
	case "iso-8859-5":
		return charmap.ISO8859_5.NewDecoder().Reader(in), nil
	case "iso-8859-1", "latin1":
		return charmap.ISO8859_1.NewDecoder().Reader(in), nil
	}
	return nil, fmt.Errorf("unsupported FB2 encoding %q", label)
}

// fbTree parses FB2 XML leniently into an ordered tree.
func fbTree(r io.Reader) (*fbNode, error) {
	dec := xml.NewDecoder(r)
	dec.CharsetReader = fbCharset
	dec.Strict = false
	dec.AutoClose = xml.HTMLAutoClose
	dec.Entity = xml.HTMLEntity
	root := &fbNode{name: "#root"}
	stack := []*fbNode{root}
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			if len(root.kids) > 0 && !strings.Contains(err.Error(), "encoding") {
				break // keep what was read from a truncated file
			}
			return nil, err
		}
		top := stack[len(stack)-1]
		switch t := tok.(type) {
		case xml.StartElement:
			n := &fbNode{name: t.Name.Local, attrs: map[string]string{}}
			for _, a := range t.Attr {
				n.attrs[a.Name.Local] = a.Value
			}
			top.kids = append(top.kids, n)
			stack = append(stack, n)
		case xml.EndElement:
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			top.kids = append(top.kids, &fbNode{text: string(t)})
		}
	}
	return root, nil
}

// fb2Book is an opened FB2 book.
type fb2Book struct {
	meta     Meta
	chapters []Chapter
	sections []*fbNode
	notes    map[string]*fbNode // note id → note section
	ids      map[string]int     // element id → chapter
}

func init() {
	open := func(p string) (Reader, error) {
		b, err := openFB2(p)
		if err != nil {
			return nil, err
		}
		return b, nil
	}
	openers[".fb2"] = open
	openers[".fb2.zip"] = open
}

func openFB2(p string) (*fb2Book, error) {
	if bookExt(p) != ".fb2.zip" {
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		return parseFB2(data)
	}
	zr, err := zip.OpenReader(p)
	if err != nil {
		return nil, fmt.Errorf("not an FB2 zip: %w", err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		if !strings.EqualFold(path.Ext(f.Name), ".fb2") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(io.LimitReader(rc, 64<<20))
		rc.Close()
		if err != nil {
			return nil, err
		}
		return parseFB2(data)
	}
	return nil, errors.New("no .fb2 file inside the zip")
}

func parseFB2(data []byte) (*fb2Book, error) {
	root, err := fbTree(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("the FB2 file is damaged: %w", err)
	}
	fb := root.child("FictionBook")
	if fb == nil {
		return nil, errors.New("not an FB2 book")
	}
	b := &fb2Book{notes: map[string]*fbNode{}, ids: map[string]int{}}
	if ti := fb.child("description").child("title-info"); ti != nil {
		b.meta.Title = ti.child("book-title").plain()
		if a := ti.child("author"); a != nil {
			b.meta.Author = strings.Join(strings.Fields(a.child("first-name").plain()+" "+
				a.child("middle-name").plain()+" "+a.child("last-name").plain()), " ")
			if b.meta.Author == "" {
				b.meta.Author = a.child("nickname").plain()
			}
		}
		b.meta.Lang = ti.child("lang").plain()
	}
	for _, body := range fb.children("body") {
		if name := body.attrs["name"]; name == "notes" || name == "comments" {
			for _, s := range body.children("section") {
				if id := s.attrs["id"]; id != "" {
					b.notes[id] = s
				}
			}
			continue
		}
		if len(b.sections) > 0 {
			continue // the first body is the book
		}
		intro := &fbNode{name: "section"}
		for _, k := range body.kids {
			if k.name != "section" {
				intro.kids = append(intro.kids, k)
			}
		}
		secs := body.children("section")
		if len(secs) == 0 || intro.plain() != "" {
			b.sections = append(b.sections, intro)
		}
		b.sections = append(b.sections, secs...)
	}
	if len(b.sections) == 0 {
		return nil, errors.New("the FB2 book has no text")
	}
	for i, s := range b.sections {
		title := ""
		if t := s.child("title"); t != nil {
			title = t.titleText()
		}
		b.chapters = append(b.chapters, Chapter{Title: title})
		s.walk(func(n *fbNode) {
			if id := n.attrs["id"]; id != "" {
				b.ids[id] = i
			}
		})
	}
	return b, nil
}

func (b *fb2Book) Info() Meta          { return b.meta }
func (b *fb2Book) Contents() []Chapter { return b.chapters }
func (b *fb2Book) Close() error        { return nil }

func (b *fb2Book) ChapterDoc(i int, link func(ch int) string) (*doc.Document, error) {
	if i < 0 || i >= len(b.sections) {
		return nil, errors.New("no such chapter")
	}
	c := &fbConv{book: b, d: &doc.Document{Lang: b.meta.Lang}, link: link, seen: map[string]string{}}
	c.d.Blocks = c.blocks(b.sections[i], 1)
	if len(c.notes) > 0 {
		c.d.Blocks = append(c.d.Blocks, doc.Footnotes{Notes: c.notes})
	}
	c.d.Renumber()
	return c.d, nil
}

// fbConv turns one chapter into document blocks.
type fbConv struct {
	book  *fb2Book
	d     *doc.Document
	link  func(int) string
	notes []doc.Footnote
	seen  map[string]string // note id → label
}

func (c *fbConv) addLink(href, text string) int {
	c.d.Links = append(c.d.Links, doc.Link{Href: href, Text: text})
	return len(c.d.Links)
}

func (c *fbConv) blocks(sec *fbNode, depth int) []doc.Block {
	var out []doc.Block
	for _, n := range sec.kids {
		switch n.name {
		case "title":
			out = append(out, doc.Heading{Level: min(depth+1, 3), Text: c.titleInline(n)})
		case "subtitle":
			out = append(out, doc.Heading{Level: 3, Text: c.inline(n, 0)})
		case "p":
			if in := c.inline(n, 0); len(in) > 0 {
				out = append(out, doc.Paragraph{Text: in})
			}
		case "section":
			out = append(out, c.blocks(n, depth+1)...)
		case "epigraph", "cite", "annotation":
			out = append(out, doc.Quote{Blocks: c.blocks(n, depth)})
		case "text-author", "date":
			out = append(out, doc.Paragraph{Text: c.inline(n, doc.Italic)})
		case "poem":
			out = append(out, c.poem(n, depth)...)
		case "table":
			out = append(out, c.table(n))
		case "image":
			out = append(out, doc.Image{Alt: n.attrs["alt"]})
		}
	}
	return out
}

func (c *fbConv) titleInline(n *fbNode) doc.Inline {
	var out doc.Inline
	for _, p := range n.children("p") {
		if len(out) > 0 {
			out = append(out, doc.Span{Text: " — "})
		}
		out = append(out, c.inline(p, 0)...)
	}
	if len(out) == 0 {
		return c.inline(n, 0)
	}
	return out
}

func (c *fbConv) poem(n *fbNode, depth int) []doc.Block {
	var out []doc.Block
	for _, k := range n.kids {
		switch k.name {
		case "title":
			out = append(out, doc.Heading{Level: 3, Text: c.titleInline(k)})
		case "epigraph":
			out = append(out, doc.Quote{Blocks: c.blocks(k, depth)})
		case "stanza":
			var in doc.Inline
			for _, v := range k.children("v") {
				if len(in) > 0 {
					in = append(in, doc.Span{Break: true})
				}
				in = append(in, c.inline(v, 0)...)
			}
			if len(in) > 0 {
				out = append(out, doc.Paragraph{Text: in})
			}
		case "text-author", "date":
			out = append(out, doc.Paragraph{Text: c.inline(k, doc.Italic)})
		}
	}
	return out
}

func (c *fbConv) table(n *fbNode) doc.Block {
	var t doc.Table
	for i, tr := range n.children("tr") {
		var row []doc.Inline
		for _, cell := range tr.kids {
			if cell.name == "th" || cell.name == "td" {
				if i == 0 && cell.name == "th" {
					t.Header = true
				}
				row = append(row, c.inline(cell, 0))
			}
		}
		t.Rows = append(t.Rows, row)
	}
	return t
}

var fbStyles = map[string]doc.Style{"emphasis": doc.Italic, "strong": doc.Bold, "strikethrough": doc.Strike,
	"code": doc.Code, "sup": doc.Sup, "sub": doc.Sup}

// inline is an element's text as spans, trimmed at both ends.
func (c *fbConv) inline(n *fbNode, st doc.Style) doc.Inline {
	out := c.spans(n, st)
	if len(out) > 0 {
		out[0].Text = strings.TrimLeft(out[0].Text, " ")
		out[len(out)-1].Text = strings.TrimRight(out[len(out)-1].Text, " ")
	}
	return out
}

// spans keeps the spaces between nested elements ("<emphasis>word </emphasis>next").
func (c *fbConv) spans(n *fbNode, st doc.Style) doc.Inline {
	var out doc.Inline
	for _, k := range n.kids {
		switch {
		case k.name == "":
			if t := collapseWS(k.text); t != "" {
				out = append(out, doc.Span{Text: t, Style: st})
			}
		case k.name == "a":
			out = append(out, c.anchor(k, st)...)
		default:
			out = append(out, c.spans(k, st|fbStyles[k.name])...)
		}
	}
	return out
}

// collapseWS turns runs of white space (non-breaking spaces included) into
// one space, keeping a leading or trailing one to separate spans.
func collapseWS(s string) string {
	s = strings.ReplaceAll(s, " ", " ")
	f := strings.Fields(s)
	if len(f) == 0 {
		if s != "" {
			return " "
		}
		return ""
	}
	out := strings.Join(f, " ")
	if strings.TrimLeft(s, " \t\r\n") != s {
		out = " " + out
	}
	if strings.TrimRight(s, " \t\r\n") != s {
		out += " "
	}
	return out
}

func (c *fbConv) anchor(a *fbNode, st doc.Style) doc.Inline {
	href, text := a.attrs["href"], a.plain()
	if strings.HasPrefix(href, "#") {
		id := href[1:]
		if note, ok := c.book.notes[id]; ok {
			return doc.Inline{{Text: "[" + c.noteLabel(id, note) + "]", Style: doc.Sup}}
		}
		if ch, ok := c.book.ids[id]; ok {
			return doc.Inline{{Text: text, Style: st, Link: c.addLink(c.link(ch), text)}}
		}
		return doc.Inline{{Text: text, Style: st}}
	}
	if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") {
		return doc.Inline{{Text: text, Style: st, Link: c.addLink(href, text)}}
	}
	return doc.Inline{{Text: text, Style: st}}
}

// noteLabel numbers a note on first use and collects its text for the
// chapter's footnotes.
func (c *fbConv) noteLabel(id string, note *fbNode) string {
	if l, ok := c.seen[id]; ok {
		return l
	}
	l := strconv.Itoa(len(c.seen) + 1)
	c.seen[id] = l
	var in doc.Inline
	for _, p := range note.children("p") {
		if len(in) > 0 {
			in = append(in, doc.Span{Text: " "})
		}
		in = append(in, c.inline(p, 0)...)
	}
	c.notes = append(c.notes, doc.Footnote{Label: l, Text: in})
	return l
}
