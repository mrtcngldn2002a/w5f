package discover

import (
	"context"
	"os"
	"testing"
	"time"

	"w5f/internal/fetch"
)

// TestUndergroundLive draws from each source once against the real sites.
// It needs the network, so it runs only with W5F_LIVE=1 (W5F_SOLVER=<url>
// names a bot-check helper for Erowid).
func TestUndergroundLive(t *testing.T) {
	if os.Getenv("W5F_LIVE") == "" {
		t.Skip("set W5F_LIVE=1 to draw from the real sites")
	}
	f := fetch.New(t.TempDir(), "test")
	f.SolverURL = os.Getenv("W5F_SOLVER")
	env := Env{Fetcher: f}
	for _, s := range undergroundSources {
		for range 3 {
			ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
			target, title, err := s.pick(ctx, env)
			cancel()
			if err != nil {
				t.Errorf("%s: %v", s.name, err)
				continue
			}
			t.Logf("%-22s %s  [%s]", s.name, target, title)
		}
	}
}
