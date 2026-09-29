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
	"w5f/internal/store"
)

// IsTarget reports whether target belongs to Internet Fiction.
func IsTarget(target string) bool {
	return target == "w5f:fiction" || strings.HasPrefix(target, "w5f:fiction/") || strings.HasPrefix(target, "w5f:fiction?") ||
		strings.HasPrefix(target, "w5f:serial/") || target == "w5f:following" || strings.HasPrefix(target, "w5f:following/")
}

// routes added by other files (lists, searches, the FanFicFare bridge).
var extraRoutes = map[string]func(ctx context.Context, q url.Values, env Env) (*doc.Document, error){}

// Route builds the document for an Internet Fiction address.
func Route(ctx context.Context, target string, env Env) (*doc.Document, error) {
	u, err := url.Parse(target)
	if err != nil {
		return nil, err
	}
	p := strings.TrimSuffix(u.Opaque, "/")
	q := u.Query()
	if f, ok := extraRoutes[p]; ok {
		return f(ctx, q, env)
	}
	switch {
	case p == "fiction":
		return homeDoc(env)
	case p == "following":
		return followingDoc(env, "")
	case p == "following/check":
		rep := SyncFollowed(ctx, env, nil)
		return followingDoc(env, rep.String())
	case p == "serial/open":
		su, err := url.Parse(q.Get("u"))
		if err != nil || su.Host == "" {
			return nil, errors.New("serial/open needs an address")
		}
		return openURL(ctx, env, su, true)
	case strings.HasPrefix(p, "serial/"):
		parts := strings.Split(strings.TrimPrefix(p, "serial/"), "/")
		id, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return nil, errors.New("bad serial address")
		}
		s, err := env.DB.Serial(id)
		if err != nil {
			return nil, err
		}
		switch {
		case len(parts) == 1:
			return serialDoc(env, s, nil)
		case len(parts) == 2 && parts[1] == "continue":
			return chapterByID(ctx, env, id, s.Chapter, s.Pos)
		case len(parts) == 2 && parts[1] == "toc":
			return tocDoc(env, s)
		case len(parts) == 2 && parts[1] == "follow":
			if _, err := ToggleFollow(env.DB, id); err != nil {
				return nil, err
			}
			s, _ = env.DB.Serial(id)
			return serialDoc(env, s, nil)
		case len(parts) == 2 && parts[1] == "refresh":
			_, note := Refresh(ctx, env, id)
			s, _ = env.DB.Serial(id)
			return serialDoc(env, s, note)
		case len(parts) == 3 && parts[1] == "ch":
			n, _ := strconv.Atoi(parts[2])
			return chapterByID(ctx, env, id, n, -1)
		}
	}
	return nil, errors.New("unknown Internet Fiction address: " + target)
}

func link(d *doc.Document, href, text string) int {
	d.Links = append(d.Links, doc.Link{Href: href, Text: text})
	return len(d.Links)
}

func para(in ...doc.Span) doc.Paragraph { return doc.Paragraph{Text: doc.Inline(in)} }

func plain(s string, st doc.Style) doc.Span { return doc.Span{Text: s, Style: st} }

func serialHref(id int64, rest string) string {
	if rest == "" {
		return fmt.Sprintf("w5f:serial/%d", id)
	}
	return fmt.Sprintf("w5f:serial/%d/%s", id, rest)
}

