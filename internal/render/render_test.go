package render

import (
	"flag"
	"os"
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"

	"w5f/internal/doc"
	"w5f/internal/htmlconv"
)

var update = flag.Bool("update", false, "rewrite golden files")

func text(l *Layout) string {
	var b strings.Builder
	for _, ln := range l.Lines {
		b.WriteString(strings.TrimRight(ln.Text(), " "))
		b.WriteByte('\n')
	}
	return b.String()
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := "../../testdata/golden/" + name
	if *update {
		if err := os.MkdirAll("../../testdata/golden", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run: go test ./internal/render -update)", err)
	}
	if string(want) != got {
		t.Errorf("%s differs from golden; run with -update if the change is intended\n--- got ---\n%s", name, got)
	}
}

func fixture(t *testing.T, name, u string) *doc.Document {
	f, err := os.Open("../../testdata/pages/" + name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	d, err := htmlconv.Wikidot(f, u)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestGoldenSCP173(t *testing.T) {
	d := fixture(t, "scp-173.html", "https://scp-wiki.wikidot.com/scp-173")
	golden(t, "scp-173.w72.txt", text(Render(d, Options{Width: 72})))
	golden(t, "scp-173.w40.open.txt", text(Render(d, Options{Width: 40, Open: map[int]bool{1: true}})))
}

func TestGoldenWanderers(t *testing.T) {
	d := fixture(t, "wl-six-etchings.html", "https://wanderers-library.wikidot.com/six-etchings-in-the-basalt-of-olympus-mons")
	golden(t, "wl-six-etchings.w72.txt", text(Render(d, Options{Width: 72})))
}

func TestNoLineExceedsWidth(t *testing.T) {
	d := fixture(t, "wl-six-etchings.html", "https://wanderers-library.wikidot.com/x")
	for _, w := range []int{20, 33, 60, 72, 100} {
		open := map[int]bool{}
		for i := 1; i <= d.Collapsibles; i++ {
			open[i] = true
		}
		l := Render(d, Options{Width: w, Open: open})
		for i, ln := range l.Lines {
			if got := runewidth.StringWidth(ln.Text()); got > w {
				t.Fatalf("width %d: line %d is %d cells: %q", w, i, got, ln.Text())
			}
		}
	}
}

func TestFoldToggleAndFocus(t *testing.T) {
	d := &doc.Document{
		Links: []doc.Link{{Href: "https://a.example/x"}},
		Blocks: []doc.Block{
			doc.Paragraph{Text: doc.Inline{{Text: "see "}, {Text: "the long linked phrase here", Link: 1}, {Text: " now"}}},
			doc.Collapsible{ID: 1, Show: "Addendum", Hide: "Hide", Blocks: []doc.Block{
				doc.Paragraph{Text: doc.Inline{{Text: "secret"}}},
			}},
		},
		Collapsibles: 1,
	}
	closed := Render(d, Options{Width: 20})
	if strings.Contains(text(closed), "secret") {
		t.Error("closed collapsible leaked its content")
	}
	if len(closed.Focus) != 2 || closed.Focus[0].Kind != FocusLink || closed.Focus[1].Kind != FocusFold {
		t.Fatalf("focusables = %+v", closed.Focus)
	}
	// The link wraps across lines but must stay a single focusable.
	n := 0
	for _, ln := range closed.Lines {
		for _, s := range ln.Segs {
			if s.Link == 1 && s.Focus != 1 {
				t.Errorf("wrapped link segment has focus %d", s.Focus)
			}
			if s.Link == 1 {
				n++
			}
		}
	}
	if n < 2 {
		t.Errorf("expected the link to wrap, got %d segments", n)
	}
	open := Render(d, Options{Width: 20, Open: map[int]bool{1: true}})
	got := text(open)
	if !strings.Contains(got, "[-] Hide") || !strings.Contains(got, "│ secret") {
		t.Errorf("open collapsible rendering:\n%s", got)
	}
}

func TestHeadingCaseFollowsLanguage(t *testing.T) {
	h := []doc.Block{doc.Heading{Level: 2, Text: doc.Inline{{Text: "İçindekiler ve ışık, fiction"}}}}
	tr := text(Render(&doc.Document{Lang: "tr", Blocks: h}, Options{Width: 40}))
	if !strings.Contains(tr, "İÇİNDEKİLER VE IŞIK, FİCTİON") {
		t.Errorf("tr heading = %q", tr)
	}
	en := text(Render(&doc.Document{Lang: "en", Blocks: h}, Options{Width: 40}))
	if !strings.Contains(en, "FICTION") {
		t.Errorf("en heading = %q", en)
	}
}

// A list item with several paragraphs shows its marker once.
func TestListMarkerOnlyOnFirstBlock(t *testing.T) {
	d := &doc.Document{Blocks: []doc.Block{doc.List{Ordered: true, Items: [][]doc.Block{{
		doc.Paragraph{Text: doc.Inline{{Text: "Title"}}},
		doc.Paragraph{Text: doc.Inline{{Text: "snippet"}}},
	}}}}}
	got := text(Render(d, Options{Width: 40}))
	if strings.Count(got, "1.") != 1 || !strings.Contains(got, "   snippet") {
		t.Errorf("list rendering:\n%s", got)
	}
}
