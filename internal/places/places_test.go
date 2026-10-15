package places

import (
	"net/url"
	"strings"
	"testing"

	"w5f/internal/doc"
)

func text(d *doc.Document) string {
	var b strings.Builder
	for _, bl := range d.Blocks {
		switch x := bl.(type) {
		case doc.Paragraph:
			b.WriteString(x.Text.PlainText() + "\n")
		case doc.Notice:
			b.WriteString(x.Text + "\n")
		case doc.List:
			for _, it := range x.Items {
				b.WriteString("- " + it[0].(doc.Paragraph).Text.PlainText() + "\n")
			}
		}
	}
	return b.String()
}

// Pages are pinned in order, moved up, kept once, and unpinned after asking.
func TestPlaces(t *testing.T) {
	d := t.TempDir()
	t.Setenv("APPDATA", d)
	t.Setenv("XDG_DATA_HOME", d)
	t.Setenv("XDG_CONFIG_HOME", d)
	t.Setenv("HOME", d) // a Mac keeps its data under ~/Library
	open := func(target string) string {
		t.Helper()
		d, err := Route(target)
		if err != nil {
			t.Fatalf("%s: %v", target, err)
		}
		return text(d)
	}
	add := func(addr, name string) string {
		return open("w5f:places/add?" + url.Values{"url": {addr}, "name": {name}}.Encode())
	}
	if txt := open("w5f:places"); !strings.Contains(txt, "Nothing pinned yet.") {
		t.Errorf("empty:\n%s", txt)
	}
	add("https://www.tdv.example/", "TDV")
	if txt := add("https://marginalia.example/", ""); !strings.Contains(txt, "Pinned “marginalia.example”") ||
		!strings.Contains(txt, "- TDV  · unpin\n- marginalia.example  · move up · unpin") {
		t.Fatalf("pinned:\n%s", txt)
	}
	if txt := add("https://www.tdv.example/", "TDV İslâm Ansiklopedisi"); !strings.Contains(txt, "already in the Reading Room") || !strings.Contains(txt, "- TDV İslâm Ansiklopedisi") {
		t.Errorf("again, renamed:\n%s", txt)
	}
	open("w5f:places/up?" + url.Values{"url": {"https://marginalia.example/"}}.Encode())
	if ps, _ := Load(); len(ps) != 2 || ps[0].Name != "marginalia.example" || ps[1].Added == "" {
		t.Fatalf("moved up: %+v", ps)
	}
	remove := "w5f:places/remove?" + url.Values{"url": {"https://marginalia.example/"}}.Encode()
	if txt := open(remove); !strings.Contains(txt, "No, keep it · Yes, unpin it") {
		t.Errorf("asks first:\n%s", txt)
	}
	if txt := open(remove + "&sure=yes"); !strings.Contains(txt, "Unpinned “marginalia.example”") {
		t.Errorf("unpinned:\n%s", txt)
	}
	if ps, _ := Load(); len(ps) != 1 || ps[0].URL != "https://www.tdv.example/" {
		t.Errorf("left: %+v", ps)
	}
}