// serialDoc is a serial's page: what it is, where you are, its chapters.
func serialDoc(env Env, s store.Serial, note error) (*doc.Document, error) {
	chs, err := env.DB.SerialChapters(s.ID)
	if err != nil {
		return nil, err
	}
	d := &doc.Document{Title: s.Title, URL: serialHref(s.ID, ""), Origin: "serial", Lang: "en"}
	meta := []string{}
	if s.Author != "" {
		meta = append(meta, s.Author)
	}
	meta = append(meta, siteName(s.Kind, s.URL), fmt.Sprintf("%d chapters", len(chs)))
	if s.Followed {
		meta = append(meta, "following")
	}
	d.Meta = []doc.KV{{Key: "·", Value: strings.Join(meta, " · ")}}
	if note != nil {
		d.Blocks = append(d.Blocks, doc.Notice{Kind: "warn", Text: "Chapter list not refreshed: " + note.Error()})
	}
	if s.Summary != "" {
		d.Blocks = append(d.Blocks, para(plain(s.Summary, doc.Italic)))
	}
	var act doc.Inline
	if !s.Opened.IsZero() && s.Chapter < len(chs) {
		t := fmt.Sprintf("▶ Continue — chapter %d", s.Chapter+1)
		act = append(act, doc.Span{Text: t, Style: doc.Bold, Link: link(d, serialHref(s.ID, "continue"), t)})
	} else if len(chs) > 0 {
		act = append(act, doc.Span{Text: "▶ Start reading", Style: doc.Bold, Link: link(d, serialHref(s.ID, "ch/0"), "start")})
	}
	follow := map[bool]string{true: "unfollow", false: "follow (F)"}[s.Followed]
	act = append(act, plain("   ", 0), doc.Span{Text: follow, Link: link(d, serialHref(s.ID, "follow"), follow)},
		plain("   ", 0), doc.Span{Text: "refresh", Link: link(d, serialHref(s.ID, "refresh"), "refresh")})
	if s.Kind == "ao3" {
		act = append(act, plain("   ", 0), doc.Span{Text: "save whole work (AO3 download)", Link: link(d, "w5f:fiction/ao3/save?"+url.Values{"u": {s.URL}}.Encode(), "AO3 download")})
	}
	if s.Kind != "reddit" {
		act = append(act, plain("   ", 0), doc.Span{Text: "save as EPUB (FanFicFare)", Link: link(d, "w5f:fiction/ffr?"+url.Values{"u": {s.URL}}.Encode(), "FanFicFare")})
	}
	act = append(act, plain("   ", 0), doc.Span{Text: "open on the site", Link: link(d, siteURL(s), "site")})
	d.Blocks = append(d.Blocks, para(act...))
	if s.CheckError != "" && note == nil {
		d.Blocks = append(d.Blocks, doc.Notice{Kind: "info", Text: "Last update check failed: " + s.CheckError})
	}
	d.Blocks = append(d.Blocks, doc.Heading{Level: 2, Text: doc.Inline{{Text: "Chapters"}}}, chapterList(d, s, chs, true))
	_ = env.DB.MarkSeen(s.ID)
	return d, nil
}

func siteURL(s store.Serial) string {
	if s.Kind == "reddit" {
		parts := strings.SplitN(strings.TrimPrefix(s.URL, "reddit-series:"), "/", 4)
		if len(parts) == 4 {
			return "https://www.reddit.com/user/" + parts[2] + "/submitted/"
		}
	}
	return s.URL
}

// chapterList lists chapters; newOnes marks chapters added since last seen.
func chapterList(d *doc.Document, s store.Serial, chs []store.SerialChapter, newOnes bool) doc.Block {
	var items [][]doc.Block
	for _, c := range chs {
		t := c.Title
		if t == "" {
			t = fmt.Sprintf("Chapter %d", c.N+1)
		}
		in := doc.Inline{{Text: t, Link: link(d, serialHref(s.ID, fmt.Sprintf("ch/%d", c.N)), t)}}
		if !c.Published.IsZero() {
			in = append(in, plain("  "+c.Published.Format("2006-01-02"), doc.Italic))
		}
		if newOnes && s.Followed && c.N >= s.Seen {
			in = append(in, plain("  new", doc.Bold))
		}
		if c.N == s.Chapter && !s.Opened.IsZero() {
			in = append(in, plain("  ◀ you are here", doc.Italic))
		}
		items = append(items, []doc.Block{para(in...)})
	}
	return doc.List{Ordered: true, Items: items}
}

