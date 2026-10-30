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
	"regexp"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/net/html"

	"w5f/internal/doc"
	"w5f/internal/fetch"
	"w5f/internal/htmlconv"
)

// Project Gutenberg on the print shelf (the owner, 2026-10-30): short whole
// works only, no novels (their place is the Stacks). A book that is one
// story or one essay opens whole; an anthology or an old magazine's issue
// opens at one of its stories or articles, cut out of the book's HTML from
// its heading to the next heading of its rank
// ("w5f:discover/gutenberg?n=<book>&at=<anchor>&t=<title>"). The list of
// books is embedded (scripts/pg_short.py makes it from the catalogue).

//go:embed pg_short.tsv.gz
var pgShortGz []byte

var gutenbergSite = "https://www.gutenberg.org"

type pgBook struct {
	n, words      int
	kind          byte // 's' story, 'e' essay, 'i' magazine issue, 'a' anthology
	title, author string
}

var (
	pgOnce  sync.Once
	pgBooks = map[byte][]pgBook{}
)

func loadPG() {
	r, err := gzip.NewReader(bytes.NewReader(pgShortGz))
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
		f := strings.SplitN(line, "\t", 5)
		if len(f) != 5 || len(f[1]) != 1 {
			continue
		}
		n, e1 := strconv.Atoi(f[0])
		w, e2 := strconv.Atoi(f[2])
		if e1 != nil || e2 != nil {
			continue
		}
		pgBooks[f[1][0]] = append(pgBooks[f[1][0]], pgBook{n, w, f[1][0], f[3], f[4]})
	}
}

// pgHTML is the address of a book's HTML.
func pgHTML(n int) string {
	return fmt.Sprintf("%s/cache/epub/%d/pg%d-images.html", gutenbergSite, n, n)
}

// label is the book as Deep random's line names it: its title, and its
// writer when there is one ("Various" for a magazine is not).
func (b pgBook) label() string {
	t := b.title
	if r := []rune(t); len(r) > 110 {
		t = strings.TrimSpace(string(r[:110])) + "…"
	}
	if a := pgAuthor(b.author); a != "" && a != "Various" && a != "Anonymous" {
		t += " — " + a
	}
	return t
}

// pgAuthor is the first writer of the catalogue's list, first name first
// ("Wells, H. G. (Herbert George)" is "H. G. Wells").
func pgAuthor(s string) string {
	s, _, _ = strings.Cut(s, ";")
	if i := strings.Index(s, " ("); i > 0 {
		s = s[:i]
	}
	if last, first, ok := strings.Cut(s, ", "); ok {
		s = first + " " + last
	}
	return strings.TrimSpace(s)
}

// gutenbergShortPick draws a book that is a single story or essay; it opens
// whole.
func gutenbergShortPick(ctx context.Context, env Env) (string, string, error) {
	pgOnce.Do(loadPG)
	kind := byte('s')
	if rand.IntN(4) == 0 {
		kind = 'e'
	}
	if len(pgBooks[kind]) == 0 {
		return "", "", errors.New("no list of short works")
	}
	b := pick(pgBooks[kind])
	return pgHTML(b.n), b.label(), nil
}

// gutenbergPiecePick draws a story or an article of an anthology or an old
// magazine's issue: a book, then one of its pieces that is whole and of a
// length to read (up to three books).
func gutenbergPiecePick(ctx context.Context, env Env) (string, string, error) {
	pgOnce.Do(loadPG)
	var errs []string
	for range 3 {
		kind := byte('a')
		if rand.IntN(2) == 0 {
			kind = 'i'
		}
		if len(pgBooks[kind]) == 0 {
			return "", "", errors.New("no list of anthologies")
		}
		b := pick(pgBooks[kind])
		body, err := pgBody(ctx, env.Fetcher, b.n)
		if err != nil {
			if ctx.Err() != nil {
				return "", "", ctx.Err()
			}
			errs = append(errs, fmt.Sprintf("%d: %v", b.n, err))
			continue
		}
		pieces := pgPieces(body)
		if len(pieces) == 0 {
			errs = append(errs, fmt.Sprintf("%d: no whole piece", b.n))
			continue
		}
		p := pick(pieces)
		label := p.title + " (" + b.label() + ")"
		return "w5f:discover/gutenberg?" + url.Values{"n": {strconv.Itoa(b.n)}, "at": {p.id}, "t": {p.title}}.Encode(), label, nil
	}
	return "", "", errors.New(strings.Join(errs, "; "))
}

