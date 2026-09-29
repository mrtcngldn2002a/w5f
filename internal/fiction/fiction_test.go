package fiction

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"w5f/internal/doc"
	"w5f/internal/fetch"
	"w5f/internal/store"
)

// fakeAdapter serves a serial at fake.test/story/<id>; chapters are
// fake.test/story/<id>/c/<n>.
type fakeAdapter struct {
	chapters int
	fail     bool
	failURL  string
	fetched  int
}

func (a *fakeAdapter) Kind() string { return "fake" }
func (a *fakeAdapter) Match(u *url.URL) (string, bool) {
	if u.Host != "fake.test" || !strings.HasPrefix(u.Path, "/story/") {
		return "", false
	}
	id := strings.Split(strings.TrimPrefix(u.Path, "/story/"), "/")[0]
	return "https://fake.test/story/" + id, true
}
func (a *fakeAdapter) Serial(ctx context.Context, f *fetch.Fetcher, s string) (*Serial, error) {
	a.fetched++
	if a.fail || s == a.failURL {
		return nil, errors.New("Fake: no chapter list found — the site layout may have changed")
	}
	out := &Serial{Kind: "fake", URL: s, Title: "The Fake Serial", Author: "Anon", Summary: "A story."}
	for i := 1; i <= a.chapters; i++ {
		out.Chapters = append(out.Chapters, Chapter{Title: fmt.Sprintf("Chapter %d", i), URL: fmt.Sprintf("%s/c/%d", s, i)})
	}
	return out, nil
}
func (a *fakeAdapter) Chapter(ctx context.Context, f *fetch.Fetcher, ch Chapter) (*doc.Document, error) {
	return &doc.Document{Title: ch.Title, Blocks: []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: "Text of " + ch.Title + "."}}}}}, nil
}

func flat(d *doc.Document) string {
	var b strings.Builder
	doc.ReplaceBlocks(d.Blocks, func(x doc.Block) ([]doc.Block, bool) {
		switch p := x.(type) {
		case doc.Paragraph:
			b.WriteString(p.Text.PlainText() + "\n")
		case doc.Heading:
			b.WriteString(p.Text.PlainText() + "\n")
		case doc.Notice:
			b.WriteString("! " + p.Text + "\n")
		case doc.Pre:
			b.WriteString(p.Text + "\n")
		}
		return nil, false
	})
	return b.String()
}

func testEnv(t *testing.T, a Adapter) Env {
	t.Helper()
	old := adapters
	adapters = []Adapter{a}
	t.Cleanup(func() { adapters = old })
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	// Never the real Downloads folder or library in tests.
	t.Setenv("W5F_BOOKS", t.TempDir())
	return Env{Fetcher: fetch.New("", "test"), DB: db, Downloads: t.TempDir()}
}

func TestOpenSerialByChapterAddress(t *testing.T) {
	fa := &fakeAdapter{chapters: 3}
	env := testEnv(t, fa)
	ctx := context.Background()
	u, _ := url.Parse("https://fake.test/story/7/c/2")
	d, err := OpenURL(ctx, env, u)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(flat(d), "Text of Chapter 2.") || d.Ref == "" || d.Next == "" || d.Prev == "" {
		t.Fatalf("chapter 2: %+v\n%s", d, flat(d))
	}
	id, ok := SerialRef(d.Ref)
	if !ok {
		t.Fatalf("ref %q", d.Ref)
	}
	next, err := Route(ctx, d.Next, env)
	if err != nil || !strings.Contains(flat(next), "Text of Chapter 3.") || next.Next != "" {
		t.Fatalf("next: %v\n%s", err, flat(next))
	}
	toc, _ := Route(ctx, fmt.Sprintf("w5f:serial/%d/toc", id), env)
	if !strings.Contains(flat(toc), "Chapter 1") || !strings.Contains(flat(toc), "Chapter 3") {
		t.Errorf("toc:\n%s", flat(toc))
	}
	SaveFromRef(env.DB, next.Ref, 0.4)
	resume, _ := Route(ctx, fmt.Sprintf("w5f:serial/%d", id), env)
	if !strings.Contains(flat(resume), "Continue") {
		t.Errorf("serial page:\n%s", flat(resume))
	}
	s, _ := env.DB.Serial(id)
	if s.Chapter != 2 || s.Pos != 0.4 {
		t.Errorf("progress: %+v", s)
	}
	cont, _ := Route(ctx, fmt.Sprintf("w5f:serial/%d/continue", id), env)
	if cont.Resume != 0.4 || !strings.Contains(flat(cont), "Chapter 3") {
		t.Errorf("continue: %v\n%s", cont.Resume, flat(cont))
	}
	if on, _ := ToggleFollow(env.DB, id); !on {
		t.Error("follow")
	}
	// Opening the same serial again within an hour uses the stored list.
	OpenURL(ctx, env, u)
	if fa.fetched != 1 {
		t.Errorf("serial fetched %d times", fa.fetched)
	}
}

func TestRefreshFailureKeepsTheStoredChapters(t *testing.T) {
	fa := &fakeAdapter{chapters: 2}
	env := testEnv(t, fa)
	ctx := context.Background()
	u, _ := url.Parse("https://fake.test/story/9")
	d, err := OpenURL(ctx, env, u)
	if err != nil || d.Title != "The Fake Serial" || !strings.Contains(flat(d), "Start reading") {
		t.Fatalf("serial page: %v\n%s", err, flat(d))
	}
	s, _ := env.DB.SerialByURL("https://fake.test/story/9")
	fa.fail, fa.chapters = true, 0
	d, err = Route(ctx, fmt.Sprintf("w5f:serial/%d/refresh", s.ID), env)
	if err != nil || !strings.Contains(flat(d), "layout may have changed") || !strings.Contains(flat(d), "Chapter 2") {
		t.Fatalf("refresh failure: %v\n%s", err, flat(d))
	}
	if chs, _ := env.DB.SerialChapters(s.ID); len(chs) != 2 {
		t.Errorf("chapters lost: %d", len(chs))
	}
	// A serial never stored: the error is returned.
	u2, _ := url.Parse("https://fake.test/story/10")
	if _, err := OpenURL(ctx, env, u2); err == nil {
		t.Error("a first open that fails must return the error")
	}
	if _, _, ok := Match(mustURL("https://example.org/story/1")); ok {
		t.Error("other hosts are not serials")
	}
}

func mustURL(s string) *url.URL { u, _ := url.Parse(s); return u }
