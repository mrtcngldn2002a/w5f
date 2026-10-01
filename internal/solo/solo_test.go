package solo

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"w5f/internal/dict"
	"w5f/internal/doc"
	"w5f/internal/store"
)

func TestDiceNotation(t *testing.T) {
	for _, bad := range []string{"", "2d", "d1", "2d6+", "abc", "0d6", "101d6", "3d6kh4", "5"} {
		if _, err := ParseDice(bad); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
	for i := 0; i < 300; i++ {
		r, err := Throw("2d6+1", nil)
		if err != nil || r.Total < 3 || r.Total > 13 || len(r.Terms[0].Rolls) != 2 {
			t.Fatalf("2d6+1: %+v %v", r, err)
		}
		r, _ = Throw("d%", nil)
		if r.Total < 1 || r.Total > 100 {
			t.Fatalf("d%%: %d", r.Total)
		}
		r, _ = Throw("- 1 + 1d4", nil)
		if r.Total < 0 || r.Total > 3 {
			t.Fatalf("-1+1d4: %d", r.Total)
		}
	}
}

func TestOwnDice(t *testing.T) {
	r, err := Throw("2d20kh1", []int{4, 17})
	if err != nil || r.Total != 17 || !r.Manual || r.Detail() != "2d20kh1 [(4) 17]" {
		t.Errorf("advantage: %+v %q %v", r, r.Detail(), err)
	}
	r, _ = Throw("4d6kl2", []int{6, 2, 5, 2})
	if r.Total != 4 || r.Detail() != "4d6kl2 [(6) 2 (5) 2]" {
		t.Errorf("disadvantage: %d %q", r.Total, r.Detail())
	}
	r, err = Throw("2d6+1", []int{9})
	if err != nil || r.Total != 9 {
		t.Errorf("a total: %+v %v", r, err)
	}
	for own, want := range map[string]string{"14": "gives 3 to 13", "1": "gives 3 to 13", "3 7": "does not show 7", "1 2 3": "2 dice"} {
		v, _ := ParseOwn(own)
		if _, err := Throw("2d6+1", v); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("own %q: %v", own, err)
		}
	}
	if _, err := Throw("2d20kh1", []int{17}); err == nil {
		t.Error("a total cannot say which die was kept")
	}
}

func TestOracle(t *testing.T) {
	for _, c := range []struct {
		odds  string
		roll  int
		yes   bool
		twist bool
	}{
		{"likely", 75, true, false}, {"likely", 76, false, false}, {"50/50", 44, true, true}, {"small chance", 10, true, false},
		{"almost certain", 100, false, true}, {"unlikely", 26, false, false}, {"unlikely", 25, true, false}, {"certain", 11, true, true}, {"even", 1, true, false},
	} {
		a, err := Ask("Is it so?", c.odds, c.roll)
		if err != nil || a.Yes != c.yes || a.Twist != c.twist || !a.Manual {
			t.Errorf("%s %d: %+v %v", c.odds, c.roll, a, err)
		}
	}
	if _, err := Ask("?", "maybe", 0); err == nil {
		t.Error("unknown odds")
	}
	if _, err := Ask("?", "likely", 101); err == nil {
		t.Error("a d100 does not show 101")
	}
	yes := 0
	for i := 0; i < 2000; i++ {
		a, _ := Ask("", "likely", 0)
		if a.Yes {
			yes++
		}
	}
	if yes < 1400 || yes > 1600 {
		t.Errorf("likely said yes %d times in 2000", yes)
	}
}

func TestPlainWords(t *testing.T) {
	for w, ok := range map[string]bool{"lantern": true, "ox": false, "Ankara": false, "x-ray": false, "a lot": false, "cartography": true, "abc's": false} {
		if plainWord(w) != ok {
			t.Errorf("%s: %v", w, !ok)
		}
	}
}

func testEnv(t *testing.T) (Env, *store.DB) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	notes := t.TempDir()
	dir := filepath.Join(notes, "Clippings", "2026", "09")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "2026-09-30.md"), []byte("# Clippings\n\n> The lamps of the marsh move against the wind, and nobody follows them home.\n\n> short\n"), 0o644)
	return Env{DB: db, Notes: notes, LogDir: filepath.Join(t.TempDir(), "log"),
		Dict: func() (*dict.Dict, error) { return nil, errors.New("not installed") }}, db
}

func flat(d *doc.Document) string {
	var b strings.Builder
	doc.ReplaceBlocks(d.Blocks, func(x doc.Block) ([]doc.Block, bool) {
		switch p := x.(type) {
		case doc.Paragraph:
			b.WriteString(p.Text.PlainText() + "\n")
		case doc.Heading:
			b.WriteString("# " + p.Text.PlainText() + "\n")
		case doc.Notice:
			b.WriteString("! " + p.Text + "\n")
		case doc.Pre:
			b.WriteString(p.Text + "\n")
		}
		return nil, false
	})
	return b.String()
}

