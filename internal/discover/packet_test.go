package discover

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
	packetDraws = map[string]func(context.Context, Env) (Draw, error){
		"weird": func(context.Context, Env) (Draw, error) {
			return Draw{Target: "https://scp-wiki.wikidot.com/scp-2264", Why: "weird/SCP"}, nil
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
	r, _ := todayPacket(env)
	if r.Number != 1 || !strings.Contains(r.Entries[4].Title, "Hekate") {
		t.Errorf("reshuffled: %+v", r.Entries[4])
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
