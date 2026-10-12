package discover

import (
	"context"
	"path/filepath"
	"sort"
	"testing"

	"w5f/internal/store"
)

type heavyFamily struct {
	fakeFamily
	w int
}

func (h heavyFamily) weight() int { return h.w }

func TestWeightedFamilyTakesSeveralPlacesInTheBag(t *testing.T) {
	env := testEnv(t, heavyFamily{fakeFamily{name: "big"}, 3}, fakeFamily{name: "a"}, fakeFamily{name: "b"}, fakeFamily{name: "c"})
	ctx := context.Background()
	for round := 0; round < 20; round++ {
		counts := map[string]int{}
		last := ""
		for i := 0; i < 6; i++ { // one bag: big three times, the others once
			d, _, err := Next(ctx, env)
			if err != nil {
				t.Fatal(err)
			}
			if d.Family == last {
				t.Fatalf("round %d: %s twice in a row", round, d.Family)
			}
			last = d.Family
			counts[d.Family]++
		}
		// Bags are cut by the draws, not by the rounds: only check the totals add up.
		if counts["big"] < 2 || counts["big"] > 4 {
			t.Fatalf("round %d: big came %d times in six draws: %v", round, counts["big"], counts)
		}
	}
}

func TestSpreadBagSeparatesRepeats(t *testing.T) {
	for range 200 {
		bag := []string{"a", "a", "a", "b", "b", "c", "d", "e"}
		spreadBag(bag)
		for i := 1; i < len(bag); i++ {
			if bag[i] == bag[i-1] {
				t.Fatalf("repeat at %d: %v", i, bag)
			}
		}
	}
	// Nothing to separate it with: left as it is.
	bag := []string{"a", "a"}
	spreadBag(bag)
}

func TestWeightedOrderIsAPermutation(t *testing.T) {
	first := map[int]int{}
	for range 3000 {
		o := weightedOrder([]int{1, 1, 6})
		cp := append([]int(nil), o...)
		sort.Ints(cp)
		if len(cp) != 3 || cp[0] != 0 || cp[1] != 1 || cp[2] != 2 {
			t.Fatalf("not a permutation: %v", o)
		}
		first[o[0]]++
	}
	if first[2] < 2000 { // 6 of 8
		t.Errorf("the heavy one should come first about three times in four: %v", first)
	}
}

func TestWeightOfSeesThroughJoined(t *testing.T) {
	if weightOf(textfiles{}) != 2 || weightOf(esoteric{}) != 1 {
		t.Error("textfiles weigh 2, esoteric 1")
	}
	if weightOf(joined{Family: textfiles{}}) != 2 {
		t.Error("owner's sites joined to a family do not change its weight")
	}
}

// With the real weights, ten draws still reach most of the shelves.
func TestRealWeightsStillSpanTheShelves(t *testing.T) {
	var fams []Family
	for _, f := range families {
		fams = append(fams, heavyFamily{fakeFamily{name: f.Name()}, weightOf(f)})
	}
	if len(fams) < 12 {
		t.Fatalf("only %d families registered", len(fams))
	}
	env := testEnv(t, fams...)
	var names []string
	for range 1500 {
		d, _, err := Next(context.Background(), env)
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, d.Family)
	}
	total, windows := 0, 0
	for i := 0; i+10 <= len(names); i++ {
		seen := map[string]bool{}
		for _, n := range names[i : i+10] {
			seen[n] = true
		}
		if len(seen) < 4 {
			t.Fatalf("draws %d-%d span only %d families: %v", i, i+10, len(seen), names[i:i+10])
		}
		total += len(seen)
		windows++
	}
	if mean := float64(total) / float64(windows); mean < 7.5 {
		t.Errorf("ten draws span %.1f families on average", mean)
	}
}

func TestSeenMemory(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if wasSeen(db, "x") {
		t.Fatal("nothing seen yet")
	}
	markSeen(db, "x")
	if !wasSeen(db, "x") {
		t.Fatal("x was marked")
	}
	for i := 0; i < shownMax+10; i++ {
		markSeen(db, "p"+string(rune('A'+i%26))+string(rune('a'+i/26)))
	}
	if wasSeen(db, "x") {
		t.Error("the oldest are forgotten after shownMax")
	}
	if wasSeen(nil, "x") {
		t.Error("no database, nothing seen")
	}
}

// A family is drawn again when its page was shown lately, but a shelf that
// only has one page still gives it.
func TestDrawUnseenPrefersNewPages(t *testing.T) {
	env := testEnv(t)
	markSeen(env.DB, "https://example.org/old")
	calls := 0
	f := funcFamily{"f", func() Draw {
		calls++
		if calls == 1 {
			return Draw{Target: "https://example.org/old"}
		}
		return Draw{Target: "https://example.org/new"}
	}}
	d, err := drawUnseen(context.Background(), env, f)
	if err != nil || d.Target != "https://example.org/new" || calls != 2 {
		t.Errorf("got %+v after %d draws: %v", d, calls, err)
	}
	only := funcFamily{"g", func() Draw { return Draw{Target: "https://example.org/old"} }}
	if d, err := drawUnseen(context.Background(), env, only); err != nil || d.Target != "https://example.org/old" {
		t.Errorf("a shelf with one page repeats it: %+v %v", d, err)
	}
}

type funcFamily struct {
	name string
	draw func() Draw
}

func (f funcFamily) Name() string { return f.name }
func (f funcFamily) Draw(context.Context, Env) (Draw, error) {
	return f.draw(), nil
}
