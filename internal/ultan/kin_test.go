package ultan

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"w5f/internal/doc"
	"w5f/internal/index"
	"w5f/internal/store"
)

func put(t *testing.T, db *store.DB, target, title, text string) {
	t.Helper()
	if err := db.PutDoc(store.IndexDoc{Target: target, Kind: "web", Title: title, Text: text, Updated: time.Now(),
		FoldTitle: index.Fold(title), FoldText: index.Fold(text)}); err != nil {
		t.Fatal(err)
	}
}

func TestDeskRemembers(t *testing.T) {
	now := time.Date(2026, 10, 1, 20, 0, 0, 0, time.UTC)
	db := testDB(t)
	// Nothing to report: a saying a day, none said twice in a month, the
	// same one all day.
	seen := map[string]bool{}
	for d := range 30 {
		at := now.AddDate(0, 0, d)
		n := Desk(db, at)
		if seen[n.Text] {
			t.Fatalf("day %d repeats %q", d, n.Text)
		}
		seen[n.Text] = true
		if again := Desk(db, at.Add(time.Hour)); again.Text != n.Text {
			t.Fatalf("day %d changed: %q, then %q", d, n.Text, again.Text)
		}
	}
	// Two things to say: they take turns, and each comes back in other words.
	visits := []store.LoggedVisit{{Target: "https://a.example/sand", Title: "An Undertow of Sand", Kind: "web", At: now.AddDate(0, 0, -11)}}
	for d := range 6 {
		visits = append(visits, store.LoggedVisit{Target: fmt.Sprintf("https://a.example/day%d", d), Title: "A day", Kind: "web", At: now.AddDate(0, 0, -d).Add(-time.Hour)})
	}
	db = testDB(t, visits...)
	if err := db.SavePos("https://a.example/sand", 0.34); err != nil {
		t.Fatal(err)
	}
	var texts []string
	for d := range 4 {
		texts = append(texts, Desk(db, now.Add(time.Duration(d)*24*time.Hour)).Text)
	}
	for i := 1; i < len(texts); i++ {
		if strings.Contains(texts[i], "Undertow") == strings.Contains(texts[i-1], "Undertow") {
			t.Errorf("no turns: %q", texts)
			break
		}
	}
	if texts[0] == texts[2] || texts[1] == texts[3] {
		t.Errorf("the same words again: %q", texts)
	}
	// One thing to say: it gives way to a saying the next day.
	db = testDB(t, visits[0], store.LoggedVisit{Target: "https://a.example/x", Title: "X", Kind: "web", At: now.Add(-time.Hour)})
	if err := db.SavePos("https://a.example/sand", 0.34); err != nil {
		t.Fatal(err)
	}
	a, b, c := Desk(db, now), Desk(db, now.AddDate(0, 0, 1)), Desk(db, now.AddDate(0, 0, 2))
	if !strings.Contains(a.Text, "Undertow") || strings.Contains(b.Text, "Undertow") || !strings.Contains(c.Text, "Undertow") {
		t.Errorf("one fact: %q / %q / %q", a.Text, b.Text, c.Text)
	}
}

func TestKin(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.Local)
	db := testDB(t,
		store.LoggedVisit{Target: "https://a.example/keeper", Title: "The Keeper of Eddystone", Kind: "web", At: now.AddDate(0, 0, -5)},
		store.LoggedVisit{Target: "https://a.example/once", Title: "A Lamp", Kind: "web", At: now.AddDate(0, 0, -6)},
	)
	common := " the people would always come together around their houses and gardens before evening "
	for i := range 30 {
		put(t, db, fmt.Sprintf("https://filler.example/%d", i), fmt.Sprintf("Filler %d", i), common+fmt.Sprintf(" number%c ", 'a'+i%26))
	}
	put(t, db, "https://a.example/keeper", "The Keeper of Eddystone", common+"the lighthouse keeper trimmed the paraffin wicks; Eddystone lighthouse stood through the gale")
	put(t, db, "https://a.example/once", "A Lamp", common+"a single lighthouse on a hill")
	// Unread: in the index but not in the history.
	put(t, db, "https://a.example/unread", "Unread", common+"lighthouse paraffin wicks gale Eddystone")

	page := &doc.Document{Title: "Smeaton's Tower", Blocks: []doc.Block{doc.Paragraph{Text: doc.Inline{
		{Text: "Smeaton rebuilt the Lighthouse at Eddystone; its keeper burned Paraffin in later years."}}}}}
	n, ok := Echo(db, "https://b.example/smeaton", page, now)
	if !ok || n.Href != "https://a.example/keeper" || n.Subject != "The Keeper of Eddystone" {
		t.Fatalf("echo: %+v %v", n, ok)
	}
	for _, want := range []string{"on 3 October", "“eddystone”", "“paraffin”"} {
		if !strings.Contains(n.Text, want) {
			t.Errorf("echo lacks %q: %q", want, n.Text)
		}
	}
	// One rare word in common is not kinship.
	lone := &doc.Document{Title: "Gales", Blocks: []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: "A gale blew over the moor."}}}}}
	if n, ok := Echo(db, "https://b.example/gales", lone, now); ok {
		t.Errorf("one word: %+v", n)
	}
	// The desk: a page read today, and its kin among those read before.
	if err := db.Visit("https://b.example/smeaton", "Smeaton's Tower", "web", ""); err != nil {
		t.Fatal(err)
	}
	put(t, db, "https://b.example/smeaton", "Smeaton's Tower", index.Text(page))
	k, ok := deskKin(db, time.Now())
	if !ok || k.Href != "https://a.example/keeper" || k.From != "Smeaton's Tower" {
		t.Fatalf("desk kin: %+v %v", k, ok)
	}
	// Chapters of one book are not each other's kin.
	if work("w5f:book/3/ch/2") != work("w5f:book/3/ch/7") || work("https://a.example/ch/2") == work("https://a.example/ch/7") {
		t.Error("work")
	}
}

func TestQuotedAndWhen(t *testing.T) {
	if q := quoted([]string{"a", "b", "c", "d"}); q != "“a”, “b” and “c”" {
		t.Error(q)
	}
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.Local)
	for at, want := range map[time.Time]string{now.Add(-time.Hour): "earlier today", now.AddDate(0, 0, -1): "yesterday",
		now.AddDate(0, 0, -5): "on 3 October", now.AddDate(-1, 0, 0): "on 8 October 2025"} {
		if got := when(at, now); got != want {
			t.Errorf("when(%v) = %q", at, got)
		}
	}
}
