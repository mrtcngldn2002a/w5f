package weeding

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"w5f/internal/store"
)

// A serial leaves Your serials, with its chapters and its pages in the
// history, after asking; the others stay (2026-10-15).
func TestRemoveASerial(t *testing.T) {
	env := testEnv(t)
	id, _ := env.DB.UpsertSerial(store.Serial{Kind: "royalroad", URL: "https://rr/f/1", Title: "Mother of Learning"})
	other, _ := env.DB.UpsertSerial(store.Serial{Kind: "ao3", URL: "https://ao3/w/2", Title: "Other"})
	env.DB.MergeChapters(id, []store.SerialChapter{{N: 0, Title: "1", URL: "https://rr/c/1"}})
	env.DB.Visit(fmt.Sprintf("w5f:serial/%d/continue", id), "Mother of Learning", "serial", "")
	env.DB.Visit(fmt.Sprintf("w5f:serial/%d/continue", other), "Other", "serial", "")

	d, _ := route(t, env, "w5f:weeding/serials")
	if l := sureLinks(d); len(l) > 0 || !strings.Contains(text(d), "Mother of Learning") {
		t.Fatalf("listing:\n%s %v", text(d), l)
	}
	d, _ = route(t, env, fmt.Sprintf("w5f:weeding/serial?id=%d", id))
	if d.Links[0].Href != "w5f:weeding/serials" || len(sureLinks(d)) != 1 {
		t.Fatalf("asking:\n%s", text(d))
	}
	if _, err := env.DB.Serial(id); err != nil {
		t.Fatal("asking removed it")
	}
	d, err := route(t, env, fmt.Sprintf("w5f:weeding/serial?id=%d&sure=yes", id))
	if err != nil || !strings.Contains(text(d), "! Removed: Mother of Learning") {
		t.Fatalf("removed: %v\n%s", err, text(d))
	}
	if _, err := env.DB.Serial(id); err == nil {
		t.Error("still in Your serials")
	}
	if chs, _ := env.DB.SerialChapters(id); len(chs) != 0 {
		t.Errorf("chapters left: %+v", chs)
	}
	if vs, _ := env.DB.History(0, 0); len(vs) != 1 || !strings.HasPrefix(vs[0].Target, fmt.Sprintf("w5f:serial/%d/", other)) {
		t.Errorf("history: %+v", vs)
	}
	if _, err := env.DB.Serial(other); err != nil {
		t.Error("another serial went")
	}
}

// The logs put aside when cleared can be removed one at a time, after
// asking; the live log and the table's current days are never offered.
func TestRemoveAnOldLog(t *testing.T) {
	env := testEnv(t)
	old := put(t, filepath.Join(env.DataDir, "history-20261001-120000.log"), "{}\n")
	live := put(t, filepath.Join(env.DataDir, "history.log"), "{}\n")
	day := put(t, filepath.Join(env.DataDir, "solo", "log-20261002-090000", "2026-10-01.jsonl"), "{}\n")
	current := put(t, filepath.Join(env.DataDir, "solo", "log", "2026-10-14.jsonl"), "{}\n")

	d, _ := route(t, env, "w5f:weeding/logs")
	s := text(d)
	if !strings.Contains(s, "history-20261001-120000.log") || !strings.Contains(s, "solo/log-20261002-090000") || strings.Contains(s, "history.log  ") || len(sureLinks(d)) > 0 {
		t.Fatalf("listing:\n%s", s)
	}
	if _, err := route(t, env, "w5f:weeding/log?f=history.log&sure=yes"); err == nil {
		t.Error("the live log can be removed")
	}
	if _, err := route(t, env, "w5f:weeding/log?f=../../x&sure=yes"); err == nil {
		t.Error("a path outside")
	}
	for _, f := range []string{"history-20261001-120000.log", "solo/log-20261002-090000"} {
		d, _ := route(t, env, "w5f:weeding/log?f="+f)
		if d.Links[0].Href != "w5f:weeding/logs" {
			t.Fatalf("asking:\n%s", text(d))
		}
		if d, err := route(t, env, "w5f:weeding/log?f="+f+"&sure=yes"); err != nil || !strings.Contains(text(d), "! Removed: "+f) {
			t.Fatalf("removed %s: %v", f, err)
		}
	}
	for _, p := range []string{old, day, filepath.Dir(day)} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("still there: %s", p)
		}
	}
	for _, p := range []string{live, current} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("went: %s", p)
		}
	}
}

// A removed book leaves the history and the desk too (2026-10-15).
func TestRemovedBookLeavesTheHistory(t *testing.T) {
	env := testEnv(t)
	p := put(t, filepath.Join(env.BooksDir, "Dracula.epub"), "epub")
	id, _ := env.DB.UpsertBook(store.Book{Path: p, Format: "epub", Title: "Dracula", Chapters: 2})
	env.DB.Visit(fmt.Sprintf("w5f:book/%d/ch/1", id), "Dracula", "book", "")
	env.DB.Visit("https://elsewhere", "Elsewhere", "web", "")
	if _, err := route(t, env, fmt.Sprintf("w5f:weeding/book?id=%d&sure=yes", id)); err != nil {
		t.Fatal(err)
	}
	if vs, _ := env.DB.History(0, 0); len(vs) != 1 || vs[0].Target != "https://elsewhere" {
		t.Errorf("history: %+v", vs)
	}
}
