package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"w5f/internal/doc"
	"w5f/internal/index"
	"w5f/internal/personal"
	"w5f/internal/store"
)

func textPage(title string, paras ...string) *doc.Document {
	d := &doc.Document{Title: title}
	for _, p := range paras {
		d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: p}}})
	}
	return d
}

func open(m Model, target string, d *doc.Document) Model {
	next, cmd := m.Update(loadedMsg{target: target, doc: d})
	m = next.(Model)
	if cmd != nil {
		if msg := cmd(); msg != nil {
			next, _ = m.Update(msg)
			m = next.(Model)
		}
	}
	return m
}

// The M4 criterion: pages of three kinds become searchable once read.
func TestOpenedPagesAreSearchable(t *testing.T) {
	m := sized(New("", "test"))
	m = open(m, "https://scp-wiki.wikidot.com/scp-173", textPage("SCP-173", "The statue moves when unobserved."))
	m = open(m, "https://arkeofili.com/x", &doc.Document{Title: "Göbekli Tepe", Ref: "item:77", URL: "https://arkeofili.com/x",
		Blocks: []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: "A carved statue of a fox."}}}}})
	open(m, "w5f:book/5/ch/2", &doc.Document{Title: "Dracula · Chapter 3", Ref: "book:5:2",
		Blocks: []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: "He stood like a statue in the moonlight."}}}}})
	db, _ := store.Default()
	hits, err := index.Search(db, "statue", "", 30, 0)
	kinds := map[string]bool{}
	for _, h := range hits {
		kinds[h.Kind] = true
	}
	if err != nil || !kinds["scp"] || !kinds["feed"] || !kinds["book"] {
		t.Errorf("hits %v %+v", err, hits)
	}
	vs, _ := db.History(0, 0)
	if len(vs) < 3 || vs[0].Target != "w5f:book/5/ch/2" {
		t.Errorf("history: %+v", vs)
	}
}

func TestSearchPromptQueueSaveHistoryKeys(t *testing.T) {
	m := sized(New("", "test"))
	m = open(m, "https://scp-wiki.wikidot.com/scp-173", textPage("SCP-173", strings.Repeat("Item text. ", 400)))
	m = press(m, "/", "s", "t", "a")
	if m.mode != modeFind || m.findBuf != "sta" {
		t.Fatalf("find prompt: mode %v buf %q", m.mode, m.findBuf)
	}
	m = press(m, "enter")
	if m.mode != modeRead || !strings.Contains(m.loading, "search") {
		t.Errorf("after enter: mode %v loading %q", m.mode, m.loading)
	}
	m.loading = ""
	m = press(m, "a")
	b, _ := os.ReadFile(personal.QueuePath())
	if !strings.Contains(string(b), "- [ ] [SCP-173](https://scp-wiki.wikidot.com/scp-173) · FIC·SCP·173") {
		t.Errorf("queue:\n%s", b)
	}
	if m = press(m, "a"); m.status != "already in the queue" {
		t.Errorf("status %q", m.status)
	}
	m = press(m, "s")
	if !strings.HasPrefix(m.status, "saved to Saved/SCP-173.md") {
		t.Errorf("save status %q", m.status)
	}
	m = press(m, "space", "space")
	m.leavePage()
	db, _ := store.Default()
	vs, _ := db.History(1, 0)
	if len(vs) != 1 || vs[0].Pos <= 0 {
		t.Errorf("position not saved: %+v", vs)
	}
	if m = press(m, "H"); m.loading == "" {
		t.Error("H should open the history")
	}
}

func TestQueueFocusedLink(t *testing.T) {
	m := sized(New("", "test"))
	d := textPage("List")
	d.Links = []doc.Link{{Href: "https://backrooms-wiki.wikidot.com/level-0", Text: "Level 0"}}
	d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: "Level 0", Link: 1}}})
	m = open(m, "w5f:feeds", d)
	press(m, "A")
	b, _ := os.ReadFile(personal.QueuePath())
	if !strings.Contains(string(b), "[Level 0](https://backrooms-wiki.wikidot.com/level-0) · FIC·BR·level-0") {
		t.Errorf("queue:\n%s", b)
	}
}

func TestWelcomeContinueReading(t *testing.T) {
	db, _ := store.Default()
	db.Visit("https://example.org/half", "Half read", "web", "WEB·EXAMPLE·half")
	db.SavePos("https://example.org/half", 0.4)
	d := welcomeDoc("t")
	var b strings.Builder
	doc.ReplaceBlocks(d.Blocks, func(x doc.Block) ([]doc.Block, bool) {
		switch x := x.(type) {
		case doc.Paragraph:
			b.WriteString(x.Text.PlainText() + "\n")
		case doc.Heading:
			b.WriteString(x.Text.PlainText() + "\n")
		}
		return nil, false
	})
	for _, want := range []string{"On the desk", "Half read", "40%", "Today", "Ultan's note", "— U.", "The Lectern", "The Scriptorium", "The Register", "Ultan's Ledger"} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("welcome lacks %q:\n%s", want, b.String())
		}
	}
}

func personalPath(rel string) string { return filepath.Join(personal.Dir(), filepath.FromSlash(rel)) }
