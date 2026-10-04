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
