package books

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"w5f/internal/doc"
	"w5f/internal/store"
)

// Meta is what a book says about itself.
type Meta struct{ Title, Author, Lang string }

// Reader is an open book of a format W5F reads itself.
type Reader interface {
	Info() Meta
	Contents() []Chapter // reading order; titles may be empty
	ChapterDoc(i int, link func(ch int) string) (*doc.Document, error)
	Close() error
}

// Errors a book can open with; the book page explains them and offers the
// external viewer.
var (
	ErrDRM         = errors.New("this book is DRM-protected; W5F cannot open it — use the reader it was bought for")
	ErrUnsupported = errors.New("this book format is not supported yet")
	ErrNoText      = errors.New("this PDF has no text layer (probably scanned)")
)

// openers are the readers by file extension; each format file adds its own.
var openers = map[string]func(path string) (Reader, error){
	".epub": func(p string) (Reader, error) {
		e, err := OpenEPUB(p)
		if err != nil {
			return nil, err
		}
		return e, nil
	},
}

// bookExt is a file's book extension, ".fb2.zip" included.
func bookExt(p string) string {
	if strings.HasSuffix(strings.ToLower(p), ".fb2.zip") {
		return ".fb2.zip"
	}
	return strings.ToLower(filepath.Ext(p))
}

// Open opens a book with the reader for its format.
func Open(p string) (Reader, error) {
	if f, ok := openers[bookExt(p)]; ok {
		return f(p)
	}
	return nil, ErrUnsupported
}

// chaptered reports books opened through Open (with chapters).
func chaptered(p string) bool {
	_, ok := openers[bookExt(p)]
	return ok
}

// opensExternally reports formats handed to the system viewer.
func opensExternally(p string) bool { return external[bookExt(p)] && !chaptered(p) }

// bookProblem reports errors that get the explanation page instead of a
// failure.
func bookProblem(err error) bool {
	return errors.Is(err, ErrDRM) || errors.Is(err, ErrUnsupported) || errors.Is(err, ErrNoText)
}

// Info and Contents make EPUB a Reader.
func (e *EPUB) Info() Meta          { return Meta{Title: e.Title, Author: e.Author, Lang: e.Lang} }
func (e *EPUB) Contents() []Chapter { return e.Chapters }

// problemDoc explains why a book cannot be read in W5F and offers the
// external viewer.
func problemDoc(b store.Book, err error) *doc.Document {
	d := &doc.Document{Title: b.Title, URL: fmt.Sprintf("w5f:book/%d", b.ID), Origin: "book", Lang: "en"}
	if b.Author != "" {
		d.Meta = []doc.KV{{Key: "·", Value: b.Author}}
	}
	d.Blocks = []doc.Block{
		doc.Notice{Kind: "warn", Text: err.Error()},
		doc.Paragraph{Text: doc.Inline{{Text: "→ open in the external viewer", Link: link(d, fmt.Sprintf("w5f:books/open/%d", b.ID), "open externally")}}},
	}
	return d
}
