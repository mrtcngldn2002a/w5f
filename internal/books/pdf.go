package books

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/ledongthuc/pdf"

	"w5f/internal/doc"
)

// pdfPagesPerChapter groups PDF pages into chapters.
const pdfPagesPerChapter = 10

// pdfBook is the text of a PDF.
type pdfBook struct {
	meta     Meta
	pages    []string
	chapters []Chapter
}

func init() {
	openers[".pdf"] = func(p string) (Reader, error) {
		b, err := openPDF(p)
		if err != nil {
			return nil, err
		}
		return b, nil
	}
}

// pdftotext runs poppler's pdftotext (a variable so tests can stub it).
var pdftotext = func(p string) ([]byte, error) {
	exe, err := exec.LookPath("pdftotext")
	if err != nil {
		return nil, err
	}
	return exec.Command(exe, "-enc", "UTF-8", p, "-").Output()
}

// pdfinfo reads Title and Author with poppler's pdfinfo (stubbed in tests).
var pdfinfo = func(p string) Meta {
	exe, err := exec.LookPath("pdfinfo")
	if err != nil {
		return Meta{}
	}
	out, err := exec.Command(exe, "-enc", "UTF-8", p).Output()
	if err != nil {
		return Meta{}
	}
	var m Meta
	for _, ln := range strings.Split(string(out), "\n") {
		k, v, ok := strings.Cut(ln, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(k) {
		case "Title":
			m.Title = strings.TrimSpace(v)
		case "Author":
			m.Author = strings.TrimSpace(v)
		}
	}
	return m
}

// pdfCacheDir keeps extracted text (a variable so tests can redirect it).
var pdfCacheDir = func() string {
	d, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(d, "w5f", "pdftext")
}

func openPDF(p string) (*pdfBook, error) {
	st, err := os.Stat(p)
	if err != nil {
		return nil, err
	}
	pages, meta, err := cachedPDF(p, st)
	if err != nil {
		return nil, err
	}
	total := 0
	for _, pg := range pages {
		total += len(strings.TrimSpace(pg))
	}
	if total < 50 {
		return nil, ErrNoText
	}
	b := &pdfBook{meta: meta, pages: dropRepeated(pages)}
	for i := 0; i < len(b.pages); i += pdfPagesPerChapter {
		b.chapters = append(b.chapters, Chapter{Title: fmt.Sprintf("Pages %d–%d", i+1, min(i+pdfPagesPerChapter, len(b.pages)))})
	}
	return b, nil
}

// cachedPDF extracts the text once per file version.
func cachedPDF(p string, st os.FileInfo) ([]string, Meta, error) {
	abs, _ := filepath.Abs(p)
	sum := sha1.Sum([]byte(fmt.Sprintf("%s|%d|%d", abs, st.Size(), st.ModTime().UnixNano())))
	var cache string
	if dir := pdfCacheDir(); dir != "" {
		cache = filepath.Join(dir, hex.EncodeToString(sum[:])+".txt")
		if b, err := os.ReadFile(cache); err == nil {
			head, body, _ := strings.Cut(string(b), "\n")
			title, author, _ := strings.Cut(head, "\t")
			return strings.Split(body, "\f"), Meta{Title: title, Author: author}, nil
		}
	}
	pages, meta, err := extractPDF(p)
	if err != nil {
		return nil, Meta{}, err
	}
	if cache != "" && os.MkdirAll(filepath.Dir(cache), 0o755) == nil {
		head := strings.ReplaceAll(meta.Title, "\t", " ") + "\t" + strings.ReplaceAll(meta.Author, "\t", " ")
		_ = os.WriteFile(cache, []byte(strings.ReplaceAll(head, "\n", " ")+"\n"+strings.Join(pages, "\f")), 0o644)
	}
	return pages, meta, nil
}

func extractPDF(p string) ([]string, Meta, error) {
	if out, err := pdftotext(p); err == nil {
		pages := strings.Split(strings.TrimRight(string(out), "\f\n"), "\f")
		return pages, pdfinfo(p), nil
	}
	return goPDFText(p)
}

// goPDFText reads the text with the pure-Go library, page by page.
func goPDFText(p string) (pages []string, m Meta, err error) {
	defer func() {
		if r := recover(); r != nil { // the library panics on some damaged files
			pages, err = nil, fmt.Errorf("the PDF could not be read: %v", r)
		}
	}()
	f, r, err := pdf.Open(p)
	if err != nil {
		return nil, Meta{}, fmt.Errorf("the PDF could not be read: %w", err)
	}
	defer f.Close()
	info := r.Trailer().Key("Info")
	m = Meta{Title: info.Key("Title").Text(), Author: info.Key("Author").Text()}
	for i := 1; i <= r.NumPage(); i++ {
		pg := r.Page(i)
		if pg.V.IsNull() {
			pages = append(pages, "")
			continue
		}
		t, err := pg.GetPlainText(nil)
		if err != nil {
			t = ""
		}
		pages = append(pages, t)
	}
	if len(pages) == 0 {
		return nil, Meta{}, errors.New("the PDF has no pages")
	}
	return pages, m, nil
}

func (b *pdfBook) Info() Meta          { return b.meta }
func (b *pdfBook) Contents() []Chapter { return b.chapters }
func (b *pdfBook) Close() error        { return nil }

// ChapterDoc shows ten pages as paragraphs, with a page mark after each.
func (b *pdfBook) ChapterDoc(i int, link func(ch int) string) (*doc.Document, error) {
	if i < 0 || i >= len(b.chapters) {
		return nil, errors.New("no such chapter")
	}
	d := &doc.Document{}
	start := i * pdfPagesPerChapter
	for j := start; j < min(start+pdfPagesPerChapter, len(b.pages)); j++ {
		d.Blocks = append(d.Blocks, pdfBlocks(b.pages[j])...)
		d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: fmt.Sprintf("— page %d —", j+1), Style: doc.Italic}}})
	}
	return d, nil
}

