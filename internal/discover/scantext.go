package discover

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	reParagraphBreak = regexp.MustCompile(`
[ 	]*
+`)
	reBlanks = regexp.MustCompile(`[ 	
]+`)
)

// paragraphs splits scanned text into paragraphs, mending what OCR broke:
// lines are joined, words broken at the end of a line mended, a sentence cut
// by a column or a page joined to its rest. A paragraph keeps only whole
// sentences (the head of one whose beginning is in another column, the tail
// of one whose end is, are cut away), and a paragraph that is mostly noise
// is dropped, as are scraps such as page numbers and running heads.
func paragraphs(text string) []string {
	text = cyrillicLookalikes.Replace(strings.ReplaceAll(text, "\r\n", "\n"))
	text = mendLines(markBylines(text))
	var scraps []string
	for _, p := range reParagraphBreak.Split(text, -1) {
		p = mendMarks(strings.TrimSpace(reBlanks.ReplaceAllString(p, " ")))
		if len([]rune(p)) >= 40 || fragment(p) || p == openMark {
			scraps = append(scraps, p)
		}
	}
	// A sentence the print broke off at a column or a page goes on in the
	// next paragraph, in lower case.
	var joined []string
	for _, p := range scraps {
		if n := len(joined); n > 0 && joined[n-1] != openMark && !endsSentence(joined[n-1]) && continues(p) {
			joined[n-1] += " " + p
			continue
		}
		joined = append(joined, p)
	}
	var out []string
	opens := false // the next paragraph kept is the first of a story (it follows a title and a byline)
	for _, whole := range joined {
		if whole == openMark {
			opens = true
			continue
		}
		for _, p := range splitJunk(whole) {
			if p = mendDropCap(wholeSentences(p)); len([]rune(p)) < 60 || dropCapLost(p) || boilerplate(p) || advertisement(p) {
				continue
			}
			if score, n := cleanliness(p); n >= 8 && score < paragraphMin {
				continue
			}
			if opens {
				p, opens = openMark+" "+p, false
			}
			out = append(out, p)
		}
	}
	return out
}

// fragment tells a short piece of a sentence (the last line of a paragraph
// that a page break cut off: "courting.", "I said. “Your shining") from a page
// number or a running head: it has words in lower case that are not those of
// a title, or it ends on a mark of the sentence.
func fragment(p string) bool {
	if p == strings.ToUpper(p) {
		return false
	}
	words := strings.Fields(p)
	if last, _ := utf8.DecodeLastRuneInString(p); strings.ContainsRune(`.,;:!?)”’"'—-`, last) {
		return len(words) >= 2 || startsLower(strings.TrimRight(p, `.,;:!?)”’"'—-`))
	}
	for _, w := range words {
		w = strings.TrimFunc(w, func(r rune) bool { return !unicode.IsLetter(r) })
		if startsLower(w) && !titleWords[w] {
			return true
		}
	}
	return false
}

// titleWords are the small words a title keeps in lower case.
var titleWords = map[string]bool{"of": true, "the": true, "and": true, "in": true, "a": true, "an": true, "to": true, "for": true,
	"on": true, "at": true, "by": true, "with": true, "from": true, "or": true}

// continues tells a paragraph that goes on with a sentence begun in the
// one before: it begins with a plain word in lower case.
func continues(p string) bool {
	return startsLower(strings.TrimRight(firstWord(p), `.,;:!?)”’"'—`))
}

func firstWord(p string) string {
	w, _, _ := strings.Cut(p, " ")
	return w
}

// dropCapLost tells a paragraph whose first word is in capitals and no word:
// the big first letter of a story's opening was a picture, and the OCR left
// "UDGE TUCKER'S sparse beard".
func dropCapLost(p string) bool {
	w := strings.TrimFunc(firstWord(p), func(r rune) bool { return !unicode.IsLetter(r) })
	if len(w) < 3 || strings.ToUpper(w) != w {
		return false
	}
	return !englishWords()[strings.ToLower(w)]
}

// openMark begins a paragraph that follows a story's title and byline
// ("THE TRIAL" / "by Brian Stableford"): magazines of later years set no
// capitals at a story's start, but they name it and its writer. It is taken
// off before the paragraph is shown (shown).
const openMark = "\uE000"

var reByline = regexp.MustCompile(`^(?i:by)\s+\p{Lu}[\p{L}'’.-]*(?:\s+\p{Lu}[\p{L}'’.-]*){0,3}$`)

// markBylines puts an openMark between the title and byline of a story and
// what follows them: a byline is a line "by" and a name of up to four words,
// after a line in capitals (the title).
func markBylines(text string) string {
	lines := strings.Split(text, "\n")
	prev := "" // the last line that was not blank
	for i, ln := range lines {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		if len([]rune(ln)) <= 50 && reByline.MatchString(ln) && titleLine(prev) {
			lines[i] = "\n" + openMark + "\n"
		}
		prev = ln
	}
	return strings.Join(lines, "\n")
}

