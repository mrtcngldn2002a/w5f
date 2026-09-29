// Package fiction reads internet fiction like books: web serials (Royal
// Road, AO3, WordPress), forum stories (XenForo threadmarks) and Reddit
// series, with chapters, contents, progress and follow-for-updates.
package fiction

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"w5f/internal/doc"
	"w5f/internal/fetch"
	"w5f/internal/store"
)

// Serial is what an adapter reads from a site.
type Serial struct {
	Kind, URL, Title, Author, Summary string
	Chapters                          []Chapter // reading order
}

// Chapter is one chapter of a serial.
type Chapter struct {
	Title, URL string
	Published  time.Time
}

// Adapter reads one kind of site.
type Adapter interface {
	Kind() string
	// Match returns the canonical serial address for any address of a
	// serial or one of its chapters.
	Match(u *url.URL) (serial string, ok bool)
	Serial(ctx context.Context, f *fetch.Fetcher, serial string) (*Serial, error)
	Chapter(ctx context.Context, f *fetch.Fetcher, ch Chapter) (*doc.Document, error)
}

// keyer lets an adapter say when two chapter addresses are the same chapter
// (e.g. a forum post reached through different paths).
type keyer interface{ Key(u *url.URL) string }

// adapters are the site readers; each adapter file adds its own.
var adapters []Adapter

// errNotSerial means the address is on a known site but is not a serial
// (e.g. a forum thread without threadmarks): it opens as a normal page.
var errNotSerial = errors.New("not a serial")

// IsNotSerial reports errNotSerial.
func IsNotSerial(err error) bool { return errors.Is(err, errNotSerial) }

// Env carries what the fiction pages need.
type Env struct {
	Fetcher *fetch.Fetcher
	DB      *store.DB
	// Mature shows adult forums (Questionable Questing) on the home page.
	Mature bool
	// Reddit loads a Reddit address with the owner's session.
	Reddit func(ctx context.Context, u *url.URL) (*doc.Document, error)
	// Downloads is the browser's download folder (AO3 EPUBs are imported
	// from it); empty means ~/Downloads.
	Downloads string
}

// freshFor is how long a stored chapter list is used without asking the site.
const freshFor = time.Hour

// Match finds the adapter for an address.
func Match(u *url.URL) (Adapter, string, bool) {
	for _, a := range adapters {
		if s, ok := a.Match(u); ok {
			return a, s, true
		}
	}
	return nil, "", false
}

func adapterFor(kind string) Adapter {
	for _, a := range adapters {
		if a.Kind() == kind {
			return a
		}
	}
	return nil
}

// chapterKey compares chapter addresses: the adapter's key, else host
// (without www.) and path without a trailing slash.
func chapterKey(a Adapter, raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if k, ok := a.(keyer); ok {
		if s := k.Key(u); s != "" {
			return s
		}
	}
	return strings.TrimPrefix(strings.ToLower(u.Host), "www.") + strings.TrimSuffix(u.Path, "/")
}

// refresh reads the serial from the site and stores it. With a stored
// chapter list, a failure leaves the list as it was and is returned as a
// note together with the stored serial.
func refresh(ctx context.Context, env Env, a Adapter, serialURL string) (store.Serial, error, error) {
	old, known := env.DB.SerialByURL(serialURL)
	var s *Serial
	var err error
	if c, ok := a.(continuer); ok && known && old.Chapters > 0 {
		stored, serr := env.DB.SerialChapters(old.ID)
		if serr != nil {
			return store.Serial{}, nil, serr
		}
		chs := make([]Chapter, 0, len(stored))
		for _, x := range stored {
			chs = append(chs, Chapter{Title: x.Title, URL: x.URL, Published: x.Published})
		}
		s, err = c.SerialFrom(ctx, env.Fetcher, serialURL, chs)
	} else {
		s, err = a.Serial(ctx, env.Fetcher, serialURL)
	}
	if err == nil && len(s.Chapters) == 0 {
		err = fmt.Errorf("%s: no chapters found — the site layout may have changed", siteName(a.Kind(), serialURL))
	}
	if err != nil {
		if known && old.Chapters > 0 {
			_ = env.DB.SetChecked(old.ID, err.Error())
			return old, err, nil
		}
		return store.Serial{}, nil, err
	}
	id, err := env.DB.UpsertSerial(store.Serial{Kind: a.Kind(), URL: serialURL, Title: s.Title, Author: s.Author, Summary: s.Summary})
	if err != nil {
		return store.Serial{}, nil, err
	}
	chs := make([]store.SerialChapter, 0, len(s.Chapters))
	for _, c := range s.Chapters {
		chs = append(chs, store.SerialChapter{Title: c.Title, URL: c.URL, Published: c.Published})
	}
	if _, err := env.DB.MergeChapters(id, chs); err != nil {
		return store.Serial{}, nil, err
	}
	_ = env.DB.SetChecked(id, "")
	st, err := env.DB.Serial(id)
	if err == nil && !known {
		_ = env.DB.MarkSeen(id) // a serial just added has nothing new yet
	}
	return st, nil, err
}

// load returns the stored serial when its list is fresh, else refreshes it.
func load(ctx context.Context, env Env, a Adapter, serialURL string) (store.Serial, error, error) {
	if s, ok := env.DB.SerialByURL(serialURL); ok && s.Chapters > 0 && time.Since(s.Checked) < freshFor {
		return s, nil, nil
	}
	if env.Fetcher != nil && env.Fetcher.Offline {
		if s, ok := env.DB.SerialByURL(serialURL); ok && s.Chapters > 0 {
			return s, nil, nil
		}
	}
	return refresh(ctx, env, a, serialURL)
}

