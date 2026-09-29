package books

import (
	"strings"
	"testing"

	"w5f/internal/books/fixtures"
)

const kf8Text = `<?xml version="1.0" encoding="utf-8"?><html><head><title>t</title></head><body></body></html>` +
	`<div><h1>Part A</h1><p>Alpha text of the first part.</p><img src="kindle:embed:0001?mime=image/jpeg"/></div>` +
	`<?xml version="1.0" encoding="utf-8"?><html><body><h2>Part B</h2><p>Beta text, <a href="kindle:pos:fid:0001:off:0000000000">back</a>.</p></body></html>`

func TestKF8ComboAndStandalone(t *testing.T) {
	old := []byte("<html><body><p>The old MOBI 6 half of the combo file.</p></body></html>")
	kf8 := &fixtures.MOBI{Records: fixtures.Records([]byte(kf8Text), fixtures.PalmDOC), TextLength: len(kf8Text),
		Compression: 2, Encoding: 65001, Version: 8, Title: "Combo"}
	combo := fixtures.MOBI{Records: fixtures.Records(old, nil), TextLength: len(old), Compression: 1, Encoding: 65001,
		Version: 6, Title: "Combo", Author: "Someone", KF8: kf8}
	for name, data := range map[string][]byte{"combo.azw3": combo.Build(), "standalone.azw3": kf8.Build()} {
		r, err := Open(writeBook(t, name, data))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		chs := r.Contents()
		if len(chs) != 2 || chs[0].Title != "Part A" || chs[1].Title != "Part B" {
			t.Fatalf("%s: chapters %+v", name, chs)
		}
		d, _ := r.ChapterDoc(0, func(int) string { return "" })
		if !strings.Contains(flat(d), "Alpha text") || strings.Contains(flat(d), "old MOBI 6") {
			t.Errorf("%s: part A:\n%s", name, flat(d))
		}
		d, _ = r.ChapterDoc(1, func(int) string { return "" })
		if !strings.Contains(flat(d), "Beta text, back.") || len(d.Links) != 0 {
			t.Errorf("%s: part B %+v:\n%s", name, d.Links, flat(d))
		}
	}
}
