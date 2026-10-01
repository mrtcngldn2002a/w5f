package ultan

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"w5f/internal/doc"
	"w5f/internal/store"
)

// testDB is a database whose history was read from a log of visits made
// at the given times (the log is how history is written in the past).
func testDB(t *testing.T, visits ...store.LoggedVisit) *store.DB {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("W5F_NOTES", filepath.Join(dir, "notes"))
	var b strings.Builder
	for _, v := range visits {
		fmt.Fprintf(&b, `{"t":%q,"ti":%q,"k":%q,"at":%d}`+"\n", v.Target, v.Title, v.Kind, v.At.UnixNano())
	}
	if err := os.WriteFile(filepath.Join(dir, "history.log"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(dir, "w5f.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.RestoreHistory(); err != nil {
		t.Fatal(err)
	}
	return db
}

func flat(bs []doc.Block) string {
	var b strings.Builder
	doc.ReplaceBlocks(bs, func(x doc.Block) ([]doc.Block, bool) {
		switch x := x.(type) {
		case doc.Paragraph:
			b.WriteString(x.Text.PlainText() + "\n")
		case doc.Heading:
			b.WriteString(x.Text.PlainText() + "\n")
		case doc.Table:
			for _, r := range x.Rows {
				for _, c := range r {
					b.WriteString(c.PlainText() + " | ")
				}
				b.WriteString("\n")
			}
		}
		return nil, false
	})
	return b.String()
}

func TestDeskNotes(t *testing.T) {
	now := time.Date(2026, 10, 1, 20, 0, 0, 0, time.UTC)
	// Nothing at all: one of his sayings, the same all day.
	if n := Desk(nil, now); n.Text == "" || Desk(nil, now.Add(time.Hour)).Text != n.Text {
		t.Errorf("saying: %+v", n)
	}

	// Away for a while: that comes first.
	db := testDB(t, store.LoggedVisit{Target: "https://a.example/x", Title: "X", Kind: "web", At: now.AddDate(0, 0, -9)})
	if n := Desk(db, now); !strings.Contains(n.Text, "Nine days") {
		t.Errorf("absence: %q", n.Text)
	}

	// A page left half-read eleven days ago, while other pages were read
	// since: Ultan keeps its place.
	db = testDB(t,
		store.LoggedVisit{Target: "https://a.example/sand", Title: "An Undertow of Sand", Kind: "web", At: now.AddDate(0, 0, -11)},
		store.LoggedVisit{Target: "https://a.example/other", Title: "Other", Kind: "web", At: now.Add(-2 * time.Hour)},
	)
	if err := db.SavePos("https://a.example/sand", 0.34); err != nil {
		t.Fatal(err)
	}
	n := Desk(db, now)
	if !strings.Contains(n.Text, "An Undertow of Sand") || !strings.Contains(n.Text, "eleven days") || !strings.Contains(n.Text, "34%") ||
		n.Subject != "An Undertow of Sand" || n.Href != "https://a.example/sand" {
		t.Errorf("stale: %+v", n)
	}
	// On the page: the title is a link, the note signed.
	var links []string
	text := flat(n.Blocks(func(href, text string) int { links = append(links, href); return len(links) }))
	if len(links) != 1 || !strings.HasSuffix(strings.TrimSpace(text), Signature) {
		t.Errorf("blocks: %q %v", text, links)
	}
}

func TestShelfNotes(t *testing.T) {
	db := testDB(t)
	now := time.Date(2026, 10, 1, 20, 0, 0, 0, time.UTC)
	if n := Shelf(db, "esoteric", now); !strings.HasPrefix(n.Text, "From the locked shelves") || strings.Contains(n.Text, "this week") {
		t.Errorf("first draw: %q", n.Text)
	}
	Shelf(db, "weird", now.Add(time.Hour))
	if n := Shelf(db, "esoteric", now.Add(2*time.Hour)); !strings.Contains(n.Text, "twice this week") {
		t.Errorf("second esoteric draw: %q", n.Text)
	}
	if n := Shelf(db, "esoteric", now.Add(3*time.Hour)); !strings.Contains(n.Text, "three times this week") {
		t.Errorf("third: %q", n.Text)
	}
	if n := Shelf(db, "esoteric", now.AddDate(0, 0, 10)); strings.Contains(n.Text, "this week") {
		t.Errorf("a week later: %q", n.Text)
	}
	if n := Shelf(nil, "nonesuch", now); !strings.Contains(n.Text, "do not know") {
		t.Errorf("unknown shelf: %q", n.Text)
	}
}

func TestCoverNote(t *testing.T) {
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	n := Cover([]string{"periodical", "periodical", "periodical", "weird", "esoteric", "queue"}, "XVI The Tower", now)
	for _, want := range []string{"three periodicals", "a world that never was", "an esoteric text", " and a page from your queue", "XVI The Tower"} {
		if !strings.Contains(n.Text, want) {
			t.Errorf("cover lacks %q: %q", want, n.Text)
		}
	}
}

func TestLedger(t *testing.T) {
	// Noon today: an hour ago and a day ago must be different days, which
	// they are not just after midnight.
	y, mo, day := time.Now().Date()
	now := time.Date(y, mo, day, 12, 0, 0, 0, time.Local)
	db := testDB(t,
		store.LoggedVisit{Target: "w5f:item/1", Title: "An essay", Kind: "feed", At: now.AddDate(0, 0, -20)},
		store.LoggedVisit{Target: "https://a.example/b", Title: "B", Kind: "web", At: now.AddDate(0, 0, -2)},
		store.LoggedVisit{Target: "https://a.example/b", Title: "B", Kind: "web", At: now.AddDate(0, 0, -1)},
		store.LoggedVisit{Target: "https://a.example/c", Title: "C", Kind: "web", At: now.Add(-time.Hour)},
	)
	d, err := Ledger(db, now)
	if err != nil {
		t.Fatal(err)
	}
	text := flat(d.Blocks)
	for _, want := range []string{"Ultan's Ledger", "Days at the desk | 3 | 4 | 4", "Pages opened | 3 | 4 | 4", "Different pages | 2 | 3 | 3",
		"Periodicals | 0 | 1 | 1", "The open web | 2 | 2 | 2", "Most returned to", "B", Signature} {
		if !strings.Contains(d.Title+"\n"+text, want) {
			t.Errorf("ledger lacks %q:\n%s", want, text)
		}
	}
	empty, _ := Ledger(testDB(t), now)
	if !strings.Contains(flat(empty.Blocks), "nothing has been read yet") {
		t.Error("an empty ledger")
	}
}
