package discover

import (
	"regexp"
	"strings"
	"unicode"
)

// A clean scan is not all something to read: the masthead, the notices and
// the advertisements are clean print too, and a page of them passes for
// prose when only its spelling is counted. These tell them from prose by what
// they say, and a dirty page from a clean one by the marks that OCR leaves.

// reBoilerplate are phrases of mastheads, notices and advertisements.
var reBoilerplate = regexp.MustCompile(`(?i)second[- ]class|entered as|all rights reserved|subscription (?:price|rate)s?|owned and published|` +
	`published (?:every|daily|weekly|monthly|bi-?weekly|semi-?monthly)|printed in (?:the )?u\.? ?s|advertising rates?|` +
	`advertisements? (?:accepted|must|rates)|cash in advance|return postage|unsolicited manuscripts?|manuscripts? (?:must|should|are)|` +
	`send no money|\bc\. ?o\. ?d\b|mail (?:this |the )?coupon|money[- ]back|\bpost-?paid\b|\bdept\. ?[a-z0-9]|` +
	`write (?:today|for (?:free|our|full))|order (?:now|today|by mail)|\bfor sale\b|\bfor rent\b|\bwanted:|\bphone \d|\btelephone \d`)

// boilerplate tells a paragraph that is a masthead, a notice or an
// advertisement by what it says.
func boilerplate(p string) bool { return reBoilerplate.MatchString(p) }

// adWords are the words of an advertisement; strongAdWords the ones that
// hardly turn up in anything else.
var adWords, strongAdWords = func() (map[string]bool, map[string]bool) {
	ad, strong := map[string]bool{}, map[string]bool{}
	for _, w := range strings.Fields(`price prices cost costs sale sales buy buying order orders ordering guarantee guaranteed guarantees
		dealer dealers bargain bargains offer offered offers quality service services models model cash discount catalog catalogue
		coupon satisfaction satisfied customers customer patrons store stores stock goods merchandise brand famous genuine product
		products manufactured manufacturer manufacturers delivery equipment complete special free write mail send address trial
		samples sample repair repairing repairs batteries battery tires wholesale retail values reduced ladies misses sizes shoes
		credit terms payments efficient modern latest reasonable prompt proprietor proprietors inc phone`) {
		ad[w] = true
	}
	for _, w := range strings.Fields(`guarantee guaranteed dealers dealer bargain bargains customers customer patrons merchandise genuine
		manufacturer manufacturers coupon catalog catalogue discount prices price`) {
		strong[w] = true
	}
	return ad, strong
}()

// adShare is the share of the words of a text that are words of an
// advertisement, and the share that are the strong ones.
func adShare(text string) (ad, strong float64, words int) {
	a, s := 0, 0
	for _, w := range strings.Fields(text) {
		w = strings.ToLower(strings.TrimFunc(w, func(r rune) bool { return !unicode.IsLetter(r) }))
		if w == "" {
			continue
		}
		words++
		if adWords[w] {
			a++
		}
		if strongAdWords[w] {
			s++
		}
	}
	if words == 0 {
		return 0, 0, 0
	}
	return float64(a) / float64(words), float64(s) / float64(words), words
}

// advertisement tells a paragraph that speaks as one: two or more of the
// words that hardly turn up in anything else (prices, dealers, guarantees).
func advertisement(p string) bool {
	_, strong, n := adShare(p)
	return n >= 12 && strong*float64(n) >= 1.5
}

// smallWordsOK are the words of one or two letters that English has (and the
// titles and the abbreviations that are written with one or two).
var smallWordsOK = func() map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(`a i o of to in is it as at be by do go he if me my no on or so up us we am an ah oh ye
		mr dr st jr sr vs ms mt co tv ok pm`) {
		m[w] = true
	}
	return m
}()

var reOddWord = regexp.MustCompile(`[«»°^*~|_\\{}\[\]<>#@]`)
var reDigitLetter = regexp.MustCompile(`[A-Za-z]\d|\d[A-Za-z]`)
var reOrdinal = regexp.MustCompile(`^\d+(?:st|nd|rd|th|s|d|x)$`)

// strayMarks are the shares of the words of a text that are a letter or two
// that are no word ("ft", "df", "J") and that hold marks that print does not
// (« ° ^ | or a digit among letters): what a bad scan leaves between the
// words, which a word list does not see because each of them is not a word
// that could be mistaken for one.
func strayMarks(text string) (stray, odd float64) {
	s, o, n := 0, 0, 0
	for _, w := range strings.Fields(text) {
		core := strings.TrimFunc(w, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
		if core == "" {
			continue
		}
		n++
		letters := 0
		for _, r := range core {
			if unicode.IsLetter(r) {
				letters++
			}
		}
		if letters == len([]rune(core)) && letters <= 2 && !smallWordsOK[strings.ToLower(core)] {
			// an initial: a capital and a full stop
			if !(letters == 1 && unicode.IsUpper([]rune(core)[0]) && strings.HasSuffix(w, ".")) {
				s++
			}
		}
		if reOddWord.MatchString(w) || reDigitLetter.MatchString(core) && !reOrdinal.MatchString(core) {
			o++
		}
	}
	if n == 0 {
		return 0, 0
	}
	return float64(s) / float64(n), float64(o) / float64(n)
}

const (
	strayMax  = 0.010 // share of stray letters a window may have
	oddMax    = 0.006 // share of words with odd marks
	adMax     = 0.033 // share of the words of advertisements
	strongMax = 0.008
)

// proseOK tells a window that is prose to read: not a page of advertisements
// and not strewn with the marks of a bad scan.
func proseOK(text string) bool {
	stray, odd := strayMarks(text)
	if stray > strayMax || odd > oddMax {
		return false
	}
	ad, strong, _ := adShare(text)
	return ad < adMax && strong < strongMax
}
