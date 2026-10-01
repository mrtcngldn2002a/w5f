// Package solo is the solo RPG table (chosen with the owner, 2026-10-01):
// dice, an oracle with odds, and sparks drawn from the dictionary, tarot,
// the I Ching and what the owner has been reading. Every throw can be the
// owner's own: type what your dice showed and W5F reads that instead.
// Each answer is logged with where it came from.
package solo

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"regexp"
	"strconv"
	"strings"
)

// Term is one part of a dice expression: NdM (keep highest/lowest K), or a
// number to add.
type Term struct {
	Sign     int // +1 or -1
	Count    int // dice; 0 for a constant
	Sides    int
	Keep     int  // 0: all
	KeepHigh bool // kh (advantage) or kl
	Constant int
	Rolls    []int // what the dice showed
	Kept     []int // indexes of the dice that count
	Subtotal int
	Notation string
}

// Roll is a dice expression and its result.
type Roll struct {
	Expr   string
	Terms  []Term
	Total  int
	Manual bool // the owner's own dice
}

var reTerm = regexp.MustCompile(`^(\d*)d(\d+|%)(?:(kh|kl)(\d+))?$`)

// ParseDice reads "2d6+1", "d100", "d%", "2d20kh1", "4d6kh3 - 2".
func ParseDice(expr string) ([]Term, error) {
	s := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(expr), " ", ""))
	if s == "" {
		return nil, errors.New("no dice: write them like 2d6+1, d100 or 2d20kh1")
	}
	var terms []Term
	sign, start := 1, 0
	if s[0] == '+' || s[0] == '-' {
		if s[0] == '-' {
			sign = -1
		}
		start = 1
	}
	for i := start; i <= len(s); i++ {
		if i < len(s) && s[i] != '+' && s[i] != '-' {
			continue
		}
		part := s[start:i]
		t, err := parseTerm(part)
		if err != nil {
			return nil, err
		}
		t.Sign = sign
		terms = append(terms, t)
		if i < len(s) {
			sign = 1
			if s[i] == '-' {
				sign = -1
			}
		}
		start = i + 1
	}
	dice := 0
	for _, t := range terms {
		dice += t.Count
	}
	if dice == 0 {
		return nil, errors.New("no dice in " + expr)
	}
	if dice > 100 {
		return nil, errors.New("at most 100 dice at once")
	}
	return terms, nil
}

func parseTerm(part string) (Term, error) {
	if part == "" {
		return Term{}, errors.New("a sign with nothing after it")
	}
	if n, err := strconv.Atoi(part); err == nil {
		return Term{Constant: n, Notation: part}, nil
	}
	m := reTerm.FindStringSubmatch(part)
	if m == nil {
		return Term{}, fmt.Errorf("%q is not dice (like 2d6, d100, 2d20kh1)", part)
	}
	t := Term{Count: 1, Notation: part}
	if m[1] != "" {
		t.Count, _ = strconv.Atoi(m[1])
	}
	if m[2] == "%" {
		t.Sides = 100
	} else {
		t.Sides, _ = strconv.Atoi(m[2])
	}
	if t.Count < 1 || t.Sides < 2 || t.Sides > 1000 {
		return Term{}, fmt.Errorf("%q: from 1 die and 2 sides, up to 1000 sides", part)
	}
	if m[3] != "" {
		t.Keep, _ = strconv.Atoi(m[4])
		t.KeepHigh = m[3] == "kh"
		if t.Keep < 1 || t.Keep > t.Count {
			return Term{}, fmt.Errorf("%q keeps %d of %d dice", part, t.Keep, t.Count)
		}
	}
	return t, nil
}

