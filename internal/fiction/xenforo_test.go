package fiction

import (
	"context"
	"strings"
	"testing"

	"w5f/internal/doc"
	"w5f/internal/fetch"
)

func TestXenForo(t *testing.T) {
	const thread = "/threads/with-this-ring-young-justice-si-thread-twelve.25032/"
	srv := fixtureServer(t, map[string]string{
		thread + "threadmarks?threadmark_category=1&per_page=200": "sv-threadmarks-1.html",
		thread + "threadmarks?per_page=25&page=2":                 "sv-threadmarks-2.html",
		"/posts/5046455/": "sv-post.html",
		"/posts/77/":      "sv-spoiler.html",
	})
	a := xenForo{}
	for in, want := range map[string]string{
		"https://forums.sufficientvelocity.com/threads/with-this-ring-young-justice-si-thread-twelve.25032/page-40#post-1": "https://forums.sufficientvelocity.com/threads/with-this-ring-young-justice-si-thread-twelve.25032/",
		"https://forums.spacebattles.com/threads/some-story.123/threadmarks":                                               "https://forums.spacebattles.com/threads/some-story.123/",
	} {
		if s, ok := a.Match(mustURL(in)); !ok || s != want {
			t.Errorf("match %s = %q %v", in, s, ok)
		}
	}
	if _, ok := a.Match(mustURL("https://forums.spacebattles.com/forums/creative-writing.18/")); ok {
		t.Error("forum index pages are not serials")
	}
	f := fetch.New("", "test")
	f.HostGap = 0
	ctx := context.Background()
	s, err := a.Serial(ctx, f, srv.URL+thread)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(s.Title, "With This Ring") || s.Author != "Mr Zoat" || len(s.Chapters) != 48 {
		t.Fatalf("serial: %q %q %d", s.Title, s.Author, len(s.Chapters))
	}
	c0 := s.Chapters[0]
	if !strings.HasSuffix(c0.URL, "/posts/5046455/") || c0.Title != "Stars, Crossed (part 22)" || c0.Published.Year() != 2016 {
		t.Errorf("chapter 0: %+v", c0)
	}
	if a.Key(mustURL(srv.URL+thread+"#post-5046455")) != a.Key(mustURL(c0.URL)) {
		t.Error("a #post- address and /posts/ are the same chapter")
	}
	d, err := a.Chapter(ctx, f, c0)
	if err != nil || !strings.Contains(flat(d), "Five move to surround me") {
		t.Fatalf("post: %v\n%.300s", err, flat(d))
	}
	d, err = a.Chapter(ctx, f, Chapter{Title: "Spoiler test", URL: srv.URL + "/posts/77/"})
	if err != nil {
		t.Fatal(err)
	}
	spoiler := false
	for _, b := range d.Blocks {
		if c, ok := b.(doc.Collapsible); ok && strings.Contains(c.Show, "Omake") {
			spoiler = true
		}
	}
	if !spoiler || !strings.Contains(flat(d), "After the spoiler.") {
		t.Errorf("spoiler folding:\n%s", flat(d))
	}
}
