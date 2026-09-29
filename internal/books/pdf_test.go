package books

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"w5f/internal/books/fixtures"
	"w5f/internal/doc"
)

// stubPoppler replaces pdftotext/pdfinfo and the cache folder for a test.
func stubPoppler(t *testing.T, text string, fail bool) *int {
	t.Helper()
	calls := 0
	oldT, oldI, oldC := pdftotext, pdfinfo, pdfCacheDir
	dir := t.TempDir()
	pdftotext = func(string) ([]byte, error) {
		calls++
		if fail {
			return nil, errors.New("pdftotext: not installed")
		}
		return []byte(text), nil
	}
	pdfinfo = func(string) Meta { return Meta{Title: "Stubbed Title", Author: "Stub Author"} }
	pdfCacheDir = func() string { return dir }
	t.Cleanup(func() { pdftotext, pdfinfo, pdfCacheDir = oldT, oldI, oldC })
	return &calls
}

func TestPDFViaPdftotext(t *testing.T) {
	var pages []string
	for i := 1; i <= 12; i++ {
		body := fmt.Sprintf("Paragraph of page %d starts here and con-\ntinues on the next line.\nIt ends here.", i)
		if i == 1 {
			body = "Introduction\n\n" + body
		}
		pages = append(pages, fmt.Sprintf("The Book of Tests\n\n%s\n\n%d", body, i))
	}
	calls := stubPoppler(t, strings.Join(pages, "\f")+"\f", false)
	p := writeBook(t, "tests.pdf", []byte("%PDF-1.4 stub"))
	r, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if r.Info().Title != "Stubbed Title" {
		t.Errorf("meta %+v", r.Info())
	}
	chs := r.Contents()
	if len(chs) != 2 || chs[0].Title != "Pages 1–10" || chs[1].Title != "Pages 11–12" {
		t.Fatalf("chapters %+v", chs)
	}
	d, _ := r.ChapterDoc(0, func(int) string { return "" })
	text := flat(d)
	if !strings.Contains(text, "Paragraph of page 1 starts here and continues on the next line. It ends here.") {
		t.Errorf("paragraph joining:\n%s", text)
	}
	if strings.Contains(text, "The Book of Tests") || strings.Contains(text, "\n1\n") {
		t.Errorf("running header or page number kept:\n%s", text)
	}
	heading := false
	for _, b := range d.Blocks {
		if h, ok := b.(doc.Heading); ok {
			switch h.Text.PlainText() {
			case "Introduction":
				heading = true
			case "The Book of Tests":
				t.Error("running header kept as a heading")
			}
		}
	}
	if !heading {
		t.Error("short standalone line not taken as a heading")
	}
	Open(p) // second open comes from the cache
	if *calls != 1 {
		t.Errorf("pdftotext ran %d times", *calls)
	}
}

func TestPDFWithoutTextAndGoFallback(t *testing.T) {
	stubPoppler(t, "\f\f", false)
	if _, err := Open(writeBook(t, "scan.pdf", []byte("%PDF-1.4 stub"))); !errors.Is(err, ErrNoText) {
		t.Errorf("scanned PDF: %v", err)
	}
	stubPoppler(t, "", true) // pdftotext missing: the Go library reads the file
	r, err := Open(writeBook(t, "hello.pdf", fixtures.PDF("Hello from a real PDF file with enough text to read.")))
	if err != nil {
		t.Fatal(err)
	}
	d, _ := r.ChapterDoc(0, func(int) string { return "" })
	if !strings.Contains(strings.Join(strings.Fields(flat(d)), " "), "Hello from a real PDF file") || r.Info().Title != "Test PDF" {
		t.Errorf("Go path: %+v\n%s", r.Info(), flat(d))
	}
	if _, err := Open(writeBook(t, "broken.pdf", []byte("%PDF-1.4 not really"))); err == nil {
		t.Error("an unreadable PDF must give an error")
	}
}
