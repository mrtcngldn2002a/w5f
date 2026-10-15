package discover

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"w5f/internal/doc"
	"w5f/internal/fetch"
)

// A scanned magazine or newspaper issue is hundreds of pages of text. Deep
// random does not open all of it: it opens a window of about a page or two at
// a place the draw picked ("w5f:discover/window?u=<text>&t=<title>&k=<seed>"),
// the same place every time the address is opened again, with a link to the
// whole text.

// windowTarget is the address of a window onto the text at src.
func windowTarget(src, title string) string {
	v := url.Values{"u": {src}, "t": {title}, "k": {strconv.FormatInt(rand.Int64N(1<<40), 10)}}
	return "w5f:discover/window?" + v.Encode()
}

// windowPagesTarget is the address of a window onto the pages in bytes
// from..to (of size) of the DjVu XML at src, where every word of the scan
// has its place on the page (see layout.go).
func windowPagesTarget(src, title string, from, to, size int) string {
	v := url.Values{"u": {src}, "t": {title}, "k": {strconv.FormatInt(rand.Int64N(1<<40), 10)},
		"r": {fmt.Sprintf("%d-%d/%d", from, to, size)}}
	return "w5f:discover/window?" + v.Encode()
}

// pagesText reads the bytes of r ("from-to/size") of the DjVu XML at src and
// makes the text of the pages in them, columns in order. The cache key of a
// range is the address with the range after a #, which is not sent.
// The text that follows begins at next: the last whole page of these, read
// again, for the sentence it ends on goes on in the pages after it (or, when
// there is one page only, the byte just past it).
func pagesText(ctx context.Context, f *fetch.Fetcher, src *url.URL, r string) (text string, from, to, size, next int, err error) {
	if _, err = fmt.Sscanf(r, "%d-%d/%d", &from, &to, &size); err != nil || from < 0 || to < from || size < to {
		return "", 0, 0, 0, 0, errors.New("window: bad range")
	}
	u := *src
	u.Fragment = "range=" + r
	resp, err := f.Get(ctx, &u, fetch.Options{Header: http.Header{"Range": {fmt.Sprintf("bytes=%d-%d", from, to)}}})
	if err != nil {
		return "", 0, 0, 0, 0, fmt.Errorf("%w: %v", errFetch, err)
	}
	pages := djvuPages(string(resp.Body))
	if len(pages) == 0 {
		return "", 0, 0, 0, 0, errNoPage
	}
	end := bytes.LastIndex(resp.Body, []byte("</OBJECT>"))
	next = from + end + len("</OBJECT>")
	if len(pages) > 1 {
		next = from + bytes.LastIndex(resp.Body[:end], []byte("<OBJECT "))
	}
	return reflowPages(pages), from, to, size, next, nil
}

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
func mendDropCap(p string) string {
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

const (
	windowMin = 3000 // characters a window grows to at least
	windowMax = 9000 // and at most

	windowClean = 0.95 // share of its words that must be words
)

// pickWindow picks a run of paragraphs worth reading: of every run in the
// middle of the text that is clean (see cleanliness), readable and prose, the
// cleanest of 16 picked by the seed (the same seed always picks the same
// one). Every place is looked at, not a few: in a dirty scan there are not
// many clean ones, and they are found only by looking.
func pickWindow(paras []string, seed int64) (win []string, from int) {
	if len(paras) == 0 {
		return nil, 0
	}
	lo, hi := len(paras)/20, len(paras)-len(paras)/20
	if hi <= lo {
		lo, hi = 0, len(paras)
	}
	// each paragraph is read for its words once, the runs add them up
	type counts struct{ good, bad int }
	cs := make([]counts, len(paras))
	for i, p := range paras {
		good, bad, junk := cleanCounts(p)
		cs[i] = counts{good, bad + junk/3}
	}
	type candidate struct {
		start, end int
		score      float64
	}
	var found []candidate
	for start := lo; start < hi; start++ {
		size, good, bad, end := 0, 0, 0, start
		for ; end < len(paras) && size < windowMin; end++ {
			if size+len(paras[end]) > windowMax && size > 0 {
				break
			}
			size += len(paras[end])
			good, bad = good+cs[end].good, bad+cs[end].bad
		}
		if size < windowMin/2 {
			continue
		}
		if score, _ := cleanScore(good, bad); score >= windowClean {
			found = append(found, candidate{start, end, score})
		}
	}
	rng := rand.New(rand.NewPCG(uint64(seed), 7))
	rng.Shuffle(len(found), func(a, b int) { found[a], found[b] = found[b], found[a] })
	// Runs that start where a story or a chapter does come first: a window
	// opened in the middle of a scene is a page torn out of a book.
	slices.SortStableFunc(found, func(a, b candidate) int {
		return cmp.Compare(b2i(!opening(paras[a.start])), b2i(!opening(paras[b.start])))
	})
	// Of the openings the first that reads is taken, so that the seed picks
	// one story of the issue and not always the cleanest; of other runs the
	// cleanest of 16.
	best, checked, opened := 0.0, 0, false
	for _, c := range found {
		if checked == 16 {
			break
		}
		if text := strings.Join(paras[c.start:c.end], " "); !readable(text) || !proseOK(text) {
			continue
		}
		checked++
		if opening(paras[c.start]) {
			win, from, opened = paras[c.start:c.end], c.start, true
			break
		}
		if c.score > best {
			best, win, from = c.score, paras[c.start:c.end], c.start
		}
	}
	if opened { // it reads on to the end of the chapter
		win = paras[from:readOn(paras, from)]
	}
	return win, from
}

// lastWords are the last words of a paragraph, enough to find it again.
func lastWords(p string) string {
	r := []rune(p)
	if len(r) <= 40 {
		return p
	}
	cut := string(r[len(r)-40:])
	if i := strings.IndexByte(cut, ' '); i >= 0 {
		cut = cut[i+1:]
	}
	return cut
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// windowLong is how far a window that starts at an opening reads: to the
// next opening (the chapter or the story is over), or about this far.
const windowLong = 16000

// readOn is the end of the window that starts at paragraph from and reads
// on: to the next opening once it has windowMin, or windowLong at most.
func readOn(paras []string, from int) int {
	size, end := 0, from
	for end < len(paras) && size < windowLong {
		if end > from && size >= windowMin && opening(paras[end]) {
			break
		}
		size += len(paras[end])
		end++
	}
	return end
}

// errFetch is the answer when the Archive's file cannot be had (a data node
// that does not answer): the same file will not come from the same place
// again soon.
var errFetch = errors.New("window: the file cannot be had")

// errNoPage is the answer when the bytes asked for hold no whole page: the
// pages of the scan are bigger than they are.
var errNoPage = errors.New("window: no whole page in the range")

// errDirtyScan is the answer for a text of which hardly anything is clean
// (a bad microfilm, a language that is not English): another place in the
// same scan will not do better, as one with a few clean words in it might.
var errDirtyScan = errors.New("window: too dirty to read")

// dirtyShare is the percent of a text that must be left after the cleaning
// for it not to be dirty; scans that read have been found with a tenth.
const dirtyShare = 8

func windowRoute(ctx context.Context, env Env, target string) (*doc.Document, error) {
	return buildWindow(ctx, env.Fetcher, target)
}

// buildWindow makes the page of a window address; a draw calls it too, to
// know the text exists and has a passage worth showing (the fetch is cached,
// so opening the page costs nothing more).
func buildWindow(ctx context.Context, f *fetch.Fetcher, target string) (*doc.Document, error) {
	q, err := url.ParseQuery(strings.TrimPrefix(target, "w5f:discover/window?"))
	if err != nil {
		return nil, err
	}
	src, err := url.Parse(q.Get("u"))
	if err != nil || (src.Scheme != "http" && src.Scheme != "https") || src.Host == "" {
		return nil, errors.New("window: bad address")
	}
	seed, _ := strconv.ParseInt(q.Get("k"), 10, 64)
	var text string
	rFrom, rTo, rSize, rNext := 0, 0, 0, 0
	if r := q.Get("r"); r != "" {
		if text, rFrom, rTo, rSize, rNext, err = pagesText(ctx, f, src, r); err != nil {
			return nil, err
		}
	} else {
		resp, err := f.Get(ctx, src, fetch.Options{})
		if err != nil {
			return nil, err
		}
		text = string(resp.Body)
	}
	paras := paragraphs(text)
	var win []string
	var from int
	// "p": reading on from paragraph p (the "read on" of the window before).
	readingOn := q.Has("p")
	if readingOn {
		if from, err = strconv.Atoi(q.Get("p")); err != nil || from < 0 || from >= len(paras) {
			return nil, errors.New("window: nothing more to read here")
		}
		win = paras[from:readOn(paras, from)]
		// "a": the pages after the window before, which ended on these
		// words: they are read on from just after them.
		if a := q.Get("a"); a != "" {
			for i, p := range paras {
				j := strings.Index(shown(p), a)
				if j < 0 {
					continue
				}
				from = i
				rest := strings.TrimSpace(shown(p)[j+len(a):])
				if rest == "" {
					from = i + 1
				}
				if from >= len(paras) {
					return nil, errors.New("window: nothing more to read here")
				}
				win = slices.Clone(paras[from:readOn(paras, from)])
				if rest != "" {
					win[0] = rest
				}
				break
			}
		}
	} else {
		win, from = pickWindow(paras, seed)
	}
	if len(win) == 0 {
		kept := 0
		for _, p := range paras {
			kept += len(p)
		}
		if kept < len(text)*dirtyShare/100 {
			return nil, fmt.Errorf("%w (%s)", errDirtyScan, src.Host)
		}
		return nil, errors.New("window: no readable passage in " + src.Host)
	}
	percent := from * 100 / max(len(paras), 1)
	whole := src.String()
	if rSize > 0 { // pages of the XML: the way in is by bytes, the whole text is the Archive's text file
		percent = (rFrom + (rTo-rFrom)*from/max(len(paras), 1)) * 100 / rSize
		whole = strings.Replace(whole, "_djvu.xml", "_djvu.txt", 1)
	}
	title := strings.TrimSpace(q.Get("t"))
	if title == "" {
		title = "A passage"
	}
	what := "a passage"
	if readingOn {
		what = "read on"
	} else if opening(win[0]) {
		what = "from the start of a story or chapter"
	}
	d := &doc.Document{URL: target, Title: title, Lang: "en", Origin: "archive",
		Byline: fmt.Sprintf("%s, %d%% into the text", what, percent)}
	d.Links = []doc.Link{{Href: whole, Text: "The whole text"}}
	for _, p := range win {
		d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: shown(p)}}})
	}
	// What follows: the rest of these pages, or the pages after them.
	v := url.Values{"u": {q.Get("u")}, "t": {q.Get("t")}, "k": {q.Get("k")}}
	if end := from + len(win); end < len(paras) {
		if r := q.Get("r"); r != "" {
			v.Set("r", r)
		}
		v.Set("p", strconv.Itoa(end))
		d.Next = "w5f:discover/window?" + v.Encode()
	} else if rSize > 0 && rNext < rSize-1 {
		v.Set("r", fmt.Sprintf("%d-%d/%d", rNext, min(rNext+rTo-rFrom, rSize-1), rSize))
		v.Set("p", "0")
		v.Set("a", lastWords(shown(win[len(win)-1])))
		d.Next = "w5f:discover/window?" + v.Encode()
	}
	foot := doc.Inline{{Text: "The whole text", Link: 1}}
	if d.Next != "" {
		d.Links = append(d.Links, doc.Link{Href: d.Next, Text: "Read on"})
		foot = append(foot, doc.Span{Text: "  ·  "}, doc.Span{Text: "Read on ›", Link: 2})
	}
	d.Blocks = append(d.Blocks, doc.Paragraph{Text: foot})
	return d, nil
}
