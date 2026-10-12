package discover

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"w5f/internal/doc"
	"w5f/internal/fetch"
)

// A leaf of a book alone is a poor thing to read: it starts and ends in the
// middle of a sentence, with the running head and the page number in it, and
// the next page of the same book is another file. A BHL draw is a passage of
// a few leaves in a row, joined, tidied and cut at sentence ends
// ("w5f:discover/bhl?item=<id>&seq=<first leaf>&t=<title>").

const (
	bhlMaxLeaves  = 5    // leaves in a passage at most
	bhlPassageMin = 3200 // characters a passage grows to at least
	bhlPassageOK  = 2400 // and under which it is not worth showing
)

type bhlLeaf struct {
	key        string
	item, page int
	seq, size  int
}

type s3Listing struct {
	Contents []struct {
		Key  string `xml:"Key"`
		Size int    `xml:"Size"`
	} `xml:"Contents"`
}

var reLeafKey = regexp.MustCompile(`item-(\d+)-(\d+)-(\d+)\.txt$`)

// bhlLeaves lists the pages of a book that have a text file, in order.
func bhlLeaves(ctx context.Context, f *fetch.Fetcher, item int) ([]bhlLeaf, error) {
	prefix := fmt.Sprintf("ocr/item-%06d/", item)
	u, _ := url.Parse(bhlBucket + "/?list-type=2&max-keys=1000&prefix=" + url.QueryEscape(prefix))
	resp, err := f.Get(ctx, u, fetch.Options{})
	if err != nil {
		return nil, err
	}
	var l s3Listing
	if err := xml.Unmarshal(resp.Body, &l); err != nil {
		return nil, errors.New("unexpected listing")
	}
	var out []bhlLeaf
	for _, c := range l.Contents {
		m := reLeafKey.FindStringSubmatch(c.Key)
		if m == nil {
			continue
		}
		it, _ := strconv.Atoi(m[1])
		pg, _ := strconv.Atoi(m[2])
		sq, _ := strconv.Atoi(m[3])
		out = append(out, bhlLeaf{key: c.Key, item: it, page: pg, seq: sq, size: c.Size})
	}
	return out, nil
}

// bhlPassage joins the leaves from index i on until the passage is long
// enough; it returns the paragraphs and the leaves used.
func bhlPassage(ctx context.Context, f *fetch.Fetcher, leaves []bhlLeaf, i int) ([]string, []bhlLeaf, error) {
	var pages []string
	var used []bhlLeaf
	chars := 0
	for j := i; j < len(leaves) && len(used) < bhlMaxLeaves; j++ {
		if j > i && leaves[j].seq != leaves[j-1].seq+1 {
			break // a gap: the leaf between has no text of its own
		}
		u, _ := url.Parse(bhlBucket + "/" + leaves[j].key)
		resp, err := f.Get(ctx, u, fetch.Options{})
		if err != nil {
			return nil, nil, err
		}
		pages = append(pages, string(resp.Body))
		used = append(used, leaves[j])
		if chars += len(resp.Body); chars >= bhlPassageMin*5/4 {
			break
		}
	}
	paras := tidyPages(pages)
	size := 0
	for _, p := range paras {
		size += len(p)
	}
	if size < bhlPassageOK {
		return nil, nil, errors.New("too short")
	}
	return paras, used, nil
}

// bhlPage picks a passage of a random book that reads: English prose, not an
// index, a table, a list of names or a plate.
func bhlPage(ctx context.Context, env Env) (string, string, error) {
	var errs []string
	for range 5 {
		it, ok := pickBHL()
		if !ok {
			return "", "", errors.New("no list of books")
		}
		leaves, err := bhlLeaves(ctx, env.Fetcher, it.id)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		// Starts: leaves of a good size, not the first or last few (covers,
		// plates, indexes), tried in random order.
		var starts []int
		for i, l := range leaves {
			if l.size >= 1500 && l.size <= 12000 && i >= 3 && i < len(leaves)-6 {
				starts = append(starts, i)
			}
		}
		for _, k := range rand.Perm(len(starts))[:min(len(starts), 4)] {
			paras, used, err := bhlPassage(ctx, env.Fetcher, leaves, starts[k])
			if err != nil || !readable(strings.Join(paras, " ")) {
				continue
			}
			title := strings.TrimSpace(it.title)
			if it.year > 0 {
				title += " (" + strconv.Itoa(it.year) + ")"
			}
			v := url.Values{"item": {strconv.Itoa(it.id)}, "seq": {strconv.Itoa(used[0].seq)}, "t": {title}}
			return "w5f:discover/bhl?" + v.Encode(), title + ", " + leafRange(used), nil
		}
		errs = append(errs, fmt.Sprintf("item %d: no readable passage", it.id))
	}
	return "", "", errors.New(strings.Join(errs, "; "))
}

func leafRange(used []bhlLeaf) string {
	if len(used) == 1 {
		return fmt.Sprintf("leaf %d", used[0].seq)
	}
	return fmt.Sprintf("leaves %d–%d", used[0].seq, used[len(used)-1].seq)
}

