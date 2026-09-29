package catalog

import (
	"testing"
	"time"

	"w5f/internal/doc"
)

func TestNumber(t *testing.T) {
	cases := map[string]string{
		"https://scp-wiki.wikidot.com/scp-173":                                             "FIC·SCP·173",
		"https://scp-wiki.wikidot.com/scp-173-j":                                           "FIC·SCP·scp-173-j",
		"https://wanderers-library.wikidot.com/six-etchings-in-the-basalt-of-olympus-mons": "FIC·WL·six-etchings-in-the-basa",
		"https://backrooms-wiki.wikidot.com/level-0":                                       "FIC·BR·level-0",
		"https://www.reddit.com/r/nosleep/comments/abc/a_story/":                           "WEB·RDT·nosleep",
		"https://arkeofili.com/gobekli-tepe/":                                              "WEB·ARKEOFIL·gobekli-tepe",
		"https://example.org/":                                                             "WEB·EXAMPLE·home",
		"w5f:feeds":                                                                        "",
	}
	for in, want := range cases {
		if got := Number(in, &doc.Document{}); got != want {
			t.Errorf("Number(%q) = %q, want %q", in, got, want)
		}
	}
	if got := Number("https://x.example/a", &doc.Document{Catalog: "PER·ARKEOF·2026-09-24"}); got != "PER·ARKEOF·2026-09-24" {
		t.Errorf("document catalog not used: %q", got)
	}
	if got := Number("https://scp-wiki.wikidot.com/scp-096", nil); got != "FIC·SCP·096" {
		t.Errorf("nil document: %q", got)
	}
}

func TestFeedBookLabel(t *testing.T) {
	if got := Feed("arkeofili", time.Date(2026, 9, 24, 8, 0, 0, 0, time.UTC)); got != "PER·ARKEOF·2026-09-24" {
		t.Errorf("Feed = %q", got)
	}
	if got := Feed("nasa", time.Time{}); got != "PER·NASA" {
		t.Errorf("Feed without date = %q", got)
	}
	books := map[string]string{"gutenberg:345": "BK·GUT·345", "se:bram-stoker/dracula": "BK·SE·dracula",
		"site:fadedpage-com:https://x/1": "BK·FADEDPAG·9", "": "BK·LOC·9"}
	for src, want := range books {
		if got := Book(src, 9); got != want {
			t.Errorf("Book(%q) = %q, want %q", src, got, want)
		}
	}
	if Label("feed") != "[RSS]" || Label("clip") != "[KES]" || Label("other") != "[OTHER]" {
		t.Error("labels")
	}
}