// Throw rolls the dice: with W5F's dice, or with the owner's own when
// own is given — the total alone (dice without keep) or each die's face.
func Throw(expr string, own []int) (Roll, error) {
	terms, err := ParseDice(expr)
	if err != nil {
		return Roll{}, err
	}
	r := Roll{Expr: expr, Manual: own != nil}
	dice, keeps := 0, false
	for _, t := range terms {
		dice += t.Count
		keeps = keeps || t.Keep > 0
	}
	if own != nil {
		switch {
		case len(own) == dice:
		case len(own) == 1 && !keeps:
			// A total: check it can be thrown, then show it as such.
			lo, hi := 0, 0
			for _, t := range terms {
				a, b := t.Count, t.Count*t.Sides
				if t.Count == 0 {
					a, b = t.Constant, t.Constant
				}
				if t.Sign < 0 {
					a, b = -b, -a
				}
				lo, hi = lo+a, hi+b
			}
			if own[0] < lo || own[0] > hi {
				return Roll{}, fmt.Errorf("%s gives %d to %d, not %d", expr, lo, hi, own[0])
			}
			r.Terms, r.Total = terms, own[0]
			return r, nil
		default:
			return Roll{}, fmt.Errorf("%s is %d dice: give each die's face (or just the total)", expr, dice)
		}
	}
	next := 0
	for i := range terms {
		t := &terms[i]
		if t.Count == 0 {
			t.Subtotal = t.Constant
		} else {
			for d := 0; d < t.Count; d++ {
				face := 1 + rand.IntN(t.Sides)
				if own != nil {
					face = own[next]
					if face < 1 || face > t.Sides {
						return Roll{}, fmt.Errorf("a d%d does not show %d", t.Sides, face)
					}
				}
				next++
				t.Rolls = append(t.Rolls, face)
			}
			t.Kept = keep(t.Rolls, t.Keep, t.KeepHigh)
			for _, k := range t.Kept {
				t.Subtotal += t.Rolls[k]
			}
		}
		r.Total += t.Sign * t.Subtotal
	}
	r.Terms = terms
	return r, nil
}

// keep chooses the dice that count (all, or the k highest/lowest).
func keep(rolls []int, k int, high bool) []int {
	idx := make([]int, len(rolls))
	for i := range idx {
		idx[i] = i
	}
	if k == 0 {
		return idx
	}
	for i := 1; i < len(idx); i++ { // a stable sort by face
		for j := i; j > 0; j-- {
			a, b := rolls[idx[j-1]], rolls[idx[j]]
			if (high && b > a) || (!high && b < a) {
				idx[j-1], idx[j] = idx[j], idx[j-1]
			}
		}
	}
	kept := append([]int{}, idx[:k]...)
	for i := 1; i < len(kept); i++ { // back in throwing order
		for j := i; j > 0 && kept[j] < kept[j-1]; j-- {
			kept[j-1], kept[j] = kept[j], kept[j-1]
		}
	}
	return kept
}

// Detail shows how the total came about: "2d6 [3 5] + 1".
func (r Roll) Detail() string {
	var b strings.Builder
	for i, t := range r.Terms {
		switch {
		case i > 0 && t.Sign < 0:
			b.WriteString(" − ")
		case i > 0:
			b.WriteString(" + ")
		case t.Sign < 0:
			b.WriteString("−")
		}
		if t.Count == 0 {
			b.WriteString(strconv.Itoa(t.Constant))
			continue
		}
		b.WriteString(t.Notation)
		if len(t.Rolls) > 0 {
			kept := map[int]bool{}
			for _, k := range t.Kept {
				kept[k] = true
			}
			var faces []string
			for j, f := range t.Rolls {
				s := strconv.Itoa(f)
				if !kept[j] {
					s = "(" + s + ")"
				}
				faces = append(faces, s)
			}
			b.WriteString(" [" + strings.Join(faces, " ") + "]")
		}
	}
	return b.String()
}

// ParseOwn reads the owner's own dice after "=": "9" or "3 5" or "3,5".
func ParseOwn(s string) ([]int, error) {
	var out []int
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == ',' }) {
		n, err := strconv.Atoi(f)
		if err != nil {
			return nil, fmt.Errorf("%q is not a number", f)
		}
		out = append(out, n)
	}
	if len(out) == 0 {
		return nil, errors.New("nothing after =")
	}
	return out, nil
}
