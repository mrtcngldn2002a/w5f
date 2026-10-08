package ultan

import (
	"encoding/json"
	"math"
	"strings"

	"w5f/internal/store"
)

// saidKey keeps what Ultan has said at the desk, so he does not repeat
// himself: the note of each day, the last forty.
const saidKey = "ultan:said"

type said struct {
	Day  int    `json:"d"`
	Key  string `json:"k"` // the fact's key
	Form int    `json:"f"` // which way of saying it
}

type memory struct {
	db   *store.DB
	list []said
}

func recall(db *store.DB) *memory {
	m := &memory{db: db}
	if db != nil {
		_ = json.Unmarshal([]byte(db.Get(saidKey)), &m.list)
	}
	return m
}

// last is how recently something was said (its place in the memory, the
// higher the later), -1 when never.
func (m *memory) last(match func(said) bool) int {
	for i := len(m.list) - 1; i >= 0; i-- {
		if match(m.list[i]) {
			return i
		}
	}
	return -1
}

// today is the note already said today, if any.
func (m *memory) today(day int) (said, bool) {
	if n := len(m.list); n > 0 && m.list[n-1].Day == day {
		return m.list[n-1], true
	}
	return said{}, false
}

// remember keeps the day's note; a later note the same day replaces it.
func (m *memory) remember(s said) {
	if m.db == nil {
		return
	}
	if n := len(m.list); n > 0 && m.list[n-1].Day == s.Day {
		m.list = m.list[:n-1]
	}
	m.list = append(m.list, s)
	if len(m.list) > 40 {
		m.list = m.list[len(m.list)-40:]
	}
	if b, err := json.Marshal(m.list); err == nil {
		_ = m.db.Set(saidKey, string(b))
	}
}

// choose takes the fact said longest ago (never said comes first); the
// day decides between equals.
func (m *memory) choose(pool []fact, seed int) fact {
	best, bestAt := 0, math.MaxInt
	for j := range pool {
		i := (j + seed) % len(pool)
		if at := m.last(func(s said) bool { return s.Key == pool[i].key }); at < bestAt {
			best, bestAt = i, at
		}
	}
	return pool[best]
}

// form takes the way of saying a kind of fact used longest ago.
func (m *memory) form(f fact, seed int) int {
	best, bestAt := 0, math.MaxInt
	for j := range f.forms {
		i := (j + seed) % len(f.forms)
		if at := m.last(func(s said) bool { return kindOf(s.Key) == kindOf(f.key) && s.Form == i }); at < bestAt {
			best, bestAt = i, at
		}
	}
	return best
}

func kindOf(key string) string {
	k, _, _ := strings.Cut(key, " ")
	return k
}
