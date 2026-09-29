package htmlconv

import (
	"strings"
	"testing"
)

// Web pages must not be able to trigger W5F's own actions (remove a
// catalog, download a file) through w5f: links.
func TestRemoteW5FLinksAreDropped(t *testing.T) {
	d, err := Generic(strings.NewReader(`<html><body><p><a href="w5f:catalog/x/remove">Next chapter</a> and <a href="/real">a real link</a></p></body></html>`), "https://evil.example/page")
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range d.Links {
		if strings.HasPrefix(strings.ToLower(l.Href), "w5f:") {
			t.Errorf("w5f link kept: %+v", l)
		}
	}
	if len(d.Links) != 1 {
		t.Errorf("links = %+v", d.Links)
	}
}
