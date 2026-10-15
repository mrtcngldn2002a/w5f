package htmlconv

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"w5f/internal/doc"
)

func TestArticleArkeofili(t *testing.T) {
	body, err := os.ReadFile("../../testdata/pages/arkeofili-article.html")
	if err != nil {
		t.Fatal(err)
	}
	d, err := Article(body, "https://arkeofili.com/misirda-grekce-siirli-bir-roma-piramit-mezari-kesfedildi/")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(d.Title, "Piramit Mezarı") {
		t.Errorf("title = %q", d.Title)
	}
	if !strings.HasPrefix(d.Lang, "tr") {
		t.Errorf("lang = %q", d.Lang)
	}
	text := allText(d.Blocks)
	if len(text) < 1500 {
		t.Errorf("article body too short (%d chars):\n%s", len(text), text)
	}
	for _, junk := range []string{"Çerez", "cookie", "Abone ol", "Paylaş"} {
		if strings.Contains(text, junk) {
			t.Logf("possible chrome leaked: %q", junk)
		}
	}
}

// A poem whose stanzas are parted by <br><br> inside one <p> (the markup of
// many old poetry sites) keeps its stanzas: Readability turns each chain into
// a new <p> by name, and the converter must see it as one.
func TestArticleKeepsStanzasParted(t *testing.T) {
	var b strings.Builder
	b.WriteString("<html><head><title>Break Our Harp</title></head><body><div><p class=\"poetry\">\n")
	for i := range 6 {
		fmt.Fprintf(&b, "Stanza %d opens with a long line of the ode about harps and reeds, <BR>\nand closes with its second line of verse. <BR>\n<BR>\n", i+1)
	}
	b.WriteString("</p></div></body></html>")
	d, err := Article([]byte(b.String()), "https://example.org/gh-0110.html")
	if err != nil {
		t.Fatal(err)
	}
	var stanzas []string
	for _, bl := range d.Blocks {
		if p, ok := bl.(doc.Paragraph); ok {
			stanzas = append(stanzas, p.Text.PlainText())
		}
	}
	if len(stanzas) != 6 {
		t.Fatalf("got %d paragraphs, want 6 stanzas: %q", len(stanzas), stanzas)
	}
	if s := stanzas[1]; !strings.HasPrefix(strings.TrimSpace(s), "Stanza 2") || strings.Contains(s, "Stanza 3") {
		t.Errorf("stanza 2 = %q", s)
	}
}

// A style rule pasted into a class attribute (the Ibn Arabi Society's papers:
// `class="et_pb_text comment {margin-left: 40px; ..."`) must not make the
// paper look like a reader's comment: before, only its notes were kept.
func TestArticleIgnoresCSSInClass(t *testing.T) {
	para := strings.Repeat("The aim of this paper is to describe the meaning of the People of the Night in the Futuhat. ", 8)
	note := strings.Repeat("[1] Al-Futuhat al-Makkiyya, I, pp. 237ff., Beirut edition. ", 5)
	page := `<html><head><title>The People of the Night</title></head><body><div class="et_pb_row">` +
		`<div class="et_pb_module et_pb_text et_pb_text_4 comment {margin-left: 40px; font-size: 12px; et_pb_text_align_left">` +
		strings.Repeat("<p>"+para+"</p>", 6) + `</div>` +
		`<div class="et_pb_module et_pb_text et_pb_text_6 et_pb_text_align_left">` + strings.Repeat("<p>"+note+"</p>", 6) + `</div>` +
		`</div></body></html>`
	d, err := Article([]byte(page), "https://ibnarabisociety.org/p-a-shamash-the-people-of-the-night/")
	if err != nil {
		t.Fatal(err)
	}
	if text := allText(d.Blocks); !strings.Contains(text, "The aim of this paper") {
		t.Errorf("the paper was dropped:\n%.300s", text)
	}
}

func TestUnescapeTwice(t *testing.T) {
	for in, want := range map[string]string{
		"Salaman and Absal: Sal&aacute;m&aacute;n": "Salaman and Absal: Salámán",
		"Tom & Jerry": "Tom & Jerry",
		"Plain":       "Plain",
	} {
		if got := unescapeTwice(in); got != want {
			t.Errorf("unescapeTwice(%q) = %q, want %q", in, got, want)
		}
	}
}
