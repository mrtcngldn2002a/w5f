package discover

import (
	"context"
	"strings"
	"testing"
)

func TestWorldsCatalog(t *testing.T) {
	ws, err := Worlds()
	if err != nil || len(ws) < 20 {
		t.Fatalf("worlds: %v %d", err, len(ws))
	}
	seen := map[string]bool{}
	for _, w := range ws {
		if w.Name == "" || !strings.HasPrefix(w.URL, "https://") || w.Tier < 1 || w.Tier > 3 || seen[w.URL] {
			t.Errorf("bad entry: %+v", w)
		}
		seen[w.URL] = true
	}
	// Tier weighting: tier-1 worlds come up about three times as often as tier 3.
	count := map[int]int{}
	byTier := map[int]int{}
	for _, w := range ws {
		byTier[w.Tier]++
	}
	for i := 0; i < 6000; i++ {
		count[pickWorld(ws).Tier]++
	}
	per1 := float64(count[1]) / float64(byTier[1])
	per3 := float64(count[3]) / float64(byTier[3])
	if r := per1 / per3; r < 2.3 || r > 3.8 {
		t.Errorf("tier weighting ratio %.2f, want about 3", r)
	}
	env := testEnv(t)
	d, err := Route(context.Background(), "w5f:worlds", env)
	if err != nil || !strings.Contains(d.Title, "Worlds") || len(d.Links) < 20 {
		t.Fatalf("page: %v %d", err, len(d.Links))
	}
}

func TestWeirdAndFictionFamilies(t *testing.T) {
	env := testEnv(t)
	env.Crom = func(ctx context.Context, preset string) (string, error) {
		return "https://scp-wiki.wikidot.com/random-" + preset, nil
	}
	ctx := context.Background()
	for i := 0; i < 20; i++ {
		d, err := weird{}.Draw(ctx, env)
		if err != nil || d.Target == "" || !strings.HasPrefix(d.Why, "weird/") {
			t.Fatalf("weird: %+v %v", d, err)
		}
	}
	d, err := fiction{}.Draw(ctx, env)
	if err != nil || !strings.Contains(d.Target, "random-tale") {
		t.Errorf("fiction without Reddit falls back to a tale: %+v %v", d, err)
	}
	env.RedditTop = func(ctx context.Context, sub string) ([]string, error) {
		return []string{"https://www.reddit.com/r/" + sub + "/comments/x/story/"}, nil
	}
	d, _ = fiction{}.Draw(ctx, env)
	if !strings.Contains(d.Target, "/comments/") || !strings.HasPrefix(d.Why, "fiction/r/") {
		t.Errorf("fiction with Reddit: %+v", d)
	}
}
