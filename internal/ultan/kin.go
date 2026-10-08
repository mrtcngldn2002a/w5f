package ultan

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
	"unicode"

	"w5f/internal/doc"
	"w5f/internal/index"
	"w5f/internal/store"
)

// Kin is a page already read that has rare words in common with another:
// Ultan knows his catalogue. From is the other page's title.
type Kin struct {
	From, Title, Href string
	Last              time.Time // when the kin was last opened
	Shared            []string  // the words, as the other page writes them
}

const (
	kinTerms  = 10 // the rarest words of a page that are looked for
	kinShared = 2  // how many of them a kin must have
)

type term struct {
	fold, word string // folded; as written, lower-case
	score      float64
}

// rareWords are a text's words that are in few other indexed pages, the
// rarest and most used first. self tells whether the text is itself in
// the index (its own count is taken off).
func rareWords(db *store.DB, text string, self bool) []term {
	orig := []rune(text)
	fold := []rune(index.Fold(text))
	if len(fold) != len(orig) {
		orig = fold
	}
	tf, first := map[string]int{}, map[string]int{}
	for i := 0; i < len(fold); {
		if !unicode.IsLetter(fold[i]) {
			i++
			continue
		}
		j := i
		for j < len(fold) && unicode.IsLetter(fold[j]) {
			j++
		}
		if n := j - i; n >= 5 && n <= 24 {
			w := string(fold[i:j])
			if tf[w] == 0 {
				first[w] = i
			}
			tf[w]++
		}
		i = j
	}
	ws := make([]string, 0, len(tf))
	for w := range tf {
		ws = append(ws, w)
	}
	// The most used few hundred are enough to find the rare ones among.
	sort.Slice(ws, func(i, j int) bool { return tf[ws[i]] > tf[ws[j]] || tf[ws[i]] == tf[ws[j]] && ws[i] < ws[j] })
	ws = ws[:min(len(ws), 400)]
	df, total, err := db.DocFreq(ws)
	if err != nil || total < 20 {
		return nil
	}
	most := max(3, total/50)
	var out []term
	for _, w := range ws {
		n := df[w]
		others := n
		if self {
			others--
		}
		if others < 1 || n > most {
			continue
		}
		i := first[w]
		out = append(out, term{w, strings.ToLower(string(orig[i : i+len([]rune(w))])),
			(1 + math.Log(float64(tf[w]))) * math.Log(float64(total+1)/float64(n))})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].score > out[j].score })
	return out[:min(len(out), kinTerms)]
}

// work is what a page belongs to: a chapter to its book or serial.
func work(target string) string {
	if i := strings.Index(target, "/ch/"); i >= 0 && strings.HasPrefix(target, "w5f:") {
		return target[:i]
	}
	return target
}

// findKin finds, among the pages read before a time, the one sharing the
// most of a text's rare words (at least kinShared).
func findKin(db *store.DB, key, title, text string, before time.Time) (Kin, bool) {
	_, self := db.Doc(key)
	ts := rareWords(db, title+"\n"+text, self)
	if len(ts) < kinShared {
		return Kin{}, false
	}
	parts := make([]string, len(ts))
	for i, t := range ts {
		parts[i] = `"` + t.fold + `"`
	}
	ds, err := db.FindRead(strings.Join(parts, " OR "), key, before, 15)
	if err != nil {
		return Kin{}, false
	}
	var best Kin
	for _, d := range ds {
		if work(d.Target) == work(key) || strings.EqualFold(d.Title, title) {
			continue
		}
		has := map[string]bool{}
		for _, w := range strings.FieldsFunc(index.Fold(d.Title+"\n"+d.Text), func(r rune) bool { return !unicode.IsLetter(r) }) {
			has[w] = true
		}
		var shared []string
		for _, t := range ts {
			if has[t.fold] {
				shared = append(shared, t.word)
			}
		}
		if len(shared) >= kinShared && len(shared) > len(best.Shared) {
			best = Kin{From: title, Title: d.Title, Href: d.Target, Last: d.Last, Shared: shared}
		}
	}
	return best, best.Href != ""
}

// Echo is the note for a page just opened: a page read before that it is
// kin to, if there is one.
func Echo(db *store.DB, target string, page *doc.Document, now time.Time) (Note, bool) {
	if db == nil || page == nil {
		return Note{}, false
	}
	_, key := index.Kind(target, page)
	if key == "" {
		key = target
	}
	k, ok := findKin(db, key, page.Title, index.Text(page), now)
	if !ok {
		return Note{}, false
	}
	t := fmt.Sprintf(pick([]string{
		"It put me in mind of %[1]s, which you read %[2]s; both speak of %[3]s.",
		"I would shelve it beside %[1]s, read %[2]s: %[3]s, in both.",
		"You have met its kin before: %[1]s, %[2]s. They share %[3]s.",
	}, day(now)+len(k.Title)), k.Title, when(k.Last, now), quoted(k.Shared))
	return Note{Text: t, Subject: k.Title, Href: k.Href}, true
}

// And joins two notes; the second's subject is kept when the first has none.
func (n Note) And(o Note) Note {
	n.Text += " " + o.Text
	if n.Subject == "" {
		n.Subject, n.Href = o.Subject, o.Href
	}
	return n
}

const kinKey = "ultan:kin"

// deskKin is the kin of a page read in the last two days among those read
// before them, found once a day.
func deskKin(db *store.DB, now time.Time) (Kin, bool) {
	var c struct {
		Day int
		Kin *Kin
	}
	if json.Unmarshal([]byte(db.Get(kinKey)), &c) == nil && c.Day == day(now) {
		if c.Kin == nil {
			return Kin{}, false
		}
		return *c.Kin, true
	}
	c.Day, c.Kin = day(now), nil
	recent, _ := db.History(10, 0)
	tries := 0
	for _, h := range recent {
		if now.Sub(h.Last) > 48*time.Hour || tries == 3 {
			break
		}
		d, ok := db.Doc(h.Target)
		if !ok {
			continue
		}
		tries++
		if k, ok := findKin(db, h.Target, d.Title, d.Text, now.AddDate(0, 0, -3)); ok {
			c.Kin = &k
			break
		}
	}
	if b, err := json.Marshal(c); err == nil {
		_ = db.Set(kinKey, string(b))
	}
	if c.Kin == nil {
		return Kin{}, false
	}
	return *c.Kin, true
}

// when says when a page was read, as one would say it.
func when(t, now time.Time) string {
	y1, m1, d1 := t.Date()
	y2, m2, d2 := now.Date()
	switch {
	case y1 == y2 && m1 == m2 && d1 == d2:
		return "earlier today"
	case now.AddDate(0, 0, -1).Format("2006-01-02") == t.Format("2006-01-02"):
		return "yesterday"
	case y1 == y2:
		return "on " + t.Format("2 January")
	}
	return "on " + t.Format("2 January 2006")
}

// quoted lists up to three words: “a”, “b” and “c”.
func quoted(ws []string) string {
	ws = ws[:min(len(ws), 3)]
	q := make([]string, len(ws))
	for i, w := range ws {
		q[i] = "“" + w + "”"
	}
	if len(q) < 2 {
		return strings.Join(q, "")
	}
	return strings.Join(q[:len(q)-1], ", ") + " and " + q[len(q)-1]
}
