package personal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"w5f/internal/doc"
)

func useDir(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	t.Setenv("W5F_NOTES", d)
	return d
}

var now = time.Date(2026, 9, 28, 10, 21, 0, 0, time.Local)

func TestDirFromEnvConfigDefault(t *testing.T) {
	d := useDir(t)
	if Dir() != d {
		t.Errorf("env: %s", Dir())
	}
	t.Setenv("W5F_NOTES", "")
	data := t.TempDir()
	t.Setenv("APPDATA", data)
	t.Setenv("XDG_DATA_HOME", data)
	cfgDir := filepath.Join(data, "w5f")
	os.MkdirAll(cfgDir, 0o755)
	os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte(`notes = "/vault/W5F"`), 0o644)
	if Dir() != "/vault/W5F" {
		t.Errorf("config: %s", Dir())
	}
}

func TestFileNamesAreSafeAndDistinct(t *testing.T) {
	useDir(t)
	if got := FileName(`SCP-173: "The Sculpture" / <Ω> #1 [draft]`); got != "SCP-173 The Sculpture Ω 1 draft" {
		t.Errorf("FileName = %q", got)
	}
	if got := FileName(strings.Repeat("ç", 200)); len([]rune(got)) != 80 {
		t.Errorf("long title: %d runes", len([]rune(got)))
	}
	if FileName("...") != "Untitled" {
		t.Error("empty name")
	}
	a := Source{Title: "Dracula", URL: "https://a/dracula"}
	b := Source{Title: "Dracula", URL: "https://b/dracula"}
	pa, _ := AppendNote(a, "first", now)
	pb, _ := AppendNote(b, "other source", now)
	pa2, _ := AppendNote(a, "second", now.Add(time.Hour))
	if filepath.Base(pa) != "Dracula.md" || filepath.Base(pb) != "Dracula (2).md" || pa2 != pa {
		t.Errorf("paths %s %s %s", pa, pb, pa2)
	}
}

func TestNoteClippingSavedFiles(t *testing.T) {
	dir := useDir(t)
	src := Source{Title: "SCP-173", URL: "https://scp-wiki.wikidot.com/scp-173", Catalog: "FIC·SCP·173", Kind: "scp"}
	p, err := AppendNote(src, "Compare with SCP-096.", now)
	if err != nil {
		t.Fatal(err)
	}
	AppendNote(src, "Second thought.", now.Add(time.Minute))
	b, _ := os.ReadFile(p)
	note := string(b)
	for _, want := range []string{"title: \"SCP-173\"", "url: \"https://scp-wiki.wikidot.com/scp-173\"", "catalog: \"FIC·SCP·173\"",
		"tags: [w5f, w5f/note]", "# SCP-173", "## 2026-09-28 10:21\nCompare with SCP-096.", "## 2026-09-28 10:22\nSecond thought."} {
		if !strings.Contains(note, want) {
			t.Errorf("note lacks %q:\n%s", want, note)
		}
	}
	if strings.Count(note, "---") != 2 {
		t.Errorf("frontmatter written twice:\n%s", note)
	}
	cp, err := AppendClipping(src, []string{"SCP-173 is to be kept in a locked container.", "Second paragraph."}, now)
	if err != nil || cp != filepath.Join(dir, "Clippings", "2026", "09", "2026-09-28.md") {
		t.Fatalf("clipping path %s %v", cp, err)
	}
	AppendClipping(Source{Title: "Other", URL: "https://x/y z"}, []string{"More."}, now.Add(time.Hour))
	c, _ := os.ReadFile(cp)
	clip := string(c)
	for _, want := range []string{"date: \"2026-09-28\"", "## 10:21 · [SCP-173](https://scp-wiki.wikidot.com/scp-173) · FIC·SCP·173",
		"> SCP-173 is to be kept in a locked container.\n>\n> Second paragraph.", "## 11:21 · [Other](<https://x/y z>)\n> More."} {
		if !strings.Contains(clip, want) {
			t.Errorf("clipping lacks %q:\n%s", want, clip)
		}
	}
	sp, err := SavePage(src, &doc.Document{Title: "SCP-173", Blocks: []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: "Item #: SCP-173"}}}}}, now)
	s, _ := os.ReadFile(sp)
	if err != nil || filepath.Dir(sp) != filepath.Join(dir, "Saved") || !strings.Contains(string(s), "tags: [w5f, w5f/saved]") ||
		!strings.Contains(string(s), "Item #: SCP-173") {
		t.Errorf("saved page %s %v:\n%s", sp, err, s)
	}
	if Rel(cp) != "Clippings/2026/09/2026-09-28.md" {
		t.Errorf("Rel = %q", Rel(cp))
	}
	if !strings.HasPrefix(FileURL(cp), "file:///") {
		t.Errorf("FileURL = %q", FileURL(cp))
	}
	if _, err := AppendNote(src, "  \n ", now); err == nil {
		t.Error("an empty note must be refused")
	}
}
