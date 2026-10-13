package discover

import (
	_ "embed"
	"regexp"
	"strings"
	"sync"
	"unicode"
)

// The text the Internet Archive keeps for a scan is OCR, and OCR leaves
// marks of its own: words broken at the end of a line, letters from the wrong
// alphabet, quotes turned to asterisks, columns cut off in the middle of a
// sentence, whole paragraphs of noise where the print was faint. This file
// mends what can be mended, and measures what cannot, so that a window is
// cut from the cleanest part of an issue and starts and ends on a sentence.

// english_words.txt is the ordinary vocabulary of English (inflections
// included), made by scripts/english_words.py from public domain books.
//
//go:embed english_words.txt
var englishWordsText string

var englishWords = sync.OnceValue(func() map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(englishWordsText) {
		m[w] = true
	}
	for _, w := range strings.Fields(`a i of to in is it he be as at by on or no so we me my up us do go if an am oh ah eh ye ha ho hi la
		ok mr mrs dr st vs`) {
		m[w] = true
	}
	return m
})

// cyrillicLookalikes are the Cyrillic letters OCR puts in place of Latin ones
// in English print.
var cyrillicLookalikes = strings.NewReplacer(
	"А", "A", "В", "B", "С", "C", "Е", "E", "Н", "H", "І", "I", "К", "K", "М", "M", "О", "O", "Р", "P", "Т", "T", "Х", "X", "У", "Y",
	"а", "a", "с", "c", "е", "e", "і", "i", "о", "o", "р", "p", "х", "x", "у", "y", "к", "k", "п", "n", "т", "m", "в", "b", "н", "h",
	"Г", "I", "г", "r",
	"ﬁ", "fi", "ﬂ", "fl", "ﬀ", "ff", "ﬃ", "ffi", "ﬄ", "ffl",
	"‘‘", "“", "’’", "”", "``", "“", "''", "\"",
	"_", "", "|", " ", "~", "", "^", "", "¬", "-", "­", "-",
)

var (
	// A hyphen at the end of a line (the soft hyphen and the "not" sign some
	// scans use for it are already hyphens), with the spaces or blank lines
	// after it. A hyphen with no space after it is the text's own.
	reLineHyphen = regexp.MustCompile(`(\p{L}+)-\s+(\p{Ll}+)`)
	// OCR for a w: "w'hen", "w-hipped".
	reWApostrophe = regexp.MustCompile(`\b([Ww])['’-]([a-z])`)
	// A capital turned into two words: "T HEY".
	reSplitCap = regexp.MustCompile(`^([A-Z]) ([A-Z]{2,})\b`)
	// An asterisk where an opening quote should be.
	reStarQuote = regexp.MustCompile(`(^|\s)\*+\s?(\p{L})`)
	reSpaceMark = regexp.MustCompile(` +([.,;:!?])`)
)

// hyphenKeeps are the words after a hanging hyphen ("pre- and post-war")
// that mean the hyphen belongs to the text.
var hyphenKeeps = map[string]bool{"and": true, "or": true, "nor": true, "to": true, "of": true, "but": true}

// mendLines joins words broken at the end of a line or a column. When the
// two halves make a word ("Gover- nor") they are joined; when they are two
// words ("three- year") the hyphen stays, and the space goes; a hanging
// hyphen ("pre- and post-war") is left as it is.
func mendLines(s string) string {
	known := englishWords()
	return reLineHyphen.ReplaceAllStringFunc(s, func(m string) string {
		sub := reLineHyphen.FindStringSubmatch(m)
		left, right := strings.ToLower(sub[1]), sub[2]
		switch {
		case known[left+right]:
			return sub[1] + right
		case hyphenKeeps[right]:
			return m
		case known[left] && known[right]:
			return sub[1] + "-" + right
		}
		return sub[1] + right // neither half is a word: the scan is damaged, the word is the two together
	})
}

var rePageNumber = regexp.MustCompile(`^\d{1,4} (\p{Ll})`)

