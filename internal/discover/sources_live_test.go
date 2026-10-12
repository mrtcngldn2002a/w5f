package discover

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"w5f/internal/doc"
	"w5f/internal/fetch"
	"w5f/internal/store"
)

// TestNewSourcesLive draws a few times from every source of the print,
// nature, literature and fiction families against the real sites, and opens
// what was drawn the way the reader does (a window onto a scan is built by
// Route). It needs the network, so it runs only with W5F_LIVE=1
// (W5F_SOLVER=<url> names a bot-check helper, which Poetry Foundation needs).
func TestNewSourcesLive(t *testing.T) {
	if os.Getenv("W5F_LIVE") == "" {
		t.Skip("set W5F_LIVE=1 to draw from the real sites")
	}
	f := fetch.New(t.TempDir(), "test")
	f.SolverURL = os.Getenv("W5F_SOLVER")
	db, err := store.Open(filepath.Join(t.TempDir(), "live.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	env := Env{Fetcher: f, DB: db}
	env.Load = func(ctx context.Context, target string) (*doc.Document, error) {
		if strings.HasPrefix(target, "w5f:") {
			return Route(ctx, target, env)
		}
		return nil, nil // the page itself is the reader's business; the draw is what is tested
	}

	type pick struct {
		name string
		do   func(ctx context.Context) (target, title string, err error)
	}
	var picks []pick
	for _, s := range printSources {
		picks = append(picks, pick{"print/" + s.name, func(ctx context.Context) (string, string, error) {
			d, err := printDraw(ctx, env, s)
			return d.Target, d.Why, err
		}})
	}
	for _, s := range natureSources {
		picks = append(picks, pick{"nature/" + s.name, func(ctx context.Context) (string, string, error) { return s.pick(ctx, env) }})
	}
	for _, s := range litSources {
		picks = append(picks, pick{"literature/" + s.name, func(ctx context.Context) (string, string, error) { return s.pick(ctx, env) }})
	}
	for _, s := range fictionWeb {
		picks = append(picks, pick{"fiction/" + s.name, func(ctx context.Context) (string, string, error) { return s.pick(ctx, env) }})
	}
	for _, p := range picks {
		for range 3 {
			ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
			target, title, err := p.do(ctx)
			if err == nil && strings.HasPrefix(target, "w5f:") {
				var page *doc.Document
				if page, err = env.Load(ctx, target); err == nil {
					title += " → " + page.Byline + ", " + strings.TrimSpace(firstText(page))
				}
			}
			cancel()
			if err != nil {
				t.Errorf("%-34s %v", p.name, err)
				continue
			}
			t.Logf("%-34s %s  [%s]", p.name, target, title)
		}
	}
}

func firstText(d *doc.Document) string {
	for _, b := range d.Blocks {
		if p, ok := b.(doc.Paragraph); ok && len(p.Text) > 0 {
			s := p.Text[0].Text
			if len([]rune(s)) > 70 {
				s = string([]rune(s)[:70]) + "…"
			}
			return s
		}
	}
	return ""
}
