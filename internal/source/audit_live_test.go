package source

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode"

	"w5f/internal/discover"
	"w5f/internal/doc"
	"w5f/internal/fetch"
	"w5f/internal/store"
)

// TestSourceAuditLive draws from every Deep random shelf against the real
// sites and opens each page the way the reader does, then says what is wrong
// with it: nothing opens, too little text, a text that starts in the middle
// of a sentence, an error page. It needs the network, so it runs only with
// W5F_AUDIT=1 (W5F_AUDIT_N=<draws per shelf>, W5F_AUDIT_ONLY=<shelf,…>).
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
	bad, passed := 0, 0
	for _, name := range discover.FamilyNames() {
		if len(only) > 0 && !only[name] {
			continue
		}
		fam := discover.FamilyByName(name)
		for range n {
			ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
			start := time.Now()
			d, err := fam.Draw(ctx, env)
			var page *doc.Document
			if err == nil {
				page, err = Load(ctx, d.Target, Options{})
			}
			cancel()
			took := time.Since(start).Round(time.Second)
			if err != nil {
				bad++
				t.Logf("✗ %-12s %-5s %s\n      %v", name, took, d.Why, err)
				continue
			}
			words, first := auditText(page)
			if words < 80 { // Deep random and the Daily Packet pass such a page over (thin)
				passed++
				t.Logf("↷ %-12s %-5s %5dw %s (passed over)\n      %s", name, took, words, d.Why, d.Target)
				continue
			}
			var faults []string
			if words < 150 {
				faults = append(faults, fmt.Sprintf("only %d words", words))
			}
			if midSentence(first) {
				faults = append(faults, "starts mid-sentence")
			}
			if strings.TrimSpace(page.Title) == "" {
				faults = append(faults, "no title")
			}
			if words < 300 && looksLikeError(page, first) {
				faults = append(faults, "error page")
			}
			mark := "✓"
			if len(faults) > 0 {
				mark = "!"
				bad++
			}
			t.Logf("%s %-12s %-5s %5dw %s\n      %s\n      %s\n      %q %s", mark, name, took, words, d.Why, d.Target, page.Byline, clip(first, 90), strings.Join(faults, "; "))
		}
	}
	t.Logf("%d draws with something wrong, %d thin pages passed over", bad, passed)
}

// auditText counts a page's words of prose and gives its first real paragraph
// (past the notices and headings a page opens with).
func auditText(d *doc.Document) (int, string) {
	words, first := 0, ""
	var walk func([]doc.Block)
	walk = func(bs []doc.Block) {
		for _, b := range bs {
			switch b := b.(type) {
			case doc.Paragraph:
				var s strings.Builder
				for _, sp := range b.Text {
					if sp.Link == 0 {
						s.WriteString(sp.Text)
					}
				}
				w := len(strings.Fields(s.String()))
				words += w
				if first == "" && w >= 8 {
					first = strings.TrimSpace(s.String())
				}
			case doc.Pre:
				words += len(strings.Fields(b.Text))
				if first == "" && len(strings.Fields(b.Text)) >= 8 {
					first = strings.Join(strings.Fields(b.Text), " ")
				}
			case doc.Quote:
				walk(b.Blocks)
			case doc.Columns:
				for _, c := range b.Cols {
					walk(c)
				}
			case doc.Collapsible:
				walk(b.Blocks)
			}
		}
	}
	walk(d.Blocks)
	return words, first
}

// midSentence: a text that opens on a small letter or on punctuation was cut
// out of the middle of something.
func midSentence(s string) bool {
	for _, r := range s {
		if unicode.IsSpace(r) || r == '"' || r == '“' || r == '\'' || r == '‘' || r == '(' || r == '[' || r == '*' || r == '_' {
			continue
		}
		return unicode.IsLower(r) || r == ',' || r == ';' || r == '.' || r == ')' || r == '-'
	}
	return false
}

func looksLikeError(d *doc.Document, first string) bool {
	low := strings.ToLower(d.Title + " " + first)
	for _, w := range []string{"404", "not found", "access denied", "forbidden", "enable javascript", "just a moment", "page not found", "error"} {
		if strings.Contains(low, w) {
			return true
		}
	}
	return false
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
