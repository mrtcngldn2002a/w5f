package books

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"w5f/internal/books/fixtures"
)

// mobiText is two chapters with a filepos link from the first to the second
// and a Windows-1252 é.
func mobiText() []byte {
	t := []byte("<html><body><h1>Chapter One</h1><p>It was a dark night in Styria.</p>" +
		"<p><a filepos=XXXXXXXXXX>see two</a></p><mbp:pagebreak/><h1>Chapter Two</h1><p>Caf\xe9 au lait.</p></body></html>")
	pos := bytes.Index(t, []byte("<h1>Chapter Two"))
	return bytes.Replace(t, []byte("XXXXXXXXXX"), []byte(fmt.Sprintf("%010d", pos)), 1)
}

func writeBook(t *testing.T, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestMOBI6(t *testing.T) {
	raw := mobiText()
	for _, c := range []struct {
		name     string
		compress func([]byte) []byte
		comp     int
		trailing bool
	}{{"none", nil, 1, false}, {"palmdoc", fixtures.PalmDOC, 2, false}, {"trailing", fixtures.PalmDOC, 2, true}} {
		m := fixtures.MOBI{Records: fixtures.Records(raw, c.compress), TextLength: len(raw), Compression: c.comp,
			Encoding: 1252, Version: 6, Trailing: c.trailing, Title: "Carmilla", Author: "J. Sheridan Le Fanu", Lang: "en"}
		r, err := Open(writeBook(t, "carmilla.mobi", m.Build()))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got := r.Info(); got != (Meta{"Carmilla", "J. Sheridan Le Fanu", "en"}) {
			t.Errorf("%s: meta %+v", c.name, got)
		}
		chs := r.Contents()
		if len(chs) != 2 || chs[0].Title != "Chapter One" || chs[1].Title != "Chapter Two" {
			t.Fatalf("%s: chapters %+v", c.name, chs)
		}
		d, err := r.ChapterDoc(0, func(ch int) string { return fmt.Sprintf("w5f:book/1/ch/%d", ch) })
		if err != nil || len(d.Links) != 1 || d.Links[0].Href != "w5f:book/1/ch/1" || !strings.Contains(flat(d), "Styria") {
			t.Errorf("%s: chapter 1 %v %+v\n%s", c.name, err, d.Links, flat(d))
		}
		d, _ = r.ChapterDoc(1, func(int) string { return "" })
		if !strings.Contains(flat(d), "Café au lait.") {
			t.Errorf("%s: Windows-1252 text:\n%s", c.name, flat(d))
		}
	}
}

func TestChapterTitleFallsBackToAShortFirstParagraph(t *testing.T) {
	gutenberg := `</mbp:pagebreak><p height="5em" align="center"><font size="4"><b>CHAPTER I</b></font><br></br>` +
		`<font size="4"><b> </b></font>JONATHAN HARKER&#8217;S JOURNAL</p><p>3 May. Bistritz.—Left Munich at 8:35 P.M.</p>`
	if got := firstHeading(gutenberg); got != "CHAPTER I JONATHAN HARKER’S JOURNAL" {
		t.Errorf("title = %q", got)
	}
	long := `<p>` + strings.Repeat("A long opening paragraph without any title. ", 3) + `</p>`
	if got := firstHeading(`<p> </p>` + long); got != "" {
		t.Errorf("a long first paragraph is not a title: %q", got)
	}
}

func TestPalmDOCBackReferencesAndSpaces(t *testing.T) {
	in := []byte{'a', 'b', 'c', 0x80, 0x18, 0xF8, 0x02, 0xE9, 0x01}
	if got := string(palmdoc(in)); got != "abcabc x\xe9\x01" {
		t.Errorf("palmdoc = %q", got)
	}
}

func TestMOBIWithoutPageBreaksIsOneChapter(t *testing.T) {
	raw := []byte("<html><body><p>All in one place, without any page breaks at all.</p></body></html>")
	m := fixtures.MOBI{Records: fixtures.Records(raw, nil), TextLength: len(raw), Compression: 1, Encoding: 65001, Version: 6, Title: "One"}
	r, err := Open(writeBook(t, "one.azw", m.Build()))
	if err != nil || len(r.Contents()) != 1 {
		t.Fatalf("%v %+v", err, r)
	}
}

func TestMOBIRefusesDRMAndKFXAndSurvivesDamage(t *testing.T) {
	raw := mobiText()
	locked := fixtures.MOBI{Records: fixtures.Records(raw, nil), TextLength: len(raw), Compression: 1, Encoding: 1252, Version: 6, Encryption: 2}
	if _, err := Open(writeBook(t, "locked.azw3", locked.Build())); !errors.Is(err, ErrDRM) {
		t.Errorf("DRM: %v", err)
	}
	if _, err := Open(writeBook(t, "new.azw", append([]byte("CONT"), make([]byte, 100)...))); !errors.Is(err, ErrUnsupported) || !strings.Contains(err.Error(), "KFX") {
		t.Errorf("KFX: %v", err)
	}
	if _, err := Open(writeBook(t, "book.kfx", []byte("x"))); !errors.Is(err, ErrUnsupported) {
		t.Errorf(".kfx: %v", err)
	}
	good := fixtures.MOBI{Records: fixtures.Records(raw, fixtures.PalmDOC), TextLength: len(raw), Compression: 2, Encoding: 1252, Version: 6}.Build()
	for _, cut := range []int{10, 80, 100, len(good) / 2, len(good) - 5} {
		if _, err := Open(writeBook(t, "cut.mobi", good[:cut])); err == nil && cut < 100 {
			t.Errorf("truncated at %d: no error", cut)
		}
	}
	bad := append([]byte{}, good...)
	copy(bad[76:], []byte{0xFF, 0xFF}) // 65535 records claimed
	if _, err := Open(writeBook(t, "bad.mobi", bad)); err == nil {
		t.Error("impossible record count accepted")
	}
	junk := append([]byte{}, good...)
	for i := 78; i < 78+16; i++ {
		junk[i] = 0xEE // record offsets past the end of the file
	}
	if _, err := Open(writeBook(t, "junk.mobi", junk)); err == nil {
		t.Error("offsets past the end accepted")
	}
}
