package index

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"w5f/internal/doc"
	"w5f/internal/store"
)

func tmpDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func textDoc(title string, paras ...string) *doc.Document {
	d := &doc.Document{Title: title}
	for _, p := range paras {
		d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: p}}})
	}
	return d
}

func TestKind(t *testing.T) {
	cases := []struct{ target, ref, kind, key string }{
		{"https://scp-wiki.wikidot.com/scp-173", "", "scp", "https://scp-wiki.wikidot.com/scp-173"},
		{"https://wanderers-library.wikidot.com/x", "", "wiki", "https://wanderers-library.wikidot.com/x"},
		{"https://old.reddit.com/r/nosleep/", "", "reddit", "https://old.reddit.com/r/nosleep/"},
		{"https://arkeofili.com/x", "item:12", "feed", "w5f:item/12"},
		{"w5f:book/3/ch/4", "book:3:4", "book", "w5f:book/3/ch/4"},
		{"https://example.org/a", "", "web", "https://example.org/a"},
		{"w5f:feeds", "", "", ""},
		{"C:/notes/a.md", "", "", ""},
	}
	for _, c := range cases {
		k, key := Kind(c.target, &doc.Document{Ref: c.ref})
		if k != c.kind || key != c.key {
			t.Errorf("Kind(%q, %q) = %q %q", c.target, c.ref, k, key)
		}
	}
}

func TestTextCoversBlocksAndIsCapped(t *testing.T) {
	d := &doc.Document{Blocks: []doc.Block{
		doc.Heading{Level: 2, Text: doc.Inline{{Text: "Head"}}},
		doc.List{Items: [][]doc.Block{{doc.Paragraph{Text: doc.Inline{{Text: "item one"}}}}}},
		doc.Quote{Blocks: []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: "quoted"}}}}},
		doc.Collapsible{ID: 1, Show: "more", Blocks: []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: "folded away"}}}}},
		doc.Table{Rows: [][]doc.Inline{{{{Text: "a"}}, {{Text: "b"}}}}},
		doc.Pre{Text: "code line"},
	}}
	got := Text(d)
	for _, want := range []string{"Head", "item one", "quoted", "folded away", "a · b", "code line"} {
		if !strings.Contains(got, want) {
			t.Errorf("Text lacks %q: %q", want, got)
		}
	}
	big := textDoc("big", strings.Repeat("şüphe ", 1_000_000)) // ~7 MB
	if n := len(Text(big)); n > MaxText || n < MaxText-8 {
		t.Errorf("text not capped at %d: %d", MaxText, n)
	}
}

func TestSearchAcrossKindsTurkishAndSnippets(t *testing.T) {
	db := tmpDB(t)
	Page(db, "https://scp-wiki.wikidot.com/scp-173", textDoc("SCP-173", "The statue moves when nobody looks."))
	Page(db, "https://arkeofili.com/a", &doc.Document{Title: "Göbekli Tepe", Ref: "item:5",
		Blocks: []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: "Kafatası kültü ve bir statue parçası bulundu."}}}}})
	Page(db, "w5f:book/2/ch/1", &doc.Document{Title: "Dracula · Chapter 2", Ref: "book:2:1",
		Blocks: []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: "He stood like a statue."}}}}})
	hits, err := Search(db, "statue", "", 30, 0)
	if err != nil || len(hits) != 3 {
		t.Fatalf("search: %v %+v", err, hits)
	}
	hits, _ = Search(db, "kafatasi", "", 30, 0)
	if len(hits) != 1 || hits[0].Kind != "feed" || hits[0].Target != "w5f:item/5" {
		t.Fatalf("Turkish folding: %+v", hits)
	}
	var sn strings.Builder
	bold := ""
	for _, s := range hits[0].Snippet {
		sn.WriteString(s.Text)
		if s.Style&doc.Bold != 0 {
			bold += s.Text
		}
	}
	if !strings.Contains(sn.String(), "Kafatası") || bold != "Kafatası" {
		t.Errorf("snippet %q bold %q", sn.String(), bold)
	}
	if hits, _ := Search(db, "statue", "book", 30, 0); len(hits) != 1 || hits[0].Kind != "book" {
		t.Errorf("kind filter: %+v", hits)
	}
	if hits, _ := Search(db, `"moves when"`, "", 30, 0); len(hits) != 1 {
		t.Errorf("phrase: %+v", hits)
	}
	if hits, _ := Search(db, "stat", "", 30, 0); len(hits) != 3 {
		t.Errorf("prefix: %+v", hits)
	}
}

func TestSearchInputNeverBreaksTheQuery(t *testing.T) {
	db := tmpDB(t)
	Page(db, "https://example.org/a", textDoc("A", "near the end and more"))
	for _, q := range []string{`"`, `near"the`, `*`, `-`, `AND`, `NEAR(`, `title:x`, `"unbalanced`, `^ ~ ( )`, `...`} {
		if _, err := Search(db, q, "", 30, 0); err != nil {
			t.Errorf("Search(%q): %v", q, err)
		}
	}
	if hits, _ := Search(db, "AND near", "", 30, 0); len(hits) != 1 {
		t.Errorf("operator words are plain words: %+v", hits)
	}
}

func TestFeedsIndexesNewItems(t *testing.T) {
	db := tmpDB(t)
	db.UpsertItem(store.Item{FeedID: "arkeofili", GUID: "g1", Title: "Göbekli Tepe", Content: "<p>Taş <b>heykel</b></p>", Published: time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)})
	n, err := Feeds(db)
	if err != nil || n != 1 {
		t.Fatalf("Feeds: %v %d", err, n)
	}
	if n, _ := Feeds(db); n != 0 {
		t.Error("items indexed twice")
	}
	hits, _ := Search(db, "heykel", "", 30, 0)
	if len(hits) != 1 || hits[0].Catalog != "PER·ARKEOF·2026-09-24" {
		t.Errorf("feed hit: %+v", hits)
	}
}

func TestResultsPage(t *testing.T) {
	db := tmpDB(t)
	for i := 0; i < 31; i++ {
		Page(db, "https://example.org/p"+string(rune('a'+i%26))+string(rune('a'+i/26)), textDoc("Page", "statue"))
	}
	d, err := Route("w5f:find?q=statue", db)
	if err != nil {
		t.Fatal(err)
	}
	var hrefs []string
	for _, l := range d.Links {
		hrefs = append(hrefs, l.Href)
	}
	all := strings.Join(hrefs, " ")
	if !strings.Contains(all, "kind=feed") || !strings.Contains(all, "page=2") || !strings.Contains(all, "https://example.org/p") {
		t.Errorf("links: %s", all)
	}
	if d, _ := Route("w5f:find?q=nothingatall", db); !strings.Contains(d.Blocks[len(d.Blocks)-1].(doc.Paragraph).Text.PlainText(), "Nothing found") {
		t.Error("empty result message")
	}
}

func now() time.Time { return time.Date(2026, 9, 28, 10, 21, 0, 0, time.Local) }

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
