package solo

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// The table's own lists (asked for by the owner, 2026-10-01): the
// characters met, the threads left open and the counters kept, in
// table.json beside the log.

// Character is someone met in the story.
type Character struct {
	Name string `json:"name"`
	Note string `json:"note,omitempty"`
}

// Thread is something left to be resolved.
type Thread struct {
	Text   string `json:"text"`
	Closed bool   `json:"closed,omitempty"`
}

// Counter is a number kept: health, supply, a clock; Max 0 has no maximum.
type Counter struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
	Max   int    `json:"max,omitempty"`
}

// Table is what lies on the table besides the dice.
type Table struct {
	Characters []Character `json:"characters"`
	Threads    []Thread    `json:"threads"`
	Counters   []Counter   `json:"counters"`
}

func (e Env) tablePath() string { return filepath.Join(filepath.Dir(e.LogDir), "table.json") }

// LoadTable reads the table (an empty one when there is none yet).
func (e Env) LoadTable() (Table, error) {
	var t Table
	if e.LogDir == "" {
		return t, nil
	}
	b, err := os.ReadFile(e.tablePath())
	if os.IsNotExist(err) {
		return t, nil
	}
	if err != nil {
		return t, err
	}
	return t, json.Unmarshal(b, &t)
}

// SaveTable writes the table whole (through a temporary file, so a crash
// never leaves half of it).
func (e Env) SaveTable(t Table) error {
	if e.LogDir == "" {
		return errors.New("no place to keep the table")
	}
	p := e.tablePath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// ParseCharacter reads "Name — note" (or "Name - note", "Name: note").
func ParseCharacter(s string) (Character, error) {
	s = strings.TrimSpace(s)
	for _, sep := range []string{" — ", " – ", " - ", ": "} {
		if name, note, ok := strings.Cut(s, sep); ok && strings.TrimSpace(name) != "" {
			return Character{Name: strings.TrimSpace(name), Note: strings.TrimSpace(note)}, nil
		}
	}
	if s == "" {
		return Character{}, errors.New("a character needs a name: Name — a few words")
	}
	return Character{Name: s}, nil
}

var reCounter = regexp.MustCompile(`^(.*?)\s+(-?\d+)(?:\s*/\s*(\d+))?$`)

// ParseCounter reads "Health 5/5", "Supply 3" or "Doom clock 0/6"; a
// name alone starts at 0.
func ParseCounter(s string) (Counter, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Counter{}, errors.New("a counter needs a name: Health 5/5, Supply 3, Clock 0/6")
	}
	m := reCounter.FindStringSubmatch(s)
	if m == nil || strings.TrimSpace(m[1]) == "" {
		return Counter{Name: s}, nil
	}
	c := Counter{Name: strings.TrimSpace(m[1])}
	c.Value, _ = strconv.Atoi(m[2])
	if m[3] != "" {
		c.Max, _ = strconv.Atoi(m[3])
		if c.Max < 1 || c.Max > 100 {
			return Counter{}, fmt.Errorf("a maximum is 1 to 100, not %d", c.Max)
		}
		c.Value = min(c.Value, c.Max)
	}
	return c, nil
}

// Bar draws a counter with a maximum: [####..] 4/6.
func (c Counter) Bar() string {
	if c.Max == 0 {
		return strconv.Itoa(c.Value)
	}
	s := fmt.Sprintf("%d/%d", c.Value, c.Max)
	if c.Max > 20 {
		return s
	}
	v := max(0, min(c.Value, c.Max))
	return "[" + strings.Repeat("#", v) + strings.Repeat(".", c.Max-v) + "] " + s
}

// Change applies an action (up, down, remove, close, open) to the entry
// at i of a list, checking by its name that the page was not stale.
func (t *Table) Change(list, action string, i int, name string) (string, error) {
	stale := errors.New("that entry has changed since the page was drawn; look again")
	switch list {
	case "character":
		if i < 0 || i >= len(t.Characters) || t.Characters[i].Name != name {
			return "", stale
		}
		if action == "remove" {
			t.Characters = append(t.Characters[:i], t.Characters[i+1:]...)
			return name + " left the table", nil
		}
	case "thread":
		if i < 0 || i >= len(t.Threads) || t.Threads[i].Text != name {
			return "", stale
		}
		switch action {
		case "close":
			t.Threads[i].Closed = true
			return "Closed: " + name, nil
		case "open":
			t.Threads[i].Closed = false
			return "Open again: " + name, nil
		case "remove":
			t.Threads = append(t.Threads[:i], t.Threads[i+1:]...)
			return "Struck out: " + name, nil
		}
	case "counter":
		if i < 0 || i >= len(t.Counters) || t.Counters[i].Name != name {
			return "", stale
		}
		c := &t.Counters[i]
		switch action {
		case "up":
			if c.Max == 0 || c.Value < c.Max {
				c.Value++
			}
			return c.Name + " " + c.Bar(), nil
		case "down":
			if c.Max == 0 || c.Value > 0 { // a clock stops at 0; a plain count may go below
				c.Value--
			}
			return c.Name + " " + c.Bar(), nil
		case "remove":
			t.Counters = append(t.Counters[:i], t.Counters[i+1:]...)
			return name + " put away", nil
		}
	}
	return "", fmt.Errorf("no %s action %q", list, action)
}

// Pick draws one character, or one open thread, at random.
func (t Table) Pick(list string) (string, error) {
	var from []string
	switch list {
	case "character":
		for _, c := range t.Characters {
			s := c.Name
			if c.Note != "" {
				s += " — " + c.Note
			}
			from = append(from, s)
		}
	case "thread":
		for _, th := range t.Threads {
			if !th.Closed {
				from = append(from, th.Text)
			}
		}
	default:
		return "", fmt.Errorf("nothing to pick from %q", list)
	}
	if len(from) == 0 {
		return "", fmt.Errorf("no %ss on the table yet", list)
	}
	return from[rand.IntN(len(from))], nil
}

// IsCounter reports text that names a counter with its number ("Health
// 5/5", "Supply 3").
func IsCounter(s string) bool {
	m := reCounter.FindStringSubmatch(strings.TrimSpace(s))
	return m != nil && strings.TrimSpace(m[1]) != ""
}
