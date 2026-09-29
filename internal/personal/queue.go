package personal

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Queue sections.
const (
	ThisWeek = "This week"
	Someday  = "Someday"
)

// Entry is one line of the reading queue.
type Entry struct {
	Title, URL, Catalog string
	Done                bool
	Section             string
}

const newQueue = "---\ntags: [w5f]\n---\n# Reading queue\n\n## This week\n\n## Someday\n"

var reEntry = regexp.MustCompile(`^- \[( |x|X)\] \[((?:\\.|[^\]\\])*)\]\((<[^>]*>|[^)\s]+)\)(?: · (.*?))?\s*$`)

// Queue is Queue.md as lines; entries are recognised in place so the
// owner's own lines survive every change.
type Queue struct {
	path  string
	lines []string
}

// QueuePath is the queue file.
func QueuePath() string { return filepath.Join(Dir(), "Queue.md") }

// LoadQueue reads the queue (a new one when the file does not exist yet).
func LoadQueue() (*Queue, error) {
	p := QueuePath()
	data, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		data, err = []byte(newQueue), nil
	}
	if err != nil {
		return nil, err
	}
	s := strings.TrimRight(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	return &Queue{path: p, lines: strings.Split(s, "\n")}, nil
}

func parseEntry(line string) (Entry, bool) {
	m := reEntry.FindStringSubmatch(strings.TrimRight(line, " "))
	if m == nil {
		return Entry{}, false
	}
	return Entry{Done: m[1] != " ", Title: mdUnescape(m[2]),
		URL: strings.TrimSuffix(strings.TrimPrefix(m[3], "<"), ">"), Catalog: m[4]}, true
}

func entryLine(e Entry) string {
	box := " "
	if e.Done {
		box = "x"
	}
	l := fmt.Sprintf("- [%s] %s", box, mdLink(e.Title, e.URL))
	if e.Catalog != "" {
		l += " · " + e.Catalog
	}
	return l
}

// Entries lists the queue's entries in file order with their section.
func (q *Queue) Entries() []Entry {
	var out []Entry
	section := ""
	for _, l := range q.lines {
		if strings.HasPrefix(l, "## ") {
			section = strings.TrimSpace(l[3:])
			continue
		}
		if e, ok := parseEntry(l); ok {
			e.Section = section
			out = append(out, e)
		}
	}
	return out
}

func (q *Queue) find(u string) int {
	for i, l := range q.lines {
		if e, ok := parseEntry(l); ok && e.URL == u {
			return i
		}
	}
	return -1
}

// Add puts a page at the end of a section; false when it is already queued
// and not done. A done entry for the page is re-opened and moved instead of
// adding a second line.
func (q *Queue) Add(e Entry, section string) bool {
	for _, x := range q.Entries() {
		if x.URL == e.URL && !x.Done {
			return false
		}
	}
	if i := q.findState(e.URL, true); i >= 0 {
		q.lines = append(q.lines[:i], q.lines[i+1:]...)
	}
	e.Done = false
	q.addLine(entryLine(e), section)
	return true
}

// findState finds an entry for u: with done=true only a done (checked)
// entry; with done=false an open entry, else any entry.
func (q *Queue) findState(u string, done bool) int {
	any := -1
	for i, l := range q.lines {
		if e, ok := parseEntry(l); ok && e.URL == u {
			if e.Done == done {
				return i
			}
			if any < 0 {
				any = i
			}
		}
	}
	if any >= 0 && done {
		return -1 // only look for a done entry when asked for one
	}
	return any
}

func (q *Queue) addLine(line, section string) {
	h := -1
	for i, l := range q.lines {
		if strings.TrimSpace(l) == "## "+section {
			h = i
			break
		}
	}
	if h < 0 {
		if n := len(q.lines); n > 0 && strings.TrimSpace(q.lines[n-1]) != "" {
			q.lines = append(q.lines, "")
		}
		q.lines = append(q.lines, "## "+section, line)
		return
	}
	end := h + 1
	for end < len(q.lines) && !strings.HasPrefix(q.lines[end], "## ") {
		end++
	}
	at := h + 1
	for i := h + 1; i < end; i++ {
		if strings.TrimSpace(q.lines[i]) != "" {
			at = i + 1
		}
	}
	q.lines = append(q.lines[:at], append([]string{line}, q.lines[at:]...)...)
	if at+1 < len(q.lines) && strings.HasPrefix(q.lines[at+1], "## ") {
		q.lines = append(q.lines[:at+1], append([]string{""}, q.lines[at+1:]...)...)
	}
}

// SetDone checks or unchecks an entry.
func (q *Queue) SetDone(u string, done bool) bool {
	i := q.findState(u, !done)
	if i < 0 {
		return false
	}
	e, _ := parseEntry(q.lines[i])
	e.Done = done
	q.lines[i] = entryLine(e)
	return true
}

// Move puts an entry at the end of another section.
func (q *Queue) Move(u, section string) bool {
	i := q.findState(u, false)
	if i < 0 {
		return false
	}
	line := q.lines[i]
	q.lines = append(q.lines[:i], q.lines[i+1:]...)
	q.addLine(line, section)
	return true
}

// Remove deletes an entry.
func (q *Queue) Remove(u string) bool {
	i := q.find(u)
	if i < 0 {
		return false
	}
	q.lines = append(q.lines[:i], q.lines[i+1:]...)
	return true
}

// Save writes the queue back (atomically).
func (q *Queue) Save() error {
	if err := os.MkdirAll(filepath.Dir(q.path), 0o755); err != nil {
		return err
	}
	tmp := q.path + ".tmp"
	if err := os.WriteFile(tmp, []byte(strings.Join(q.lines, "\n")+"\n"), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, q.path)
}
