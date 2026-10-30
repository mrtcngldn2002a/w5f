package source

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"w5f/internal/discover"
	"w5f/internal/fetch"
	"w5f/internal/store"
)

// TestAheadLive measures Deep random with pages drawn ahead against the real
// sites, as a reader who reads each page to its end: how long x takes, how
// much is downloaded for each page shown (cache folder, the pages turned
// away included) and how much memory the pages waiting take. It needs the
// network, so it runs only with W5F_AHEAD=1 (W5F_AHEAD_N=<pages shown>).
func TestAheadLive(t *testing.T) {
	if os.Getenv("W5F_AHEAD") == "" {
		t.Skip("set W5F_AHEAD=1 to measure drawing ahead on the real sources")
	}
	n := 10
	if v, err := strconv.Atoi(os.Getenv("W5F_AHEAD_N")); err == nil && v > 0 {
		n = v
	}
	cache := t.TempDir()
	old := Fetcher
	Fetcher = fetch.New(cache, "test")
	Fetcher.SolverURL = os.Getenv("W5F_SOLVER")
	t.Cleanup(func() { Fetcher = old })
	db, err := store.Open(filepath.Join(t.TempDir(), "ahead.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store.SetDefault(db)
	env, err := DiscoverEnv()
	if err != nil {
		t.Fatal(err)
	}
	env.SitesPath = ""
	env.Ahead = discover.NewAhead(2)

	heap := func() uint64 {
		runtime.GC()
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		return m.HeapAlloc
	}
	base := heap()
	var waits []time.Duration
	var lines []string
	for i := range n {
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		d, page, err := discover.Next(ctx, env)
		cancel()
		wait := time.Since(start)
		waits = append(waits, wait)
		if err != nil {
			lines = append(lines, fmt.Sprintf("%2d ✗ %5.1fs %v", i, wait.Seconds(), err))
		} else {
			lines = append(lines, fmt.Sprintf("%2d ✓ %5.1fs %-12s %s · %s", i, wait.Seconds(), d.Family, d.Why, page.Title))
		}
		t.Log(lines[len(lines)-1])
		fillStart := time.Now()
		env.Ahead.Wait() // the reader reads on until the next pages are ready
		t.Logf("   ahead: %d ready in %.1fs, heap %.1f MB over the start", env.Ahead.Ready(), time.Since(fillStart).Seconds(), float64(heap()-min(base, heap()))/1e6)
	}
	sorted := append([]time.Duration(nil), waits...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	var bytes int64
	_ = filepath.WalkDir(cache, func(_ string, e fs.DirEntry, err error) error {
		if err == nil && !e.IsDir() {
			if fi, err := e.Info(); err == nil {
				bytes += fi.Size()
			}
		}
		return nil
	})
	var b strings.Builder
	fmt.Fprintf(&b, "%d pages shown, first x %.1fs, then median %.2fs, slowest %.1fs\n", n, waits[0].Seconds(), sorted[len(sorted)/2].Seconds(), sorted[len(sorted)-1].Seconds())
	fmt.Fprintf(&b, "downloaded %.1f MB in all (%.2f MB for each page shown, the %d waiting and those turned away included)\n", float64(bytes)/1e6, float64(bytes)/1e6/float64(n), env.Ahead.Ready())
	fmt.Fprintf(&b, "heap over the start with %d pages waiting: %.1f MB\n", env.Ahead.Ready(), float64(heap()-min(base, heap()))/1e6)
	t.Log("\n" + strings.Join(lines, "\n") + "\n" + b.String())
}