// bhlRoute builds the passage a draw promised (the same one every time).
func bhlRoute(ctx context.Context, env Env, target string) (*doc.Document, error) {
	q, err := url.ParseQuery(strings.TrimPrefix(target, "w5f:discover/bhl?"))
	if err != nil {
		return nil, err
	}
	item, err1 := strconv.Atoi(q.Get("item"))
	seq, err2 := strconv.Atoi(q.Get("seq"))
	if err1 != nil || err2 != nil || item <= 0 {
		return nil, errors.New("bhl: bad address")
	}
	leaves, err := bhlLeaves(ctx, env.Fetcher, item)
	if err != nil {
		return nil, err
	}
	at := -1
	for i, l := range leaves {
		if l.seq == seq {
			at = i
		}
	}
	if at < 0 {
		return nil, errors.New("bhl: that leaf has no text")
	}
	paras, used, err := bhlPassage(ctx, env.Fetcher, leaves, at)
	if err != nil {
		return nil, err
	}
	d := &doc.Document{URL: target, Title: q.Get("t"), Lang: "en", Origin: "archive",
		Byline: "Biodiversity Heritage Library · " + leafRange(used)}
	d.Links = []doc.Link{
		{Href: fmt.Sprintf("https://www.biodiversitylibrary.org/page/%d", used[0].page), Text: "This page on BHL"},
		{Href: fmt.Sprintf("https://www.biodiversitylibrary.org/item/%d", item), Text: "The whole book"},
	}
	for _, p := range paras {
		d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: p}}})
	}
	d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{
		{Text: "This page on BHL", Link: 1}, {Text: "  ·  "}, {Text: "The whole book", Link: 2}}})
	return d, nil
}

// --- Tidying the OCR of consecutive leaves.

var (
	reNumeral     = regexp.MustCompile(`^[ivxlcdm]+\.?$`)
	reLongHyphen  = regexp.MustCompile(`(\p{L})[-¬]\n[ \t]*(\p{Ll})`)
	reSentenceEnd = regexp.MustCompile(`[.!?]["”’')\]]*$`)
	reSentenceGap = regexp.MustCompile(`[.!?]["”’')\]]*\s+["“‘(]?\p{Lu}`)
)

// isRunningHead tells the line at the top or foot of a page that is no part
// of the text: a page number, a roman numeral, or a short line of capitals.
func isRunningHead(line string) bool {
	line = strings.TrimSpace(line)
	if line == "" || len([]rune(line)) > 70 {
		return false
	}
	upper, lower := 0, 0
	for _, r := range line {
		switch {
		case unicode.IsUpper(r):
			upper++
		case unicode.IsLower(r):
			lower++
		}
	}
	if lower == 0 || reNumeral.MatchString(line) { // numbers, numerals and lines of capitals
		return true
	}
	return upper > 3*lower && upper > 4
}

// tidyPages joins the pages' text into paragraphs: the running heads dropped,
// lines joined, a word broken across lines mended, a paragraph that runs over
// a page break kept whole, the start cut to the first whole sentence and the
// end to the last.
func tidyPages(pages []string) []string {
	var paras []string
	cont := false // the last paragraph is not finished
	for _, page := range pages {
		page = strings.ReplaceAll(page, "\r\n", "\n")
		page = reLongHyphen.ReplaceAllString(page, "$1$2")
		lines := strings.Split(page, "\n")
		// Running heads: the first and last non-empty lines.
		first, last := -1, -1
		for i, l := range lines {
			if strings.TrimSpace(l) != "" {
				if first < 0 {
					first = i
				}
				last = i
			}
		}
		for _, i := range []int{first, last} {
			if i >= 0 && isRunningHead(lines[i]) {
				lines[i] = ""
			}
		}
		var cur []string
		flush := func() {
			if len(cur) == 0 {
				return
			}
			p := strings.Join(strings.Fields(strings.Join(cur, " ")), " ")
			cur = nil
			if p == "" {
				return
			}
			if cont && len(paras) > 0 {
				paras[len(paras)-1] += " " + p
			} else {
				paras = append(paras, p)
			}
			cont = !reSentenceEnd.MatchString(paras[len(paras)-1])
		}
		for _, l := range lines {
			if strings.TrimSpace(l) == "" {
				flush()
				continue
			}
			cur = append(cur, l)
		}
		flush()
	}
	if len(paras) == 0 {
		return nil
	}
	// The start: a first paragraph that begins in the middle of a sentence
	// loses that fragment.
	if r := []rune(paras[0]); !unicode.IsUpper(r[0]) && !unicode.IsDigit(r[0]) && r[0] != '"' && r[0] != '“' {
		if loc := reSentenceGap.FindStringIndex(paras[0]); loc != nil && loc[0] < 700 {
			// The match ends on the capital that starts the next sentence.
			_, w := lastRune(paras[0][:loc[1]])
			paras[0] = strings.TrimSpace(paras[0][loc[1]-w:])
		} else {
			paras = paras[1:]
		}
	}
	// The end: the last paragraph is cut after its last whole sentence.
	if n := len(paras); n > 0 && !reSentenceEnd.MatchString(paras[n-1]) {
		p := paras[n-1]
		end := -1
		for _, m := range reSentenceGap.FindAllStringIndex(p, -1) {
			end = m[0] + 1
			for end < len(p) && strings.ContainsRune(`"”’')]`, rune(p[end])) {
				end++
			}
		}
		if end > 0 {
			paras[n-1] = strings.TrimSpace(p[:end])
		} else {
			paras = paras[:n-1]
		}
	}
	return paras
}

// lastRune is the last rune of s and its width in bytes.
func lastRune(s string) (rune, int) {
	r := []rune(s)
	last := r[len(r)-1]
	return last, len(string(last))
}
