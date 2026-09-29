package source

import "testing"

func TestResolvePersonalWords(t *testing.T) {
	cases := map[string]string{
		"queue":               "w5f:queue",
		"notes":               "w5f:notes",
		"history":             "w5f:history",
		"find kafatası kültü": "w5f:find?q=kafatas%C4%B1+k%C3%BClt%C3%BC",
	}
	for in, want := range cases {
		if got := Resolve(in); got != want {
			t.Errorf("Resolve(%q) = %q, want %q", in, got, want)
		}
	}
}
