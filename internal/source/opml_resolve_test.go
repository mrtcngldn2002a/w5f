package source

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOPMLCommands(t *testing.T) {
	home, _ := os.UserHomeDir()
	for in, want := range map[string]string{
		`opml-import ~/My Feeds.opml`: filepath.Join(home, "My Feeds.opml"),
		`opml-import "~/a b.opml"`:    filepath.Join(home, "a b.opml"),
		`opml-export ~/out.opml`:      filepath.Join(home, "out.opml"),
		`OPML-IMPORT ~/Upper.opml`:    filepath.Join(home, "Upper.opml"),
	} {
		got := Resolve(in)
		u, err := url.Parse(got)
		if err != nil || !strings.HasPrefix(got, "w5f:feeds/") || u.Query().Get("f") != want {
			t.Errorf("%s → %s (file %q, want %q)", in, got, u.Query().Get("f"), want)
		}
	}
	for in, want := range map[string]string{"tarot": "w5f:discover/tarot", "I Ching": "w5f:discover/iching", "iching": "w5f:discover/iching", "almanac": "w5f:almanac",
		"sync": "w5f:feeds/sync", "Sync": "w5f:feeds/sync", "feeds sync": "w5f:feeds/sync",
		"usenet": "w5f:usenet", "usenet folklore": "w5f:usenet/find?q=folklore", "news:alt.magick": "w5f:usenet/g/alt.magick"} {
		if got := Resolve(in); got != want {
			t.Errorf("%s → %s, want %s", in, got, want)
		}
	}
	for in, want := range map[string]string{"solo": "w5f:solo", "roll 2d6+1": "w5f:solo/roll?d=2d6%2B1", "roll 2d6 = 3 5": "w5f:solo/roll?d=2d6+%3D+3+5",
		"ask likely Is it guarded?": "w5f:solo/ask/likely?q=Is+it+guarded%3F", "ask almost certain The door holds": "w5f:solo/ask/certain?q=The+door+holds",
		"ask 50/50 Rain?": "w5f:solo/ask/even?q=Rain%3F", "spark tarot": "w5f:solo/spark/tarot", "spark": "w5f:solo/spark/words",
		"npc Agia: a liar": "w5f:solo/add/character?q=Agia%3A+a+liar", "thread Find the key": "w5f:solo/add/thread?q=Find+the+key",
		"counter Health 5/5": "w5f:solo/add/counter?q=Health+5%2F5", "pick npc": "w5f:solo/pick/character", "pick thread": "w5f:solo/pick/thread"} {
		if got := Resolve(in); got != want {
			t.Errorf("%s → %s, want %s", in, got, want)
		}
	}
	for _, web := range []string{"roll call", "ask me anything", "spark plugs", "counter strike"} {
		if strings.HasPrefix(Resolve(web), "w5f:solo") {
			t.Errorf("%q should stay a web search", web)
		}
	}
	if got := Resolve("opml-export"); got != "w5f:feeds/export" {
		t.Errorf("opml-export → %s", got)
	}
}

func TestSessionFrom(t *testing.T) {
	for in, want := range map[string]string{"reddit-login browser": "reddit/browser", "ao3-login chromium": "ao3/chromium",
		"AO3-login Firefox": "ao3/firefox", "reddit-login": "", "reddit-login opera": "", "w5f reddit-login browser": ""} {
		site, from, ok := SessionFrom(in)
		got := ""
		if ok {
			got = site + "/" + from
		}
		if got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
}
