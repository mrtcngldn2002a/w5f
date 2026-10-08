// Package ultan is the librarian of W5F's library: he does not speak, he
// leaves notes in the margin (chosen with the owner, 2026-10-01). Each note
// is made from the owner's own reading — history, the desk, the shelves —
// by rule, in a voice after Gene Wolfe's blind librarian (the words are
// W5F's own, nothing is quoted). When there is nothing to say he offers one
// of his sayings instead.
package ultan

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"w5f/internal/doc"
	"w5f/internal/feeds"
	"w5f/internal/personal"
	"w5f/internal/store"
)

// Signature closes every note.
const Signature = "— U."

// Note is one marginal note. Subject, when set, appears in Text and links
// to Href.
type Note struct {
	Text    string
	Subject string
	Href    string
}

// Blocks sets a note on a page: italic, the subject a link, signed.
func (n Note) Blocks(link func(href, text string) int) []doc.Block {
	in := doc.Inline{}
	text := n.Text
	if n.Subject != "" && n.Href != "" {
		if i := strings.Index(text, n.Subject); i >= 0 {
			in = append(in, doc.Span{Text: text[:i], Style: doc.Italic},
				doc.Span{Text: n.Subject, Style: doc.Italic, Link: link(n.Href, n.Subject)})
			text = text[i+len(n.Subject):]
		}
	}
	in = append(in, doc.Span{Text: text, Style: doc.Italic}, doc.Span{Text: "   " + Signature})
	return []doc.Block{doc.Paragraph{Text: in}}
}

// day is a number that changes once a day: the same note all day.
func day(now time.Time) int { return now.Year()*400 + now.YearDay() }

