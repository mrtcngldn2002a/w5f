package discover

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"w5f/internal/doc"
	"w5f/internal/fetch"
)

// countLoads records the pages an environment opens.
func countLoads(env *Env) func() []string {
	var mu sync.Mutex
	var got []string
	load := env.Load
	env.Load = func(ctx context.Context, target string) (*doc.Document, error) {
		mu.Lock()
		got = append(got, target)
		mu.Unlock()
		return load(ctx, target)
	}
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), got...)
	}
}

// uniqFamily draws a new page each time.
func uniqFamily(name string) Family {
	var n atomic.Int64
	return funcFamily{name, func() Draw {
		return Draw{Target: fmt.Sprintf("https://example.org/%s/%d", name, n.Add(1)), Why: name + "/page"}
	}}
}

func TestAheadShowsAPageDrawnBehindTheOneRead(t *testing.T) {
	env := testEnv(t, uniqFamily("a"), uniqFamily("b"), uniqFamily("c"), uniqFamily("d"))
	env.Ahead = NewAhead(2)
	loads := countLoads(&env)
	ctx := context.Background()
	d, _, err := Next(ctx, env)
	if err != nil {
		t.Fatal(err)
	}
	env.Ahead.Wait()
	if got := loads(); len(got) != 3 {
		t.Fatalf("one page drawn now and two behind it: %v", got)
	}
	last := d.Family
	for i := range 12 {
		before := len(loads())
		d, page, err := Next(ctx, env)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(loads()[:before], d.Target) {
			t.Errorf("draw %d was opened when asked for, not ahead", i)
		}
		if d.Family == last {
			t.Errorf("the same family twice in a row: %s", d.Family)
		}
		last = d.Family
		if !strings.HasPrefix(page.Blocks[0].(doc.Notice).Text, "Deep random · "+d.Why) {
			t.Errorf("why line: %+v", page.Blocks[0])
		}
		env.Ahead.Wait()
		if n := len(env.Ahead.ready); n != 2 {
			t.Errorf("draw %d: %d pages ready", i, n)
		}
	}
}

// A page the judge turns away is not kept ahead, and a shelf that fails
// does not stop the others.
func TestAheadKeepsOnlyReadingMatter(t *testing.T) {
	env := testEnv(t, fakeFamily{name: "thin"}, fakeFamily{name: "down", fail: true}, fakeFamily{name: "a"}, fakeFamily{name: "b"})
	env.Ahead = NewAhead(2)
	load := env.Load
	env.Load = func(ctx context.Context, target string) (*doc.Document, error) {
		if strings.HasSuffix(target, "/thin") {
			return &doc.Document{Title: "7 Novels About Grief", Blocks: []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: fakeBody}}}}}, nil
		}
		return load(ctx, target)
	}
	for range 10 {
		d, _, err := Next(context.Background(), env)
		if err != nil {
			continue // three bad shelves in a row may come
		}
		if d.Family == "thin" || d.Family == "down" {
			t.Fatalf("shown: %+v", d)
		}
		env.Ahead.Wait()
		for _, r := range env.Ahead.ready {
			if r.d.Family == "thin" || r.d.Family == "down" {
				t.Fatalf("kept ahead: %+v", r.d)
			}
		}
	}
}

func TestAheadTakesNotTheFamilyShownLastNorAStalePage(t *testing.T) {
	env := testEnv(t)
	now := time.Now()
	a := NewAhead(2)
	a.now = func() time.Time { return now }
	page := func() *doc.Document { return &doc.Document{} }
	a.ready = []aheadPage{
		{Draw{Target: "u:a", Family: "a"}, page(), now},
		{Draw{Target: "u:b", Family: "b"}, page(), now},
	}
	if d, _, ok := a.take(env.DB, "a"); !ok || d.Family != "b" {
		t.Errorf("after a page of a, took %+v", d)
	}
	if len(a.ready) != 1 || !a.holds("u:a") {
		t.Errorf("the page of a waits for its turn: %+v", a.ready)
	}
	if _, _, ok := a.take(env.DB, "a"); ok {
		t.Error("only a page of a is ready right after one: nothing to take")
	}
	markSeen(env.DB, "u:a")
	if _, _, ok := a.take(env.DB, "b"); ok || len(a.ready) != 0 {
		t.Error("a page shown meanwhile goes")
	}
	a.ready = []aheadPage{{Draw{Target: "u:c", Family: "c"}, page(), now}}
	now = now.Add(aheadStale + time.Minute)
	if _, _, ok := a.take(env.DB, ""); ok || len(a.ready) != 0 {
		t.Error("a page kept too long goes")
	}
}

func TestAheadDrawsNothingOffline(t *testing.T) {
	env := testEnv(t, fakeFamily{name: "a"}, fakeFamily{name: "b"})
	env.Fetcher = fetch.New(t.TempDir(), "test")
	env.Fetcher.Offline = true
	env.Ahead = NewAhead(2)
	loads := countLoads(&env)
	if _, _, err := Next(context.Background(), env); err != nil {
		t.Fatal(err)
	}
	env.Ahead.Wait()
	if got := loads(); len(got) != 1 {
		t.Errorf("offline, only the page asked for opens: %v", got)
	}
}
