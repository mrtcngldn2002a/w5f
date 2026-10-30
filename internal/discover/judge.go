package discover

import (
	"regexp"
	"strings"
	"unicode"

	"w5f/internal/doc"
)

// The reading-matter judge says whether a drawn page is a whole piece to read
// (a story, an essay, a chapter, an entry, a poem) and not a list, a news
// item, a link roundup, an event report, a short review, an index or a
// gallery (the owner's bar, 2026-10-30: "bütün parça + raf çıtası"). Words
// alone do not tell: an index of a library or a gallery of vase paintings
// has eighty words easily. So the judge also weighs how much of the page is
// links, lists and headings, whether its prose comes in paragraphs, how it
// starts and ends, and what its title says it is.

// Verdict is the judge's say on one page: its measures, and the reasons it
// is not reading matter (none: it is).
type Verdict struct {
	Words     int      `json:"words"`      // words of prose, link text left out (lists and tables count)
	Bar       int      `json:"bar"`        // the least the shelf asks for
	LinkShare float64  `json:"link_share"` // link text in all the page's words
	ListShare float64  `json:"list_share"` // lists, tables and headings in all the page's words
	LongShare float64  `json:"long_share"` // prose in paragraphs of longParaWords words or more
	Paras     int      `json:"paras"`      // paragraphs of prose
	Verse     bool     `json:"verse"`      // set in short lines (a poem)
	Clean     float64  `json:"clean"`      // share of English words that are words (0: too few to say)
	First     string   `json:"first"`      // the first paragraph of prose
	Reasons   []string `json:"reasons,omitempty"`
}

// OK tells reading matter.
func (v Verdict) OK() bool { return len(v.Reasons) == 0 }

// The reasons a page is turned away, as the source report counts them.
const (
	whyShort     = "short"           // less than the shelf's bar
	whyLinks     = "links"           // mostly links: a directory, a menu
	whyLists     = "lists"           // mostly lists and headings: an index, a contents page
	whyFragments = "fragments"       // no paragraphs of prose: captions, a gallery
	whyTitle     = "title"           // the title says list, review, news, event, index, gallery
	whyMidStart  = "starts mid-text" // cut out of the middle of something
	whyMidEnd    = "ends mid-text"   // cut off before its end
	whyGarbled   = "garbled"         // bad OCR
	whyError     = "error page"
)

// shelfBars are the fewest words a shelf's page has (the owner, 2026-10-30:
// essays and fiction about 600, the small web and text files about 200, no
// bar for poems). The other shelves are set between them, by what a whole
// piece of theirs is: a folktale or an encyclopedia entry is shorter than an
// essay. The source audit measures these before they decide anything.
var shelfBars = map[string]int{
	"literature":   600,
	"fiction":      600,
	"weird":        600,
	"knowledge":    600,
	"print":        600,
	"esoteric":     400,
	"nature":       400,
	"sufism":       300,
	"folklore":     300,
	"encyclopedic": 300,
	"textfiles":    200,
	"smallweb":     200,
	"underground":  200,
}

// defaultBar is the bar of a shelf of the owner's own.
const defaultBar = 300

// informal shelves are written by anyone, as they like: their pieces may
// stop without a full stop.
var informal = map[string]bool{"textfiles": true, "smallweb": true, "underground": true}

// ShelfBar is the fewest words a page of the shelf has.
func ShelfBar(family string) int {
	if b, ok := shelfBars[family]; ok {
		return b
	}
	return defaultBar
}

const (
	longParaWords = 30   // a paragraph of prose, not a caption or a line of a menu
	verseLineMax  = 10.0 // the most words a line of verse has, on average
	verseLinesMin = 4    // a poem has at least this many lines
)

