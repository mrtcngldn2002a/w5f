// Package index is W5F's full-text search over everything read: folding,
// indexing documents, searching and the results page.
package index

import (
	"strings"
	"unicode"
)

// foldMap maps a lower-case letter to its base letter, one rune to one rune.
var foldMap = map[rune]rune{
	'ı': 'i', 'ş': 's', 'ğ': 'g', 'ü': 'u', 'ö': 'o', 'ç': 'c',
	'á': 'a', 'à': 'a', 'â': 'a', 'ä': 'a', 'ã': 'a', 'å': 'a', 'ā': 'a', 'ă': 'a', 'ą': 'a',
	'é': 'e', 'è': 'e', 'ê': 'e', 'ë': 'e', 'ē': 'e', 'ė': 'e', 'ę': 'e', 'ě': 'e',
	'í': 'i', 'ì': 'i', 'î': 'i', 'ï': 'i', 'ī': 'i', 'į': 'i',
	'ó': 'o', 'ò': 'o', 'ô': 'o', 'õ': 'o', 'ø': 'o', 'ō': 'o', 'ő': 'o',
	'ú': 'u', 'ù': 'u', 'û': 'u', 'ū': 'u', 'ů': 'u', 'ű': 'u', 'ų': 'u',
	'ñ': 'n', 'ń': 'n', 'ň': 'n', 'ý': 'y', 'ÿ': 'y', 'ć': 'c', 'č': 'c', 'ď': 'd',
	'ł': 'l', 'ľ': 'l', 'ř': 'r', 'ś': 's', 'š': 's', 'ť': 't', 'ź': 'z', 'ż': 'z', 'ž': 'z',
	'’': '\'', '‘': '\'',
}

// Fold lower-cases s and removes diacritics (Turkish ı and İ included),
// one rune for one rune, so positions in folded text match the original.
func Fold(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		b.WriteRune(foldRune(r))
	}
	return b.String()
}

func foldRune(r rune) rune {
	if r == 'İ' {
		return 'i'
	}
	r = unicode.ToLower(r)
	if f, ok := foldMap[r]; ok {
		return f
	}
	return r
}
