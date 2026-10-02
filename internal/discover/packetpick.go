package discover

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/url"
	"path"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"w5f/internal/store"
)

// The Daily Packet's memory and its rotating sections (2026-10-02, chosen
// with the owner, who found the issues repetitive): nothing an issue showed
// in the last 30 days comes back; the periodicals come from three different
// shelves, those not seen lately first; and the Deep Random shelves take
// turns, four to an issue, through a shuffle bag.

const (
	seenKey      = "packet:seen"
	packetBagKey = "packet:bag"
	memoryDays   = 30
	issueDraws   = 4 // rotating sections in an issue
)

// packetPool is the order the rotating sections appear in on the cover.
var packetPool = []string{"weird", "fiction", "esoteric", "folklore", "knowledge", "encyclopedic", "public", "archive", "smallweb"}

// seenEntry is one thing an issue showed.
type seenEntry struct {
	Date   string `json:"d"`
	Kind   string `json:"k"`
	Target string `json:"t"`
	Feed   string `json:"f,omitempty"`
	Shelf  string `json:"s,omitempty"`
}

// memory is what the issues of the last 30 days showed: its addresses, and
// how many days ago each feed and shelf was last in an issue.
type memory struct {
	entries  []seenEntry
	targets  map[string]bool
	feedAge  map[string]int
	shelfAge map[string]int
}

func loadMemory(db *store.DB, date string) *memory {
	m := &memory{targets: map[string]bool{}, feedAge: map[string]int{}, shelfAge: map[string]int{}}
	now, err := time.Parse("2006-01-02", date)
	if err != nil {
		return m
	}
	var es []seenEntry
	_ = json.Unmarshal([]byte(db.Get(seenKey)), &es)
	for _, e := range es {
		t, err := time.Parse("2006-01-02", e.Date)
		if err != nil {
			continue
		}
		age := int(now.Sub(t).Hours() / 24)
		if age < 0 || age >= memoryDays {
			continue
		}
		m.entries = append(m.entries, e)
		m.targets[e.Target] = true
		seen(m.feedAge, e.Feed, age)
		seen(m.shelfAge, e.Shelf, age)
	}
	return m
}

// seen keeps the most recent day key was in an issue.
func seen(ages map[string]int, key string, age int) {
	if a, ok := ages[key]; key != "" && (!ok || age < a) {
		ages[key] = age
	}
}

// age is how many days ago key was last in an issue (memoryDays: not lately).
func age(ages map[string]int, key string) int {
	if a, ok := ages[key]; ok {
		return a
	}
	return memoryDays
}

// remember adds an issue's entries to the memory (a reshuffled issue's
// too, so a reshuffle brings new things).
func (m *memory) remember(db *store.DB, p Packet, shelfOf map[string]string) {
	for _, e := range p.Entries {
		s := seenEntry{Date: p.Date, Kind: e.Kind, Target: e.Target}
		if e.Kind == "periodical" {
			s.Feed, s.Shelf = e.Source, shelfFor(shelfOf, e.Source)
		}
		m.entries = append(m.entries, s)
	}
	if b, err := json.Marshal(m.entries); err == nil {
		_ = db.Set(seenKey, string(b))
	}
}

// shelfFor is a feed's shelf; without a catalog every feed is its own.
func shelfFor(shelfOf map[string]string, feed string) string {
	if s := shelfOf[feed]; s != "" {
		return s
	}
	return feed
}

func itemTarget(id int64) string { return fmt.Sprintf("w5f:item/%d", id) }

// pickPeriodicals takes n unread items, each from another shelf when it
// can: shelves and feeds not in a recent issue first, never an item shown in
// the last 30 days. shelfOf maps the catalog's feeds to their shelves; with
// it, items of feeds taken off the catalog are left out.
func pickPeriodicals(db *store.DB, shelfOf map[string]string, m *memory, n int) []store.Item {
	q := store.Query{Unread: true, Limit: 2000}
	for id := range shelfOf {
		q.Feeds = append(q.Feeds, id)
	}
	items, err := db.Items(q)
	if err != nil {
		return nil
	}
	byShelf := map[string]map[string][]store.Item{} // shelf → feed → items, newest first
	for _, it := range items {
		if it.Title == "" || m.targets[itemTarget(it.ID)] {
			continue
		}
		s := shelfFor(shelfOf, it.FeedID)
		if byShelf[s] == nil {
			byShelf[s] = map[string][]store.Item{}
		}
		byShelf[s][it.FeedID] = append(byShelf[s][it.FeedID], it)
	}
	shelves := shuffled(keys(byShelf))
	sort.SliceStable(shelves, func(i, j int) bool { return age(m.shelfAge, shelves[i]) > age(m.shelfAge, shelves[j]) })
	var out []store.Item
	usedShelf, usedFeed := map[string]bool{}, map[string]bool{}
	// A second round takes another feed of a shelf already used, when there
	// are fewer shelves than items.
	for round := 0; round < 2 && len(out) < n; round++ {
		for _, s := range shelves {
			if len(out) == n {
				break
			}
			if round == 0 && usedShelf[s] {
				continue
			}
			var feeds []string
			for f := range byShelf[s] {
				if !usedFeed[f] {
					feeds = append(feeds, f)
				}
			}
			if len(feeds) == 0 {
				continue
			}
			feeds = shuffled(feeds)
			sort.SliceStable(feeds, func(i, j int) bool { return age(m.feedAge, feeds[i]) > age(m.feedAge, feeds[j]) })
			list := byShelf[s][feeds[0]]
			out = append(out, list[rand.IntN(min(3, len(list)))]) // one of the feed's three newest
			usedShelf[s], usedFeed[feeds[0]] = true, true
		}
	}
	return out
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out) // the shuffle that follows is then the only randomness
	return out
}

