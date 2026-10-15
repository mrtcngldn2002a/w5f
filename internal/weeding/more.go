package weeding

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"w5f/internal/doc"
)

// The serials and the old logs (2026-10-15, the owner's "the deleting and
// clearing are half done"): a serial once opened stayed in Your serials
// for good, and the logs put aside by Clear the history and Clear the
// Gaming Table's log could be weighed here but never removed.

func (env Env) serials(note string) *doc.Document {
	p := newPage("Remove a serial", Target+"/serials")
	var items [][]doc.Block
	if env.DB != nil {
		ss, _ := env.DB.Serials(false)
		for _, s := range ss {
			sub := []string{fmt.Sprintf("%d chapters", s.Chapters)}
			if !s.Opened.IsZero() {
				sub = append(sub, fmt.Sprintf("read to chapter %d", s.Chapter+1))
			}
			if s.Followed {
				sub = append(sub, "followed")
			}
			items = append(items, []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: serialName(s.Title, s.URL)},
				{Text: "  " + strings.Join(sub, " · ") + "  ", Style: doc.Italic}, p.link(fmt.Sprintf("%s/serial?id=%d", Target, s.ID), "remove")}}})
		}
	}
	if len(items) == 0 {
		p.italic("No serials in The Serial Hall.")
	} else {
		p.add(doc.List{Items: items})
	}
	p.italic("Only W5F's record of a serial goes; the story stays on its site, and opening it again starts it afresh. Chapters saved as books stay in The Stacks.")
	p.add(doc.Rule{}, doc.Paragraph{Text: doc.Inline{p.link(Target, "← the Weeding Room")}})
	p.notice(note)
	return p.d
}

func serialName(title, addr string) string {
	if title != "" {
		return title
	}
	return addr
}

func (env Env) serial(id int64, sure bool) (*doc.Document, error) {
	if env.DB == nil {
		return nil, errors.New("no library database")
	}
	s, err := env.DB.Serial(id)
	if err != nil {
		return nil, fmt.Errorf("no serial %d in The Serial Hall", id)
	}
	name := serialName(s.Title, s.URL)
	if !sure {
		p := newPage("Remove this serial?", fmt.Sprintf("%s/serial?id=%d", Target, id))
		what := []string{name, s.URL, "It leaves Your serials and the desk; its known chapters, where you were and its following go."}
		p.ask(what, Target+"/serials", fmt.Sprintf("%s/serial?id=%d&sure=yes", Target, id))
		return p.d, nil
	}
	if err := env.DB.ForgetSerial(id); err != nil {
		return nil, err
	}
	_ = env.DB.ForgetVisits(fmt.Sprintf("w5f:serial/%d", id), true)
	return env.serials("Removed: " + name), nil
}

// oldLog is a log put aside when it was cleared: a dated history log
// beside the database, or a dated folder of Gaming Table days.
type oldLog struct {
	rel, what string
	dir       bool
	size      int64
}

func (env Env) oldLogs() []oldLog {
	if env.DataDir == "" {
		return nil
	}
	var out []oldLog
	hs, _ := filepath.Glob(filepath.Join(env.DataDir, "history-*.log"))
	for _, h := range hs {
		if regular(h) {
			out = append(out, oldLog{rel: filepath.Base(h), what: "history log", size: fileSize(h)})
		}
	}
	ds, _ := filepath.Glob(filepath.Join(env.DataDir, "solo", "log-*"))
	for _, d := range ds {
		if st, err := os.Lstat(d); err == nil && st.IsDir() {
			n, _ := dirSize(d)
			out = append(out, oldLog{rel: "solo/" + filepath.Base(d), what: "Gaming Table days", dir: true, size: n})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].rel < out[j].rel })
	return out
}

func (env Env) logs(note string) *doc.Document {
	p := newPage("Remove an old log", Target+"/logs")
	var items [][]doc.Block
	for _, l := range env.oldLogs() {
		items = append(items, []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: l.rel},
			{Text: "  " + l.what + " · " + Size(l.size) + "  ", Style: doc.Italic},
			p.link(Target+"/log?"+url.Values{"f": {l.rel}}.Encode(), "remove")}}})
	}
	if len(items) == 0 {
		p.italic("No old logs: nothing has been put aside.")
	} else {
		p.add(doc.List{Items: items})
	}
	p.italic("The live history log (history.log) and the Gaming Table's current days are not listed: they are cleared from The Register and the table.")
	p.add(doc.Rule{}, doc.Paragraph{Text: doc.Inline{p.link(Target, "← the Weeding Room")}})
	p.notice(note)
	return p.d
}

func (env Env) oldLog(rel string, sure bool) (*doc.Document, error) {
	var l oldLog
	for _, x := range env.oldLogs() {
		if x.rel == rel {
			l = x
		}
	}
	if l.rel == "" {
		return nil, errors.New("no such old log: " + rel)
	}
	path := filepath.Join(env.DataDir, filepath.FromSlash(l.rel))
	if !sure {
		p := newPage("Remove this old log?", Target+"/log?"+url.Values{"f": {rel}}.Encode())
		p.ask([]string{l.rel + " (" + l.what + ", " + Size(l.size) + ")", "What was read or played then can no longer be read back or restored from it."},
			Target+"/logs", Target+"/log?"+url.Values{"f": {rel}, "sure": {"yes"}}.Encode())
		return p.d, nil
	}
	if l.dir {
		if err := removeDays(path); err != nil {
			return nil, err
		}
	} else if err := os.Remove(path); err != nil {
		return nil, err
	}
	return env.logs("Removed: " + l.rel), nil
}

// removeDays deletes a put-aside folder of Gaming Table days: its .jsonl
// files, nothing else, and the folder when that leaves it empty.
func removeDays(dir string) error {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range ents {
		if e.Type().IsRegular() && strings.EqualFold(filepath.Ext(e.Name()), ".jsonl") {
			if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
				return err
			}
		}
	}
	_ = os.Remove(dir) // only succeeds when empty
	return nil
}
