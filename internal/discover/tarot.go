package discover

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"w5f/internal/fetch"
	"w5f/internal/store"
)

// The tarot deck: A. E. Waite's The Pictorial Key to the Tarot (1910), at
// the Internet Sacred Text Archive. Each card's text is read from there
// the first time it comes up, cleaned, and kept in the database, so a card
// reads the same every time and without the network (asked for by the
// owner, 2026-10-01).

// Card is one of the 78 cards.
type Card struct {
	Key     string // the page's name: ar16, wa07
	Numeral string // XVI, 0, VII, Q
	Name    string // The Tower, Seven of Wands
	Suit    string // "" for the Greater Arcana
}

// CardText is what Waite says of a card.
type CardText struct {
	Description string `json:"description"`
	Upright     string `json:"upright"`
	Reversed    string `json:"reversed"`
	Source      string `json:"source"`
}

var majorNames = []string{"The Fool", "The Magician", "The High Priestess", "The Empress", "The Emperor", "The Hierophant",
	"The Lovers", "The Chariot", "Strength", "The Hermit", "Wheel of Fortune", "Justice", "The Hanged Man", "Death",
	"Temperance", "The Devil", "The Tower", "The Star", "The Moon", "The Sun", "The Last Judgment", "The World"}

var suits = []struct{ code, name string }{{"wa", "Wands"}, {"cu", "Cups"}, {"sw", "Swords"}, {"pe", "Pentacles"}}

var ranks = []struct{ code, name, numeral string }{
	{"ac", "Ace", "I"}, {"02", "Two", "II"}, {"03", "Three", "III"}, {"04", "Four", "IV"}, {"05", "Five", "V"},
	{"06", "Six", "VI"}, {"07", "Seven", "VII"}, {"08", "Eight", "VIII"}, {"09", "Nine", "IX"}, {"10", "Ten", "X"},
	{"pa", "Page", "P"}, {"kn", "Knight", "Kn"}, {"qu", "Queen", "Q"}, {"ki", "King", "K"},
}

// Deck is the 78 cards, the Greater Arcana first.
func Deck() []Card {
	var d []Card
	for i, n := range majorNames {
		num := "0"
		if i > 0 {
			num = romanNumeral(i)
		}
		d = append(d, Card{Key: fmt.Sprintf("ar%02d", i), Numeral: num, Name: n})
	}
	for _, s := range suits {
		for _, r := range ranks {
			d = append(d, Card{Key: s.code + r.code, Numeral: r.numeral, Name: r.name + " of " + s.name, Suit: s.name})
		}
	}
	return d
}

// CardByKey finds a card.
func CardByKey(key string) (Card, bool) {
	for _, c := range Deck() {
		if c.Key == key {
			return c, true
		}
	}
	return Card{}, false
}

func romanNumeral(n int) string {
	vals := []struct {
		v int
		s string
	}{{10, "X"}, {9, "IX"}, {5, "V"}, {4, "IV"}, {1, "I"}}
	var b strings.Builder
	for _, x := range vals {
		for n >= x.v {
			b.WriteString(x.s)
			n -= x.v
		}
	}
	return b.String()
}

func cardPage(key string) string {
	return strings.TrimSuffix(tarotIndex, "index.htm") + "pkt" + key + ".htm"
}

// majorMeanings is Part III § 3, the Greater Arcana's meanings.
func majorMeanings() string { return strings.TrimSuffix(tarotIndex, "index.htm") + "pkt0303.htm" }

var (
	reMajorMeaning = regexp.MustCompile(`^(ZERO|\d{1,2})\.\s+[A-Z][A-Z ,'.]*?\.\s*-{1,2}\s*(.*)$`)
	reSpaces       = regexp.MustCompile(`\s+`)
)

func clean(s string) string { return strings.TrimSpace(reSpaces.ReplaceAllString(s, " ")) }

// reDivinatory finds "Divinatory Meanings:" (spelt "Divanatory" once).
var reDivinatory = regexp.MustCompile(`Div[ai]n[ai]tory Meanings\s*:`)

// waiteErrata mends the scanning slips of the sacred-texts edition (Waite
// wrote "symbolism", "implied", "ruin", "the House of Life").
var waiteErrata = map[string][][2]string{
	"ar02": {{"highest name in bolism", "highest name in symbolism"}},
	"ar10": {{"is imphed through", "is implied through"}},
	"ar16": {{"depicts min in", "depicts ruin in"}, {"House of We,", "House of Life,"}},
}

