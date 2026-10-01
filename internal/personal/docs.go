package personal

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"w5f/internal/catalog"
	"w5f/internal/doc"
	"w5f/internal/store"
)

// IsTarget reports whether target is one of the personal pages.
func IsTarget(t string) bool {
	return t == "w5f:queue" || strings.HasPrefix(t, "w5f:queue/") || t == "w5f:notes" ||
		t == "w5f:history" || strings.HasPrefix(t, "w5f:history?") || t == "w5f:history/clear" || strings.HasPrefix(t, "w5f:history/clear?")
}

func link(d *doc.Document, href, text string) int {
	d.Links = append(d.Links, doc.Link{Href: href, Text: text})
	return len(d.Links)
}

// Route builds the queue, notes and history pages and applies queue
// actions (w5f:queue/done|undone|move|remove?u=…) and the clearing of the
// history (w5f:history/clear asks first; ?sure=yes clears).
func Route(target string, db *store.DB) (*doc.Document, error) {
	u, err := url.Parse(target)
	if err != nil {
		return nil, err
	}
	p, q := u.Opaque, u.Query()
	switch {
	case p == "notes":
		return notesDoc()
	case p == "history":
		page, _ := strconv.Atoi(q.Get("page"))
		return historyDoc(db, max(page, 1))
	case p == "history/clear":
		if q.Get("sure") != "yes" {
			return clearHistoryDoc(db)
		}
		kept, err := db.ClearHistory(time.Now())
		if err != nil {
			return nil, err
		}
		d, err := historyDoc(db, 1)
		if err == nil {
			note := "The history is cleared."
			if kept != "" {
				note += " The old log is kept as " + kept + "."
			}
			d.Blocks = append([]doc.Block{doc.Notice{Kind: "info", Text: note}}, d.Blocks...)
		}
		return d, err
	case p == "queue":
		return queueDoc("")
	case strings.HasPrefix(p, "queue/"):
		qq, err := LoadQueue()
		if err != nil {
			return nil, err
		}
		link := q.Get("u")
		var ok bool
		var note string
		switch strings.TrimPrefix(p, "queue/") {
		case "done":
			ok, note = qq.SetDone(link, true), "marked done"
		case "undone":
			ok, note = qq.SetDone(link, false), "back in the queue"
		case "move":
			ok, note = qq.Move(link, q.Get("to")), "moved to "+q.Get("to")
		case "remove":
			ok, note = qq.Remove(link), "removed"
		default:
			return nil, errors.New("unknown queue action: " + target)
		}
		if !ok {
			note = "that entry is no longer in the queue"
		} else if err := qq.Save(); err != nil {
			return nil, err
		}
		return queueDoc(note)
	}
	return nil, errors.New("unknown address: " + target)
}

func queueDoc(note string) (*doc.Document, error) {
	qq, err := LoadQueue()
	if err != nil {
		return nil, err
	}
	d := &doc.Document{Title: "The Lectern", URL: "w5f:queue", Origin: "local", Lang: "en"}
	if note != "" {
		d.Blocks = append(d.Blocks, doc.Notice{Kind: "info", Text: note})
	}
	es := qq.Entries()
	var cols [][]doc.Block
	for _, sec := range []string{ThisWeek, Someday} {
		var items [][]doc.Block
		for _, e := range es {
			if e.Section == sec && !e.Done {
				items = append(items, entryBlocks(d, e))
			}
		}
		col := []doc.Block{doc.Heading{Level: 2, Text: doc.Inline{{Text: sec}}}}
		if len(items) == 0 {
			col = append(col, doc.Paragraph{Text: doc.Inline{{Text: "empty", Style: doc.Italic}}})
		} else {
			col = append(col, doc.List{Items: items})
		}
		cols = append(cols, col)
	}
	d.Blocks = append(d.Blocks, doc.Columns{Cols: cols})
	var done [][]doc.Block
	for _, e := range es {
		if e.Done {
			done = append(done, entryBlocks(d, e))
		}
	}
	if len(done) > 0 {
		d.Blocks = append(d.Blocks, doc.Heading{Level: 2, Text: doc.Inline{{Text: "Done"}}}, doc.List{Items: done})
	}
	d.Blocks = append(d.Blocks, doc.Rule{}, doc.Paragraph{Text: doc.Inline{{
		Text: "Press a on any page (A on a selected link) to add it here. The queue is " + QueuePath() + " — you can edit it in Obsidian too.", Style: doc.Italic}}})
	return d, nil
}

func entryBlocks(d *doc.Document, e Entry) []doc.Block {
	head := doc.Inline{{Text: e.Title, Link: link(d, e.URL, e.Title)}}
	if e.Catalog != "" {
		head = append(head, doc.Span{Text: "  " + e.Catalog, Style: doc.Italic})
	}
	var acts doc.Inline
	add := func(label, href string) {
		if len(acts) > 0 {
			acts = append(acts, doc.Span{Text: " · ", Style: doc.Italic})
		}
		acts = append(acts, doc.Span{Text: label, Style: doc.Italic, Link: link(d, href, label)})
	}
	v := url.Values{"u": {e.URL}}
	if e.Done {
		add("back to queue", "w5f:queue/undone?"+v.Encode())
	} else {
		add("done", "w5f:queue/done?"+v.Encode())
		other := Someday
		if e.Section == Someday {
			other = ThisWeek
		}
		add("→ "+strings.ToLower(other), "w5f:queue/move?"+url.Values{"u": {e.URL}, "to": {other}}.Encode())
	}
	add("remove", "w5f:queue/remove?"+v.Encode())
	return []doc.Block{doc.Paragraph{Text: head}, doc.Paragraph{Text: acts}}
}