// reNotWhole: titles of what is not a whole piece. Listicles ("7 Novels
// About Grief"), reviews, news and roundups, event reports, indexes and
// galleries, and an encyclopedia's letter pages ("Encyclopedia P").
var reNotWhole = regexp.MustCompile(`(?i)^(\d{1,3}|ten|five|seven|eight|nine|twelve|fifteen|twenty)\s+(\S+\s+){0,4}?(books|novels|stories|poems|poets|essays|writers|authors|memoirs|collections|works|titles|reads|things|ways|reasons|films|movies|graphic novels|picks)\b` +
	`|\b(books|novels|stories|reads) (about|for|to read|that|you should|we can't wait)\b` +
	`|^(book )?reviews?\b|\breview:|\bin review\b|\ba review of\b|\breviewed\b` +
	`|\b(event|conference|symposium|seminar) report\b|\breport on the\b.*\b(event|conference|symposium|seminar|meeting)\b` +
	`|^news\b|\bnews (roundup|digest|briefs?)\b|\b(roundup|round-up|this week in|week in review|what we're reading|newsletter)\b` +
	`|\b(gallery|galleries|index of|table of contents|site ?map|bibliography)\b` +
	`|^(encyclopedia|dictionary|glossary|index)\s+[a-z](-[a-z])?$` +
	`|^contents$|^index$|^home$|^welcome\b`)

// reLibraryName: a library's own front page ("Theoi Classical Texts
// Library"). Only the title's first part says so: after it comes the site's
// name, and the books of a library carry it ("HOMER, ILIAD BOOK 5 - Theoi
// Classical Texts Library").
var reLibraryName = regexp.MustCompile(`(?i)\b(texts?|digital|e-?texts?|virtual) library$`)

var (
	reBlankLine = regexp.MustCompile(`\n\s*\n`)
	// A dash may lean on the word before it ("MEGARA & DEAD ADONIS- Theoi
	// Classical Texts Library").
	reTitleSep = regexp.MustCompile(`\s*(?:[-|–—]|--)\s+`)
	reLetter   = regexp.MustCompile(`(?i)^[a-z](-[a-z])?$`)
	// reOpenMark: quotes, brackets, placeholders and page or line numbers a
	// paragraph opens with ("[image] Adam's Alchemy Weblog", a leaf's "723
	// sections made with a sharp razor").
	reOpenMark = regexp.MustCompile(`^(?:[\s"“‘'(*_—–]|\[[^\]]{0,20}\]|\d+\s)+`)
	// reCredit: a line of who wrote, edited or translated it, or of what
	// kind of tale follows, which a page opens with in small letters
	// ("folktales of type 1176 translated and edited by", "by Elaine
	// Pagels", "and other folktales of Aarne-Thompson-Uther type 1645").
	reCredit = regexp.MustCompile(`(?i)\b(edited|translated|selected|compiled|abstracted|written|retold|adapted)\b.*\bby\b|^by\s|\b(aarne|thompson|uther|christiansen)\b`)
)

var errorWords = []string{"404", "not found", "access denied", "forbidden", "enable javascript", "just a moment", "page not found", "error"}

// Judge weighs a page drawn from the shelf family.
func Judge(d *doc.Document, family string) Verdict {
	if family == "plates" {
		return judgePlate(d)
	}
	m := measure(d.Blocks)
	v := Verdict{Words: m.prose + m.listWords, Bar: ShelfBar(family), Paras: len(m.paras), First: m.first}
	all := m.prose + m.listWords + m.headWords + m.linkWords
	if all > 0 {
		v.LinkShare = float64(m.linkWords) / float64(all)
		v.ListShare = float64(m.listWords+m.headWords) / float64(all)
	}
	long := 0
	for _, p := range m.paras {
		if p >= longParaWords {
			long += p
		}
	}
	if m.prose > 0 {
		v.LongShare = float64(long) / float64(m.prose)
	}
	v.Verse = m.lines >= verseLinesMin && m.lineWords*2 >= m.prose && float64(m.lineWords)/float64(m.lines) <= verseLineMax ||
		poemAddress(d.URL)
	if score, n := cleanliness(m.text.String()); n >= 150 {
		v.Clean = score
	}

	title := strings.Join(strings.Fields(d.Title), " ")
	low := strings.ToLower(title + " " + m.first)
	why := func(r string) { v.Reasons = append(v.Reasons, r) }
	if v.Words < 300 && containsAny(low, errorWords) {
		why(whyError)
	}
	switch {
	case v.Verse:
		if m.lines < verseLinesMin && v.Words < 40 {
			why(whyShort)
		}
	case v.Words < v.Bar:
		why(whyShort)
	}
	if v.LinkShare > 0.5 {
		why(whyLinks)
	}
	if v.ListShare > 0.6 {
		why(whyLists)
	}
	if !v.Verse && v.Words >= 50 && v.LongShare < 0.2 {
		why(whyFragments)
	}
	// A title carries its site's name ("Kingdoms | Theoi Greek Mythology"):
	// each part is read on its own. A poem may be called anything.
	if !v.Verse && notWholeTitle(title) {
		why(whyTitle)
	}
	// Text files and the small web start and stop where their writers do,
	// on a drawing or a line of code as often as on a sentence; they were
	// never printed, so their words are not judged as print either.
	if informal[family] {
		return v
	}
	// Someone who writes all in small letters is not cut off.
	smallHand := m.starts >= 3 && m.lowerStarts*2 > m.starts
	if !v.Verse && !smallHand && m.first != "" && !opensWell(m.first, title) {
		why(whyMidStart)
	}
	// A piece cut out of something is short; a long page that stops on no
	// sentence stops on its footer.
	if !v.Verse && v.Words < 3000 && m.last != "" && cutOff(m.last) {
		why(whyMidEnd)
	}
	// Below half it is another language, not bad print; translations full
	// of names and numbered lines (the Masnavi) score about 0.77, and a
	// short poem full of another language's words is no bad print.
	if v.Words >= 400 && v.Clean >= 0.5 && v.Clean < 0.7 {
		why(whyGarbled)
	}
	return v
}