var (
	// A typewriter face's I, read as P or T: "Pll", "TIl", "Pm".
	reIContraction = regexp.MustCompile(`(^|[\s"“‘'(])(?:Pll|TIl|Tll|Pd|Pm|Pve)\b`)
	reICapital     = regexp.MustCompile(`(^|[\s"“‘'(])T (never|was|am|have|had|will|would|think|know|can|could|do|did|shall|must|should|see|want|guess|suppose|mean|say|said|hope|wish|thought)\b`)
)

var iContractions = map[string]string{"Pll": "I'll", "TIl": "I'll", "Tll": "I'll", "Pd": "I'd", "Pm": "I'm", "Pve": "I've"}

func mendPronoun(p string) string {
	p = reIContraction.ReplaceAllStringFunc(p, func(m string) string {
		lead := ""
		if c := m[0]; c == ' ' || c == '\t' || c == '"' || c == '\'' || c == '(' {
			lead = m[:1]
		} else if strings.HasPrefix(m, "“") || strings.HasPrefix(m, "‘") {
			lead = m[:len("“")]
		}
		return lead + iContractions[m[len(lead):]]
	})
	return reICapital.ReplaceAllString(p, "${1}I $2")
}

// mendMarks mends the small marks of OCR within a paragraph.
func mendMarks(p string) string {
	p = reWApostrophe.ReplaceAllString(p, "$1$2")
	p = reSplitCap.ReplaceAllString(p, "$1$2")
	p = reStarQuote.ReplaceAllString(p, "$1“$2")
	p = reSpaceMark.ReplaceAllString(p, "$1")
	p = rePageNumber.ReplaceAllString(p, "$1") // a page number in front of the rest of a sentence
	p = mendPronoun(p)
	toks := strings.Fields(p)
	for i := 1; i < len(toks); i++ {
		// A typewriter's capital I read for a small i: "participants In history".
		if capitalSmallWords[toks[i]] && startsLower(toks[i-1]) {
			toks[i] = strings.ToLower(toks[i])
		}
	}
	return repairWords(strings.Join(toks, " "))
}

// ocrConfusions are letters OCR takes for one another, read both ways: a
// word that is none, and becomes one by changing one place, is mended.
var ocrConfusions = func() [][2]string {
	var r [][2]string
	for _, p := range [][2]string{{"b", "h"}, {"e", "c"}, {"u", "n"}, {"i", "l"}, {"i", "u"}, {"f", "s"}, {"li", "h"}, {"ii", "u"},
		{"ii", "n"}, {"rn", "m"}, {"vv", "w"}, {"tl", "th"}} {
		r = append(r, p, [2]string{p[1], p[0]})
	}
	return r
}()

// repairWords mends the words of p that are not words but are one mistaken
// letter from exactly one word ("tbe", "bis", "mich").
func repairWords(p string) string {
	known := englishWords()
	locs := reWord.FindAllStringIndex(p, -1)
	var b strings.Builder
	last := 0
	for _, loc := range locs {
		b.WriteString(p[last:loc[0]])
		last = loc[1]
		b.WriteString(repairWord(p[loc[0]:loc[1]], known, sentenceStart(p, loc[0])))
	}
	b.WriteString(p[last:])
	return b.String()
}

// glueTails are the little words OCR runs onto the word before them
// ("yearsof", "cityof", "nota").
var glueTails = map[string]bool{"of": true, "to": true, "in": true, "at": true, "the": true, "and": true, "for": true, "with": true,
	"was": true, "that": true, "but": true, "a": true}

// unglue splits "yearsof" into "years of": a word that is none, made of a
// word of four letters or more and one of glueTails, in one way only.
func unglue(w string, known map[string]bool) string {
	found := ""
	for i := 4; i < len(w); i++ {
		if known[w[:i]] && glueTails[w[i:]] {
			if found != "" {
				return ""
			}
			found = w[:i] + " " + w[i:]
		}
	}
	return found
}

