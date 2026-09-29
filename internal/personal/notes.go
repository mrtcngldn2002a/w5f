package personal

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"w5f/internal/doc"
)

// mdLink is a Markdown link; addresses with spaces or parentheses are
// wrapped in <…>.
func mdLink(title, u string) string {
	if u == "" {
		return mdEscape(title)
	}
	if strings.ContainsAny(u, " ()") {
		u = "<" + u + ">"
	}
	return "[" + mdEscape(title) + "](" + u + ")"
}

// newlines normalises line ends; Linux terminals paste lines with a lone CR.
var newlines = strings.NewReplacer("\r\n", "\n", "\r", "\n")

// AppendNote adds a dated note to the source's note file.
func AppendNote(s Source, text string, now time.Time) (string, error) {
	text = strings.TrimSpace(newlines.Replace(text))
	if text == "" {
		return "", errors.New("the note is empty")
	}
	p := pathFor(filepath.Join(Dir(), "Notes"), s)
	var b strings.Builder
	if !exists(p) {
		b.WriteString(frontmatter([]kv{{"title", s.Title}, {"url", s.URL}, {"catalog", s.Catalog}, {"kind", s.Kind},
			{"created", now.Format("2006-01-02")}}, "w5f, w5f/note"))
		b.WriteString("# " + s.Title + "\n")
	}
	b.WriteString("\n## " + now.Format("2006-01-02 15:04") + "\n" + text + "\n")
	return p, appendFile(p, b.String())
}

// AppendClipping adds paragraphs, with their source, to the day's
// clippings file.
func AppendClipping(s Source, paras []string, now time.Time) (string, error) {
	if len(paras) == 0 {
		return "", errors.New("nothing selected")
	}
	p := filepath.Join(Dir(), "Clippings", now.Format("2006"), now.Format("01"), now.Format("2006-01-02")+".md")
	var b strings.Builder
	if !exists(p) {
		b.WriteString(frontmatter([]kv{{"date", now.Format("2006-01-02")}}, "w5f, w5f/clippings"))
	}
	b.WriteString("\n## " + now.Format("15:04") + " · " + mdLink(s.Title, s.URL))
	if s.Catalog != "" {
		b.WriteString(" · " + s.Catalog)
	}
	b.WriteString("\n")
	for i, para := range paras {
		if i > 0 {
			b.WriteString(">\n")
		}
		// Escaped: text taken from a web page must stay text (no links).
		for _, ln := range strings.Split(strings.TrimSpace(newlines.Replace(para)), "\n") {
			b.WriteString("> " + mdEscape(ln) + "\n")
		}
	}
	return p, appendFile(p, b.String())
}

// SavePage writes a readable Markdown copy of a page to Saved/ (the latest
// save replaces the earlier one).
func SavePage(s Source, d *doc.Document, now time.Time) (string, error) {
	p := pathFor(filepath.Join(Dir(), "Saved"), s)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	content := frontmatter([]kv{{"title", s.Title}, {"url", s.URL}, {"catalog", s.Catalog}, {"kind", s.Kind},
		{"saved", now.Format("2006-01-02 15:04")}}, "w5f, w5f/saved") + ToMarkdown(d)
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return "", err
	}
	return p, os.Rename(tmp, p)
}
