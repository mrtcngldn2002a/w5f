package index

import (
	"regexp"
	"strings"
	"unicode"

	"w5f/internal/doc"
	"w5f/internal/store"
)

// Hit is one search result.
type Hit struct {
	Target, Kind, Title, Catalog string
	Snippet                      doc.Inline
}

var (
	rePhrase = regexp.MustCompile(`"([^"]*)"`)
	// kindGroups are the result filters.
	kindGroups = map[string][]string{"feed": {"feed"}, "scp": {"scp", "wiki"}, "book": {"book"},
		"web": {"web", "reddit"}, "notes": {"note", "clip", "saved"}}
)

// Match builds an FTS5 query from what the owner typed: every word must
// match (as a prefix when it has 3+ letters) and "quoted text" is a phrase.
// FTS syntax in the input is always taken literally. terms are the folded
// words and phrases, for snippets.
func Match(q string) (match string, terms []string) {
	var parts []string
	add := func(t string, prefix bool) {
		t = strings.Join(strings.FieldsFunc(Fold(t), func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r)
		}), " ")
		if t == "" {
			return
		}
		terms = append(terms, t)
		p := `"` + strings.ReplaceAll(t, `"`, `""`) + `"`
		if prefix && !strings.Contains(t, " ") && len([]rune(t)) >= 3 {
			p += "*"
		}
		parts = append(parts, p)
	}
	for _, m := range rePhrase.FindAllStringSubmatch(q, -1) {
		add(m[1], false)
	}
	for _, w := range strings.Fields(strings.ReplaceAll(rePhrase.ReplaceAllString(q, " "), `"`, " ")) {
		add(w, true)
	}
	return strings.Join(parts, " "), terms
}

// Search finds documents; kind is "" or one of the result filters.
func Search(db *store.DB, q, kind string, limit, offset int) ([]Hit, error) {
	match, terms := Match(q)
	if match == "" {
		return nil, nil
	}
	ds, err := db.FindDocs(match, kindGroups[kind], limit, offset)
	if err != nil {
		return nil, err
	}
	hits := make([]Hit, 0, len(ds))
	for _, d := range ds {
		hits = append(hits, Hit{Target: d.Target, Kind: d.Kind, Title: d.Title, Catalog: d.Catalog, Snippet: Snippet(d.Text, terms)})
	}
	return hits, nil
}

// Snippet cuts about 160 characters of the original text around the first
// match and marks the matched words bold.
func Snippet(text string, terms []string) doc.Inline {
	rt := []rune(strings.Join(strings.Fields(text), " "))
	rf := []rune(Fold(string(rt))) // same length: Fold keeps one rune per rune
	at := -1
	for i := range rf {
		if wordStart(rf, i) && matchAt(rf, i, terms) > 0 {
			at = i
			break
		}
	}
	start := 0
	if at > 60 {
		start = at - 60
	}
	end := min(len(rt), start+160)
	var out doc.Inline
	if start > 0 {
		out = append(out, doc.Span{Text: "…"})
	}
	plain := start
	for i := start; i < end; {
		if n := matchAt(rf, i, terms); n > 0 && wordStart(rf, i) {
			if i > plain {
				out = append(out, doc.Span{Text: string(rt[plain:i])})
			}
			e := min(i+n, end)
			out = append(out, doc.Span{Text: string(rt[i:e]), Style: doc.Bold})
			i, plain = e, e
			continue
		}
		i++
	}
	if plain < end {
		out = append(out, doc.Span{Text: string(rt[plain:end])})
	}
	if end < len(rt) {
		out = append(out, doc.Span{Text: "…"})
	}
	return out
}

func wordStart(rf []rune, i int) bool {
	return i == 0 || !(unicode.IsLetter(rf[i-1]) || unicode.IsDigit(rf[i-1]))
}

func matchAt(rf []rune, i int, terms []string) int {
	for _, t := range terms {
		tr := []rune(t)
		if i+len(tr) <= len(rf) && string(rf[i:i+len(tr)]) == t {
			return len(tr)
		}
	}
	return 0
}
