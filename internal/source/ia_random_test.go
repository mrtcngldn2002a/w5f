package source

import "testing"

func TestResolveArchiveAndOwnSites(t *testing.T) {
	cases := map[string]string{
		"ia":                                "w5f:books/ia",
		"archive":                           "w5f:books/ia",
		"ia alchemy":                        "w5f:books/ia?q=alchemy",
		"ia subject:alchemy":                "w5f:books/ia?q=subject%3Aalchemy",
		"ia @americana":                     "w5f:books/ia?c=americana",
		"ia @americana old roads":           "w5f:books/ia?c=americana&q=old+roads",
		"random-sites":                      "w5f:discover/sites",
		"random-add example.org":            "w5f:discover/sites/check?url=example.org",
		"random-add https://a.org/x occult": "w5f:discover/sites/check?family=occult&url=https%3A%2F%2Fa.org%2Fx",
	}
	for in, want := range cases {
		if got := Resolve(in); got != want {
			t.Errorf("Resolve(%q) = %q, want %q", in, got, want)
		}
	}
}

// Commands are read without case; caps lock on a Turkish keyboard (LİBGEN)
// too, while the words of a search keep their letters.
func TestResolveWithoutCase(t *testing.T) {
	cases := map[string]string{
		"TAROT":          "w5f:discover/tarot",
		"Deep Random":    "w5f:discover/random",
		"IA":             "w5f:books/ia",
		"İA":             "w5f:books/ia",
		"LİBGEN dracula": "w5f:books/libgen?q=dracula",
		"HİSTORY":        "w5f:history",
		"RANDOM-SİTES":   "w5f:discover/sites",
		"PICK NPC":       "w5f:solo/pick/character",
		"W5F:books":      "w5f:books",
		"R/nosleep":      "https://www.reddit.com/r/nosleep/",
		"U/someone":      "https://www.reddit.com/u/someone/",
		"gut İstanbul":   "w5f:books/gutenberg?q=%C4%B0stanbul",
		"ılık bir gün":   "w5f:search/web?q=%C4%B1l%C4%B1k+bir+g%C3%BCn",
		"İstanbul":       "w5f:search/web?q=%C4%B0stanbul",
	}
	for in, want := range cases {
		if got := Resolve(in); got != want {
			t.Errorf("Resolve(%q) = %q, want %q", in, got, want)
		}
	}
}