// titleLine tells a line set in capitals, as a title is: short, and of its
// letters (two words at least) all but a few upper case.
func titleLine(ln string) bool {
	if n := len([]rune(ln)); n == 0 || n > 80 {
		return false
	}
	upper, letters := 0, 0
	for _, r := range ln {
		if unicode.IsLetter(r) {
			letters++
			if unicode.IsUpper(r) {
				upper++
			}
		}
	}
	return letters >= 3 && upper*10 >= letters*9
}

// shown is a paragraph as it is shown, without its openMark.
func shown(p string) string {
	return strings.TrimPrefix(strings.TrimPrefix(p, openMark), " ")
}

// mendDropCap gives back the big first letter of a story's opening that the
// OCR lost because it was a picture ("HEY went" for "THEY went"): when the
// first word, in capitals, is no word, or one that hardly begins a story,
// and one letter only before it makes one of the commonest words, or it is
// no word and one letter only makes a word at all.
//
// A quotation mark before the word is kept ("“HEY went" for "“THEY
// went"), and a letter the OCR read in front of a lone word is taken away
// ("“TI crept" for "“I crept").
func mendDropCap(p string) string {
	body := strings.TrimLeft(p, "“\"‘'")
	return p[:len(p)-len(body)] + mendFirstWord(body)
}

func mendFirstWord(p string) string {
	first := firstWord(p)
	w := strings.TrimRightFunc(first, func(r rune) bool { return !unicode.IsLetter(r) })
	if len(w) < 2 || strings.ToUpper(w) != w || !strings.HasPrefix(first, w) {
		return p
	}
	goesOn, next := false, "" // after the run of capitals the text goes on in lower case: an opening
	rest := strings.Fields(strings.TrimPrefix(p, first))
	for i, n := range rest[:min(4, len(rest))] {
		if strings.ContainsAny(n, "!?") {
			break
		}
		if n = strings.TrimFunc(n, func(r rune) bool { return !unicode.IsLetter(r) && r != '\'' && r != '’' }); strings.ToUpper(n) != n {
			goesOn = startsLower(n)
			if i == 0 {
				next = n
			}
			break
		}
	}
	if !goesOn || strings.ContainsAny(first, "!?") {
		return p
	}
	lw := strings.ToLower(w)
	// "HE" and "HIS" are words, and what is left of "THE" and "THIS": the
	// word after them tells which ("HE international exchange", "HIS is").
	switch {
	case lw == "he" && next != "" && !heGoesOn(next), lw == "his" && auxiliaries[next]:
		return "T" + p
	}
	known := englishWords()
	if known[lw] && !rareOpeners[lw] {
		return p
	}
	var common, any []rune
	for c := 'a'; c <= 'z'; c++ {
		if smallWords[string(c)+lw] || openers[string(c)+lw] {
			common = append(common, c)
		}
		if known[string(c)+lw] {
			any = append(any, c)
		}
	}
	switch {
	case !known[lw] && smallWords[lw[1:]]: // "TI crept": the OCR read a letter in front of "I"
		return p[1:]
	case len(common) == 1:
		return string(unicode.ToUpper(common[0])) + p
	case len(common) == 0 && len(any) == 1 && !known[lw]:
		return string(unicode.ToUpper(any[0])) + p
	}
	return p
}

// auxiliaries are the verbs that go with "this" and not with "his".
var auxiliaries = map[string]bool{"is": true, "was": true, "has": true, "had": true, "will": true, "would": true, "could": true,
	"may": true, "might": true, "must": true, "should": true, "can": true, "does": true, "did": true, "seemed": true, "seems": true}

// heGoesOn tells a word that may follow "he": a verb or an adverb (or one
// that looks like one: -ed, -s, -ly, -n't), not a noun or an adjective as
// after "the".
func heGoesOn(w string) bool {
	if auxiliaries[w] || heWords[w] {
		return true
	}
	for _, end := range []string{"ed", "s", "ly", "n't", "n’t"} {
		if strings.HasSuffix(w, end) {
			return true
		}
	}
	return false
}

var heWords = func() map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(`said came went saw knew thought felt stood sat took made found got told began
		shall never always also only just still then now too himself ran lay kept left heard put gave held let set read
		wrote spoke shook drew grew threw rose woke fell fought brought bought caught taught sought meant sent spent built
		felt slept swept wept dug hung struck stuck swung won wore tore bore broke chose froze spoke stole drove rode wrote
		hid bit lit led fed fled met sang rang sank swam began knew blew flew drew threw saw ate forgot understood
		would could should might must can will may did do had has is was ought dared once often seldom rarely even
		almost hardly barely looked turned walked stopped smiled laughed nodded opened and alone who
		arose awoke bade beheld bent bled bred burnt clung crept dealt dove dreamt drank dwelt flung forbade forgave
		forsook gave ground hurt knelt laid leant leapt lent lost overcame paid quit shed shone shot shrank shut slew slid
		slung slunk smote sped spat split spread sprang stank strode strove swore thrust trod upset wove wrung wound`) {
		m[w] = true
	}
	return m
}()

