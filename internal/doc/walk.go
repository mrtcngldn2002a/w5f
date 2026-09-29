package doc

import "strings"

// ReplaceBlocks walks the block tree depth-first and lets fn replace any
// block with zero or more blocks. fn returns ok=false to keep a block as is
// (its children are still visited).
func ReplaceBlocks(bs []Block, fn func(Block) (repl []Block, ok bool)) []Block {
	var out []Block
	for _, b := range bs {
		if repl, ok := fn(b); ok {
			out = append(out, repl...)
			continue
		}
		switch x := b.(type) {
		case Collapsible:
			x.Blocks = ReplaceBlocks(x.Blocks, fn)
			b = x
		case Quote:
			x.Blocks = ReplaceBlocks(x.Blocks, fn)
			b = x
		case List:
			items := make([][]Block, 0, len(x.Items))
			for _, it := range x.Items {
				if it = ReplaceBlocks(it, fn); len(it) > 0 {
					items = append(items, it)
				}
			}
			if len(items) == 0 {
				continue
			}
			x.Items = items
			b = x
		}
		out = append(out, b)
	}
	return out
}

// ShiftLinks adds offset to every link reference in the block tree. It is used
// when blocks from another document are merged into this one.
func ShiftLinks(bs []Block, offset int) []Block {
	shift := func(in Inline) Inline {
		out := make(Inline, len(in))
		for i, s := range in {
			if s.Link > 0 {
				s.Link += offset
			}
			out[i] = s
		}
		return out
	}
	return ReplaceBlocks(bs, func(b Block) ([]Block, bool) {
		switch x := b.(type) {
		case Paragraph:
			x.Text = shift(x.Text)
			return []Block{x}, true
		case Heading:
			x.Text = shift(x.Text)
			return []Block{x}, true
		case Table:
			rows := make([][]Inline, len(x.Rows))
			for i, r := range x.Rows {
				rows[i] = make([]Inline, len(r))
				for j, c := range r {
					rows[i][j] = shift(c)
				}
			}
			x.Rows = rows
			return []Block{x}, true
		case Footnotes:
			notes := make([]Footnote, len(x.Notes))
			for i, n := range x.Notes {
				n.Text = shift(n.Text)
				notes[i] = n
			}
			x.Notes = notes
			return []Block{x}, true
		}
		return nil, false
	})
}

// TextLength returns the number of non-space characters of readable text.
func TextLength(bs []Block) int {
	n := 0
	count := func(in Inline) {
		for _, s := range in {
			n += len(strings.Join(strings.Fields(s.Text), ""))
		}
	}
	ReplaceBlocks(bs, func(b Block) ([]Block, bool) {
		switch x := b.(type) {
		case Paragraph:
			count(x.Text)
		case Heading:
			count(x.Text)
		case Pre:
			n += len(strings.Join(strings.Fields(x.Text), ""))
		case Table:
			for _, r := range x.Rows {
				for _, c := range r {
					count(c)
				}
			}
		}
		return nil, false
	})
	return n
}

// Renumber assigns collapsible IDs in document order and updates the count.
func (d *Document) Renumber() {
	n := 0
	var visit func([]Block) []Block
	visit = func(bs []Block) []Block {
		return ReplaceBlocks(bs, func(b Block) ([]Block, bool) {
			if c, ok := b.(Collapsible); ok {
				n++
				c.ID = n
				c.Blocks = visit(c.Blocks)
				return []Block{c}, true
			}
			return nil, false
		})
	}
	d.Blocks = visit(d.Blocks)
	d.Collapsibles = n
}
