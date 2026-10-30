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