func (t CardText) mended(key string) CardText {
	for _, e := range waiteErrata[key] {
		t.Description = strings.ReplaceAll(t.Description, e[0], e[1])
		t.Upright = strings.ReplaceAll(t.Upright, e[0], e[1])
		t.Reversed = strings.ReplaceAll(t.Reversed, e[0], e[1])
	}
	return t
}

// splitReversed takes "…meanings. Reversed: …" apart.
func splitReversed(s string) (string, string) {
	if i := strings.Index(s, "Reversed:"); i >= 0 {
		return clean(s[:i]), clean(s[i+len("Reversed:"):])
	}
	if i := strings.Index(s, "Reversed :"); i >= 0 {
		return clean(s[:i]), clean(s[i+len("Reversed :"):])
	}
	return clean(s), ""
}

// CardTextOf reads a card's text: kept, or fetched and kept.
func CardTextOf(ctx context.Context, f *fetch.Fetcher, db *store.DB, c Card) (CardText, error) {
	var t CardText
	key := "tarot2:" + c.Key // the number: how the pages are read; read anew, a new key
	if db != nil {
		if json.Unmarshal([]byte(db.Get(key)), &t) == nil && t.Description != "" {
			return t.mended(c.Key), nil
		}
	}
	t, err := fetchCardText(ctx, f, c)
	if err != nil {
		return t, err
	}
	if db != nil {
		b, _ := json.Marshal(t)
		_ = db.Set(key, string(b))
	}
	return t.mended(c.Key), nil
}

func fetchCardText(ctx context.Context, f *fetch.Fetcher, c Card) (CardText, error) {
	t := CardText{Source: cardPage(c.Key)}
	gq, _, err := get(ctx, f, t.Source)
	if err != nil {
		return t, err
	}
	// The card's paragraphs come after its picture.
	var paras []string
	after := false
	gq.Find("p").EachWithBreak(func(_ int, p *goquery.Selection) bool {
		s := clean(p.Text())
		switch {
		case strings.Contains(s, "Click to enlarge"):
			after = true
		case strings.HasPrefix(s, "Next:") || strings.HasPrefix(s, "Sacred Texts"):
			// The page's own links: the card's text has ended.
			return !after
		case after && s != "":
			paras = append(paras, s)
		}
		return true
	})
	if len(paras) == 0 {
		return t, errors.New("Waite's page for " + c.Name + " has no text W5F can read")
	}
	text := strings.Join(paras, "\n\n")
	if c.Suit != "" {
		// The Lesser Arcana: picture, then "Divinatory Meanings: …", then "Reversed: …".
		desc, rest := text, ""
		if m := reDivinatory.FindStringIndex(text); m != nil {
			desc, rest = text[:m[0]], text[m[1]:]
		}
		t.Description = clean(desc)
		t.Upright, t.Reversed = splitReversed(rest)
		return t, nil
	}
	t.Description = strings.TrimSpace(text)
	// The Greater Arcana's meanings are listed together in Part III § 3.
	mg, _, err := get(ctx, f, majorMeanings())
	if err != nil {
		return t, nil // the description alone is still worth showing
	}
	want := strconv.Itoa(majorIndex(c.Key))
	if want == "0" {
		want = "ZERO"
	}
	mg.Find("p").EachWithBreak(func(_ int, p *goquery.Selection) bool {
		m := reMajorMeaning.FindStringSubmatch(clean(p.Text()))
		if m != nil && m[1] == want {
			t.Upright, t.Reversed = splitReversed(m[2])
			return false
		}
		return true
	})
	return t, nil
}

func majorIndex(key string) int {
	n, _ := strconv.Atoi(strings.TrimPrefix(key, "ar"))
	return n
}

// DrawCard draws a card and whether it is reversed.
func DrawCard() (Card, bool) {
	d := Deck()
	return d[rand.IntN(len(d))], rand.IntN(2) == 0
}

// TarotHref is a drawn card's page.
func TarotHref(key string, reversed bool) string {
	h := "w5f:discover/tarot/" + key
	if reversed {
		h += "?r=1"
	}
	return h
}

// hexagram text: James Legge's I Ching (1882), kept like the cards.

// Hexagram is what Legge gives for one of the 64.
type Hexagram struct {
	N        int       `json:"n"`
	Name     string    `json:"name"`
	Judgment string    `json:"judgment"`
	Lines    [7]string `json:"lines"` // 1–6, and 7 for hexagrams 1 and 2 ("the use of nine/six")
	Note     string    `json:"note"`  // Legge's note on the name
	Source   string    `json:"source"`
}

