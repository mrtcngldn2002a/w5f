package personal

import (
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"w5f/internal/doc"
	"w5f/internal/store"
)

func flat(d *doc.Document) string {
	var b strings.Builder
	doc.ReplaceBlocks(d.Blocks, func(x doc.Block) ([]doc.Block, bool) {
		switch x := x.(type) {
		case doc.Paragraph:
			b.WriteString(x.Text.PlainText() + "\n")
		case doc.Heading:
			b.WriteString("# " + x.Text.PlainText() + "\n")
		case doc.Notice:
			b.WriteString("[N] " + x.Text + "\n")
		}
		return nil, false
	})
	return b.String()
}

func TestQueuePageActions(t *testing.T) {
	useDir(t)
	db, _ := store.Open(filepath.Join(t.TempDir(), "t.db"))
	defer db.Close()
	q, _ := LoadQueue()
	q.Add(Entry{Title: "SCP-173", URL: "https://scp-wiki.wikidot.com/scp-173", Catalog: "FIC·SCP·173"}, ThisWeek)
	q.Save()
	d, err := Route("w5f:queue", db)
	if err != nil || !strings.Contains(flat(d), "SCP-173  FIC·SCP·173") || !strings.Contains(flat(d), "done · → someday · remove") {
		t.Fatalf("queue page: %v\n%s", err, flat(d))
	}
	u := url.Values{"u": {"https://scp-wiki.wikidot.com/scp-173"}}
	d, _ = Route("w5f:queue/done?"+u.Encode(), db)
	if !strings.Contains(flat(d), "[N] marked done") || !strings.Contains(flat(d), "# Done") {
		t.Errorf("done:\n%s", flat(d))
	}
	d, _ = Route("w5f:queue/remove?"+u.Encode(), db)
	if strings.Contains(flat(d), "SCP-173  FIC") {
		t.Errorf("removed entry still listed:\n%s", flat(d))
	}
}

func TestNotesAndHistoryPages(t *testing.T) {
	useDir(t)
	db, _ := store.Open(filepath.Join(t.TempDir(), "t.db"))
	defer db.Close()
	src := Source{Title: "SCP-173", URL: "https://scp-wiki.wikidot.com/scp-173"}
	AppendNote(src, "A note.", now)
	AppendClipping(src, []string{"A clip."}, now)
	d, err := Route("w5f:notes", db)
	if err != nil || !strings.Contains(flat(d), "Notes/SCP-173.md") || !strings.Contains(flat(d), "Clippings/2026/09/2026-09-28.md") {
		t.Errorf("notes page: %v\n%s", err, flat(d))
	}
	db.Visit("https://scp-wiki.wikidot.com/scp-173", "SCP-173", "scp", "FIC·SCP·173")
	db.SavePos("https://scp-wiki.wikidot.com/scp-173", 0.42)
	d, _ = Route("w5f:history", db)
	if !strings.Contains(flat(d), "SCP-173") || !strings.Contains(flat(d), "[SCP] · FIC·SCP·173") || !strings.Contains(flat(d), "42%") {
		t.Errorf("history page:\n%s", flat(d))
	}
}
