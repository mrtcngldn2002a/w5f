package solo

import (
	"fmt"
	"math/rand/v2"
	"strings"
)

// Odds are the oracle's likelihoods (after Ironsworn's "Ask the Oracle",
// CC BY 4.0): a d100 at or under the chance answers yes.
var Odds = []struct {
	Key, Label string
	Chance     int
}{
	{"certain", "almost certain", 90},
	{"likely", "likely", 75},
	{"even", "50/50", 50},
	{"unlikely", "unlikely", 25},
	{"small", "small chance", 10},
}

// OddsOf finds odds by key or label ("50/50", "fifty", "even" …).
func OddsOf(s string) (key, label string, chance int, ok bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	alias := map[string]string{"almost": "certain", "almost-certain": "certain", "50/50": "even", "50": "even", "fifty": "even",
		"small-chance": "small", "smallchance": "small"}
	if a, ok := alias[s]; ok {
		s = a
	}
	for _, o := range Odds {
		if o.Key == s || o.Label == s {
			return o.Key, o.Label, o.Chance, true
		}
	}
	return "", "", 0, false
}

// Answer is an oracle's answer to a yes/no question.
type Answer struct {
	Question string
	Odds     string
	Chance   int
	Roll     int // 1–100
	Yes      bool
	Twist    bool // the two digits match (11, 22 … 100): an extreme result or a twist
	Manual   bool
}

// Ask answers with a d100: W5F's, or the owner's own (1–100) when given.
func Ask(question, odds string, own int) (Answer, error) {
	_, label, chance, ok := OddsOf(odds)
	if !ok {
		return Answer{}, fmt.Errorf("odds are one of: almost certain, likely, 50/50, unlikely, small chance (not %q)", odds)
	}
	a := Answer{Question: strings.TrimSpace(question), Odds: label, Chance: chance, Manual: own != 0}
	a.Roll = own
	if own == 0 {
		a.Roll = 1 + rand.IntN(100)
	}
	if a.Roll < 1 || a.Roll > 100 {
		return Answer{}, fmt.Errorf("a d100 shows 1 to 100, not %d", a.Roll)
	}
	a.Yes = a.Roll <= chance
	a.Twist = a.Roll == 100 || (a.Roll >= 11 && a.Roll%11 == 0)
	return a, nil
}

// Verdict is the answer in words.
func (a Answer) Verdict() string {
	switch {
	case a.Yes && a.Twist:
		return "YES — and it is extreme, or a twist"
	case a.Yes:
		return "YES"
	case a.Twist:
		return "NO — and it is extreme, or a twist"
	}
	return "NO"
}
