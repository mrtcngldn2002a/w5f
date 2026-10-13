package discover

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/url"
	"regexp"
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
func pagesText(ctx context.Context, f *fetch.Fetcher, src *url.URL, r string) (text string, from, to, size int, err error) {
	if _, err = fmt.Sscanf(r, "%d-%d/%d", &from, &to, &size); err != nil || from < 0 || to < from || size < to {
		return "", 0, 0, 0, errors.New("window: bad range")
	}
	u := *src
	u.Fragment = "range=" + r
	resp, err := f.Get(ctx, &u, fetch.Options{Header: http.Header{"Range": {fmt.Sprintf("bytes=%d-%d", from, to)}}})
	if err != nil {
		return "", 0, 0, 0, fmt.Errorf("%w: %v", errFetch, err)
	}
	pages := djvuPages(string(resp.Body))
	if len(pages) == 0 {
		return "", 0, 0, 0, errNoPage
	}
	return reflowPages(pages), from, to, size, nil
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
	text = mendLines(text)
	var scraps []string
	for _, p := range reParagraphBreak.Split(text, -1) {
		p = mendMarks(strings.TrimSpace(reBlanks.ReplaceAllString(p, " ")))
		if len([]rune(p)) >= 40 || fragment(p) {
			scraps = append(scraps, p)
		}
	}
	// A sentence the print broke off at a column or a page goes on in the
	// next paragraph, in lower case.
	var joined []string
	for _, p := range scraps {
		if n := len(joined); n > 0 && !endsSentence(joined[n-1]) && continues(p) {
			joined[n-1] += " " + p
			continue
		}
		joined = append(joined, p)
	}
	var out []string
	for _, whole := range joined {
		for _, p := range splitJunk(whole) {
			if p = wholeSentences(p); len([]rune(p)) < 60 || dropCapLost(p) || boilerplate(p) || advertisement(p) {
				continue
			}
			if score, n := cleanliness(p); n >= 8 && score < paragraphMin {
				continue
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
	best, checked := 0.0, 0
	for _, c := range found {
		if checked == 16 {
			break
		}
		if text := strings.Join(paras[c.start:c.end], " "); !readable(text) || !proseOK(text) {
			continue
		}
		checked++
		if c.score > best {
			best, win, from = c.score, paras[c.start:c.end], c.start
		}
	}
	return win, from
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
	rFrom, rTo, rSize := 0, 0, 0
	if r := q.Get("r"); r != "" {
		if text, rFrom, rTo, rSize, err = pagesText(ctx, f, src, r); err != nil {
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
	win, from := pickWindow(paras, seed)
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
	d := &doc.Document{URL: target, Title: title, Lang: "en", Origin: "archive",
		Byline: fmt.Sprintf("a passage, %d%% into the text", percent)}
	d.Links = []doc.Link{{Href: whole, Text: "The whole text"}}
	for _, p := range win {
		d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: p}}})
	}
	d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: "The whole text", Link: 1}}})
	return d, nil
}
