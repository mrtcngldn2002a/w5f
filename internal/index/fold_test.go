package index

import (
	"testing"
	"unicode/utf8"
)

func TestFoldKeepsPositions(t *testing.T) {
	cases := map[string]string{
		"Kafatası İSTANBUL Şehir": "kafatasi istanbul sehir",
		"Café Ñandú Øre":          "cafe nandu ore",
		"çğıöşü ÇĞIÖŞÜ":           "cgiosu cgiosu",
		"plain text 173":          "plain text 173",
	}
	for in, want := range cases {
		got := Fold(in)
		if got != want {
			t.Errorf("Fold(%q) = %q, want %q", in, got, want)
		}
		if utf8.RuneCountInString(got) != utf8.RuneCountInString(in) {
			t.Errorf("Fold(%q) changed the rune count", in)
		}
	}
}