// Refresh re-reads a stored serial from its site; it returns how many
// chapters were added.
func Refresh(ctx context.Context, env Env, id int64) (int, error) {
	s, err := env.DB.Serial(id)
	if err != nil {
		return 0, err
	}
	a := adapterFor(s.Kind)
	if a == nil {
		return 0, fmt.Errorf("no reader for %s serials", s.Kind)
	}
	before := s.Chapters
	st, note, err := refresh(ctx, env, a, s.URL)
	if err != nil {
		return 0, err
	}
	if note != nil {
		return 0, note
	}
	return st.Chapters - before, nil
}

// OpenURL opens an address of a serial: the chapter when it is one, else
// the serial page.
func OpenURL(ctx context.Context, env Env, u *url.URL) (*doc.Document, error) {
	return openURL(ctx, env, u, false)
}

// KnownChapter reports an address stored as a chapter (e.g. of a WordPress
// serial), so following it from a page opens it inside its serial.
func KnownChapter(db *store.DB, u *url.URL) bool {
	_, _, ok := db.SerialByChapterURL(u.String())
	return ok
}

// openURL: explicit means the owner asked for this address as a serial.
func openURL(ctx context.Context, env Env, u *url.URL, explicit bool) (*doc.Document, error) {
	a, serialURL, ok := Match(u)
	if !ok {
		if id, n, ok := env.DB.SerialByChapterURL(u.String()); ok {
			return chapterByID(ctx, env, id, n, -1)
		}
		if !explicit {
			return nil, fmt.Errorf("%s is not a serial W5F can read", u.Host)
		}
		// Asked for by the owner (g → serial, presets): an independent
		// serial read from its contents page or its next-chapter links.
		a, serialURL = wordPress{}, u.String()
	}
	s, note, err := load(ctx, env, a, serialURL)
	var adult errAdult
	if errors.As(err, &adult) {
		return adultDoc(adult), nil
	}
	if err != nil {
		return nil, err
	}
	if u.String() != serialURL && chapterKey(a, u.String()) != chapterKey(a, serialURL) {
		chs, err := env.DB.SerialChapters(s.ID)
		if err != nil {
			return nil, err
		}
		want := chapterKey(a, u.String())
		for _, c := range chs {
			if chapterKey(a, c.URL) == want {
				d, err := chapterDoc(ctx, env, s, chs, c.N, -1)
				if d != nil && note != nil {
					d.Blocks = append([]doc.Block{doc.Notice{Kind: "warn", Text: "Chapter list not refreshed: " + note.Error()}}, d.Blocks...)
				}
				return d, err
			}
		}
	}
	return serialDoc(env, s, note)
}

func chapterByID(ctx context.Context, env Env, id int64, n int, resume float64) (*doc.Document, error) {
	s, err := env.DB.Serial(id)
	if err != nil {
		return nil, err
	}
	chs, err := env.DB.SerialChapters(id)
	if err != nil {
		return nil, err
	}
	return chapterDoc(ctx, env, s, chs, n, resume)
}

// SerialRef parses a chapter's "serial:<id>:<n>" reference.
func SerialRef(ref string) (int64, bool) {
	if !strings.HasPrefix(ref, "serial:") {
		return 0, false
	}
	id, err := strconv.ParseInt(strings.SplitN(strings.TrimPrefix(ref, "serial:"), ":", 2)[0], 10, 64)
	return id, err == nil
}

// SerialOfPage finds the serial a page belongs to when it is not a chapter:
// a serial's own page or contents, or a Reddit post of a detected series
// (which is stored as a serial on first use).
func SerialOfPage(db *store.DB, d *doc.Document) (int64, bool) {
	if strings.HasPrefix(d.URL, "w5f:serial/") {
		id, err := strconv.ParseInt(strings.Split(strings.TrimPrefix(d.URL, "w5f:serial/"), "/")[0], 10, 64)
		return id, err == nil
	}
	if strings.HasPrefix(d.Ref, "rseries:") {
		return storeRedditSeries(db, d)
	}
	return 0, false
}

// SaveFromRef records reading progress for a "serial:<id>:<n>" page.
func SaveFromRef(db *store.DB, ref string, frac float64) {
	parts := strings.Split(strings.TrimPrefix(ref, "serial:"), ":")
	if !strings.HasPrefix(ref, "serial:") || len(parts) != 2 {
		return
	}
	id, err1 := strconv.ParseInt(parts[0], 10, 64)
	n, err2 := strconv.Atoi(parts[1])
	if err1 == nil && err2 == nil {
		_ = db.SaveSerialProgress(id, n, frac)
	}
}

// ToggleFollow follows or unfollows a serial; it returns the new state.
func ToggleFollow(db *store.DB, id int64) (bool, error) {
	s, err := db.Serial(id)
	if err != nil {
		return false, err
	}
	if err := db.SetFollowed(id, !s.Followed); err != nil {
		return false, err
	}
	if !s.Followed {
		_ = db.MarkSeen(id)
	}
	return !s.Followed, nil
}

// siteName names a serial's site for pages and errors.
func siteName(kind, raw string) string {
	switch kind {
	case "royalroad":
		return "Royal Road"
	case "ao3":
		return "AO3"
	case "reddit":
		return "Reddit"
	}
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		h := strings.TrimPrefix(u.Hostname(), "www.")
		switch {
		case strings.Contains(h, "spacebattles"):
			return "SpaceBattles"
		case strings.Contains(h, "sufficientvelocity"):
			return "Sufficient Velocity"
		case strings.Contains(h, "questionablequesting"):
			return "Questionable Questing"
		}
		return h
	}
	return kind
}
