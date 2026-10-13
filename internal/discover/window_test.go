package discover

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"w5f/internal/doc"
)

const goodText = "The old naturalist walked down to the shore every morning and wrote what he saw in a small book, " +
	"for he held that nothing seen once is ever seen twice in quite the same way."

func issueText(n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "%s Entry %d records the tide, the weather and the birds that came with them.\n\n", goodText, i)
		if i%10 == 0 {
			b.WriteString("12\n\nTHE WEIRD MONTHLY\n\n")
		}
	}
	return b.String()
}

func TestParagraphsJoinLinesAndMendHyphens(t *testing.T) {
	text := "A first paragraph of the scanned page that runs over sev-\neral lines and is long\nenough to be kept as it is here.\n\n47\n\nTHE WEIRD\n\nSecond paragraph, also long enough to keep, with a body of words that goes on and on."
	got := paragraphs(text)
	if len(got) != 2 {
		t.Fatalf("paragraphs: %q", got)
	}
	if !strings.Contains(got[0], "several lines and is long enough") {
		t.Errorf("lines joined and hyphen mended: %q", got[0])
	}
}

func TestReadableTellsTextFromScanNoise(t *testing.T) {
	if !readable(strings.Repeat(goodText+" ", 3)) {
		t.Error("ordinary prose is readable")
	}
	noise := strings.Repeat("Biri A94a b znartattas 8i sat 44 hét 8 MLLAKULTTONT TET 1 VAYTAT jat papa ", 6)
	if readable(noise) {
		t.Error("scan noise is not readable")
	}
	if readable("too short") {
		t.Error("a scrap is not readable")
	}
	if readable(strings.Repeat("|| -- ~~ // __ ## ** ", 40)) {
		t.Error("punctuation is not readable")
	}
}

func TestPickWindowIsSteadyAndReadable(t *testing.T) {
	paras := paragraphs(issueText(120))
	a, from := pickWindow(paras, 42)
	b, from2 := pickWindow(paras, 42)
	if len(a) == 0 || from != from2 || strings.Join(a, "|") != strings.Join(b, "|") {
		t.Fatal("the same seed must pick the same passage")
	}
	size := 0
	for _, p := range a {
		size += len(p)
	}
	if size < windowMin/2 || size > windowMax {
		t.Errorf("passage of %d characters", size)
	}
	other, _ := pickWindow(paras, 43)
	if strings.Join(a, "|") == strings.Join(other, "|") {
		t.Error("another seed should usually pick another place")
	}
	if w, _ := pickWindow(paragraphs(strings.Repeat("Biri A94a b znartattas 8i sat 44 hét 8 MLLAKULTTONT TET 1 VAYTAT jat papa\n\n", 80)), 1); w != nil {
		t.Error("a text of noise has no passage")
	}
}

func TestWindowRoute(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprint(w, issueText(150))
	}))
	defer srv.Close()
	src := srv.URL + "/download/x/x_djvu.txt"
	target := windowTarget(src, "Weird Tales v22 (1933)")
	if !strings.HasPrefix(target, "w5f:discover/window?") {
		t.Fatalf("target %q", target)
	}
	q, _ := url.ParseQuery(strings.TrimPrefix(target, "w5f:discover/window?"))
	if q.Get("u") != src || q.Get("t") != "Weird Tales v22 (1933)" || q.Get("k") == "" {
		t.Fatalf("query %v", q)
	}
	env := Env{Fetcher: testFetcher()}
	d, err := Route(context.Background(), target, env)
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "Weird Tales v22 (1933)" || len(d.Blocks) < 3 || len(d.Links) != 1 || d.Links[0].Href != src {
		t.Fatalf("document: %+v", d)
	}
	last := d.Blocks[len(d.Blocks)-1].(doc.Paragraph)
	if last.Text[0].Link != 1 {
		t.Error("the last paragraph links to the whole text")
	}
	again, err := Route(context.Background(), target, env)
	if err != nil || again.Blocks[0].(doc.Paragraph).Text[0].Text != d.Blocks[0].(doc.Paragraph).Text[0].Text {
		t.Error("opening the address again shows the same passage")
	}
	if _, err := Route(context.Background(), "w5f:discover/window?u=file%3A%2F%2F%2Fetc%2Fpasswd", env); err == nil {
		t.Error("only web addresses are windowed")
	}
}

// In a dirty scan the clean stretches are few, and they are found by looking
// at every place, not at a few.
func TestPickWindowFindsTheOneCleanStretch(t *testing.T) {
	noise := strings.Repeat("xq vz kt jj qq zz bk wq lx jh rr tt ", 12)
	var paras []string
	for i := 0; i < 80; i++ {
		if i >= 40 && i < 60 {
			paras = append(paras, fmt.Sprintf("%s Entry %d records the tide, the weather and the birds that came with them.", goodText, i))
			continue
		}
		paras = append(paras, noise)
	}
	for seed := int64(1); seed <= 20; seed++ {
		win, from := pickWindow(paras, seed)
		if len(win) == 0 || from < 40 || from > 59 {
			t.Fatalf("seed %d: a window of %d paragraphs from %d", seed, len(win), from)
		}
		for _, p := range win {
			if strings.HasPrefix(p, "xq") {
				t.Fatalf("seed %d: noise in the window", seed)
			}
		}
	}
}