func TestTablePagesAndLog(t *testing.T) {
	env, _ := testEnv(t)
	ctx := context.Background()

	d, err := Route(ctx, "w5f:solo/roll?d=2d6%2B1+%3D+3+5", env) // "2d6+1 = 3 5"
	if err != nil || !strings.Contains(flat(d), "! 2d6+1 = 9") || !strings.Contains(flat(d), "2d6 [3 5] + 1  (your dice)") {
		t.Fatalf("roll: %v\n%s", err, flat(d))
	}
	// A twist with no dictionary installed: the spark comes from the reading.
	d, err = Route(ctx, "w5f:solo/ask/even?q=Is+the+bridge+guarded%3F+%3D+33", env)
	if err != nil {
		t.Fatal(err)
	}
	tx := flat(d)
	for _, want := range []string{"! YES — and it is extreme, or a twist", "Is the bridge guarded?", "50/50 (50) · d100 = 33 (your d100)",
		"The twist: The lamps of the marsh move against the wind", "from a clipping of yours, 2026-09-30"} {
		if !strings.Contains(tx, want) {
			t.Errorf("ask lacks %q:\n%s", want, tx)
		}
	}
	if _, err := Route(ctx, "w5f:solo/spark/word", env); err == nil || !strings.Contains(err.Error(), "dict-install") {
		t.Errorf("no dictionary: %v", err)
	}
	if _, err := Route(ctx, "w5f:solo/roll?d=roll+call", env); err == nil {
		t.Error("not dice")
	}

	log := env.Recent(10)
	if len(log) != 2 || log[0].Kind != "ask" || log[1].Kind != "roll" || !log[1].Manual || log[1].Result != "9" {
		t.Fatalf("log: %+v", log)
	}
	if !strings.Contains(log[0].Detail, "twist: The lamps of the marsh") {
		t.Errorf("the twist is logged: %q", log[0].Detail)
	}
	home, _ := Route(ctx, "w5f:solo", env)
	if !strings.Contains(flat(home), "# Log") || !strings.Contains(flat(home), "ask · 50/50: Is the bridge guarded? → YES") {
		t.Errorf("home:\n%s", flat(home))
	}
	full, _ := Route(ctx, "w5f:solo/log", env)
	if !strings.Contains(flat(full), "roll · 2d6+1 → 9 (your dice)") {
		t.Errorf("log page:\n%s", flat(full))
	}
}

func TestReadingSparkWithNothingRead(t *testing.T) {
	env, _ := testEnv(t)
	env.Notes = t.TempDir()
	if _, err := env.Draw(context.Background(), "reading"); err == nil || !strings.Contains(err.Error(), "nothing read yet") {
		t.Errorf("empty: %v", err)
	}
}

func TestTableLists(t *testing.T) {
	for in, want := range map[string]Character{
		"Severian — a torturer, exiled": {"Severian", "a torturer, exiled"},
		"Dorcas - drowned once":         {"Dorcas", "drowned once"},
		"Agia: a liar":                  {"Agia", "a liar"},
		"Jonas":                         {"Jonas", ""},
	} {
		if c, err := ParseCharacter(in); err != nil || c != want {
			t.Errorf("%q → %+v %v", in, c, err)
		}
	}
	for in, want := range map[string]Counter{
		"Health 5/5": {"Health", 5, 5}, "Doom clock 0 / 6": {"Doom clock", 0, 6}, "Supply 3": {"Supply", 3, 0},
		"Momentum -2": {"Momentum", -2, 0}, "Wounds": {"Wounds", 0, 0}, "Hope 9/4": {"Hope", 4, 4},
	} {
		if c, err := ParseCounter(in); err != nil || c != want {
			t.Errorf("%q → %+v %v", in, c, err)
		}
	}
	if _, err := ParseCounter("Doom 0/500"); err == nil {
		t.Error("a maximum of 500")
	}
	if IsCounter("strike") || !IsCounter("Health 5/5") || !IsCounter("Supply 3") {
		t.Error("IsCounter")
	}
	if b := (Counter{"Clock", 2, 6}).Bar(); b != "[##....] 2/6" {
		t.Errorf("bar: %q", b)
	}

	env, _ := testEnv(t)
	ctx := context.Background()
	for _, u := range []string{
		"w5f:solo/add/character?q=Severian+%E2%80%94+a+torturer",
		"w5f:solo/add/thread?q=Find+who+burned+the+archive",
		"w5f:solo/add/counter?q=Doom+5%2F6",
	} {
		if _, err := Route(ctx, u, env); err != nil {
			t.Fatalf("%s: %v", u, err)
		}
	}
	d, _ := Route(ctx, "w5f:solo", env)
	tx := flat(d)
	for _, want := range []string{"Severian — a torturer  x", "Find who burned the archive  close x", "Doom  [#####.] 5/6  - +  x", "add…   pick one"} {
		if !strings.Contains(tx, want) {
			t.Errorf("table lacks %q:\n%s", want, tx)
		}
	}
	// A clock stops at its maximum, and at 0.
	for range 3 {
		d, _ = Route(ctx, "w5f:solo/counter/up?i=0&n=Doom", env)
	}
	if !strings.Contains(flat(d), "! Doom [######] 6/6") {
		t.Errorf("up past the maximum:\n%s", flat(d))
	}
	// A stale page (the entry at 0 is no longer Doom) changes nothing.
	if _, err := Route(ctx, "w5f:solo/counter/remove?i=0&n=Health", env); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Errorf("stale: %v", err)
	}
	d, _ = Route(ctx, "w5f:solo/pick/thread", env)
	if !strings.Contains(flat(d), "! Picked (thread): Find who burned the archive") {
		t.Errorf("pick:\n%s", flat(d))
	}
	d, _ = Route(ctx, "w5f:solo/thread/close?i=0&n=Find+who+burned+the+archive", env)
	if !strings.Contains(flat(d), "No thread left open.") {
		t.Errorf("closed:\n%s", flat(d))
	}
	if _, err := Route(ctx, "w5f:solo/pick/thread", env); err == nil {
		t.Error("a closed thread was picked")
	}
	log := env.Recent(10)
	if len(log) != 3 || log[0].Kind != "thread" || log[0].Input != "closed" || log[1].Kind != "pick" || log[2].Input != "opened" {
		t.Errorf("log: %+v", log)
	}
	// Kept for next time.
	tb, _ := env.LoadTable()
	if len(tb.Characters) != 1 || len(tb.Threads) != 1 || !tb.Threads[0].Closed || tb.Counters[0].Value != 6 {
		t.Errorf("kept: %+v", tb)
	}
}
