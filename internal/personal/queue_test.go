package personal

import (
	"os"
	"strings"
	"testing"
)

func TestQueueEditsKeepOwnersLines(t *testing.T) {
	useDir(t)
	// A queue edited in Obsidian on Windows: CRLF, own notes, an own
	// section, an entry without a catalog number, a URL with a space.
	own := "---\r\ntags: [w5f]\r\n---\r\n# Reading queue\r\nMy own intro line.\r\n\r\n## This week\r\n" +
		"- [ ] [SCP-173](https://scp-wiki.wikidot.com/scp-173) · FIC·SCP·173\r\n- [ ] [Plain](https://x.example/a)\r\n\r\n" +
		"## Ideas\r\nread more folklore\r\n\r\n## Someday\r\n- [x] [Spaced](<https://x.example/a b>) · WEB·X·a b\r\n"
	os.WriteFile(QueuePath(), []byte(own), 0o644)
	q, err := LoadQueue()
	if err != nil {
		t.Fatal(err)
	}
	es := q.Entries()
	if len(es) != 3 || es[1].Catalog != "" || es[2].URL != "https://x.example/a b" || !es[2].Done || es[2].Section != Someday {
		t.Fatalf("entries: %+v", es)
	}
	if !q.Add(Entry{Title: "Dracula [1897]", URL: "w5f:book/3", Catalog: "BK·GUT·345"}, ThisWeek) {
		t.Fatal("add refused")
	}
	if q.Add(Entry{Title: "again", URL: "w5f:book/3"}, Someday) {
		t.Error("a queued page must not be added twice")
	}
	q.Move("https://x.example/a", Someday)
	q.SetDone("https://scp-wiki.wikidot.com/scp-173", true)
	q.Remove("https://x.example/a b")
	if err := q.Save(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(QueuePath())
	s := string(b)
	for _, want := range []string{"My own intro line.", "## Ideas\nread more folklore",
		"- [x] [SCP-173](https://scp-wiki.wikidot.com/scp-173) · FIC·SCP·173\n- [ ] [Dracula \\[1897\\]](w5f:book/3) · BK·GUT·345",
		"## Someday\n- [ ] [Plain](https://x.example/a)"} {
		if !strings.Contains(s, want) {
			t.Errorf("queue lacks %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "Spaced") || strings.Contains(s, "\r") {
		t.Errorf("removed entry or CRLF left:\n%s", s)
	}
	q, _ = LoadQueue()
	if es := q.Entries(); len(es) != 3 || es[1].Title != "Dracula [1897]" {
		t.Errorf("reloaded: %+v", es)
	}
}

func TestNewQueueFile(t *testing.T) {
	useDir(t)
	q, _ := LoadQueue()
	q.Add(Entry{Title: "A", URL: "https://a"}, Someday)
	q.Add(Entry{Title: "B", URL: "https://b"}, ThisWeek)
	q.Save()
	b, _ := os.ReadFile(QueuePath())
	if !strings.Contains(string(b), "## This week\n- [ ] [B](https://b)\n\n## Someday\n- [ ] [A](https://a)") {
		t.Errorf("new queue:\n%s", b)
	}
}