// whyNoPicture: a plate page whose picture is missing.
const whyNoPicture = "no picture"

// plateWords is the least a plate is labelled with: its title, caption and
// text together (a title alone, "Untitled", is not a label).
const plateWords = 4

// judgePlate: a plate is its picture and its label; there is no bar of
// prose.
func judgePlate(d *doc.Document) Verdict {
	m := measure(d.Blocks)
	v := Verdict{Words: m.prose + m.listWords, Bar: plateWords, Paras: len(m.paras), First: m.first}
	label := len(strings.Fields(d.Title))
	pictures := 0
	for _, b := range d.Blocks {
		if im, ok := b.(doc.Image); ok && im.Src != "" {
			pictures++
			label += len(strings.Fields(im.Caption))
		}
	}
	if pictures == 0 {
		v.Reasons = append(v.Reasons, whyNoPicture)
	}
	if label+v.Words < plateWords {
		v.Reasons = append(v.Reasons, whyShort)
	}
	return v
}

// notWholeTitle: a part of the title says the page is not a whole piece. A
// lone letter is an encyclopedia's letter page ("C - Greek Gods, Spirits &
// Monsters") only at the front, and not among initials ("P | A | Jaffray").
func notWholeTitle(title string) bool {
	parts := reTitleSep.Split(title, -1)
	for i, part := range parts {
		if reLetter.MatchString(part) {
			if i == 0 && (len(parts) < 2 || !reLetter.MatchString(parts[1])) {
				return true
			}
			continue
		}
		if reNotWhole.MatchString(part) || i == 0 && reLibraryName.MatchString(part) {
			return true
		}
	}
	return false
}

// poemAddress: the poetry sites keep their poems under /poem(s)/.
func poemAddress(u string) bool {
	return strings.Contains(u, "poets.org/poem/") || strings.Contains(u, "poetryfoundation.org/poems/")
}

func containsAny(s string, words []string) bool {
	for _, w := range words {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}

// opensWell: a text that opens on a small letter or on punctuation was cut
// out of the middle of something, unless it opens on its own headword
// (Britannica: "epigraphy, the study of written matter").
func opensWell(p, title string) bool {
	rest := reOpenMark.ReplaceAllString(p, "")
	if rest == "" {
		return true
	}
	r := []rune(rest)[0]
	if !unicode.IsLower(r) && !strings.ContainsRune(",;.)-", r) {
		return true
	}
	head, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(title)), " ")
	return head != "" && strings.HasPrefix(strings.ToLower(rest), head)
}

// cutOff: the last long paragraph stops on a small letter or a comma, not
// on a sentence (a reference, a date or a signature ends on something else).
func cutOff(p string) bool {
	p = strings.TrimRight(p, " \t*")
	if p == "" || endsSentence(p) {
		return false
	}
	r := []rune(p)
	last := r[len(r)-1]
	return unicode.IsLower(last) || last == ',' || last == '-'
}

