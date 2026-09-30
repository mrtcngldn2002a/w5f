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
	for in, want := range map[string]string{"tarot": "w5f:discover/tarot", "I Ching": "w5f:discover/iching", "iching": "w5f:discover/iching", "almanac": "w5f:almanac"} {
		if got := Resolve(in); got != want {
			t.Errorf("%s → %s, want %s", in, got, want)
		}
	}
	if got := Resolve("opml-export"); got != "w5f:feeds/export" {
		t.Errorf("opml-export → %s", got)
	}
}
