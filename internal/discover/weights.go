package discover

import (
	"encoding/json"
	"math/rand/v2"

	"w5f/internal/store"
)

// weighted is implemented by families that are worth more than one place in
// the shuffle bag: a shelf with many sources comes up that many times more.
type weighted interface{ weight() int }

func (textfiles) weight() int      { return 2 }
func (knowledge) weight() int      { return 2 }
func (smallwebFamily) weight() int { return 2 }
func (fiction) weight() int        { return 3 } // three magazines more (2026-10-30)
func (printFamily) weight() int    { return 2 }
func (literature) weight() int     { return 1 }

// weightOf is how many places a family takes in the bag (at least one).
func weightOf(f Family) int {
	if j, ok := f.(joined); ok {
		f = j.Family
	}
	if w, ok := f.(weighted); ok {
		return max(w.weight(), 1)
	}
	return 1
}

// apart reports whether bag[i] differs from its neighbours.
func apart(bag []string, i int) bool {
	return (i == 0 || bag[i] != bag[i-1]) && (i == len(bag)-1 || bag[i] != bag[i+1])
}

// spreadBag moves repeated names apart, so a family with several places in
// the bag does not come up twice in a row (a few passes of swaps; a bag too
// full of one name to be separated is left as it is).
func spreadBag(bag []string) {
	for range 50 {
		clean := true
		for i := 1; i < len(bag); i++ {
			if bag[i] != bag[i-1] {
				continue
			}
			clean = false
			for _, j := range rand.Perm(len(bag)) {
				if j == i || j == i-1 {
					continue
				}
				bag[i], bag[j] = bag[j], bag[i]
				if apart(bag, i) && apart(bag, j) {
					break
				}
				bag[i], bag[j] = bag[j], bag[i]
			}
		}
		if clean {
			return
		}
	}
}

// notFirst puts something else than name at the front of the bag, if there is
// anything else, without bringing two equal names together.
func notFirst(bag []string, name string) {
	if len(bag) < 2 || bag[0] != name {
		return
	}
	first := -1
	for i := 1; i < len(bag); i++ {
		if bag[i] == name {
			continue
		}
		if first < 0 {
			first = i
		}
		bag[0], bag[i] = bag[i], bag[0]
		if apart(bag, 0) && apart(bag, i) {
			return
		}
		bag[0], bag[i] = bag[i], bag[0]
	}
	if first > 0 {
		bag[0], bag[first] = bag[first], bag[0]
	}
}

// weightedOrder lists 0..len(w)-1 in a random order in which the heavier
// ones tend to come first (each step draws among the rest by weight).
func weightedOrder(w []int) []int {
	rest := make([]int, len(w))
	for i := range rest {
		rest[i] = i
	}
	out := make([]int, 0, len(w))
	for len(rest) > 0 {
		total := 0
		for _, i := range rest {
			total += max(w[i], 1)
		}
		n := rand.IntN(total)
		for k, i := range rest {
			if n -= max(w[i], 1); n < 0 {
				out = append(out, i)
				rest = append(rest[:k], rest[k+1:]...)
				break
			}
		}
	}
	return out
}

const (
	shownKey = "discover:seen"
	shownMax = 300
)

// wasSeen reports whether Deep random showed this address lately.
func wasSeen(db *store.DB, target string) bool {
	if db == nil {
		return false
	}
	var seen []string
	_ = json.Unmarshal([]byte(db.Get(shownKey)), &seen)
	for _, s := range seen {
		if s == target {
			return true
		}
	}
	return false
}

// markSeen remembers an address shown (the last shownMax of them).
func markSeen(db *store.DB, target string) {
	if db == nil || target == "" {
		return
	}
	var seen []string
	_ = json.Unmarshal([]byte(db.Get(shownKey)), &seen)
	seen = append(seen, target)
	if len(seen) > shownMax {
		seen = seen[len(seen)-shownMax:]
	}
	b, _ := json.Marshal(seen)
	_ = db.Set(shownKey, string(b))
}
