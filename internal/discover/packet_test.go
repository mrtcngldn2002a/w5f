package discover

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"w5f/internal/doc"
	"w5f/internal/store"
)

func packetEnv(t *testing.T) Env {
	t.Helper()
	env := testEnv(t)
	notes := t.TempDir()
	t.Setenv("W5F_NOTES", notes)
	os.WriteFile(filepath.Join(notes, "Queue.md"), []byte("# Reading queue\n\n## This week\n- [x] [Done one](https://example.org/done)\n- [ ] [Six Etchings](https://wanderers-library.wikidot.com/six-etchings)\n"), 0o644)
	for i, feed := range []string{"aeon", "aeon", "arkeofili", "quanta", "jstor"} {
		env.DB.UpsertItem(store.Item{FeedID: feed, GUID: fmt.Sprint(i), URL: fmt.Sprintf("https://%s.example/%d", feed, i),
			Title: fmt.Sprintf("%s article %d", feed, i), Published: time.Now().Add(-time.Duration(i) * time.Hour)})
	}
	old := packetDraws
	n := 0
	packetDraws = map[string]func(context.Context, Env) (Draw, error){
		"weird": func(context.Context, Env) (Draw, error) {
			n++ // a new page each time: the packet does not repeat itself
			return Draw{Target: fmt.Sprintf("https://scp-wiki.wikidot.com/scp-%d", 2263+n), Why: "weird/SCP"}, nil
		},
		"esoteric": func(context.Context, Env) (Draw, error) {
			return Draw{Target: "http://gnosis.org/naghamm/gosphil.html", Why: "esoteric/gnosis.org · Gospel of Philip"}, nil
		},
		"public": func(context.Context, Env) (Draw, error) {
			return Draw{Target: "https://publicdomainreview.org/essay/x", Why: "public domain/PDR · Monsters"}, nil
		},
		"archive": func(context.Context, Env) (Draw, error) {
			return Draw{Target: "http://www.textfiles.com/occult/chaos.txt", Why: "textfiles/occult · chaos.txt"}, nil
		},
	}
	t.Cleanup(func() { packetDraws = old })
	return env
}

func TestDailyPacket(t *testing.T) {
	env := packetEnv(t)
	ctx := context.Background()
	cover, err := Route(ctx, "w5f:packet", env)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := todayPacket(env)
	if p.Number != 1 || len(p.Entries) != 8 {
		t.Fatalf("packet: %+v", p)
	}
	feeds := map[string]bool{}
	for _, e := range p.Entries[:3] {
		if e.Kind != "periodical" {
			t.Fatalf("first three are periodicals: %+v", p.Entries)
		}
		feeds[e.Source] = true
	}
	if len(feeds) != 3 {
		t.Errorf("three different feeds: %v", feeds)
	}
	last := p.Entries[len(p.Entries)-1]
	if last.Kind != "queue" || last.Title != "Six Etchings" {
		t.Errorf("queue entry: %+v", last)
	}
	txt := cover.Title + "\n" + flatText(cover)
	if !strings.Contains(txt, "No. 1") || !strings.Contains(txt, "VIII.") || !strings.Contains(txt, "Gospel of Philip") {
		t.Errorf("cover:\n%s", txt)
	}
	// Same issue all day.
	again, _ := todayPacket(env)
	if again.Entries[3].Target != p.Entries[3].Target {
		t.Error("the day's packet must stay the same")
	}
	// Reading: an entry page with packet navigation.
	d, err := Route(ctx, fmt.Sprintf("w5f:packet/%s/2", p.Date), env)
	if err != nil || d.Prev == "" || d.Next == "" || !strings.Contains(flatText(d), "II of VIII") {
		t.Fatalf("entry: %v %+v\n%s", err, d, flatText(d))
	}
	// Save the issue as Markdown in the notes folder.
	s, err := Route(ctx, fmt.Sprintf("w5f:packet/%s/save", p.Date), env)
	if err != nil {
		t.Fatal(err)
	}
	matches, _ := filepath.Glob(filepath.Join(os.Getenv("W5F_NOTES"), "Saved", "Daily Packet No. 1*.md"))
	if len(matches) != 1 || !strings.Contains(flatText(s), "Saved") {
		t.Fatalf("saved: %v %s", matches, flatText(s))
	}
	b, _ := os.ReadFile(matches[0])
	if !strings.Contains(string(b), "Gospel of Philip") || !strings.Contains(string(b), "body") {
		t.Errorf("issue file:\n%.400s", b)
	}
	// Reshuffle rebuilds today's issue with the same number.
	packetDraws["esoteric"] = func(context.Context, Env) (Draw, error) {
		return Draw{Target: "https://other.example/", Why: "folklore/Theoi · Hekate"}, nil
	}
	Route(ctx, fmt.Sprintf("w5f:packet/%s/reshuffle", p.Date), env)
	// It shows nothing of the first: the sections that only had the same
	// page again are left out.
	r, _ := todayPacket(env)
	i := slices.IndexFunc(r.Entries, func(e Entry) bool { return e.Kind == "esoteric" })
	if r.Number != 1 || i < 0 || !strings.Contains(r.Entries[i].Title, "Hekate") {
		t.Errorf("reshuffled: %+v", r.Entries)
	}
	for _, e := range r.Entries {
		if e.Kind != "queue" && slices.ContainsFunc(p.Entries, func(o Entry) bool { return o.Target == e.Target }) {
			t.Errorf("reshuffle kept %+v", e)
		}
	}
}