func repairWord(w string, known map[string]bool, atStart bool) string {
	if len(w) < 3 || strings.ContainsAny(w, "'’") {
		return w
	}
	lw := strings.ToLower(w)
	if known[lw] {
		return w
	}
	title := isCapitalized(w)
	if title && !atStart || !title && lw != w { // a name, a word in capitals, a word with a capital in it
		return w
	}
	if !title && len(lw) >= 5 {
		if split := unglue(lw, known); split != "" {
			return split
		}
	}
	found, n := "", 0
	for _, rule := range ocrConfusions {
		for from := 0; ; {
			j := strings.Index(lw[from:], rule[0])
			if j < 0 {
				break
			}
			at := from + j
			from = at + 1
			if cand := lw[:at] + rule[1] + lw[at+len(rule[0]):]; known[cand] && cand != found {
				found = cand
				n++
			}
		}
	}
	if n != 1 {
		return w
	}
	if title {
		found = strings.ToUpper(found[:1]) + found[1:]
	}
	return found
}

var capitalSmallWords = map[string]bool{"Is": true, "In": true, "Its": true, "It": true, "If": true, "Of": true, "On": true, "To": true,
	"As": true, "At": true, "By": true, "And": true, "But": true, "Or": true}

// startsLower tells a plain lower-case word (no mark after it) from the end
// of a sentence.
func startsLower(w string) bool {
	for _, r := range w {
		if !unicode.IsLower(r) {
			return false
		}
	}
	return w != ""
}

// isCapitalized tells a word written with a capital and small letters.
func isCapitalized(w string) bool {
	for i, r := range w {
		if i == 0 && !unicode.IsUpper(r) || i > 0 && !unicode.IsLower(r) {
			return false
		}
	}
	return len(w) > 1
}

// abbreviations that end in a full stop without ending a sentence.
var abbreviations = map[string]bool{"mr": true, "mrs": true, "dr": true, "st": true, "capt": true, "gen": true, "col": true, "lieut": true,
	"gov": true, "sen": true, "rep": true, "hon": true, "messrs": true, "prof": true, "vs": true, "no": true, "ft": true, "co": true,
	"inc": true, "corp": true, "bros": true, "mt": true, "rev": true, "jr": true, "sr": true, "etc": true, "jan": true, "feb": true,
	"mar": true, "apr": true, "aug": true, "sept": true, "oct": true, "nov": true, "dec": true, "ave": true, "blvd": true, "mme": true}

// sentenceEnds lists the offsets just after each sentence end in p: a full
// stop, question or exclamation mark (and the quotes closing it), a space,
// and not an abbreviation or an initial.
func sentenceEnds(p string) []int {
	var ends []int
	rs := []rune(p)
	off := 0
	for i, r := range rs {
		n := len(string(r))
		off += n
		if r != '.' && r != '!' && r != '?' {
			continue
		}
		j := i + 1
		o := off
		for j < len(rs) && strings.ContainsRune(`"”’')]`, rs[j]) {
			o += len(string(rs[j]))
			j++
		}
		if j < len(rs) && rs[j] != ' ' {
			continue
		}
		if r == '.' {
			k := i
			for k > 0 && unicode.IsLetter(rs[k-1]) {
				k--
			}
			word := strings.ToLower(string(rs[k:i]))
			if len([]rune(word)) <= 1 || abbreviations[word] {
				continue
			}
		}
		ends = append(ends, o)
	}
	return ends
}

// endsSentence tells whether p ends on the end of a sentence.
func endsSentence(p string) bool {
	e := sentenceEnds(p + " ")
	return len(e) > 0 && e[len(e)-1] >= len(p)
}

func startsSentence(p string) bool {
	rest := strings.TrimLeft(p, `"“‘'(—- `)
	for i, r := range rest {
		if !unicode.IsUpper(r) && !unicode.IsDigit(r) {
			return false
		}
		// A lone capital that is not a word (a big first letter that was a
		// picture, or a stray mark): "X be given increased longevity".
		if unicode.IsUpper(r) && !strings.ContainsRune("AIO", r) {
			if tail := rest[i+len(string(r)):]; strings.HasPrefix(tail, " ") && len(tail) > 1 && unicode.IsLower([]rune(tail[1:])[0]) {
				return false
			}
		}
		return true
	}
	return false
}

