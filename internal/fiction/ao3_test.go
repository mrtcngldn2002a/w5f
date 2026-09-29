package fiction

import (
	"context"
	"errors"
	"strings"
	"testing"

	"w5f/internal/doc"
	"w5f/internal/fetch"
)

func TestAO3(t *testing.T) {
	srv := fixtureServer(t, map[string]string{
		"/works/555":               "ao3-work.html",
		"/works/555/navigate":      "ao3-navigate.html",
		"/works/555/chapters/1001": "ao3-work.html",
		"/works/666":               "ao3-adult.html",
	})
	a := ao3{}
	for in, want := range map[string]string{
		"https://archiveofourown.org/works/555/chapters/1002":   "https://archiveofourown.org/works/555",
		"https://archiveofourown.org/works/555?view_adult=true": "https://archiveofourown.org/works/555?view_adult=true",
	} {
		if s, ok := a.Match(mustURL(in)); !ok || s != want {
			t.Errorf("match %s = %q", in, s)
		}
	}
	f := fetch.New("", "test")
	f.HostGap = 0
	ctx := context.Background()
	s, err := a.Serial(ctx, f, srv.URL+"/works/555")
	if err != nil {
		t.Fatal(err)
	}
	if s.Title != "Test Work" || s.Author != "Writer" || s.Summary != "A test summary." || len(s.Chapters) != 3 ||
		s.Chapters[1].Title != "2. The Middle" || s.Chapters[2].Published.Month() != 3 {
		t.Fatalf("serial: %+v", s)
	}
	d, err := a.Chapter(ctx, f, s.Chapters[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(flat(d), "grey morning") || strings.Contains(flat(d), "Chapter Text") {
		t.Errorf("chapter:\n%s", flat(d))
	}
	notes := false
	for _, b := range d.Blocks {
		if c, ok := b.(doc.Collapsible); ok && strings.Contains(c.Show, "Notes") {
			notes = true
		}
	}
	if !notes {
		t.Error("notes not folded")
	}
	_, err = a.Serial(ctx, f, srv.URL+"/works/666")
	var adult errAdult
	if !errors.As(err, &adult) || !strings.Contains(adult.text, "adult content") {
		t.Fatalf("adult warning: %v", err)
	}
	page := adultDoc(adult)
	if len(page.Links) == 0 || !strings.Contains(page.Links[0].Href, "view_adult%3Dtrue") {
		t.Errorf("proceed link: %+v", page.Links)
	}
}