var (
	reDigitsRun = regexp.MustCompile(`\d+`)
	rePageNum   = regexp.MustCompile(`^\d{1,4}$`)
)

// dropRepeated removes running headers and footers — a first or last line
// that repeats (digits ignored) on most pages — and bare page numbers.
func dropRepeated(pages []string) []string {
	norm := func(s string) string { return strings.Join(strings.Fields(reDigitsRun.ReplaceAllString(s, "#")), " ") }
	edges := func(lines []string) (int, int) {
		first, last := -1, -1
		for j, l := range lines {
			if strings.TrimSpace(l) != "" {
				if first < 0 {
					first = j
				}
				last = j
			}
		}
		return first, last
	}
	count := map[string]int{}
	for _, pg := range pages {
		lines := strings.Split(pg, "\n")
		f, l := edges(lines)
		if f < 0 {
			continue
		}
		count[norm(lines[f])]++
		if l != f {
			count[norm(lines[l])]++
		}
	}
	out := make([]string, len(pages))
	for i, pg := range pages {
		lines := strings.Split(pg, "\n")
		f, l := edges(lines)
		var keep []string
		for j, ln := range lines {
			if j == f || j == l {
				t := strings.TrimSpace(ln)
				if rePageNum.MatchString(t) || (len(pages) >= 4 && count[norm(ln)]*10 >= len(pages)*6) {
					continue
				}
			}
			keep = append(keep, ln)
		}
		out[i] = strings.Join(keep, "\n")
	}
	return out
}

// pdfBlocks turns page text into paragraphs: blank lines separate them,
// hyphenated line ends are joined, a short standalone line is a heading.
func pdfBlocks(text string) []doc.Block {
	var out []doc.Block
	var cur []string
	flush := func() {
		if len(cur) > 0 {
			out = append(out, doc.Paragraph{Text: doc.Inline{{Text: joinPDFLines(cur)}}})
			cur = nil
		}
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r", ""), "\n")
	blank := func(i int) bool { return i < 0 || i >= len(lines) || strings.TrimSpace(lines[i]) == "" }
	for i, ln := range lines {
		t := strings.TrimSpace(ln)
		if t == "" {
			flush()
			continue
		}
		r := []rune(t)
		if len(cur) == 0 && blank(i-1) && blank(i+1) && len(r) <= 60 && !strings.ContainsRune(".,;:!?)", r[len(r)-1]) {
			out = append(out, doc.Heading{Level: 3, Text: doc.Inline{{Text: t}}})
			continue
		}
		cur = append(cur, t)
	}
	flush()
	return out
}

func joinPDFLines(ls []string) string {
	res := ls[0]
	for _, next := range ls[1:] {
		r := []rune(res)
		nr := []rune(next)
		if len(r) > 1 && r[len(r)-1] == '-' && unicode.IsLetter(r[len(r)-2]) && len(nr) > 0 && unicode.IsLower(nr[0]) {
			res = string(r[:len(r)-1]) + next
			continue
		}
		res += " " + next
	}
	return res
}
