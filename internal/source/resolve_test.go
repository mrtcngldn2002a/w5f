package source

import (
	"net/url"
	"strings"
	"testing"
)

func TestResolveSearches(t *testing.T) {
	cases := map[string]string{
		"gobekli tepe":      "w5f:search/web?q=gobekli+tepe",
		"gnosticism":        "w5f:search/web?q=gnosticism",
		"?scp-173":          "w5f:search/web?q=scp-173",
		"scp antimemetics":  "w5f:search/wikis?q=antimemetics",
		"r/nosleep":         "https://www.reddit.com/r/nosleep/",
		"reddit lighthouse": "https://www.reddit.com/search/?q=lighthouse",
	}
	for in, want := range cases {
		if got := Resolve(in); got != want {
			t.Errorf("Resolve(%q) = %q, want %q", in, got, want)
		}
	}
	if got := Resolve("w Göbekli Tepe"); !strings.HasPrefix(got, "https://en.wikipedia.org/w/index.php?") || !strings.Contains(got, "go=Go") {
		t.Errorf("wikipedia search = %q", got)
	}
}

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"https://www.scp-wiki.wikidot.com/scp-173":  "https://scp-wiki.wikidot.com/scp-173",
		"http://www.wanderers-library.wikidot.com/": "https://wanderers-library.wikidot.com/",
		"https://scp-wiki.net/scp-173":              "https://scp-wiki.wikidot.com/scp-173",
		"https://www.wikipedia.org":                 "https://en.wikipedia.org/wiki/Main_Page",
		"https://wikipedia.com/":                    "https://en.wikipedia.org/wiki/Main_Page",
		"https://www.wikipedia.org/wiki/X":          "https://www.wikipedia.org/wiki/X",
		"https://arkeofili.com/a":                   "https://arkeofili.com/a",
	}
	for in, want := range cases {
		u, _ := url.Parse(in)
		if got := Normalize(u).String(); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}
