package discover

import (
	"fmt"
	"strings"
	"testing"
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
