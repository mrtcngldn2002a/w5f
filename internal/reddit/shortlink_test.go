package reddit

import (
	"net/url"
	"testing"

	"w5f/internal/doc"
)

// The "Part Two" link at the end of an r/nosleep part is a redd.it short
// link; it opens the post like any Reddit address. Media hosts stay apart.
func TestShortLinks(t *testing.T) {
	for raw, want := range map[string]string{
		"https://redd.it/brsj8v":      "https://www.reddit.com/comments/brsj8v/",
		"http://www.redd.it/brsj8v/":  "https://www.reddit.com/comments/brsj8v/",
		"https://old.reddit.com/r/x/": "https://www.reddit.com/r/x/",
	} {
		u, _ := url.Parse(raw)
		if !IsReddit(u) || Canonical(u).String() != want {
			t.Errorf("%s → %v %s, want %s", raw, IsReddit(u), Canonical(u), want)
		}
	}
	for _, raw := range []string{"https://i.redd.it/abc.jpg", "https://v.redd.it/abc", "https://redd.it/", "https://redd.it/a/b"} {
		if u, _ := url.Parse(raw); IsReddit(u) {
			t.Errorf("%s is not a post", raw)
		}
	}
	d := &doc.Document{Links: []doc.Link{{Href: "https://redd.it/brsj8v"}, {Href: "https://i.redd.it/abc.jpg"}}}
	fixLinks(d, "http://127.0.0.1:8080")
	if d.Links[0].Href != "https://www.reddit.com/comments/brsj8v/" || d.Links[1].Href != "https://i.redd.it/abc.jpg" {
		t.Errorf("links: %+v", d.Links)
	}
}
