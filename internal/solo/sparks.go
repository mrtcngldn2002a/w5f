package solo

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"w5f/internal/dict"
	"w5f/internal/discover"
	"w5f/internal/fetch"
	"w5f/internal/store"
)

// Spark is a prompt for the imagination: words, a card, a hexagram or a
// line from what the owner has read, with where it came from.
type Spark struct {
	Kind   string // words, tarot, iching, reading
	Text   string
	Source string // where it came from
	Link   string // the text behind it, when there is one
	Lines  []int  // I Ching: the cast lines
}

// SparkKinds are the kinds of spark, in the order the table offers them.
var SparkKinds = []struct{ Key, Label string }{
	{"word", "a word"}, {"words", "two words"}, {"tarot", "a tarot card"}, {"iching", "an I Ching hexagram"}, {"reading", "from your reading"},
}

// plainWord is a dictionary headword fit to inspire: one English word,
// lower case (no names or abbreviations), 3–14 letters.
func plainWord(w string) bool {
	if len(w) < 3 || len(w) > 14 {
		return false
	}
	for _, r := range w {
		if r < 'a' || r > 'z' {
			return false
		}
	}
	return true
}

// RandomWords draws n English words from the dictionary (its headwords
// only, never the Turkish side: chosen with the owner).
func RandomWords(d *dict.Dict, n int) ([]string, error) {
	if d == nil || d.Len() == 0 {
		return nil, errors.New("no dictionary installed (g → dict-install)")
	}
	var out []string
	for tries := 0; len(out) < n && tries < 400*n; tries++ {
		if w := d.Word(rand.IntN(d.Len())); plainWord(w) {
			out = append(out, w)
		}
	}
	if len(out) < n {
		return nil, errors.New("the dictionary gave too few plain words")
	}
	return out, nil
}

// Env is what the table needs.
type Env struct {
	DB      *store.DB
	Fetcher *fetch.Fetcher
	Dict    func() (*dict.Dict, error)
	Notes   string // the notes folder (clippings live in it)
	LogDir  string
}

// Draw makes a spark of a kind.
func (e Env) Draw(ctx context.Context, kind string) (Spark, error) {
	switch kind {
	case "word", "words":
		d, err := e.Dict()
		if err != nil || d == nil {
			return Spark{}, errors.New("no dictionary installed: g → dict-install downloads the English–Turkish one (16 MB); the words come from its English side")
		}
		n := 1
		if kind == "words" {
			n = 2
		}
		ws, err := RandomWords(d, n)
		if err != nil {
			return Spark{}, err
		}
		return Spark{Kind: kind, Text: strings.Join(ws, " · "), Source: "the English–Turkish dictionary's English headwords"}, nil
	case "tarot", "iching":
		o, err := discover.DrawOracle(ctx, e.Fetcher, e.DB, kind)
		if err != nil {
			return Spark{}, err
		}
		s := Spark{Kind: kind, Text: o.Title + " — " + o.Detail, Link: o.Target, Lines: o.Lines}
		s.Source = "Waite, The Pictorial Key to the Tarot (1910)"
		if kind == "iching" {
			s.Source = "the I Ching, Legge's translation (1882)"
		}
		return s, nil
	case "reading":
		return e.readingSpark()
	}
	return Spark{}, fmt.Errorf("no spark of kind %q", kind)
}

// readingSpark takes a line from what the owner has read: a clipping, the
// title of a page read, or of a periodical's article.
func (e Env) readingSpark() (Spark, error) {
	type line struct{ text, source, link string }
	var pools [][]line
	if lines := clippingLines(e.Notes); len(lines) > 0 {
		var p []line
		for _, l := range lines {
			p = append(p, line{l.text, "a clipping of yours, " + l.from, ""})
		}
		pools = append(pools, p)
	}
	if e.DB != nil {
		if vs, err := e.DB.History(500, 0); err == nil {
			var p []line
			for _, v := range vs {
				if strings.TrimSpace(v.Title) != "" && !strings.HasPrefix(v.Target, "w5f:") {
					p = append(p, line{v.Title, "a page you read (" + v.Last.Format("2 Jan 2006") + ")", v.Target})
				}
			}
			if len(p) > 0 {
				pools = append(pools, p)
			}
		}
		if its, err := e.DB.Items(store.Query{Limit: 400}); err == nil {
			var p []line
			for _, it := range its {
				if strings.TrimSpace(it.Title) != "" {
					p = append(p, line{it.Title, "your periodicals (" + it.FeedID + ")", it.URL})
				}
			}
			if len(p) > 0 {
				pools = append(pools, p)
			}
		}
	}
	if len(pools) == 0 {
		return Spark{}, errors.New("nothing read yet to draw from")
	}
	pool := pools[rand.IntN(len(pools))]
	l := pool[rand.IntN(len(pool))]
	return Spark{Kind: "reading", Text: l.text, Source: l.source, Link: l.link}, nil
}

type clip struct{ text, from string }

// clippingLines reads the quoted lines of the clippings files (newest
// files first, at most 60 files).
func clippingLines(notes string) []clip {
	if notes == "" {
		return nil
	}
	files, _ := filepath.Glob(filepath.Join(notes, "Clippings", "*", "*", "*.md"))
	if len(files) > 60 {
		files = files[len(files)-60:]
	}
	var out []clip
	for _, f := range files {
		fh, err := os.Open(f)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(fh)
		for sc.Scan() {
			t := strings.TrimSpace(sc.Text())
			if !strings.HasPrefix(t, ">") {
				continue
			}
			t = strings.TrimSpace(strings.TrimLeft(t, "> "))
			if len([]rune(t)) >= 20 && strings.IndexFunc(t, unicode.IsLetter) >= 0 {
				if len([]rune(t)) > 220 {
					t = string([]rune(t)[:220]) + "…"
				}
				out = append(out, clip{t, strings.TrimSuffix(filepath.Base(f), ".md")})
			}
		}
		fh.Close()
	}
	return out
}