// rareOpeners are words that hardly begin a story, but are what is left of
// one that does when its first letter is lost.
var rareOpeners = map[string]bool{"hey": true, "or": true}

// openers are words that often begin a story besides the commonest ones.
var openers = map[string]bool{"why": true, "where": true, "while": true, "once": true, "every": true, "never": true,
	"even": true, "still": true, "just": true, "yes": true, "nobody": true, "everyone": true, "somewhere": true}

// opening tells a paragraph that begins a story, a chapter or an article:
// magazines set its first word or words in capitals ("THE ship came down",
// "A STRANGE thing"). A shout ("NO!" he cried) is not one, nor a line of
// capitals; a first word in capitals that is no word (a big first letter the
// OCR lost) was dropped already (dropCapLost).
func opening(p string) bool {
	if strings.HasPrefix(p, openMark) {
		return true
	}
	words := strings.Fields(p)
	if len(words) < 8 || strings.Contains(strings.ToLower(p), "next issue") { // the magazine's own notices
		return false
	}
	long := false
	for i, w := range words[:5] {
		if strings.ContainsAny(w, "!?") {
			return false
		}
		w = strings.TrimFunc(w, func(r rune) bool { return !unicode.IsLetter(r) })
		if w == "" {
			return false
		}
		if strings.ToUpper(w) != w {
			// the first word that is not in capitals ends the run: it is
			// the text going on, in lower case
			return i > 0 && long && startsLower(w)
		}
		if len([]rune(w)) >= 2 {
			long = true
		}
	}
	return false
}

// wholeSentences cuts away from a paragraph the head that begins in the
// middle of a sentence and the tail that ends in the middle of one.
func wholeSentences(p string) string {
	ends := sentenceEnds(p)
	if !startsSentence(p) {
		i := 0
		for i < len(ends) && ends[i] < len(p) && !startsSentence(strings.TrimSpace(p[ends[i]:])) {
			i++
		}
		if i >= len(ends) || ends[i] >= len(p) {
			return ""
		}
		p, ends = strings.TrimSpace(p[ends[i]:]), nil
		ends = sentenceEnds(p)
	}
	if n := len(ends); n > 0 && ends[n-1] < len(p) && !endsSentence(p) {
		p = strings.TrimSpace(p[:ends[n-1]])
	} else if n == 0 {
		return ""
	}
	return p
}

// paragraphMin is the share of a paragraph's words that must be words.
const paragraphMin = 0.85

// readable tells English prose from the noise of a bad scan, a table, an
// index, a list of Latin names or a text in another language: most of its
// words are whole words of ordinary length; a good share of them are the
// small words of English (the, of, and, to...); few characters are digits;
// and the letter f is not as frequent as in a text printed with the long s
// ("fhells", "firft"). The limits come from 60 pages of the Biodiversity
// Heritage Library read by hand: prose had 0.38 or more small words, indexes
// and lists 0.35 or less; prose 0.036 f at most, long-s texts 0.056 or more.
func readable(text string) bool {
	words := strings.Fields(text)
	if len(words) < 40 {
		return false
	}
	good, letters, total, small, f, digits := 0, 0, 0, 0, 0, 0
	for _, w := range words {
		w = strings.TrimFunc(w, func(r rune) bool { return unicode.IsPunct(r) || unicode.IsSymbol(r) })
		n := 0
		ok := w != ""
		for _, r := range w {
			if unicode.IsLetter(r) {
				n++
				if r == 'f' || r == 'F' {
					f++
				}
			} else if r != '\'' && r != '’' && r != '-' {
				ok = false
			}
			if unicode.IsDigit(r) {
				digits++
			}
		}
		letters += n
		total += len([]rune(w))
		if ok && n >= 1 && n <= 14 {
			good++
		}
		if smallWords[strings.ToLower(w)] {
			small++
		}
	}
	avg := float64(letters) / float64(len(words))
	if !(float64(good)/float64(len(words)) >= 0.8 && avg >= 3 && avg <= 9 && letters*10 >= total*9) {
		return false
	}
	return float64(small)/float64(len(words)) >= 0.33 && float64(f)/float64(max(letters, 1)) <= 0.043 &&
		float64(digits)/float64(max(total, 1)) <= 0.02
}

// smallWords are the commonest words of English.
var smallWords = func() map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(`the of and to a in is that it was he for on are as with his they i at be this from or
		had by not but what all were when we there can an your which their said if do will each about how up out them then
		she many some so these would other into has her two like him see time could no make than first been its who now
		people my made over did down only way find use may water long little very after`) {
		m[w] = true
	}
	return m
}()
