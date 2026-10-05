package source

import (
	"fmt"
	"strings"
	"testing"

	"w5f/internal/sitecat"
)

func TestResolveCatalogShortcuts(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("APPDATA", dir)
	t.Setenv("XDG_DATA_HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	ps := []sitecat.Profile{{ID: "gutenberg-org", Name: "Project Gutenberg"}, {ID: "fadedpage-com", Name: "Faded Page"}}
	if err := sitecat.SaveAll(sitecat.Path(), ps); err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"cat videos funny":                      "w5f:search/web?q=cat+videos+funny",
		"catalog-add https://www.gutenberg.org": "w5f:catalog/check?url=https%3A%2F%2Fwww.gutenberg.org",
		"catalog-add fadedpage.com dracula":     "w5f:catalog/check?url=fadedpage.com&w=dracula",
		"catalogs":                              "w5f:catalogs",
		"cat gutenberg-org":                     "w5f:catalog/gutenberg-org",
		"cat gutenberg-org author:Bram Stoker":  "w5f:catalog/gutenberg-org?q=author%3ABram+Stoker",
		"cat fadedpage-com dracula":             "w5f:catalog/fadedpage-com?q=dracula",
		"CAT Fadedpage-Com dracula":             "w5f:catalog/fadedpage-com?q=dracula",
	}
	for in, want := range cases {
		if got := Resolve(in); got != want {
			t.Errorf("Resolve(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFriendlyRefusedDownload(t *testing.T) {
	err := fmt.Errorf("%w (HTTP 403)", sitecat.ErrRefused)
	if msg := Friendly(err); !strings.Contains(msg, "refused the download") {
		t.Errorf("Friendly = %q", msg)
	}
}
