package discover

import (
	"bytes"
	"compress/gzip"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"w5f/internal/doc"
	"w5f/internal/fetch"
)

// A BHL draw is a whole article (the owner, 2026-10-30): BHL keeps where
// each article of its journals starts and how many pages it has, and the
// article is cut out of its volume's OCR leaves, from its first page to its
// last ("w5f:discover/bhl?part=<id>"). The list is embedded
// (scripts/bhl_parts.py makes it from the open data).

//go:embed bhl_parts.tsv.gz
var bhlPartsGz []byte

type bhlArticle struct {
	part, item, year, first, pages int
	title, container               string
}

var (
	bhlPartsOnce sync.Once
	bhlParts     []bhlArticle
)

func loadBHLParts() {
	r, err := gzip.NewReader(bytes.NewReader(bhlPartsGz))
	if err != nil {
		return
	}
	b, err := io.ReadAll(r)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(b), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.SplitN(line, "\t", 7)
		if len(f) != 7 {
			continue
		}
		var n [5]int
		ok := true
		for i := range n {
			if n[i], err = strconv.Atoi(f[i]); err != nil {
				ok = false
			}
		}
		if ok && n[4] > 0 {
			bhlParts = append(bhlParts, bhlArticle{n[0], n[1], n[2], n[3], n[4], f[5], f[6]})
		}
	}
}

func bhlArticleByID(part int) (bhlArticle, bool) {
	bhlPartsOnce.Do(loadBHLParts)
	for _, a := range bhlParts {
		if a.part == part {
			return a, true
		}
	}
	return bhlArticle{}, false
}

// bhlArticlePick draws an article that opens and reads.
func bhlArticlePick(ctx context.Context, env Env) (string, string, error) {
	bhlPartsOnce.Do(loadBHLParts)
	if len(bhlParts) == 0 {
		return "", "", errors.New("no list of articles")
	}
	var errs []string
	for range 4 {
		a := bhlParts[rand.IntN(len(bhlParts))]
		if _, err := bhlArticleText(ctx, env.Fetcher, a); err != nil {
			if ctx.Err() != nil {
				return "", "", ctx.Err()
			}
			errs = append(errs, fmt.Sprintf("part %d: %v", a.part, err))
			continue
		}
		return "w5f:discover/bhl?part=" + strconv.Itoa(a.part), a.label(), nil
	}
	return "", "", errors.New(strings.Join(errs, "; "))
}

// label is the article as Deep random's line names it: its title, and the
// journal and year it is from.
func (a bhlArticle) label() string {
	s := a.title + " (" + shortContainer(a.container)
	if a.year > 0 {
		s += ", " + strconv.Itoa(a.year)
	}
	return s + ")"
}

// shortContainer: "The Emu : official organ of the Australasian
// Ornithologists' Union" is "The Emu".
func shortContainer(c string) string {
	if i := strings.IndexAny(c, ":;"); i > 0 {
		c = c[:i]
	}
	return strings.TrimSpace(c)
}

// bhlArticleText is an article's paragraphs: its leaves from the first page
// on, the lines on its first page before its title dropped (the end of the
// article before), tidied like a passage, a garbled paragraph (a table, a
// plate's noise) left out.
func bhlArticleText(ctx context.Context, f *fetch.Fetcher, a bhlArticle) ([]string, error) {
	leaves, err := bhlLeaves(ctx, f, a.item)
	if err != nil {
		return nil, err
	}
	at := -1
	for i, l := range leaves {
		if l.page == a.first {
			at = i
			break
		}
	}
	if at < 0 {
		return nil, errors.New("its first page has no text")
	}
	var pages []string
	for j := at; j < len(leaves) && j < at+a.pages; j++ {
		if j > at && leaves[j].seq != leaves[j-1].seq+1 {
			break
		}
		u, _ := url.Parse(bhlBucket + "/" + leaves[j].key)
		resp, err := f.Get(ctx, u, fetch.Options{})
		if err != nil {
			return nil, err
		}
		pages = append(pages, string(resp.Body))
	}
	pages[0] = fromTitle(pages[0], a.title)
	var paras []string
	size := 0
	for _, p := range tidyPages(pages) {
		if score, n := cleanliness(p); n >= 8 && score < paragraphMin {
			continue
		}
		paras = append(paras, p)
		size += len(p)
	}
	if size < bhlArticleMin {
		return nil, errors.New("too little text")
	}
	if !readable(strings.Join(paras, " ")) {
		return nil, errors.New("does not read")
	}
	return paras, nil
}

// bhlArticleMin is the least text of an article (characters).
const bhlArticleMin = 1500

// fromTitle drops the lines of a first page before the article's title, and
// the title itself: the OCR line that holds most of the title's words
// ("9. On a Livine Octopus. By J. P. G. Smiru" for "On a Living Octopus").
// Without such a line the page is kept whole.
func fromTitle(page, title string) string {
	want := headWords(title)
	if len(want) == 0 {
		return page
	}
	lines := strings.Split(strings.ReplaceAll(page, "\r\n", "\n"), "\n")
	for i, l := range lines {
		if i > len(lines)*2/3 {
			break
		}
		got := 0
		for w := range headWords(l) {
			for v := range want {
				if sameWord(w, v) {
					got++
					break
				}
			}
		}
		if need := min(len(want), 3); got >= need || got*2 > len(want) && got >= 2 {
			return strings.Join(lines[i+1:], "\n")
		}
	}
	return page
}

// sameWord: two words are one as OCR reads it when they agree in their first
// four letters and differ in length by one at most ("livine", "living").
func sameWord(a, b string) bool {
	ra, rb := []rune(a), []rune(b)
	d := len(ra) - len(rb)
	return string(ra[:4]) == string(rb[:4]) && d >= -1 && d <= 1
}

// headWords are a line's words of four letters or more, in small letters.
func headWords(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) }) {
		if len([]rune(w)) >= 4 {
			out[w] = true
		}
	}
	return out
}

// bhlArticleDoc builds the page of an article draw.
func bhlArticleDoc(ctx context.Context, env Env, target string, part int) (*doc.Document, error) {
	a, ok := bhlArticleByID(part)
	if !ok {
		return nil, errors.New("bhl: no such article")
	}
	paras, err := bhlArticleText(ctx, env.Fetcher, a)
	if err != nil {
		return nil, err
	}
	by := []string{shortContainer(a.container)}
	if a.year > 0 {
		by = append(by, strconv.Itoa(a.year))
	}
	d := &doc.Document{URL: target, Title: a.title, Lang: "en", Origin: "archive",
		Byline: strings.Join(append(by, "Biodiversity Heritage Library"), " · ")}
	d.Links = []doc.Link{
		{Href: fmt.Sprintf("https://www.biodiversitylibrary.org/part/%d", a.part), Text: "This article on BHL"},
		{Href: fmt.Sprintf("https://www.biodiversitylibrary.org/item/%d", a.item), Text: "The whole volume"},
	}
	for _, p := range paras {
		d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: p}}})
	}
	d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{
		{Text: "This article on BHL", Link: 1}, {Text: "  ·  "}, {Text: "The whole volume", Link: 2}}})
	return d, nil
}
