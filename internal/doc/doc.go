// Package doc defines the W5F document model. Every source (web page, wiki
// page, feed item, book chapter, gemtext) is converted into a Document before
// it reaches the reader, so the reader only ever deals with one format.
package doc

// Document is a normalized, source-independent piece of readable content.
type Document struct {
	URL    string
	Title  string
	Byline string
	Lang   string // BCP 47 language tag if known ("en", "tr")
	// Ref identifies a stored record the page shows (e.g. "item:42"), so
	// actions like star or mark-read know what they apply to.
	Ref string
	// Catalog is the library call number when the adapter knows it
	// (feed items, books); pages derive theirs from the address.
	Catalog string
	// Next and Prev are addresses of the following/preceding part (book
	// chapters); the reader binds them to ] and [.
	Next, Prev string
	// Resume, when > 0, is the fraction of the page to scroll to on open.
	Resume float64
	Meta   []KV
	Blocks []Block
	Links  []Link // 1-based: Span.Link == n refers to Links[n-1]
	// Collapsibles is the number of Collapsible blocks; their IDs are 1..n.
	Collapsibles int
	Origin       string // "live", "cache", "file", "archive"
}

// KV is an ordered metadata pair (object class, rating, tags...).
type KV struct{ Key, Value string }

// Link is a hyperlink target referenced by index from inline spans.
type Link struct {
	Href string
	Text string
}

// Block is a block-level element.
type Block interface{ block() }

type (
	Heading struct {
		Level int
		Text  Inline
	}
	Paragraph struct{ Text Inline }
	Quote     struct{ Blocks []Block }
	List      struct {
		Ordered bool
		Items   [][]Block
	}
	Table struct {
		Header bool // first row is a header row
		Rows   [][]Inline
	}
	// Collapsible is a folded section (Wikidot [[collapsible]], tabs, spoilers).
	// It starts closed unless Open is set by the converter.
	Collapsible struct {
		ID     int
		Show   string // label while closed
		Hide   string // label while open (may be empty)
		Blocks []Block
		Open   bool
	}
	Image struct {
		Src     string
		Alt     string
		Caption string
	}
	Rule   struct{}
	Pre    struct{ Text string }
	Notice struct {
		Kind string // "info", "warn", "gimmick", "archive"
		Text string
	}
	Footnotes struct{ Notes []Footnote }
	// Embed is an unresolved embedded document (e.g. a Wikidot HTML block).
	// The source layer replaces it with the embedded content or removes it;
	// the renderer only sees it when resolution was not possible.
	Embed struct{ Src string }
)

// Footnote is a single numbered footnote.
type Footnote struct {
	Label string
	Text  Inline
}

func (Heading) block()     {}
func (Paragraph) block()   {}
func (Quote) block()       {}
func (List) block()        {}
func (Table) block()       {}
func (Collapsible) block() {}
func (Image) block()       {}
func (Rule) block()        {}
func (Pre) block()         {}
func (Notice) block()      {}
func (Footnotes) block()   {}
func (Embed) block()       {}

// Style is a bit set of inline text styles.
type Style uint8

const (
	Bold Style = 1 << iota
	Italic
	Underline
	Strike
	Code
	Redacted
	Sup
)

// Span is a run of text sharing one style and (optionally) one link.
type Span struct {
	Text  string
	Style Style
	Link  int  // 0 = no link, otherwise 1-based index into Document.Links
	Break bool // hard line break; Text is ignored
}

// Inline is a sequence of spans forming flowing text.
type Inline []Span

// PlainText returns the inline content without styling.
func (in Inline) PlainText() string {
	var b []byte
	for _, s := range in {
		if s.Break {
			b = append(b, '\n')
			continue
		}
		b = append(b, s.Text...)
	}
	return string(b)
}