func pgBody(ctx context.Context, f *fetch.Fetcher, n int) ([]byte, error) {
	u, _ := url.Parse(pgHTML(n))
	resp, err := f.Get(ctx, u, fetch.Options{})
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

// pgPiece is a story or an article of a book: the anchor its contents link
// to, its title there, and where it is in the HTML.
type pgPiece struct {
	id, title string
	from, to  int
}

const (
	pieceMin = 700   // words a piece has at least (the shelf's bar, and its title and byline)
	pieceMax = 25000 // and at most: a short novel is not a piece
)

var (
	// reNotPiece: what a book's contents lists that is not a story or an
	// article to read.
	reNotPiece = regexp.MustCompile(`(?i)\b(contents|illustrations?|preface|introduction|introductory|foreword|prefatory|index|notes|footnotes|` +
		`transcriber|advertis\w*|appendix|glossary|errata|bibliography|readers'? corner|correspondence|letters to|the eyrie|` +
		`book reviews?|reviews|announcements?|list of|table of|chronolog\w*|acknowledg\w*|dedication|frontispiece|title page|` +
		`copyright|cover design|editor'?s? (table|drawer|easy chair|note)|literary notices|monthly record|science and arts|` +
		`notices to correspondents|answers to correspondents|poetry|verses|poems|sonnets)\b`)
	// reChapter: a chapter of a story, not a story.
	reChapter = regexp.MustCompile(`(?i)^(chapter|book|part|section|canto|letter|scene|act|stave|fytte)\b|^[ivxlcdm]+\.?$|^\d+\.?$`)
	// reSerial: an installment of a serial, which begins or ends in the
	// middle of its story.
	reSerial = regexp.MustCompile(`(?i)\b(part[- ]novel|(beginning|conclusion|end) of|in (two|three|four|five|six) parts|` +
		`part (one|two|three|four|five|i|ii|iii|iv|v|\d) of|to be continued|concluded from|continued from|` +
		`(second|third|fourth|fifth|final|last) (part|instal+ment)|(part|chapter)s? [ivx\d]+ (to|and|-) [ivx\d]+)\b`)
	reHeading = regexp.MustCompile(`^h([1-4])$`)
)

type pgHead struct {
	level, from, to int
	ids             []string
}

// pgPieces finds a book's stories and articles: what its contents link to
// that is a heading (an anchor in a heading or just before one: not a page
// number, a picture or a note), from that heading to the next one of its
// rank or higher, of a length to read and not a chapter or a part of a
// serial.
func pgPieces(body []byte) []pgPiece {
	heads, anchors, links := pgScan(body)
	headOf := func(off int) int { // the heading an anchor at off belongs to
		for i, h := range heads {
			if off >= h.from && off <= h.to {
				return i
			}
			if off < h.from {
				if strings.TrimSpace(stripTags(string(body[off:h.from]))) == "" {
					return i
				}
				return -1
			}
		}
		return -1
	}
	var out []pgPiece
	seen := map[int]bool{}
	for _, l := range links {
		off, ok := anchors[l.id]
		if !ok || off <= l.at {
			continue // a link back (an index), or to nothing
		}
		hi := headOf(off)
		if hi < 0 || seen[hi] {
			continue
		}
		seen[hi] = true
		h := heads[hi]
		if h.level == 1 {
			continue // the book's own title
		}
		title := strings.Join(strings.Fields(l.text), " ")
		if title == "" || reNotPiece.MatchString(title) || reChapter.MatchString(title) {
			continue
		}
		end := len(body)
		for _, n := range heads[hi+1:] {
			if n.level <= h.level {
				end = n.from
				break
			}
		}
		if i := bytes.Index(body[h.from:end], []byte("*** END OF")); i >= 0 {
			end = h.from + i
		}
		text := stripTags(string(body[h.from:end]))
		words := len(strings.Fields(text))
		if words < pieceMin || words > pieceMax || reSerial.MatchString(text) {
			continue
		}
		out = append(out, pgPiece{id: l.id, title: pgTitle(title), from: h.from, to: end})
	}
	return out
}

type pgLink struct {
	id, text string
	at       int
}

// pgScan reads where the HTML's headings are (h1–h4, with the anchors in
// them), where every anchor is (id or name), and its links to its own
// anchors, with their text.
func pgScan(body []byte) (heads []pgHead, anchors map[string]int, links []pgLink) {
	anchors = map[string]int{}
	z := html.NewTokenizer(bytes.NewReader(body))
	off := 0
	open := -1 // the heading being read
	var link *pgLink
	for {
		tt := z.Next()
		raw := z.Raw()
		at := off
		off += len(raw)
		switch tt {
		case html.ErrorToken:
			return
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			tag := string(name)
			var id, href string
			for hasAttr {
				var k, v []byte
				k, v, hasAttr = z.TagAttr()
				switch string(k) {
				case "id", "name":
					if id == "" {
						id = string(v)
					}
				case "href":
					href = string(v)
				}
			}
			if id != "" {
				if _, ok := anchors[id]; !ok {
					anchors[id] = at
				}
			}
			if m := reHeading.FindStringSubmatch(tag); m != nil && tt == html.StartTagToken {
				lv, _ := strconv.Atoi(m[1])
				heads = append(heads, pgHead{level: lv, from: at, to: at})
				open = len(heads) - 1
			}
			if open >= 0 && id != "" {
				heads[open].ids = append(heads[open].ids, id)
			}
			if tag == "a" && strings.HasPrefix(href, "#") && len(href) > 1 && tt == html.StartTagToken {
				link = &pgLink{id: href[1:], at: at}
			}
		case html.TextToken:
			if link != nil {
				link.text += html.UnescapeString(string(raw))
			}
		case html.EndTagToken:
			name, _ := z.TagName()
			tag := string(name)
			if open >= 0 && reHeading.MatchString(tag) {
				heads[open].to = off
				open = -1
			}
			if tag == "a" && link != nil {
				links = append(links, *link)
				link = nil
			}
		}
	}
}

var reTags = regexp.MustCompile(`(?s)<[^>]*>`)

func stripTags(s string) string { return html.UnescapeString(reTags.ReplaceAllString(s, " ")) }

// pgTitle: contents set in capitals ("INTO THE OCEAN'S DEPTHS") are shown
// in title case.
func pgTitle(t string) string {
	if strings.ToUpper(t) != t {
		return t
	}
	words := strings.Fields(strings.ToLower(t))
	for i, w := range words {
		if i > 0 && titleWords[w] {
			continue
		}
		r := []rune(w)
		for j, c := range r {
			if c >= 'a' && c <= 'z' {
				r[j] = c - 'a' + 'A'
				break
			}
		}
		words[i] = string(r)
	}
	return strings.Join(words, " ")
}

// reDropCap: a story's first letter set as a picture ("<img alt="I">", or
// its title: "<img alt="" title="O">"), before its first paragraph: it goes
// back into the paragraph (with the space after it when it is a word:
// "<p> read the telegram").
var reDropCap = regexp.MustCompile(`(?s)<img[^>]*\b(?:alt|title)="([A-Z])"[^>]*>\s*(?:</[a-z]+>\s*)*<p([^>]*)>`)

// gutenbergRoute opens a piece of a book.
func gutenbergRoute(ctx context.Context, env Env, target string) (*doc.Document, error) {
	q, err := url.ParseQuery(strings.TrimPrefix(target, "w5f:discover/gutenberg?"))
	if err != nil {
		return nil, errors.New("bad Gutenberg address")
	}
	n, err := strconv.Atoi(q.Get("n"))
	if err != nil {
		return nil, errors.New("bad Gutenberg address")
	}
	body, err := pgBody(ctx, env.Fetcher, n)
	if err != nil {
		return nil, err
	}
	var piece *pgPiece
	for _, p := range pgPieces(body) {
		if p.id == q.Get("at") {
			piece = &p
			break
		}
	}
	if piece == nil {
		return nil, errors.New("the book no longer has this piece")
	}
	frag := reDropCap.ReplaceAll(body[piece.from:piece.to], []byte("<p$2>$1"))
	page := "<html><head><title>" + html.EscapeString(q.Get("t")) + "</title></head><body>" + string(frag) + "</body></html>"
	d, err := htmlconv.Generic(strings.NewReader(page), pgHTML(n)+"#"+piece.id)
	if err != nil {
		return nil, err
	}
	d.URL = target
	d.Title = q.Get("t")
	d.Lang = "en"
	d.Origin = "live"
	d.Links = append(d.Links, doc.Link{Href: pgHTML(n), Text: "The whole book on Project Gutenberg"})
	d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: "The whole book on Project Gutenberg", Link: len(d.Links)}}})
	return d, nil
}
