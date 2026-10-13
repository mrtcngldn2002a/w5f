package discover

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"w5f/internal/fetch"
)

// TestFuturesLive reads old Nature Futures stories from the real Internet
// Archive: stories of each layout and of the oldest ids (2000–2001), then a
// few draws the way Deep random makes them. It needs the network, so it runs
// only with W5F_LIVE=1.
func TestFuturesLive(t *testing.T) {
	if os.Getenv("W5F_LIVE") == "" {
		t.Skip("set W5F_LIVE=1 to read from the real Archive")
	}
	f := fetch.New(t.TempDir(), "test")
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	env := Env{Fetcher: f}

	ids := []string{"516444a", "536244a", "544264a", "481540a", "454804a", "450584a", "4531284a", "4631112a", "35011126", "35012178", "35012720"}
	if extra := os.Getenv("W5F_FUTURES_IDS"); extra != "" {
		ids = strings.Fields(extra)
	}
	ok := 0
	for _, id := range ids {
		s, err := futuresStoryFor(ctx, f, id)
		if err != nil {
			t.Logf("%-10s ✗ %v", id, err)
			continue
		}
		ok++
		t.Logf("%-10s ✓ %4d words  %s  %-28q by %-18q from %s", id, wordCount(s.paras), s.from.stamp[:8], s.title, s.author, s.from.orig[strings.Index(s.from.orig, "nature.com"):])
		if len(s.paras) == 0 || strings.Contains(s.paras[0], "Nature |") || strings.Contains(strings.Join(s.paras, " "), "Credit:") {
			t.Errorf("%s: the text starts with page furniture: %.80q", id, strings.Join(s.paras, " "))
		}
	}
	t.Logf("%d of %d read", ok, len(ids))

	for range 6 {
		target, title, err := natureFuturesPick(ctx, env)
		if err != nil {
			t.Logf("draw ✗ %v", err)
			continue
		}
		t.Logf("draw ✓ %s · %s", target, title)
		if IsTarget(target) {
			d, err := Route(ctx, target, env)
			if err != nil {
				t.Errorf("%s: %v", target, err)
				continue
			}
			t.Logf("       page %q, %q, %d blocks", d.Title, d.Byline, len(d.Blocks))
		}
	}
}