func tocDoc(env Env, s store.Serial) (*doc.Document, error) {
	chs, err := env.DB.SerialChapters(s.ID)
	if err != nil {
		return nil, err
	}
	d := &doc.Document{Title: s.Title + " — contents", URL: serialHref(s.ID, "toc"), Origin: "serial", Lang: "en"}
	if s.Author != "" {
		d.Meta = []doc.KV{{Key: "·", Value: s.Author}}
	}
	d.Blocks = []doc.Block{para(doc.Span{Text: "serial page", Link: link(d, serialHref(s.ID, ""), "serial page")}), chapterList(d, s, chs, false)}
	return d, nil
}

// chapterDoc opens chapter n; resume < 0 means "from the top unless this is
// where the reader left off".
func chapterDoc(ctx context.Context, env Env, s store.Serial, chs []store.SerialChapter, n int, resume float64) (*doc.Document, error) {
	if len(chs) == 0 {
		return serialDoc(env, s, nil)
	}
	if n < 0 || n >= len(chs) {
		n = 0
	}
	if resume < 0 {
		resume = 0
		if !s.Opened.IsZero() && n == s.Chapter && s.Pos < 0.99 {
			resume = s.Pos
		}
	}
	c := chs[n]
	var d *doc.Document
	var err error
	if s.Kind == "reddit" && env.Reddit != nil {
		var u *url.URL
		if u, err = url.Parse(c.URL); err == nil {
			d, err = env.Reddit(ctx, u)
		}
	} else if a := adapterFor(s.Kind); a != nil {
		d, err = a.Chapter(ctx, env.Fetcher, Chapter{Title: c.Title, URL: c.URL, Published: c.Published})
	} else {
		err = fmt.Errorf("no reader for %s serials", s.Kind)
	}
	if err != nil {
		d = &doc.Document{Blocks: []doc.Block{doc.Notice{Kind: "warn", Text: err.Error()}}}
		d.Blocks = append(d.Blocks, para(doc.Span{Text: "→ open this chapter on the site", Link: link(d, c.URL, "site")}))
	}
	d.Title = s.Title
	d.URL = serialHref(s.ID, fmt.Sprintf("ch/%d", n))
	d.Origin = "serial"
	if d.Lang == "" {
		d.Lang = "en"
	}
	meta := []string{}
	if s.Author != "" {
		meta = append(meta, s.Author)
	}
	part := fmt.Sprintf("chapter %d/%d", n+1, len(chs))
	if c.Title != "" {
		part += " · " + c.Title
	}
	d.Meta = append([]doc.KV{{Key: "·", Value: strings.Join(append(meta, part), " · ")}}, d.Meta...)
	d.Ref = fmt.Sprintf("serial:%d:%d", s.ID, n)
	d.Resume = resume
	var nav doc.Inline
	if n > 0 {
		d.Prev = serialHref(s.ID, fmt.Sprintf("ch/%d", n-1))
		nav = append(nav, doc.Span{Text: "‹ previous", Link: link(d, d.Prev, "previous chapter")}, plain("   ", 0))
	}
	nav = append(nav, doc.Span{Text: "contents", Link: link(d, serialHref(s.ID, "toc"), "contents")}, plain("   ", 0),
		doc.Span{Text: "serial page", Link: link(d, serialHref(s.ID, ""), "serial page")})
	if n < len(chs)-1 {
		d.Next = serialHref(s.ID, fmt.Sprintf("ch/%d", n+1))
		nav = append(nav, plain("   ", 0), doc.Span{Text: "next chapter ›", Link: link(d, d.Next, "next chapter")})
	} else {
		end := "   — latest chapter —"
		if !s.Followed {
			end += " press F to follow"
		}
		nav = append(nav, plain(end, doc.Italic))
	}
	d.Blocks = append(d.Blocks, doc.Rule{}, para(nav...))
	d.Renumber()
	_ = env.DB.SaveSerialProgress(s.ID, n, resume)
	return d, nil
}

// ago is a short age ("3 h ago").
func ago(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d h ago", int(d.Hours()))
	}
	return fmt.Sprintf("%d days ago", int(d.Hours()/24))
}