var (
	reLineText  = regexp.MustCompile(`^([1-7])\.\s+(.*)$`)
	rePageMark  = regexp.MustCompile(`^p\. \d+$`)
	reFootIndex = regexp.MustCompile(`^\d+:[IVXLC]+\s*`)
)

// HexagramOf reads a hexagram's text: kept, or fetched and kept.
func HexagramOf(ctx context.Context, f *fetch.Fetcher, db *store.DB, n int) (Hexagram, error) {
	var h Hexagram
	key := "iching1:" + strconv.Itoa(n) // the number: how the pages are read; read anew, a new key
	if db != nil {
		if json.Unmarshal([]byte(db.Get(key)), &h) == nil && h.Judgment != "" {
			return h, nil
		}
	}
	h, err := fetchHexagram(ctx, f, n)
	if err != nil {
		return h, err
	}
	if db != nil {
		b, _ := json.Marshal(h)
		_ = db.Set(key, string(b))
	}
	return h, nil
}

func fetchHexagram(ctx context.Context, f *fetch.Fetcher, n int) (Hexagram, error) {
	h := Hexagram{N: n, Source: fmt.Sprintf("%sic%02d.htm", ichingBase, n)}
	u, _ := url.Parse(h.Source)
	resp, err := f.Get(ctx, u, fetch.Options{})
	if err != nil {
		return h, err
	}
	gq, err := goquery.NewDocumentFromReader(bytes.NewReader(resp.Body))
	if err != nil {
		return h, err
	}
	// The printed book's page numbers, also inside paragraphs.
	gq.Find(`a[name^="page_"]`).Remove()
	title := gq.Find("h3").First()
	if m := reHexTitle.FindStringSubmatch(title.Text()); m != nil {
		h.Name = titleCase(m[1])
	}
	var judgment []string
	last := -1 // the line being read (a paragraph may run over a page)
	inNotes := false
	title.NextAll().Each(func(_ int, s *goquery.Selection) {
		switch goquery.NodeName(s) {
		case "hr":
			return
		case "h3":
			inNotes = strings.Contains(s.Text(), "Footnotes")
			return
		case "p":
		default:
			return
		}
		t := clean(s.Text())
		if t == "" || rePageMark.MatchString(t) || strings.HasPrefix(t, "Explanation of ") {
			return
		}
		cont := strings.HasPrefix(t, "[paragraph continues]")
		t = strings.TrimSpace(strings.TrimPrefix(t, "[paragraph continues]"))
		if inNotes {
			if h.Note == "" {
				h.Note = clean(reFootIndex.ReplaceAllString(t, ""))
			}
			return
		}
		if m := reLineText.FindStringSubmatch(t); m != nil && !cont {
			i, _ := strconv.Atoi(m[1])
			last = i - 1
			h.Lines[last] = m[2]
			return
		}
		if last >= 0 {
			h.Lines[last] = clean(h.Lines[last] + " " + t)
			return
		}
		judgment = append(judgment, t)
	})
	h.Judgment = clean(strings.Join(judgment, " "))
	if h.Judgment == "" {
		return h, fmt.Errorf("Legge's page for hexagram %d has no text W5F can read", n)
	}
	return h, nil
}

// IChingHref is a cast hexagram's page: its six lines (6–9) from the bottom.
func IChingHref(lines []int) string {
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(strconv.Itoa(l))
	}
	return "w5f:discover/iching/" + b.String()
}

// parseCast reads "877869" back into lines.
func parseCast(s string) ([]int, bool) {
	if len(s) != 6 {
		return nil, false
	}
	var out []int
	for _, r := range s {
		if r < '6' || r > '9' {
			return nil, false
		}
		out = append(out, int(r-'0'))
	}
	return out, true
}

// castNumbers gives the hexagram cast and the one its moving lines turn
// it into (0 when none move).
func castNumbers(lines []int) (now, then int, moving []int) {
	var a, b [6]bool
	for i, l := range lines {
		a[i] = l == 7 || l == 9
		b[i] = a[i]
		if l == 6 || l == 9 {
			b[i] = !a[i]
			moving = append(moving, i+1)
		}
	}
	now = kingWen(a)
	if len(moving) > 0 {
		then = kingWen(b)
	}
	return now, then, moving
}
