package personal

import (
	"os"
	"strings"
	"testing"
)

// Queueing a page whose entry is done re-opens that entry; actions then
// change the right line.
func TestRequeueDoneEntry(t *testing.T) {
	useDir(t)
	q, _ := LoadQueue()
	e := Entry{Title: "A", URL: "https://a"}
	q.Add(e, ThisWeek)
	q.SetDone("https://a", true)
	if !q.Add(e, Someday) {
		t.Fatal("a done page can be queued again")
	}
	es := q.Entries()
	if len(es) != 1 || es[0].Done || es[0].Section != Someday {
		t.Fatalf("entries: %+v", es)
	}
	if !q.SetDone("https://a", true) || !q.Entries()[0].Done {
		t.Errorf("done did not apply: %+v", q.Entries())
	}
}

// Text taken from web pages must never become live links when the
// clipping or saved page is opened again.
func TestClippedAndCodeTextStayText(t *testing.T) {
	useDir(t)
	p, _ := AppendClipping(Source{Title: "Page", URL: "https://x"}, []string{"see [click me](w5f:queue/remove?u=x) now"}, now)
	b, _ := os.ReadFile(p)
	d := ReadMarkdown(b, "file:///c.md")
	for _, l := range d.Links {
		if strings.HasPrefix(l.Href, "w5f:") {
			t.Errorf("clipped text became a link: %+v", l)
		}
	}
	d = ReadMarkdown([]byte("```\n[click](w5f:queue/remove?u=x)\n```\n\nuse `[x](w5f:notes)` here\n"), "file:///s.md")
	if len(d.Links) != 0 {
		t.Errorf("code became links: %+v", d.Links)
	}
}

// Linux terminals paste lines separated by a lone carriage return.
func TestNoteWithCarriageReturns(t *testing.T) {
	useDir(t)
	p, _ := AppendNote(Source{Title: "N", URL: "https://n"}, "one\rtwo", now)
	b, _ := os.ReadFile(p)
	if strings.Contains(string(b), "\r") || !strings.Contains(string(b), "one\ntwo") {
		t.Errorf("note: %q", b)
	}
}
