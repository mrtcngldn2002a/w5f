package feeds

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// An OPML file as other readers write it: nested folders, a loose feed, a
// title with a bare &, a Turkish title in ISO-8859-9, a feed W5F already has
// (written differently) and two feeds with the same name.
var sampleOPML = "<?xml version=\"1.0\" encoding=\"ISO-8859-9\"?>\n" +
	`<opml version="1.0"><head><title>my feeds</title></head><body>
  <outline text="Weird &amp; Folklore">
    <outline text="Atlas Obscura" type="rss" xmlUrl="http://atlasobscura.com/feeds/latest/"/>
    <outline text="Strange Stuff" type="rss" xmlUrl="https://strange.example/rss" htmlUrl="https://strange.example/"/>
  </outline>
  <outline text="Blogs">
    <outline text="Friends">
      <outline text="Notes" type="rss" xmlUrl="https://a.example/feed"/>
      <outline text="Notes" type="rss" xmlUrl="https://b.example/feed"/>
    </outline>
    <outline title="Ali ` + "\xfe" + `iir & Deniz" text="x" type="rss" xmlUrl="https://c.example/atom.xml" language="tr"/>
  </outline>
  <outline text="Loose one" type="rss" xmlUrl="https://loose.example/index.xml"/>
</body></opml>`

func dataDir(t *testing.T) {
	t.Helper()
	d := t.TempDir()
	t.Setenv("APPDATA", d)
	t.Setenv("XDG_DATA_HOME", d)
}

func TestParseOPML(t *testing.T) {
	subs, err := ParseOPML(strings.NewReader(sampleOPML))
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 6 {
		t.Fatalf("%d feeds: %+v", len(subs), subs)
	}
	want := map[string]string{"Strange Stuff": "Weird & Folklore", "Notes": "Friends", "Ali şiir & Deniz": "Blogs", "Loose one": ""}
	for _, s := range subs {
		if f, ok := want[s.Name]; ok && s.Folder != f {
			t.Errorf("%s in %q, want %q", s.Name, s.Folder, f)
		}
		delete(want, s.Name)
	}
	if len(want) != 0 {
		t.Errorf("not read (the ISO-8859-9 title?): %v", want)
	}
	if _, err := ParseOPML(strings.NewReader("<html><body>no</body></html>")); err == nil {
		t.Error("a web page is not an OPML file")
	}
}

func TestImportAppendsAndSkipsWhatIsThere(t *testing.T) {
	dataDir(t)
	own := "# my own feeds\n[[feed]]\nid = \"mine\"\nname = \"Mine\"\nshelf = \"weird\"\nurl = [\"https://mine.example/feed\"]\n"
	os.MkdirAll(filepath.Dir(UserCatalogPath()), 0o755)
	os.WriteFile(UserCatalogPath(), []byte(own), 0o644)
	src := filepath.Join(t.TempDir(), "subs.opml")
	os.WriteFile(src, []byte(sampleOPML), 0o644)

	c, err := LoadCatalog()
	if err != nil {
		t.Fatal(err)
	}
	rep, err := Import(c, src)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Added) != 5 || len(rep.Duplicate) != 1 || rep.Duplicate[0] != "Atlas Obscura" {
		t.Fatalf("added %d, duplicates %v", len(rep.Added), rep.Duplicate)
	}
	b, _ := os.ReadFile(UserCatalogPath())
	if !strings.HasPrefix(string(b), own) || strings.Contains(string(b), "disabled") {
		t.Errorf("the user's file is appended to, with no empty fields:\n%s", b)
	}
	c, err = LoadCatalog()
	if err != nil {
		t.Fatal(err)
	}
	shelfOf := func(url string) string {
		for _, f := range c.Feeds {
			if f.URL[0] == url {
				return c.Shelf(f.Shelf).Label + "/" + f.ID
			}
		}
		return "missing"
	}
	for url, want := range map[string]string{
		"https://strange.example/rss":     "Weird & Folklore/strange-stuff", // the built-in shelf, by its label
		"https://a.example/feed":          "Friends/notes",
		"https://b.example/feed":          "Friends/notes-2",
		"https://c.example/atom.xml":      "Blogs/ali-iir-deniz",
		"https://loose.example/index.xml": "Imported/loose-one",
	} {
		if got := shelfOf(url); got != want {
			t.Errorf("%s: %s, want %s", url, got, want)
		}
	}
	if c.Feed("mine") == nil {
		t.Error("the user's own feed is kept")
	}

	again, err := Import(c, src)
	if err != nil || len(again.Added) != 0 || len(again.Duplicate) != 6 {
		t.Errorf("a second import adds nothing: %d added, %v", len(again.Added), err)
	}

	var out bytes.Buffer
	n, err := Export(c, nil, &out)
	if err != nil || n != len(c.Feeds) {
		t.Fatalf("export: %d of %d, %v", n, len(c.Feeds), err)
	}
	back, err := ParseOPML(&out)
	if err != nil || len(back) != n {
		t.Fatalf("export reads back: %d, %v", len(back), err)
	}
	found := false
	for _, s := range back {
		if s.URL == "https://c.example/atom.xml" && s.Folder == "Blogs" && s.Name == "Ali şiir & Deniz" && s.Lang == "tr" {
			found = true
		}
	}
	if !found {
		t.Error("an imported feed goes out with its shelf, name and language")
	}
}

func TestBrokenImportChangesNothing(t *testing.T) {
	dataDir(t)
	// A user file that does not load: the import must leave it as it was.
	bad := "[[feed]]\nid = \"x\"\nname = \n"
	os.MkdirAll(filepath.Dir(UserCatalogPath()), 0o755)
	os.WriteFile(UserCatalogPath(), []byte(bad), 0o644)
	src := filepath.Join(t.TempDir(), "subs.opml")
	os.WriteFile(src, []byte(sampleOPML), 0o644)
	c := &Catalog{}
	if _, err := Import(c, src); err == nil {
		t.Fatal("an import into a broken catalog fails")
	}
	if b, _ := os.ReadFile(UserCatalogPath()); string(b) != bad {
		t.Errorf("the file was changed:\n%s", b)
	}
}