// wordRuns are the words of a text: runs of letters, with the apostrophes
// inside them.
var reWord = regexp.MustCompile(`\p{L}+(?:['’]\p{L}+)*`)

// cleanliness measures how much of a text is English that was read rightly:
// the share of its words that are words. A capitalised word that is not in
// the list, inside a sentence, is taken for a name and left out of the count.
// Marks that belong to no print (< > { } = and so on) count against the text.
func cleanliness(text string) (score float64, words int) {
	known := englishWords()
	good, bad := 0, 0
	for _, loc := range reWord.FindAllStringIndex(text, -1) {
		w := text[loc[0]:loc[1]]
		lw := strings.ToLower(strings.ReplaceAll(w, "’", "'"))
		ok := known[lw] || known[strings.TrimSuffix(lw, "'s")] || known[strings.TrimSuffix(lw, "s'")]
		if !ok {
			if i := strings.IndexByte(lw, '\''); i > 0 { // "don't", "o'clock": judged by the first part
				ok = known[lw[:i]] && known[lw[i+1:]] || known[lw[:i]]
			}
		}
		if ok {
			good++
			continue
		}
		if isCapitalized(w) && !sentenceStart(text, loc[0]) {
			continue // a name
		}
		bad++
	}
	junk := 0
	for _, r := range text {
		if strings.ContainsRune("<>{}=\\@#$%&+*[]", r) {
			junk++
		}
	}
	bad += junk / 3
	if good+bad == 0 {
		return 0, 0
	}
	return float64(good) / float64(good+bad), good + bad
}

// sentenceStart tells whether the word at offset i opens a sentence (so that
// a capital there says nothing about names).
func sentenceStart(text string, i int) bool {
	pre := strings.TrimRight(text[:i], " \"“‘'(")
	return pre == "" || strings.ContainsRune(".!?:\n", rune(pre[len(pre)-1]))
}

// token classes for splitJunk
const (
	tokClean = iota
	tokGray  // a capitalised word that is not in the list: a name, or noise next to noise
	tokJunk  // a mark that belongs to no text, or letters that make no word and no name
)

func tokenClass(tok string, known map[string]bool) int {
	t := strings.Trim(tok, `.,;:!?"“”‘’'()—–-$%/&`)
	if t == "" {
		return tokClean
	}
	if strings.ContainsAny(t, `■»«<>{}[]^=#@°§¢£•¦\`) {
		return tokJunk
	}
	if strings.ContainsAny(t, "0123456789") {
		return tokClean
	}
	runs := reWord.FindAllString(t, -1)
	if len(runs) == 0 {
		return tokJunk
	}
	class := tokClean
	for _, r := range runs {
		switch {
		case known[strings.ToLower(r)], len(r) == 1 && strings.ToUpper(r) == r:
		case isCapitalized(r):
			class = max(class, tokGray)
		default:
			return tokJunk
		}
	}
	return class
}

// splitJunk cuts a paragraph where three or more tokens in a row are junk
// (a line of noise where the print was faint), leaving the parts around it.
// A name-like word between junk tokens is part of the noise.
func splitJunk(p string) []string {
	known := englishWords()
	toks := strings.Fields(p)
	junk := make([]bool, len(toks))
	gray := make([]bool, len(toks))
	for i, t := range toks {
		switch tokenClass(t, known) {
		case tokJunk:
			junk[i] = true
		case tokGray:
			gray[i] = true
		}
	}
	for i := 1; i < len(toks); i++ { // noise spreads over the grey words next to it, both ways
		junk[i] = junk[i] || gray[i] && junk[i-1]
	}
	for i := len(toks) - 2; i >= 0; i-- {
		junk[i] = junk[i] || gray[i] && junk[i+1]
	}
	var parts []string
	from, run := 0, 0
	for i := 0; i <= len(toks); i++ {
		if i < len(toks) && junk[i] {
			run++
			continue
		}
		if run >= 3 {
			if i-run > from {
				parts = append(parts, strings.Join(toks[from:i-run], " "))
			}
			from = i
		}
		run = 0
	}
	if from < len(toks) {
		parts = append(parts, strings.Join(toks[from:], " "))
	}
	return parts
}
