package discover

import (
	"context"
	"sync"
	"time"

	"w5f/internal/doc"
	"w5f/internal/fetch"
	"w5f/internal/store"
)

// Pages drawn ahead (2026-10-30): the reading-matter judge turns about one
// page in five away and a draw takes seconds (a bot check up to a minute),
// so while a page is read the next ones are drawn, opened and judged behind
// it, and x shows one at once. They wait in memory only: a page left when
// W5F closes is not shown, and the next start draws anew.

const (
	aheadTries   = 8                // draws one fill makes at most (failing shelves, pages turned away)
	aheadTimeout = 90 * time.Second // one draw behind the page, a bot check included
	aheadStale   = 2 * time.Hour    // a page kept longer is let go
)

// Ahead keeps pages drawn ahead of the reader. The zero value keeps none;
// use NewAhead.
type Ahead struct {
	size    int
	mu      sync.Mutex
	ready   []aheadPage
	filling bool
	wg      sync.WaitGroup
	now     func() time.Time
}

type aheadPage struct {
	d    Draw
	page *doc.Document
	at   time.Time
}

// NewAhead keeps n pages ready.
func NewAhead(n int) *Ahead { return &Ahead{size: n, now: time.Now} }

// holds tells a page that waits to be shown.
func (a *Ahead) holds(target string) bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, r := range a.ready {
		if r.d.Target == target {
			return true
		}
	}
	return false
}

// take hands out the oldest page ready that is not of the family last (the
// page shown before) and was not shown meanwhile; pages kept too long go.
func (a *Ahead) take(db *store.DB, last string) (Draw, *doc.Document, bool) {
	if a == nil {
		return Draw{}, nil, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	var (
		got  aheadPage
		ok   bool
		keep []aheadPage
	)
	for _, r := range a.ready {
		switch {
		case a.now().Sub(r.at) > aheadStale || wasSeen(db, r.d.Target):
		case !ok && r.d.Family != last:
			got, ok = r, true
		default:
			keep = append(keep, r)
		}
	}
	a.ready = keep
	return got.d, got.page, ok
}

// fill draws pages behind the one shown until enough are ready; one fill
// runs at a time, and none offline.
func (a *Ahead) fill(env Env) {
	if a == nil || env.Fetcher != nil && env.Fetcher.Offline {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.filling || len(a.ready) >= a.size {
		return
	}
	a.filling = true
	a.wg.Add(1)
	go a.run(env)
}

func (a *Ahead) run(env Env) {
	defer a.wg.Done()
	defer func() {
		a.mu.Lock()
		a.filling = false
		a.mu.Unlock()
	}()
	for range aheadTries {
		a.mu.Lock()
		n, last := len(a.ready), ""
		if n > 0 {
			last = a.ready[n-1].d.Family
		}
		a.mu.Unlock()
		if n >= a.size {
			return
		}
		if last == "" {
			last = lastShown(env.DB)
		}
		ctx, cancel := context.WithTimeout(context.Background(), aheadTimeout)
		d, page, err := pickReading(fetch.SolverOnce(ctx), env, last, 1)
		cancel()
		if err != nil {
			continue
		}
		a.mu.Lock()
		a.ready = append(a.ready, aheadPage{d, page, a.now()})
		a.mu.Unlock()
	}
}

// Wait waits for the fill running (tests and the live measure).
func (a *Ahead) Wait() { a.wg.Wait() }

// Ready is the number of pages waiting to be shown.
func (a *Ahead) Ready() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.ready)
}