// The next day's issue shows nothing of the day before: other items, other
// feeds while there are some, and a draw that only finds yesterday's page
// gives its section up.
func TestPacketRemembers(t *testing.T) {
	env := packetEnv(t)
	for i := 0; i < 6; i++ {
		feed := []string{"aeon", "arkeofili", "quanta", "jstor", "psyche", "nautilus"}[i]
		env.DB.UpsertItem(store.Item{FeedID: feed, GUID: fmt.Sprint("b", i), URL: fmt.Sprintf("https://%s.example/b%d", feed, i),
			Title: fmt.Sprintf("%s second %d", feed, i), Published: time.Now().Add(-time.Duration(i) * time.Minute)})
	}
	ctx := context.Background()
	day1 := buildPacket(ctx, env, "2026-10-01", 1)
	day2 := buildPacket(ctx, env, "2026-10-02", 2)
	shown := map[string]bool{}
	feeds1 := map[string]bool{}
	for _, e := range day1.Entries {
		if e.Kind != "queue" {
			shown[e.Target] = true
		}
		if e.Kind == "periodical" {
			feeds1[e.Source] = true
		}
	}
	for _, e := range day2.Entries {
		if shown[e.Target] {
			t.Errorf("shown again the next day: %+v", e)
		}
		if e.Kind == "periodical" && feeds1[e.Source] {
			t.Errorf("feed %s again although others had unread items", e.Source)
		}
		if e.Kind == "esoteric" {
			t.Errorf("the esoteric draw only had yesterday's page, yet: %+v", e)
		}
	}
	// A month later the memory has let go.
	if len(loadMemory(env.DB, "2026-11-05").entries) != 0 {
		t.Error("entries older than 30 days are kept")
	}
}

// Every section comes round before any comes twice.
func TestPacketSectionsTakeTurns(t *testing.T) {
	env := testEnv(t)
	first := append(nextKinds(env.DB, issueDraws, nil), nextKinds(env.DB, issueDraws, nil)...)
	seen := map[string]bool{}
	for _, k := range first {
		if seen[k] {
			t.Fatalf("%s came twice in the first two issues: %v", k, first)
		}
		seen[k] = true
	}
	third := nextKinds(env.DB, issueDraws, nil)
	left := 0
	for _, k := range packetPool {
		if !seen[k] {
			left++
			if !slices.Contains(third, k) {
				t.Errorf("%s was left for later again: %v", k, third)
			}
		}
	}
	if left != len(packetPool)-2*issueDraws {
		t.Errorf("pool %d, left %d", len(packetPool), left)
	}
	for _, kinds := range [][]string{first[:issueDraws], first[issueDraws:], third} {
		if len(kinds) != issueDraws || len(slices.Compact(slices.Sorted(slices.Values(kinds)))) != issueDraws {
			t.Errorf("an issue's sections: %v", kinds)
		}
	}
}

// The periodicals come from different shelves, those not seen lately first.
func TestPacketPeriodicalShelves(t *testing.T) {
	env := testEnv(t)
	shelves := map[string]string{"aeon": "mind", "psyche": "mind", "quanta": "science", "nautilus": "science", "arkeofili": "tr", "lithub": "books"}
	for id := range shelves {
		env.DB.UpsertItem(store.Item{FeedID: id, GUID: id, URL: "https://" + id + ".example/", Title: id, Published: time.Now()})
	}
	env.DB.UpsertItem(store.Item{FeedID: "gone", GUID: "gone", URL: "https://gone.example/", Title: "gone", Published: time.Now()})
	m := loadMemory(env.DB, "2026-10-02")
	m.shelfAge["books"] = 0 // in yesterday's issue
	for range 20 {
		got := pickPeriodicals(env.DB, shelves, m, 3)
		bySh := map[string]bool{}
		for _, it := range got {
			if it.FeedID == "gone" || it.FeedID == "lithub" {
				t.Fatalf("picked %s: %v", it.FeedID, got)
			}
			bySh[shelves[it.FeedID]] = true
		}
		if len(got) != 3 || len(bySh) != 3 {
			t.Fatalf("three shelves: %+v", got)
		}
	}
}

func TestEntryTitles(t *testing.T) {
	for d, want := range map[Draw][2]string{
		{Target: "https://scp-wiki.wikidot.com/scp-2264", Why: "weird/SCP Foundation"}:      {"SCP Foundation · scp-2264", "weird/SCP Foundation"},
		{Target: "http://gnosis.org/x.html", Why: "esoteric/gnosis.org · Gospel of Philip"}: {"Gospel of Philip", "esoteric/gnosis.org"},
	} {
		if e := entryOf("weird", d); e.Title != want[0] || e.Source != want[1] {
			t.Errorf("%+v → %q / %q", d, e.Title, e.Source)
		}
	}
}

func flatText(d *doc.Document) string {
	var b strings.Builder
	doc.ReplaceBlocks(d.Blocks, func(x doc.Block) ([]doc.Block, bool) {
		switch p := x.(type) {
		case doc.Paragraph:
			b.WriteString(p.Text.PlainText() + "\n")
		case doc.Heading:
			b.WriteString(p.Text.PlainText() + "\n")
		case doc.Notice:
			b.WriteString(p.Text + "\n")
		}
		return nil, false
	})
	for _, kv := range d.Meta {
		b.WriteString(kv.Value + "\n")
	}
	return b.String()
}