func pick(vs []string, seed int) string {
	if seed < 0 {
		seed = -seed
	}
	return vs[seed%len(vs)]
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

var numbers = []string{"no", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten", "eleven", "twelve"}

// words writes small numbers out, as a librarian would.
func words(n int) string {
	if n >= 0 && n < len(numbers) {
		return numbers[n]
	}
	return fmt.Sprint(n)
}

func capital(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// --- the desk note ---

var (
	absence = []string{
		"%s days since your lamp was lit here. Nothing has moved but the dust, and the periodicals, which never rest.",
		"You have been away %s days. The shelves kept their silence for you; they are good at it.",
		"%s days, and no step in the aisles but mine. I told the books you would return; they are used to such promises.",
		"Welcome back, after %s days. I kept the door on the latch and the chair where you left it.",
		"%s days away. A library does not miss its readers, it only waits; but I am not a library.",
	}
	stale = []string{
		"A book left open is a door left ajar. %[1]s has not stirred in %[2]s days; I have kept your place at %[3]s.",
		"%[1]s waits at %[3]s, as it has these %[2]s days. The page does not mind; pages are patient.",
		"I dusted around %[1]s again: %[3]s, untouched for %[2]s days. Some books are read in a season, some in a life.",
		"Your marker still stands in %[1]s, at %[3]s. It has stood there %[2]s days, like a sentry no one came to relieve.",
		"%[2]s days ago you set %[1]s down at %[3]s. I have let no one move it; there is no one else to move it.",
	}
	fresh = []string{
		"Word came while you were away: %[1]s has grown by %[2]s %[3]s. The ink is still wet.",
		"%[2]s new %[3]s of %[1]s lie on the desk, unopened. I have not read them; I never do.",
		"%[1]s has sent %[2]s more %[3]s. The author is still at work somewhere, and so, it seems, are you.",
		"A parcel for you: %[2]s %[3]s of %[1]s, still in their string.",
	}
	heavy = []string{
		"The shelves of periodicals grow heavy: %[1]s voices unread, the eldest from %[2]s.",
		"%[1]s periodicals wait unread, the oldest since %[2]s. A periodical is a letter that arrives on time; these are late.",
		"The periodical racks hold %[1]s unread, back to %[2]s. I hear them rustle when the door opens.",
		"%[1]s issues unopened since %[2]s. Not all news keeps; some of it improves.",
	}
	waiting = []string{
		"Your queue holds %[1]s pages. The first, %[2]s, has waited the longest; it has not complained.",
		"%[1]s pages in your queue. %[2]s stands at the head of the line, hat in hand.",
		"Of the %[1]s pages you set aside for later, %[2]s was the first. Later has come for it, I think.",
		"%[2]s heads a queue of %[1]s. A queue is a promise made in a hurry.",
	}
	habit = []string{
		"You have come to the stacks on %s of the last seven days. The lamps have learned your hours.",
		"%s days of the last seven, reading. A library is kept by such habits, not by its walls.",
		"%s days in seven at the desk. I know your step now; it is lighter than you think.",
		"On %s of the last seven days you came. The chair has kept your shape.",
	}
	// kinned is a page read lately and an older one it has rare words in
	// common with: %[1]s the older, %[2]s when it was read, %[3]s the
	// words, %[4]s the newer.
	kinned = []string{
		"%[4]s and %[1]s, read %[2]s, share %[3]s. I have filed them side by side.",
		"Something in %[4]s sent me back to %[1]s, which you read %[2]s: %[3]s, in both.",
		"%[4]s has a cousin on these shelves: %[1]s, read %[2]s. Look for %[3]s.",
	}
	// sayings are for days with nothing to report.
	sayings = []string{
		"What is lost in a library is never lost, only misfiled.",
		"A margin is the only place a reader may answer back.",
		"Every index is an argument about what matters.",
		"The shelves are learned by the hand, not the eye; I have never needed a lamp.",
		"Some books are read; others are kept. Both are a kind of faith.",
		"Old ink smells of iron. New ink smells of nothing yet.",
		"The stacks are quieter at night, though no one is sure for whose sake.",
		"A catalogue is a map drawn by someone who never travels.",
		"The book you are not reading is also waiting; it has the advantage of patience.",
		"Paper forgets more slowly than people, and the lamps forget fastest of all.",
		"There are more rooms in this library than I have walked. I suspect there are more than were built.",
		"A reader who returns to a page is not repeating himself; the page has changed.",
		"Nothing is shelved by accident, though much is shelved by mistake.",
		"Quiet is not the absence of voices here. It is the voices waiting their turn.",
		"A librarian's first duty is to the book no one has asked for.",
		"Fire is the oldest reader; it finishes everything.",
		"Some read to remember and some to forget. The books do not mind which.",
		"The catalogue says where a book is. Only a reader can say where it goes.",
		"Dust is the library's own handwriting.",
		"A shelf in order is a sentence; a shelf in disorder is a riddle.",
		"There is no last page in a library, only the last one you reached.",
		"Some doors here are bricked up. I have read the bricks.",
		"Every book was once a stranger's voice in an empty room.",
		"When I could still see, I read faster. I did not read more.",
		"The rarest book is the one you meant to come back to.",
		"A library is a crowd that has agreed to whisper.",
		"Footnotes are where a writer keeps what could not be thrown away.",
		"Lamps are for readers. The books themselves prefer the dark.",
		"I shelve by touch, and so I know which books are warm.",
		"A book lent is a book half lost; a book read is a book half found.",
	}
)

// fact is one thing worth saying at the desk, in several ways.
type fact struct {
	key    string // its kind, then what it is about: what Ultan remembers saying
	forms  []string
	say    func(form string) Note
	urgent bool // said before anything else
}

func sayingFacts() []fact {
	out := make([]fact, len(sayings))
	for i, s := range sayings {
		out[i] = fact{key: fmt.Sprintf("saying %d", i), forms: []string{s}, say: func(string) Note { return Note{Text: s} }}
	}
	return out
}

// Desk is the note for the reading room: the day's most worth saying. He
// remembers what he said: today's note holds all day while it is true, and
// what he said longest ago, or never, comes next, in words he used least
// lately.
func Desk(db *store.DB, now time.Time) Note {
	seed := day(now)
	var facts []fact
	if db != nil {
		facts = deskFacts(db, now)
	}
	m := recall(db)
	if s, ok := m.today(seed); ok {
		for _, f := range append(facts, sayingFacts()...) {
			if f.key == s.Key && s.Form < len(f.forms) {
				return f.say(f.forms[s.Form])
			}
		}
	}
	var pool []fact
	for _, f := range facts {
		if f.urgent {
			pool = append(pool, f)
		}
	}
	if len(pool) == 0 {
		pool = facts
	}
	if len(pool) == 0 {
		pool = sayingFacts()
	}
	f := m.choose(pool, seed)
	// A fact he gave yesterday too, with nothing else to say: a saying
	// today, so the desk does not nag.
	if y := len(m.list) - 1; !f.urgent && y >= 0 && m.list[y].Day == seed-1 && m.list[y].Key == f.key {
		f = m.choose(sayingFacts(), seed)
	}
	form := m.form(f, seed)
	m.remember(said{Day: seed, Key: f.key, Form: form})
	return f.say(f.forms[form])
}

func days(d time.Duration) int { return int(d.Hours() / 24) }

func deskFacts(db *store.DB, now time.Time) []fact {
	var out []fact
	if hs, _ := db.History(1, 0); len(hs) == 1 {
		if n := days(now.Sub(hs[0].Last)); n >= 3 {
			out = append(out, fact{key: "absence", forms: absence, urgent: true, say: func(form string) Note {
				return Note{Text: capital(fmt.Sprintf(form, words(n)))}
			}})
		}
	}
	// New chapters of what is followed.
	if ss, err := db.Serials(true); err == nil {
		best, bestN := store.Serial{}, 0
		for _, s := range ss {
			if n := s.Chapters - s.Seen; s.Seen > 0 && n > bestN {
				best, bestN = s, n
			}
		}
		if bestN > 0 {
			href := fmt.Sprintf("w5f:serial/%d/continue", best.ID)
			out = append(out, fact{key: "fresh " + href, forms: fresh, say: func(form string) Note {
				t := fmt.Sprintf(form, best.Title, words(bestN), plural(bestN, "chapter", "chapters"))
				return Note{Text: capital(t), Subject: best.Title, Href: href}
			}})
		}
	}
	// The longest-untouched thing left half-read.
	type open struct {
		title, href, where string
		last               time.Time
	}
	var opens []open
	if vs, err := db.Unfinished(8); err == nil {
		for _, v := range vs {
			opens = append(opens, open{v.Title, v.Target, fmt.Sprintf("%d%%", int(v.Pos*100+0.5)), v.Last})
		}
	}
	if bs, err := db.Books("recent", 5); err == nil {
		for _, b := range bs {
			if b.Chapters > 0 && !b.Opened.IsZero() && (b.Chapter < b.Chapters-1 || b.Pos < 0.95) {
				opens = append(opens, open{b.Title, fmt.Sprintf("w5f:book/%d", b.ID), fmt.Sprintf("chapter %d of %d", b.Chapter+1, b.Chapters), b.Opened})
			}
		}
	}
	if ss, err := db.Serials(false); err == nil {
		for _, s := range ss {
			if !s.Opened.IsZero() && s.Chapters > 0 && (s.Chapter < s.Chapters-1 || s.Pos < 0.95) {
				opens = append(opens, open{s.Title, fmt.Sprintf("w5f:serial/%d/continue", s.ID), fmt.Sprintf("chapter %d of %d", s.Chapter+1, s.Chapters), s.Opened})
			}
		}
	}
	var oldest *open
	for i := range opens {
		if oldest == nil || opens[i].last.Before(oldest.last) {
			oldest = &opens[i]
		}
	}
	if oldest != nil {
		if n := days(now.Sub(oldest.last)); n >= 4 {
			o := *oldest
			out = append(out, fact{key: "stale " + o.href, forms: stale, say: func(form string) Note {
				return Note{Text: capital(fmt.Sprintf(form, o.title, words(n), o.where)), Subject: o.title, Href: o.href}
			}})
		}
	}
	// The periodicals: those of the catalog's feeds, as the Periodical
	// Gallery counts them.
	if cs, err := db.Counts(); err == nil {
		var ids []string
		if cat, err := feeds.LoadCatalog(); err == nil {
			ids = cat.IDs()
		}
		unread := 0
		for id, c := range cs {
			if ids == nil || slices.Contains(ids, id) {
				unread += c[0]
			}
		}
		if unread >= 15 {
			when := "some time ago"
			if its, err := db.Items(store.Query{Unread: true, Feeds: ids, Limit: 2000}); err == nil && len(its) > 0 {
				if p := its[len(its)-1].Published; !p.IsZero() {
					when = p.Format("2 January")
				}
			}
			out = append(out, fact{key: "heavy", forms: heavy, say: func(form string) Note {
				return Note{Text: capital(fmt.Sprintf(form, words(unread), when))}
			}})
		}
	}
	// The queue.
	if q, err := personal.LoadQueue(); err == nil {
		var left []personal.Entry
		for _, e := range q.Entries() {
			if !e.Done {
				left = append(left, e)
			}
		}
		if len(left) >= 3 {
			first := left[0]
			out = append(out, fact{key: "waiting " + first.URL, forms: waiting, say: func(form string) Note {
				return Note{Text: capital(fmt.Sprintf(form, words(len(left)), first.Title)), Subject: first.Title, Href: first.URL}
			}})
		}
	}
	// A reading habit.
	if vs, err := db.VisitLog(now.AddDate(0, 0, -7)); err == nil {
		seen := map[string]bool{}
		for _, v := range vs {
			seen[v.At.Format("2006-01-02")] = true
		}
		if k := len(seen); k >= 5 {
			out = append(out, fact{key: "habit", forms: habit, say: func(form string) Note {
				return Note{Text: capital(fmt.Sprintf(form, words(min(k, 7))))}
			}})
		}
	}
	// A page read lately and an older one it is kin to.
	if k, ok := deskKin(db, now); ok {
		out = append(out, fact{key: "kin " + k.Href, forms: kinned, say: func(form string) Note {
			return Note{Text: capital(fmt.Sprintf(form, k.Title, when(k.Last, now), quoted(k.Shared), k.From)), Subject: k.Title, Href: k.Href}
		}})
	}
	return out
}

// --- discovery ---

var shelves = map[string][]string{
	"esoteric": {
		"From the locked shelves, where the old names are kept under older dust.",
		"From the cases with no labels on their spines. The binders feared what a label might summon.",
		"From behind the grille. I keep the key; no one has told me what it is for.",
	},
	"folklore": {
		"From the cabinet of tales: none of them happened, all of them are true.",
		"From the tellers' corner, where each story has outlived everyone who first heard it.",
		"From the tales told at hearths. Mind the ending; they seldom end where you expect.",
	},
	"textfiles": {
		"From the boxes of loose text files, typed into the night by people who did not sign their names.",
		"From the drawers of plain text, eighty columns wide and written past midnight.",
		"From the bulletin boards' leavings, saved by someone who thought they might matter. They were right.",
	},
	"encyclopedic": {
		"From the reference stacks. Read it as a traveller reads a map: for the places, not the lines.",
		"From the reference stacks, where everything is explained and little is understood.",
		"From the great books of facts. Each entry is a door; most readers stop at the handle.",
	},
	"knowledge": {
		"From the essayists' desks, where someone is always halfway through an argument.",
		"From the essayists' desks. Argue back; they cannot hear you, which is a mercy to both.",
		"From the long tables of the essayists, where the ink is mostly second thoughts.",
	},
	"smallweb": {
		"From the small rooms at the edge of the web, each lit by a single lamp.",
		"From the small web, where one person keeps one room, and keeps it well.",
		"From the far rooms, reached by narrow stairs. The lamps there are tended by hand.",
	},
	"underground": {
		"From the underground press, passed hand to hand on floppy disks and photocopies.",
		"From the underground press, where the type was crooked and the sentences were not.",
		"From the samizdat shelf. Every page here was copied by someone who meant it.",
	},
	"weird": {
		"From the wing of invented worlds. Its doors open inward only.",
		"From the wing of invented worlds, whose maps disagree with ours and with each other.",
		"From the shelves of other worlds. Some are better kept than this one.",
	},
	"fiction": {
		"From the serial hall, where the stories are still being written.",
		"From the serial hall. The authors are still writing; mind you do not wake them.",
		"From the hall of stories in parts. Each chapter is a promise of the next.",
	},
	"yours": {
		"From your own shelf. You chose these; I only dust them.",
		"From your own shelf, where every book is one you asked for.",
		"From the shelf you built. I know it by the order you gave it, which is no one else's.",
	},
}

const drawsKey = "ultan:draws"

type draw struct {
	Family string    `json:"f"`
	At     time.Time `json:"at"`
}

// Shelf is the note on a Deep Random draw: the shelf it came from, and how
// often that shelf has opened this week. It keeps the draw.
func Shelf(db *store.DB, family string, now time.Time) Note {
	texts, ok := shelves[family]
	if !ok {
		texts = []string{"From a shelf I do not know. The library grows in the night."}
	}
	if db == nil {
		return Note{Text: texts[0]}
	}
	var ds []draw
	_ = json.Unmarshal([]byte(db.Get(drawsKey)), &ds)
	week, month, kept := 0, 0, []draw{}
	for _, d := range ds {
		if now.Sub(d.At) < 30*24*time.Hour {
			kept = append(kept, d)
			if d.Family == family {
				month++
				if now.Sub(d.At) < 7*24*time.Hour {
					week++
				}
			}
		}
	}
	// Each draw from a shelf is greeted in the next of its words.
	text := texts[month%len(texts)]
	kept = append(kept, draw{family, now})
	if len(kept) > 200 {
		kept = kept[len(kept)-200:]
	}
	if b, err := json.Marshal(kept); err == nil {
		_ = db.Set(drawsKey, string(b))
	}
	switch week + 1 {
	case 1:
	case 2:
		text += " This shelf has opened for you twice this week."
	default:
		text += fmt.Sprintf(" This shelf has opened for you %s times this week; perhaps it wants something.", words(week+1))
	}
	return Note{Text: text}
}

// --- the Daily Packet ---

var kindWords = map[string][2]string{
	"periodical":   {"a periodical", "%s periodicals"},
	"weird":        {"a world that never was", "%s worlds that never were"},
	"esoteric":     {"an esoteric text", "%s esoteric texts"},
	"fiction":      {"a story still being written", "%s stories still being written"},
	"folklore":     {"a tale", "%s tales"},
	"knowledge":    {"an essay from the essayists' desks", "%s essays"},
	"encyclopedic": {"a page of the reference stacks", "%s pages of the reference stacks"},
	"public":       {"a public-domain find", "%s public-domain finds"},
	"archive":      {"a relic of the old internet", "%s relics of the old internet"},
	"smallweb":     {"a room of the small web", "%s rooms of the small web"},
	"queue":        {"a page from your queue", "%s pages from your queue"},
}

var kindOrder = []string{"periodical", "weird", "fiction", "esoteric", "folklore", "knowledge", "encyclopedic", "public", "archive", "smallweb", "queue"}

// Cover is the note on the Daily Packet's cover: what the issue holds,
// and the oracle's draw.
func Cover(kinds []string, oracle string, now time.Time) Note {
	count := map[string]int{}
	for _, k := range kinds {
		count[k]++
	}
	var parts []string
	for _, k := range kindOrder {
		if n := count[k]; n == 1 {
			parts = append(parts, kindWords[k][0])
		} else if n > 1 {
			parts = append(parts, fmt.Sprintf(kindWords[k][1], words(n)))
		}
	}
	list := strings.Join(parts, ", ")
	if i := strings.LastIndex(list, ", "); i >= 0 {
		list = list[:i] + " and " + list[i+2:]
	}
	var t string
	switch {
	case list != "" && oracle != "":
		t = pick([]string{
			"Today's issue is bound: %s. The oracle turned up %s; it says only what you bring to it.",
			"In today's issue, %s. The oracle drew %s. I would not read too much into it, nor too little.",
		}, day(now))
		t = fmt.Sprintf(t, list, oracle)
	case list != "":
		t = fmt.Sprintf("Today's issue is bound: %s.", list)
	default:
		t = pick(sayings, day(now))
	}
	return Note{Text: t}
}