func shuffled(xs []string) []string {
	rand.Shuffle(len(xs), func(i, j int) { xs[i], xs[j] = xs[j], xs[i] })
	return xs
}

// nextKinds takes up to n sections from the shuffle bag, none of them in
// exclude and no two alike: each section once per bag, refilled when it runs
// out.
func nextKinds(db *store.DB, n int, exclude []string) []string {
	var pool []string
	for _, k := range packetPool {
		if packetDraws[k] != nil && !slices.Contains(exclude, k) {
			pool = append(pool, k)
		}
	}
	var bag []string
	_ = json.Unmarshal([]byte(db.Get(packetBagKey)), &bag)
	bag = slices.DeleteFunc(bag, func(k string) bool { return packetDraws[k] == nil || !slices.Contains(packetPool, k) })
	var out []string
	for len(out) < min(n, len(pool)) {
		i := slices.IndexFunc(bag, func(k string) bool { return slices.Contains(pool, k) && !slices.Contains(out, k) })
		if i < 0 {
			var all []string
			for _, k := range packetPool {
				if packetDraws[k] != nil {
					all = append(all, k)
				}
			}
			bag = append(bag, shuffled(all)...)
			continue
		}
		out = append(out, bag[i])
		bag = slices.Delete(bag, i, i+1)
	}
	if b, err := json.Marshal(bag); err == nil {
		_ = db.Set(packetBagKey, string(b))
	}
	return out
}

// drawFresh draws a section up to three times for something not shown in
// the last 30 days.
func drawFresh(ctx context.Context, env Env, kind string, m *memory) (Draw, error) {
	var err error
	for range 3 {
		var d Draw
		if d, err = packetDraws[kind](ctx, env); err != nil {
			if ctx.Err() != nil {
				return Draw{}, err
			}
			continue
		}
		if !m.targets[d.Target] {
			return d, nil
		}
		err = errors.New("only things shown lately")
	}
	return Draw{}, err
}

// drawSections fills the issue's rotating sections: four from the bag,
// drawn side by side; a section that finds nothing new gives way to the
// next in the bag (two at most). They come in packetPool's order.
func drawSections(ctx context.Context, env Env, m *memory) []Entry {
	kinds := nextKinds(env.DB, issueDraws, nil)
	got := map[string]Entry{}
	var mu sync.Mutex
	run := func(kinds []string) {
		var wg sync.WaitGroup
		for _, k := range kinds {
			wg.Add(1)
			go func() {
				defer wg.Done()
				d, err := drawFresh(ctx, env, k, m)
				if err != nil {
					return
				}
				mu.Lock()
				defer mu.Unlock()
				for _, e := range got {
					if e.Target == d.Target {
						return // two sections found the same page
					}
				}
				got[k] = entryOf(k, d)
			}()
		}
		wg.Wait()
	}
	run(kinds)
	for extra := 0; extra < 2 && len(got) < len(kinds) && ctx.Err() == nil; extra++ {
		more := nextKinds(env.DB, 1, kinds)
		if len(more) == 0 {
			break
		}
		kinds = append(kinds, more...)
		run(more)
	}
	var out []Entry
	for _, k := range packetPool {
		if e, ok := got[k]; ok {
			out = append(out, e)
		}
	}
	return out
}

// entryOf turns a draw into an entry. A draw that names no page ("weird/SCP
// Foundation") is titled by its address.
func entryOf(kind string, d Draw) Entry {
	source, title, ok := strings.Cut(d.Why, " · ")
	if !ok {
		title = source
		if _, shelf, ok := strings.Cut(source, "/"); ok {
			title = shelf
		}
		if u, err := url.Parse(d.Target); err == nil {
			if name, err := url.PathUnescape(path.Base(u.Path)); err == nil && name != "/" && name != "." && name != "" {
				title += " · " + strings.ReplaceAll(name, "_", " ")
			}
		}
	}
	return Entry{Kind: kind, Title: title, Target: d.Target, Source: source}
}
