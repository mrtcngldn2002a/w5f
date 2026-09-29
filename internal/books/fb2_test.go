package books

import (
	"strings"
	"testing"

	"w5f/internal/books/fixtures"
	"w5f/internal/doc"
)

const fb2Inner = `<description><title-info><author><first-name>Николай</first-name><last-name>Гоголь</last-name></author>` +
	`<book-title>Вий</book-title><lang>ru</lang></title-info></description>` +
	`<body><title><p>Вий</p></title>` +
	`<section id="s1"><title><p>Глава I</p><p>Бурса</p></title>` +
	`<epigraph><p>Как только ударял колокол.</p><text-author>Автор</text-author></epigraph>` +
	`<p>Первый абзац&nbsp;текста<a l:href="#n1" type="note">1</a>.</p>` +
	`<poem><stanza><v>Первая строка</v><v>Вторая строка</v></stanza></poem></section>` +
	`<section><title><p>Глава II</p></title><p>Текст с <emphasis>курсивом</emphasis>, <strong>жирным</strong> и ` +
	`<a l:href="#s1">ссылкой</a>.</p><section><title><p>Подглава</p></title><p>Вложенный текст.</p></section></section>` +
	`</body><body name="notes"><section id="n1"><title><p>1</p></title><p>Примечание к тексту.</p></section></body>`

func TestFB2Encodings(t *testing.T) {
	for name, data := range map[string][]byte{
		"utf8.fb2":       fixtures.FB2(fb2Inner, "utf-8"),
		"cp1251.fb2":     fixtures.FB2(fb2Inner, "windows-1251"),
		"zipped.fb2.zip": fixtures.Zip("book.fb2", fixtures.FB2(fb2Inner, "windows-1251")),
	} {
		r, err := Open(writeBook(t, name, data))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := r.Info(); got != (Meta{"Вий", "Николай Гоголь", "ru"}) {
			t.Errorf("%s: meta %+v", name, got)
		}
		chs := r.Contents()
		if len(chs) != 3 || chs[1].Title != "Глава I — Бурса" || chs[2].Title != "Глава II" {
			t.Fatalf("%s: chapters %+v", name, chs)
		}
		d, err := r.ChapterDoc(1, func(ch int) string { return "ch" + string(rune('0'+ch)) })
		if err != nil {
			t.Fatal(err)
		}
		text := flat(d)
		if !strings.Contains(text, "Первый абзац текста[1].") || !strings.Contains(text, "Первая строка") {
			t.Errorf("%s: chapter 1:\n%s", name, text)
		}
		var quote, notes, poem bool
		for _, b := range d.Blocks {
			switch x := b.(type) {
			case doc.Quote:
				quote = true
			case doc.Footnotes:
				notes = len(x.Notes) == 1 && strings.Contains(x.Notes[0].Text.PlainText(), "Примечание")
			case doc.Paragraph:
				for _, sp := range x.Text {
					if sp.Break {
						poem = true
					}
				}
			}
		}
		if !quote || !notes || !poem {
			t.Errorf("%s: epigraph %v, notes %v, poem lines %v", name, quote, notes, poem)
		}
		d, _ = r.ChapterDoc(2, func(ch int) string { return "ch" + string(rune('0'+ch)) })
		if len(d.Links) != 1 || d.Links[0].Href != "ch1" || !strings.Contains(flat(d), "Вложенный текст.") ||
			!strings.Contains(flat(d), "Текст с курсивом, жирным и ссылкой.") {
			t.Errorf("%s: chapter 2 links %+v:\n%s", name, d.Links, flat(d))
		}
		italic := false
		doc.ReplaceBlocks(d.Blocks, func(b doc.Block) ([]doc.Block, bool) {
			if p, ok := b.(doc.Paragraph); ok {
				for _, sp := range p.Text {
					if sp.Text == "курсивом" && sp.Style&doc.Italic != 0 {
						italic = true
					}
				}
			}
			return nil, false
		})
		if !italic {
			t.Errorf("%s: emphasis lost", name)
		}
	}
}

func TestFB2EdgeCases(t *testing.T) {
	one := fixtures.FB2(`<description><title-info><book-title>Short</book-title></title-info></description><body><p>Only a body without sections.</p></body>`, "utf-8")
	r, err := Open(writeBook(t, "one.fb2", one))
	if err != nil || len(r.Contents()) != 1 {
		t.Fatalf("no sections: %v %+v", err, r)
	}
	if _, err := Open(writeBook(t, "odd.fb2", fixtures.FB2(`<body><p>x</p></body>`, "x-unknown-charset"))); err == nil {
		t.Error("an unknown encoding must give an error")
	}
	if _, err := Open(writeBook(t, "broken.fb2", []byte("<FictionBook><body><p>unclosed"))); err != nil {
		t.Errorf("unclosed tags should still read: %v", err)
	}
}
