package books

import (
	"strings"
	"testing"

	"w5f/internal/books/fixtures"
)

func TestMOBIWithHuffCDIC(t *testing.T) {
	entries := []fixtures.HuffEntry{
		{Data: []byte("<html><body><h1>Huff</h1><p>"), Literal: true},
		{Data: []byte("Compressed "), Literal: true},
		{Data: []byte("words"), Literal: true},
		{Data: []byte("</p></body></html>"), Literal: true},
		{Data: fixtures.HuffEncode(1, 2)}, // a recursive phrase: "Compressed words"
	}
	text := "<html><body><h1>Huff</h1><p>Compressed words</p></body></html>"
	m := fixtures.MOBI{Records: [][]byte{fixtures.HuffEncode(0, 4, 3)}, TextLength: len(text), Compression: 17480,
		Encoding: 65001, Version: 6, Title: "Huff", Huff: fixtures.Huff(entries)}
	r, err := Open(writeBook(t, "huff.mobi", m.Build()))
	if err != nil {
		t.Fatal(err)
	}
	d, err := r.ChapterDoc(0, func(int) string { return "" })
	if err != nil || !strings.Contains(flat(d), "Compressed words") {
		t.Errorf("%v\n%s", err, flat(d))
	}
}

func TestHuffDamagedSymbolIsAnError(t *testing.T) {
	entries := []fixtures.HuffEntry{{Data: []byte("only one"), Literal: true}}
	m := fixtures.MOBI{Records: [][]byte{fixtures.HuffEncode(7)}, TextLength: 8, Compression: 17480,
		Encoding: 65001, Version: 6, Huff: fixtures.Huff(entries)}
	if _, err := Open(writeBook(t, "bad-huff.mobi", m.Build())); err == nil {
		t.Error("a symbol outside the dictionary must fail")
	}
	loop := []fixtures.HuffEntry{{Data: fixtures.HuffEncode(0)}} // phrase 0 refers to itself
	m = fixtures.MOBI{Records: [][]byte{fixtures.HuffEncode(0)}, TextLength: 8, Compression: 17480,
		Encoding: 65001, Version: 6, Huff: fixtures.Huff(loop)}
	if _, err := Open(writeBook(t, "loop-huff.mobi", m.Build())); err == nil {
		t.Error("a self-referencing phrase must fail, not recurse forever")
	}
}
