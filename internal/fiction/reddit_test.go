package fiction

import (
	"context"
	"strings"
	"testing"
	"time"

	"w5f/internal/doc"
	"w5f/internal/reddit"
)

func TestSeriesKey(t *testing.T) {
	for title, want := range map[string]struct {
		key  string
		part int
	}{
		"I work at a lighthouse. Something is wrong. (Part 3)": {"i work at a lighthouse something is wrong", 3},
		"I work at a lighthouse. Something is wrong.":          {"i work at a lighthouse something is wrong", 0},
		"The Keeper's Log [Part II]":                           {"the keepers log", 2},
		"The Keeper's Log - Part 4 (Final)":                    {"the keepers log", 4},
		"The Keeper's Log pt. 5":                               {"the keepers log", 5},
		"My Uncle's Farm, Chapter 12":                          {"my uncles farm", 12},
		"The Keeper's Log: Update":                             {"the keepers log", 0},
	} {
		k, p := SeriesKey(title)
		if k != want.key || p != want.part {
			t.Errorf("%q → %q %d, want %q %d", title, k, p, want.key, want.part)
		}
	}
}

func post(title, id string, day int) reddit.PostInfo {
	return reddit.PostInfo{Sub: "r/nosleep", Author: "writer1", Title: title,
		URL: "https://www.reddit.com/r/nosleep/comments/" + id + "/x/", Posted: time.Date(2019, 10, day, 12, 0, 0, 0, time.UTC)}
}

func TestRedditSeriesFromTheAuthorsPosts(t *testing.T) {
	env := testEnv(t, redditAdapter{})
	posts := []reddit.PostInfo{ // newest first, as on the user page
		post("The Keeper's Log (Part 3)", "c3", 20),
		post("An unrelated story", "zz", 18),
		post("The Keeper's Log (Part 2)", "c2", 12),
		{Sub: "r/shortscarystories", Author: "writer1", Title: "The Keeper's Log (Part 2)", URL: "https://www.reddit.com/r/shortscarystories/comments/other/x/"},
		post("The Keeper's Log", "c1", 5),
	}
	old := listReddit
	listReddit = func(ctx context.Context, author string, pages int, stop func(reddit.PostInfo) bool) ([]reddit.PostInfo, error) {
		return posts, nil
	}
	defer func() { listReddit = old }()
	d := &doc.Document{Title: "The Keeper's Log (Part 2)", Blocks: []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: "Body."}}}}}
	cur := posts[2]
	DecorateRedditPost(context.Background(), env, d, &cur)
	if d.Prev != posts[4].URL || d.Next != posts[0].URL {
		t.Fatalf("prev/next: %q %q", d.Prev, d.Next)
	}
	if !strings.Contains(flat(d), "part 2 of 3") || !strings.HasPrefix(d.Ref, "rseries:") {
		t.Errorf("series line / ref: %q\n%s", d.Ref, flat(d))
	}
	id, ok := SerialOfPage(env.DB, d)
	if !ok {
		t.Fatal("F on a series post must store the series")
	}
	chs, _ := env.DB.SerialChapters(id)
	s, _ := env.DB.Serial(id)
	if len(chs) != 3 || chs[0].URL != posts[4].URL || s.Kind != "reddit" || s.Title != "The Keeper's Log" {
		t.Fatalf("stored series: %+v %+v", s, chs)
	}
	// A new part appears: the update check finds it.
	posts = append([]reddit.PostInfo{post("The Keeper's Log (Part 4)", "c4", 25)}, posts...)
	if n, err := Refresh(context.Background(), env, id); err != nil || n != 1 {
		t.Fatalf("refresh: %d %v", n, err)
	}
	// A post without other parts gets no series line.
	lone := post("A single story", "s1", 1)
	d2 := &doc.Document{}
	DecorateRedditPost(context.Background(), env, d2, &lone)
	if d2.Ref != "" || d2.Next != "" || len(d2.Blocks) != 0 {
		t.Errorf("lone post decorated: %+v", d2)
	}
}

func TestRedditSeriesOrderByPartNumbers(t *testing.T) {
	ps := []reddit.PostInfo{post("Log (Part 2)", "b", 3), post("Log (Part 1)", "a", 9), post("Log (Part 3)", "c", 1)}
	got := orderParts(ps)
	if got[0].URL != ps[1].URL || got[2].URL != ps[2].URL {
		t.Errorf("by number: %+v", got)
	}
	ps = []reddit.PostInfo{post("Log", "a", 1), post("Log (Part 3)", "c", 9), post("Log (Part 2)", "b", 5)}
	got = orderParts(ps)
	if got[0].URL != ps[0].URL || got[1].URL != ps[2].URL {
		t.Errorf("by time when a part has no number: %+v", got)
	}
}
