package doc

import (
	"regexp"
	"strings"
)

var (
	reGutStart = regexp.MustCompile(`\*\*\*\s*START OF (THE|THIS) PROJECT GUTENBERG`)
	reGutEnd   = regexp.MustCompile(`\*\*\*\s*END OF (THE|THIS) PROJECT GUTENBERG`)
)

// TidyGutenberg takes Project Gutenberg's wrapping off a text, wherever it
// was opened from (the site, a downloaded book's chapters): the site name
// in the title, the header up to "*** START OF …", the licence from
// "*** END OF …", and the bare "[Illustration]" markers. A page without
// these markers is left as it is.
func TidyGutenberg(d *Document) {
	if d == nil {
		return
	}
	d.Title = strings.TrimSuffix(d.Title, " | Project Gutenberg")
	if t, ok := strings.CutPrefix(d.Title, "The Project Gutenberg eBook of "); ok && t != "" {
		d.Title = t
	}
	start, end := -1, len(d.Blocks)
	for i, b := range d.Blocks {
		text := blockText(b)
		if start < 0 && end == len(d.Blocks) && reGutStart.MatchString(text) {
			start = i
		}
		if reGutEnd.MatchString(text) {
			end = i
			break
		}
	}
	if start < 0 && end == len(d.Blocks) {
		return
	}
	var kept []Block
	for _, b := range d.Blocks[start+1 : end] {
		if isIllustration(b) {
			continue
		}
		kept = append(kept, b)
	}
	d.Blocks = kept
}

func blockText(b Block) string {
	switch b := b.(type) {
	case Paragraph:
		return b.Text.PlainText()
	case Pre:
		return b.Text
	case Notice:
		return b.Text
	}
	return ""
}

// isIllustration is an image or paragraph that says only "[Illustration]":
// Gutenberg's stand-in for a picture with no caption.
func isIllustration(b Block) bool {
	var s string
	switch b := b.(type) {
	case Image:
		if b.Caption != "" {
			return false
		}
		s = b.Alt
	case Paragraph:
		s = b.Text.PlainText()
	default:
		return false
	}
	return strings.Trim(strings.TrimSpace(s), "[]") == "Illustration"
}
