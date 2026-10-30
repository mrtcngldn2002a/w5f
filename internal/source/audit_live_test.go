package source

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"w5f/internal/discover"
	"w5f/internal/doc"
	"w5f/internal/fetch"
	"w5f/internal/store"
)

// auditDraw is one draw of the source audit, as draws.jsonl keeps it.
type auditDraw struct {
	Family  string            `json:"family"`
	Source  string            `json:"source"`
	Why     string            `json:"why"`
	Target  string            `json:"target"`
	Title   string            `json:"title,omitempty"`
	Seconds float64           `json:"seconds"`
	Error   string            `json:"error,omitempty"` // nothing opened
	Verdict *discover.Verdict `json:"verdict,omitempty"`
}

// TestSourceAuditLive draws from every Deep random shelf against the real
// sites, opens each page the way the reader does and hands it to the
// reading-matter judge (discover.Judge): is it a whole piece to read, by its
// shelf's bar? It ends with the source report: for each source, how many
// draws opened, how many the judge took and why it turned the others away.
// It needs the network, so it runs only with W5F_AUDIT=1 (W5F_AUDIT_N=<draws
// per shelf>, W5F_AUDIT_ONLY=<shelf,…>, W5F_AUDIT_OUT=<folder> for
// draws.jsonl and report.md). The shelves are drawn side by side.
func TestSourceAuditLive(t *testing.T) {
	if os.Getenv("W5F_AUDIT") == "" {
		t.Skip("set W5F_AUDIT=1 to audit the real sources")
	}
	n := 6
	if v, err := strconv.Atoi(os.Getenv("W5F_AUDIT_N")); err == nil && v > 0 {
		n = v
	}
	old := Fetcher
	Fetcher = fetch.New(t.TempDir(), "test")
	Fetcher.SolverURL = os.Getenv("W5F_SOLVER")
	t.Cleanup(func() { Fetcher = old })
	db, err := store.Open(filepath.Join(t.TempDir(), "audit.db"))
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

	only := map[string]bool{}
	for _, s := range strings.Split(os.Getenv("W5F_AUDIT_ONLY"), ",") {
		if s = strings.TrimSpace(s); s != "" {
			only[s] = true
		}
	}
	var (
		mu    sync.Mutex
		draws []auditDraw
		wg    sync.WaitGroup
	)
	for _, name := range discover.FamilyNames() {
		if len(only) > 0 && !only[name] {
			continue
		}
		fam := discover.FamilyByName(name)
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range n {
				a := auditOne(env, name, fam)
				mu.Lock()
				draws = append(draws, a)
				mu.Unlock()
				t.Log(a.line())
			}
		}()
	}
	wg.Wait()
	sort.SliceStable(draws, func(i, j int) bool { return draws[i].Family < draws[j].Family })
	report := auditReport(draws, n)
	t.Log("\n" + report)
	if out := os.Getenv("W5F_AUDIT_OUT"); out != "" {
		if err := os.MkdirAll(out, 0o755); err != nil {
			t.Fatal(err)
		}
		var b strings.Builder
		for _, a := range draws {
			j, _ := json.Marshal(a)
			b.Write(j)
			b.WriteByte('\n')
		}
		if err := os.WriteFile(filepath.Join(out, "draws.jsonl"), []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(out, "report.md"), []byte(report), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func auditOne(env discover.Env, name string, fam discover.Family) auditDraw {
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	start := time.Now()
	d, err := fam.Draw(ctx, env)
	a := auditDraw{Family: name, Source: discover.SourceOf(d), Why: d.Why, Target: d.Target}
	if a.Source == "" {
		a.Source = name
	}
	var page *doc.Document
	if err == nil {
		page, err = Load(ctx, d.Target, Options{})
	}
	a.Seconds = time.Since(start).Round(100 * time.Millisecond).Seconds()
	if err != nil {
		a.Error = err.Error()
		return a
	}
	a.Title = page.Title
	v := discover.Judge(page, name)
	a.Verdict = &v
	return a
}

func (a auditDraw) line() string {
	if a.Error != "" {
		return fmt.Sprintf("✗ %-12s %4.0fs %s\n      %s", a.Family, a.Seconds, a.Why, a.Error)
	}
	v := a.Verdict
	mark, why := "✓", ""
	if !v.OK() {
		mark, why = "↷", " — "+strings.Join(v.Reasons, ", ")
	}
	verse := ""
	if v.Verse {
		verse = " verse"
	}
	return fmt.Sprintf("%s %-12s %4.0fs %5dw/%d links %.2f lists %.2f long %.2f clean %.2f%s%s\n      %s\n      %s\n      %q",
		mark, a.Family, a.Seconds, v.Words, v.Bar, v.LinkShare, v.ListShare, v.LongShare, v.Clean, verse, why, a.Why, a.Target, clip(v.First, 90))
}

// auditReport is the source report: for each source its draws, how many
// opened, how many the judge took, its reasons for the rest and the median
// words of what it took.
func auditReport(draws []auditDraw, n int) string {
	type tally struct {
		family            string
		draws, failed, ok int
		reasons           map[string]int
		words, secs       []float64
	}
	bySource := map[string]*tally{}
	var order []string
	for _, a := range draws {
		s := bySource[a.Source]
		if s == nil {
			s = &tally{family: a.Family, reasons: map[string]int{}}
			bySource[a.Source] = s
			order = append(order, a.Source)
		}
		s.draws++
		s.secs = append(s.secs, a.Seconds)
		switch {
		case a.Error != "":
			s.failed++
		case a.Verdict.OK():
			s.ok++
			s.words = append(s.words, float64(a.Verdict.Words))
		default:
			for _, r := range a.Verdict.Reasons {
				s.reasons[r]++
			}
		}
	}
	sort.SliceStable(order, func(i, j int) bool {
		a, b := bySource[order[i]], bySource[order[j]]
		if a.family != b.family {
			return a.family < b.family
		}
		return order[i] < order[j]
	})
	var b strings.Builder
	total, failed, ok := 0, 0, 0
	for _, s := range bySource {
		total += s.draws
		failed += s.failed
		ok += s.ok
	}
	fmt.Fprintf(&b, "# Deep random source report (%s)\n\n", time.Now().Format("2006-01-02 15:04"))
	fmt.Fprintf(&b, "%d draws per shelf, %d in all: %d did not open, %d whole pieces (%.0f%% of all), %d turned away.\n\n",
		n, total, failed, ok, 100*float64(ok)/float64(max(total, 1)), total-failed-ok)
	b.WriteString("| Source | Draws | Not opened | Taken | Turned away | Why | Median words taken | Median seconds |\n")
	b.WriteString("|---|---:|---:|---:|---:|---|---:|---:|\n")
	for _, src := range order {
		s := bySource[src]
		var rs []string
		for r, c := range s.reasons {
			rs = append(rs, fmt.Sprintf("%s %d", r, c))
		}
		slices.Sort(rs)
		fmt.Fprintf(&b, "| %s | %d | %d | %d | %d | %s | %.0f | %.1f |\n",
			src, s.draws, s.failed, s.ok, s.draws-s.failed-s.ok, strings.Join(rs, ", "), median(s.words), median(s.secs))
	}
	return b.String()
}

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := slices.Clone(xs)
	slices.Sort(s)
	return s[len(s)/2]
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