type fileInfo struct {
	path string
	mod  time.Time
}

// recent lists Markdown files under dir, newest first.
func recent(dir string, n int) []fileInfo {
	var out []fileInfo
	filepath.WalkDir(dir, func(p string, de fs.DirEntry, err error) error {
		if err != nil || de.IsDir() || !strings.EqualFold(filepath.Ext(p), ".md") {
			return nil
		}
		if info, err := de.Info(); err == nil {
			out = append(out, fileInfo{p, info.ModTime()})
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].mod.After(out[j].mod) })
	if len(out) > n {
		out = out[:n]
	}
	return out
}

func notesDoc() (*doc.Document, error) {
	d := &doc.Document{Title: "The Scriptorium", URL: "w5f:notes", Origin: "local", Lang: "en"}
	var parts [][]doc.Block
	for _, sec := range []struct{ dir, title, empty string }{
		{"Notes", "Notes", "No notes yet — press n on any page."},
		{"Clippings", "Clippings", "No clippings yet — press y on any page."},
		{"Saved", "Saved pages", "No saved pages yet — press s on any page."},
	} {
		part := []doc.Block{doc.Heading{Level: 2, Text: doc.Inline{{Text: sec.title}}}}
		files := recent(filepath.Join(Dir(), sec.dir), 20)
		if len(files) == 0 {
			parts = append(parts, append(part, doc.Paragraph{Text: doc.Inline{{Text: sec.empty, Style: doc.Italic}}}))
			continue
		}
		var items [][]doc.Block
		for _, f := range files {
			rel := Rel(f.path)
			items = append(items, []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: rel, Link: link(d, FileURL(f.path), rel)},
				{Text: "  " + f.mod.Format("2006-01-02 15:04"), Style: doc.Italic}}}})
		}
		parts = append(parts, append(part, doc.List{Items: items}))
	}
	// Notes beside clippings; the saved pages below, full width.
	d.Blocks = append(d.Blocks, doc.Columns{Cols: parts[:2]})
	d.Blocks = append(d.Blocks, parts[2]...)
	if _, err := os.Stat(Dir()); err == nil {
		d.Blocks = append(d.Blocks, doc.Rule{}, doc.Paragraph{Text: doc.Inline{{Text: "Folder: " + Dir(), Style: doc.Italic}}})
	}
	return d, nil
}

func historyDoc(db *store.DB, page int) (*doc.Document, error) {
	const per = 50
	vs, err := db.History(per+1, (page-1)*per)
	if err != nil {
		return nil, err
	}
	d := &doc.Document{Title: "The Register", URL: "w5f:history", Origin: "local", Lang: "en"}
	if len(vs) == 0 {
		d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: "Nothing read yet.", Style: doc.Italic}}})
		return d, nil
	}
	more := len(vs) > per
	if more {
		vs = vs[:per]
	}
	if page == 1 {
		d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: "clear the history…", Style: doc.Italic, Link: link(d, "w5f:history/clear", "clear the history")}}})
	}
	var items [][]doc.Block
	for _, v := range vs {
		sub := catalog.Label(v.Kind)
		if v.Catalog != "" {
			sub += " · " + v.Catalog
		}
		sub += " · " + v.Last.Format("2006-01-02 15:04")
		if v.Pos > 0 {
			sub += fmt.Sprintf(" · %d%%", int(v.Pos*100+0.5))
		}
		items = append(items, []doc.Block{
			doc.Paragraph{Text: doc.Inline{{Text: v.Title, Link: link(d, v.Target, v.Title)}}},
			doc.Paragraph{Text: doc.Inline{{Text: sub, Style: doc.Italic}}},
		})
	}
	d.Blocks = append(d.Blocks, doc.List{Items: items})
	if more {
		d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: "→ older", Link: link(d, fmt.Sprintf("w5f:history?page=%d", page+1), "older")}}})
	}
	return d, nil
}

// clearHistoryDoc asks before the history is cleared, saying what goes and
// what stays.
func clearHistoryDoc(db *store.DB) (*doc.Document, error) {
	vs, err := db.History(0, 0)
	if err != nil {
		return nil, err
	}
	d := &doc.Document{Title: "Clear the history?", URL: "w5f:history/clear", Origin: "local", Lang: "en"}
	if len(vs) == 0 {
		d.Blocks = []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: "The history is already empty. "}, {Text: "back to the history", Link: link(d, "w5f:history", "history")}}}}
		return d, nil
	}
	pages := "pages"
	if len(vs) == 1 {
		pages = "page"
	}
	d.Blocks = []doc.Block{
		doc.Paragraph{Text: doc.Inline{{Text: fmt.Sprintf("%d %s will leave the history, the desk forgets where they were left, and Ultan's ledger starts again from nothing.", len(vs), pages)}}},
		doc.Paragraph{Text: doc.Inline{{Text: "Kept: the log of every visit, under a dated name next to the database; the progress of books and serials; the queue, notes and clippings; the search index.", Style: doc.Italic}}},
		doc.Paragraph{Text: doc.Inline{{Text: "No, keep it", Link: link(d, "w5f:history", "keep")}, {Text: " · "}, {Text: "Yes, clear the history", Style: doc.Bold, Link: link(d, "w5f:history/clear?sure=yes", "clear")}}}, // "No" first: the selection starts there
	}
	return d, nil
}
