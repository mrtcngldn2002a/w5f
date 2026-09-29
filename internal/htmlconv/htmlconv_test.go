package htmlconv

import (
	"os"
	"strings"
	"testing"

	"w5f/internal/doc"
)

func loadFixture(t *testing.T, name, pageURL string) *doc.Document {
	t.Helper()
	f, err := os.Open("../../testdata/pages/" + name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	d, err := Wikidot(f, pageURL)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// allText flattens a document to plain text for content assertions.
func allText(bs []doc.Block) string {
	var b strings.Builder
	var visit func([]doc.Block)
	visit = func(bs []doc.Block) {
		for _, x := range bs {
			switch x := x.(type) {
			case doc.Paragraph:
				b.WriteString(x.Text.PlainText() + "\n")
			case doc.Heading:
				b.WriteString(x.Text.PlainText() + "\n")
			case doc.Collapsible:
				b.WriteString("[C:" + x.Show + "]\n")
				visit(x.Blocks)
			case doc.Quote:
				visit(x.Blocks)
			case doc.List:
				for _, it := range x.Items {
					visit(it)
				}
			case doc.Notice:
				b.WriteString("[N:" + x.Text + "]\n")
			}
		}
	}
	visit(bs)
	return b.String()
}

func TestWikidotSCP173(t *testing.T) {
	d := loadFixture(t, "scp-173.html", "https://scp-wiki.wikidot.com/scp-173")
	if d.Title != "SCP-173" {
		t.Errorf("title = %q", d.Title)
	}
	text := allText(d.Blocks)
	for _, want := range []string{"Special Containment Procedures:", "Object Class: Euclid", "[C:Licensing / Citation]"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q", want)
		}
	}
	for _, junk := range []string{"rating:", "I like it", "Cancel my vote"} {
		if strings.Contains(text, junk) {
			t.Errorf("widget text leaked: %q", junk)
		}
	}
	if d.Collapsibles != 1 {
		t.Errorf("collapsibles = %d, want 1", d.Collapsibles)
	}
	var rating string
	for _, kv := range d.Meta {
		if kv.Key == "rating" {
			rating = kv.Value
		}
	}
	if !strings.HasPrefix(rating, "+") {
		t.Errorf("rating meta = %q", rating)
	}
	// Internal links resolve against the page URL.
	found := false
	for _, l := range d.Links {
		if l.Href == "https://scp-wiki.wikidot.com/scp-174" {
			found = true
		}
	}
	if !found {
		t.Error("wikiwalk link to scp-174 not resolved")
	}
}

func TestCollapsibleStartsClosedWithCleanLabels(t *testing.T) {
	d := loadFixture(t, "scp-173.html", "https://scp-wiki.wikidot.com/scp-173")
	var col *doc.Collapsible
	for _, b := range d.Blocks {
		if c, ok := b.(doc.Collapsible); ok {
			col = &c
		}
	}
	if col == nil {
		t.Fatal("no collapsible found")
	}
	if col.Open {
		t.Error("collapsible must start closed")
	}
	if col.Show != "Licensing / Citation" || col.Hide != "Hide Licensing / Citation" {
		t.Errorf("labels = %q / %q", col.Show, col.Hide)
	}
	if col.ID != 1 || len(col.Blocks) == 0 {
		t.Errorf("id=%d blocks=%d", col.ID, len(col.Blocks))
	}
}

func TestHiddenAndGimmick(t *testing.T) {
	d := loadFixture(t, "scp-3125.html", "https://scp-wiki.wikidot.com/scp-3125")
	text := allText(d.Blocks)
	if strings.Contains(text, "colourless green") {
		t.Error("hidden backlinks leaked into the page")
	}
	found := false
	for _, b := range d.Blocks {
		if e, ok := b.(doc.Embed); ok && strings.Contains(e.Src, "/scp-3125/html/") {
			found = true
		}
	}
	if !found {
		t.Error("embedded html block not captured as an Embed")
	}
}

func TestWanderersTale(t *testing.T) {
	d := loadFixture(t, "wl-six-etchings.html", "https://wanderers-library.wikidot.com/six-etchings-in-the-basalt-of-olympus-mons")
	text := allText(d.Blocks)
	if !strings.Contains(text, "Olympus Mons") || !strings.Contains(text, "Its name is Regret.") {
		t.Error("tale body missing")
	}
	// "More From This Author" stays; the Translations box is deliberately dropped.
	if d.Collapsibles != 1 || strings.Contains(text, "Translations") {
		t.Errorf("collapsibles = %d\n%s", d.Collapsibles, text)
	}
}

func TestTabsBecomeCollapsibles(t *testing.T) {
	src := `<div id="page-content"><div class="yui-navset"><ul class="yui-nav"><li><a><em>Log</em></a></li><li><a><em>Addendum</em></a></li></ul>
<div class="yui-content"><div><p>first</p></div><div style="display:none"><p>second</p></div></div></div></div>`
	d, err := Wikidot(strings.NewReader(src), "https://x.wikidot.com/p")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Blocks) != 2 {
		t.Fatalf("blocks = %d", len(d.Blocks))
	}
	a, b := d.Blocks[0].(doc.Collapsible), d.Blocks[1].(doc.Collapsible)
	if a.Show != "Log" || !a.Open || b.Show != "Addendum" || b.Open {
		t.Errorf("tabs = %+v / %+v", a, b)
	}
	if allText(b.Blocks) != "second\n" {
		t.Errorf("hidden tab pane content lost: %q", allText(b.Blocks))
	}
}

func TestRedactionAndInlineStyles(t *testing.T) {
	src := `<div id="page-content"><p>The <strong>object</strong> was found at <span style="background-color: black; color: black">Site-19</span> by ████.</p></div>`
	d, err := Wikidot(strings.NewReader(src), "https://x.wikidot.com/p")
	if err != nil {
		t.Fatal(err)
	}
	p := d.Blocks[0].(doc.Paragraph)
	var bold, red bool
	for _, s := range p.Text {
		if s.Text == "object" && s.Style&doc.Bold != 0 {
			bold = true
		}
		if s.Text == "Site-19" && s.Style&doc.Redacted != 0 {
			red = true
		}
	}
	if !bold || !red {
		t.Errorf("bold=%v redacted=%v: %+v", bold, red, p.Text)
	}
	if !strings.Contains(p.Text.PlainText(), "████") {
		t.Error("block redaction characters must be preserved")
	}
}