// measures are a page's words by kind.
type measures struct {
	prose, listWords, headWords, linkWords int
	paras                                  []int // words of each paragraph of prose
	lines, lineWords                       int   // lines of paragraphs set in short lines, and their words
	starts, lowerStarts                    int   // paragraphs of prose, and those that open on a small letter
	first, last                            string
	text                                   strings.Builder // the prose, for the OCR measure
}

func measure(bs []doc.Block) *measures {
	m := &measures{}
	m.walk(bs, false)
	return m
}

func (m *measures) walk(bs []doc.Block, inList bool) {
	for _, b := range bs {
		switch b := b.(type) {
		case doc.Paragraph:
			if inList {
				m.listWords += m.inline(b.Text)
				continue
			}
			m.paragraph(b.Text)
		case doc.Pre:
			// A text file is one block: its paragraphs are its blank lines apart.
			// It ends on a signature or a drawing as often as on a sentence,
			// so its end is not judged.
			for _, chunk := range reBlankLine.Split(b.Text, -1) {
				if s := strings.Join(strings.Fields(chunk), " "); s != "" {
					m.prosePara(s, false)
				}
			}
		case doc.Heading:
			m.headWords += m.inline(b.Text)
		case doc.List:
			for _, it := range b.Items {
				m.walk(it, true)
			}
		case doc.Table:
			for _, r := range b.Rows {
				for _, c := range r {
					m.listWords += m.inline(c)
				}
			}
		case doc.Quote:
			m.walk(b.Blocks, inList)
		case doc.Collapsible:
			m.walk(b.Blocks, inList)
		case doc.Columns:
			for _, c := range b.Cols {
				m.walk(c, inList)
			}
		}
	}
}

// inline counts the words of in that are not links (and adds the link words
// to their own count).
func (m *measures) inline(in doc.Inline) int {
	n := 0
	for _, s := range in {
		w := len(strings.Fields(s.Text))
		if s.Link != 0 {
			m.linkWords += w
			continue
		}
		n += w
	}
	return n
}

func (m *measures) paragraph(in doc.Inline) {
	var s strings.Builder
	var lines []int
	cur := 0
	for _, sp := range in {
		if sp.Break {
			lines = append(lines, cur)
			cur = 0
			s.WriteString("\n")
			continue
		}
		w := len(strings.Fields(sp.Text))
		if sp.Link != 0 {
			m.linkWords += w
			continue
		}
		cur += w
		s.WriteString(sp.Text)
	}
	lines = append(lines, cur)
	// Set in lines: verse, or an address block; not pictures with their
	// captions under them (the Alchemy Web Site's art pages).
	if len(lines) >= 3 && !strings.Contains(s.String(), "[image]") {
		for _, l := range lines {
			if l > 0 {
				m.lines++
				m.lineWords += l
			}
		}
	}
	if t := strings.Join(strings.Fields(s.String()), " "); t != "" {
		m.prosePara(t, true)
	}
}

func (m *measures) prosePara(t string, ending bool) {
	w := len(strings.Fields(t))
	if w >= 8 {
		m.starts++
		if rest := reOpenMark.ReplaceAllString(t, ""); rest != "" && unicode.IsLower([]rune(rest)[0]) {
			m.lowerStarts++
		}
	}
	m.prose += w
	m.paras = append(m.paras, w)
	m.text.WriteString(t)
	m.text.WriteString("\n")
	if m.first == "" && w >= 8 && !(w < 30 && reCredit.MatchString(t)) {
		m.first = t
	}
	if w >= longParaWords && ending {
		m.last = t
	}
}

// SourceOf is the source a draw came from, as the source report counts it:
// "literature/The Believer", "smallweb/Wiby"; text files and subreddits are
// one source each, whatever their folder.
func SourceOf(d Draw) string {
	src, _, _ := strings.Cut(d.Why, " · ")
	switch {
	case strings.HasPrefix(src, "textfiles/"):
		return "textfiles"
	case strings.HasPrefix(src, "fiction/r/"):
		return "fiction/reddit"
	case strings.HasPrefix(src, "weird/archived worlds"):
		return "weird/archived worlds"
	}
	return src
}
