package discover

import (
	"context"
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

var (
	reParagraphBreak = regexp.MustCompile(`\n[ \t]*\n+`)
	reHyphenBreak    = regexp.MustCompile(`(\p{L})-\n[ \t]*(\p{Ll})`)
	reBlanks         = regexp.MustCompile(`[ \t\n]+`)
)

// paragraphs splits scanned text into paragraphs (lines joined, words broken
// by a hyphen at the end of a line mended), dropping scraps such as page
// numbers and running heads.
func paragraphs(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = reHyphenBreak.ReplaceAllString(text, "$1$2")
	var out []string
	for _, p := range reParagraphBreak.Split(text, -1) {
		p = strings.TrimSpace(reBlanks.ReplaceAllString(p, " "))
		if len([]rune(p)) >= 60 {
			out = append(out, p)
		}
	}
	return out
}

// readable tells text from the noise of a bad scan: most of its words are
// whole words of ordinary length.
func readable(text string) bool {
	words := strings.Fields(text)
	if len(words) < 40 {
		return false
	}
	good, letters, total := 0, 0, 0
	for _, w := range words {
		w = strings.TrimFunc(w, func(r rune) bool { return unicode.IsPunct(r) || unicode.IsSymbol(r) })
		n := 0
		ok := w != ""
		for _, r := range w {
			if unicode.IsLetter(r) {
				n++
			} else if r != '\'' && r != '’' && r != '-' {
				ok = false
			}
		}
		letters += n
		total += len([]rune(w))
		if ok && n >= 1 && n <= 14 {
			good++
		}
	}
	avg := float64(letters) / float64(len(words))
	return float64(good)/float64(len(words)) >= 0.8 && avg >= 3 && avg <= 9 && letters*10 >= total*9
}

const (
	windowMin = 3000 // characters a window grows to at least
	windowMax = 9000 // and at most
)

// pickWindow picks a readable run of paragraphs at a random place in the
// middle of the text; the same seed always picks the same one.
func pickWindow(paras []string, seed int64) (win []string, from int) {
	if len(paras) == 0 {
		return nil, 0
	}
	rng := rand.New(rand.NewPCG(uint64(seed), 7))
	lo, hi := len(paras)/20, len(paras)-len(paras)/20
	if hi <= lo {
		lo, hi = 0, len(paras)
	}
	for range 16 {
		start := lo + rng.IntN(hi-lo)
		size := 0
		var run []string
		for i := start; i < len(paras) && size < windowMin; i++ {
			if size+len(paras[i]) > windowMax && size > 0 {
				break
			}
			run = append(run, paras[i])
			size += len(paras[i])
		}
		if size >= windowMin/2 && readable(strings.Join(run, " ")) {
			return run, start
		}
	}
	return nil, 0
}

func windowRoute(ctx context.Context, env Env, target string) (*doc.Document, error) {
	q, err := url.ParseQuery(strings.TrimPrefix(target, "w5f:discover/window?"))
	if err != nil {
		return nil, err
	}
	src, err := url.Parse(q.Get("u"))
	if err != nil || (src.Scheme != "http" && src.Scheme != "https") || src.Host == "" {
		return nil, errors.New("window: bad address")
	}
	seed, _ := strconv.ParseInt(q.Get("k"), 10, 64)
	resp, err := env.Fetcher.Get(ctx, src, fetch.Options{})
	if err != nil {
		return nil, err
	}
	paras := paragraphs(string(resp.Body))
	win, from := pickWindow(paras, seed)
	if len(win) == 0 {
		return nil, errors.New("window: no readable passage in " + src.Host)
	}
	title := strings.TrimSpace(q.Get("t"))
	if title == "" {
		title = "A passage"
	}
	d := &doc.Document{URL: target, Title: title, Lang: "en", Origin: "archive",
		Byline: fmt.Sprintf("a passage, %d%% into the text", from*100/max(len(paras), 1))}
	d.Links = []doc.Link{{Href: src.String(), Text: "The whole text"}}
	for _, p := range win {
		d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: p}}})
	}
	d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: "The whole text", Link: 1}}})
	return d, nil
}
