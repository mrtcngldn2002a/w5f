package reddit

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"w5f/internal/doc"
	"w5f/internal/fetch"
	"w5f/internal/htmlconv"
)

const site = "https://www.reddit.com"

// IsReddit reports whether u is a Reddit address.
func IsReddit(u *url.URL) bool {
	h := strings.ToLower(u.Hostname())
	return h == "reddit.com" || strings.HasSuffix(h, ".reddit.com")
}

// Canonical rewrites old./m./np. Reddit hosts to www.reddit.com.
func Canonical(u *url.URL) *url.URL {
	n := *u
	n.Scheme, n.Host = "https", "www.reddit.com"
	return &n
}

// Communities shown on the Reddit start page instead of the algorithmic feed.
var Communities = []struct{ Group, Name string }{
	{"Fiction", "nosleep"}, {"Fiction", "shortscarystories"}, {"Fiction", "LibraryOfShadows"},
	{"Fiction", "TheCrypticCompendium"}, {"Fiction", "Odd_directions"}, {"Fiction", "HFY"},
	{"Fiction", "redditserials"}, {"Fiction", "WritingPrompts"}, {"Fiction", "Cryosleep"},
	{"Reading & ideas", "printSF"}, {"Reading & ideas", "WeirdLit"}, {"Reading & ideas", "books"},
	{"Reading & ideas", "AskHistorians"}, {"Reading & ideas", "philosophy"}, {"Reading & ideas", "Gnostic"},
	{"Reading & ideas", "folklore"}, {"Reading & ideas", "archaeology"}, {"Reading & ideas", "roguelikes"},
	{"Reading & ideas", "scp"},
}

// Load reads a Reddit address: through old.reddit.com with the user's own
// session when one is stored, through Redlib when one is configured, and
// otherwise returns a page explaining how to connect Reddit.
func Load(ctx context.Context, f *fetch.Fetcher, u *url.URL, reload bool) (*doc.Document, error) {
	u = Canonical(u)
	session := LoadSession()
	if u.Path == "" || u.Path == "/" {
		if session == "" && !redlibEnabled() {
			return setupPage(false), nil
		}
		return startPage(), nil
	}
	if session != "" {
		d, err := loadOld(ctx, f, u, session, reload)
		if errors.Is(err, ErrSessionExpired) {
			return setupPage(true), nil
		}
		return d, err
	}
	if redlibEnabled() {
		return loadRedlib(ctx, f, u, reload)
	}
	return setupPage(false), nil
}

// redlibEnabled: Redlib is opt-in (W5F_REDLIB_URL, or W5F_REDLIB=1 for the
// local binary), since its released version is currently blocked by Reddit.
func redlibEnabled() bool {
	if v := os.Getenv("W5F_REDLIB_URL"); v != "" {
		Configure(Config{URL: v})
		return true
	}
	return cfg.URL != "" || os.Getenv("W5F_REDLIB") == "1"
}

// setupPage explains how to connect Reddit with the user's own session.
func setupPage(expired bool) *doc.Document {
	d := &doc.Document{Title: "Reddit — connect your account", URL: site + "/", Origin: "local", Lang: "en"}
	if expired {
		d.Blocks = append(d.Blocks, doc.Notice{Kind: "warn", Text: "Reddit did not accept the stored session (it expired or you logged out). Paste a fresh one."})
	}
	p := func(parts ...doc.Span) { d.Blocks = append(d.Blocks, doc.Paragraph{Text: parts}) }
	p(doc.Span{Text: "Reddit shows its pages to terminal readers only when you are logged in. W5F reads "},
		doc.Span{Text: "old.reddit.com", Style: doc.Code},
		doc.Span{Text: " — the classic, JavaScript-free interface — with your own login session. Once connected you can browse freely: nested comments, every sort order, search, user pages and old posts."})
	d.Blocks = append(d.Blocks, doc.Heading{Level: 2, Text: doc.Inline{{Text: "Connect from your browser (Chromium or Firefox)"}}})
	d.Blocks = append(d.Blocks, doc.List{Ordered: true, Items: [][]doc.Block{
		{doc.Paragraph{Text: doc.Inline{{Text: "Press "}, {Text: "g", Style: doc.Bold}, {Text: ", type "}, {Text: "browser old.reddit.com/login", Style: doc.Code}, {Text: " and log in there."}}}},
		{doc.Paragraph{Text: doc.Inline{{Text: "Back in W5F: "}, {Text: "g", Style: doc.Bold}, {Text: " → "}, {Text: "reddit-login browser", Style: doc.Code}, {Text: ". W5F takes the session from Chromium's or Firefox's profile (if it says it is not there yet, wait half a minute: Chromium writes new cookies to disk in a while)."}}}},
	}})
	d.Blocks = append(d.Blocks, doc.Heading{Level: 2, Text: doc.Inline{{Text: "Or paste it (once, takes a minute)"}}})
	d.Blocks = append(d.Blocks, doc.List{Ordered: true, Items: [][]doc.Block{
		{doc.Paragraph{Text: doc.Inline{{Text: "In a desktop browser, log in to reddit.com."}}}},
		{doc.Paragraph{Text: doc.Inline{{Text: "Press "}, {Text: "F12", Style: doc.Bold}, {Text: " → "}, {Text: "Application", Style: doc.Bold}, {Text: " (Firefox: "}, {Text: "Storage", Style: doc.Bold}, {Text: ") → Cookies → https://www.reddit.com."}}}},
		{doc.Paragraph{Text: doc.Inline{{Text: "Find the cookie named "}, {Text: "reddit_session", Style: doc.Code}, {Text: " and copy its value."}}}},
		{doc.Paragraph{Text: doc.Inline{{Text: "In W5F press "}, {Text: "g", Style: doc.Bold}, {Text: ", type "}, {Text: "reddit-login", Style: doc.Code}, {Text: " and press enter; paste the value (it stays hidden) and press enter again. That's it — W5F stays connected on every start."}}}},
	}})
	d.Blocks = append(d.Blocks, doc.Heading{Level: 2, Text: doc.Inline{{Text: "Good to know"}}})
	d.Blocks = append(d.Blocks, doc.List{Items: [][]doc.Block{
		{doc.Paragraph{Text: doc.Inline{{Text: "The session works like a password for your Reddit account: never share it. W5F keeps it only in "}, {Text: SessionPath(), Style: doc.Code}, {Text: " (readable by you alone) and sends it only to old.reddit.com."}}}},
		{doc.Paragraph{Text: doc.Inline{{Text: "W5F only reads; it never posts, votes or changes anything."}}}},
		{doc.Paragraph{Text: doc.Inline{{Text: "To disconnect: "}, {Text: "g", Style: doc.Bold}, {Text: " → "}, {Text: "reddit-logout", Style: doc.Code}, {Text: ". Logging out of Reddit in the browser also ends the session."}}}},
	}})
	return d
}

func loadRedlib(ctx context.Context, f *fetch.Fetcher, u *url.URL, reload bool) (*doc.Document, error) {
	base, err := Base(ctx)
	if err != nil {
		return nil, err
	}
	ru, _ := url.Parse(base + u.EscapedPath())
	ru.RawQuery = u.RawQuery
	resp, err := f.Get(ctx, ru, fetch.Options{Revalidate: reload})
	if err != nil {
		return nil, err
	}
	gq, err := goquery.NewDocumentFromReader(bytes.NewReader(resp.Body))
	if err != nil {
		return nil, err
	}
	d := &doc.Document{URL: u.String(), Origin: "live", Lang: "en"}
	if resp.FromCache {
		d.Origin = "cache"
	}
	switch {
	case gq.Find("div.post.highlighted").Length() > 0:
		postPage(gq, d, u)
	case gq.Find("div.post").Length() > 0:
		listing(gq, d, u)
	default:
		msg := strings.TrimSpace(gq.Find("#error h1, #error h3, h1").First().Text())
		if msg == "" {
			msg = "Nothing to show here."
		}
		d.Title = "Reddit"
		d.Blocks = []doc.Block{doc.Notice{Kind: "info", Text: "Reddit (via Redlib): " + collapse(msg)}}
	}
	fixLinks(d, base)
	d.Renumber()
	return d, nil
}

func startPage() *doc.Document {
	d := &doc.Document{Title: "Reddit", URL: site + "/", Origin: "local", Lang: "en"}
	d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{
		{Text: "W5F does not show Reddit's algorithmic front page. Pick a community, press "},
		{Text: "g", Style: doc.Bold}, {Text: " and type "}, {Text: "r/name", Style: doc.Code},
		{Text: ", or search with "}, {Text: "reddit <words>", Style: doc.Code}, {Text: "."},
	}})
	group := ""
	var items [][]doc.Block
	flush := func() {
		if len(items) > 0 {
			d.Blocks = append(d.Blocks, doc.Heading{Level: 2, Text: doc.Inline{{Text: group}}}, doc.List{Items: items})
		}
		items = nil
	}
	for _, c := range Communities {
		if c.Group != group {
			flush()
			group = c.Group
		}
		d.Links = append(d.Links, doc.Link{Href: site + "/r/" + c.Name + "/", Text: "r/" + c.Name})
		items = append(items, []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: "r/" + c.Name, Link: len(d.Links)}}}})
	}
	flush()
	d.Links = append(d.Links, doc.Link{Href: site + "/r/popular/", Text: "r/popular"})
	d.Blocks = append(d.Blocks, doc.Rule{}, doc.Paragraph{Text: doc.Inline{{Text: "Everything else: "}, {Text: "r/popular", Link: len(d.Links)}}})
	return d
}

func addLink(d *doc.Document, href, text string) int {
	d.Links = append(d.Links, doc.Link{Href: href, Text: text})
	return len(d.Links)
}

func abs(u *url.URL, href string) string {
	r, err := url.Parse(href)
	if err != nil {
		return href
	}
	b := *u
	b.Scheme, b.Host = "https", "www.reddit.com"
	return b.ResolveReference(r).String()
}

// listing renders a subreddit, search or user page: sort bar, posts, paging.
func listing(gq *goquery.Document, d *doc.Document, u *url.URL) {
	d.Title = pageTitle(gq, u)
	sub := communityOf(u.Path)
	if sub != "" {
		d.Blocks = append(d.Blocks, sortBar(d, sub, u))
	}
	var items [][]doc.Block
	gq.Find("div.post").Each(func(_ int, p *goquery.Selection) {
		items = append(items, postSummary(p, d, u))
	})
	if len(items) > 0 {
		d.Blocks = append(d.Blocks, doc.List{Ordered: false, Items: items})
	}
	var nav doc.Inline
	if a := gq.Find("a[accesskey=P]").First(); a.Length() > 0 {
		nav = append(nav, doc.Span{Text: "‹ previous", Link: addLink(d, abs(u, a.AttrOr("href", "")), "previous")}, doc.Span{Text: "   "})
	}
	if a := gq.Find("a[accesskey=N]").First(); a.Length() > 0 {
		nav = append(nav, doc.Span{Text: "next page ›", Link: addLink(d, abs(u, a.AttrOr("href", "")), "next")})
	}
	if len(nav) > 0 {
		d.Blocks = append(d.Blocks, doc.Rule{}, doc.Paragraph{Text: nav})
	}
}

func sortBar(d *doc.Document, sub string, u *url.URL) doc.Block {
	cur := strings.Trim(strings.TrimPrefix(strings.TrimPrefix(u.Path, "/r/"+sub), "/"), "/")
	if cur == "" {
		cur = "hot"
	}
	t := u.Query().Get("t")
	var in doc.Inline
	add := func(label, path string, active bool) {
		if len(in) > 0 {
			in = append(in, doc.Span{Text: " · "})
		}
		if active {
			in = append(in, doc.Span{Text: label, Style: doc.Bold})
			return
		}
		in = append(in, doc.Span{Text: label, Link: addLink(d, site+path, label)})
	}
	for _, s := range []string{"hot", "new", "rising"} {
		add(s, "/r/"+sub+"/"+s+"/", cur == s)
	}
	for _, tf := range []string{"week", "month", "year", "all"} {
		add("top "+tf, "/r/"+sub+"/top/?t="+tf, cur == "top" && t == tf)
	}
	return doc.Paragraph{Text: in}
}

// postSummary is one post in a listing.
func postSummary(p *goquery.Selection, d *doc.Document, u *url.URL) []doc.Block {
	a := p.Find(".post_title > a").Last()
	title := collapse(a.Text())
	var head doc.Inline
	if flair := collapse(p.Find(".post_title .post_flair").First().Text()); flair != "" {
		head = append(head, doc.Span{Text: "[" + flair + "] ", Style: doc.Italic})
	}
	head = append(head, doc.Span{Text: title, Style: doc.Bold, Link: addLink(d, abs(u, a.AttrOr("href", "")), title)})
	if p.Find(".post_title .nsfw").Length() > 0 {
		head = append(head, doc.Span{Text: " NSFW", Style: doc.Italic})
	}
	out := []doc.Block{doc.Paragraph{Text: head}}
	out = append(out, doc.Paragraph{Text: doc.Inline{{Text: byline(p), Style: doc.Italic}}})
	if prev := collapse(p.Find(".post_body").First().Text()); prev != "" {
		if r := []rune(prev); len(r) > 280 {
			prev = string(r[:277]) + "…"
		}
		out = append(out, doc.Paragraph{Text: doc.Inline{{Text: prev}}})
	}
	return out
}

func byline(p *goquery.Selection) string {
	parts := []string{}
	if s := collapse(p.Find(".post_subreddit").First().Text()); s != "" {
		parts = append(parts, s)
	}
	if s := collapse(p.Find(".post_author").First().Text()); s != "" {
		parts = append(parts, s)
	}
	if s := collapse(p.Find(".created").First().Text()); s != "" {
		parts = append(parts, s)
	}
	score := collapse(strings.TrimSuffix(collapse(p.Find(".post_score").First().Text()), "Upvotes"))
	if score != "" && score != "•" {
		parts = append(parts, "▲"+score)
	}
	if s := collapse(p.Find(".post_comments").First().Text()); s != "" {
		parts = append(parts, s)
	}
	return strings.Join(parts, " · ")
}

// postPage renders a post with its comment tree.
func postPage(gq *goquery.Document, d *doc.Document, u *url.URL) {
	p := gq.Find("div.post.highlighted").First()
	titleSel := p.Find(".post_title").First().Clone()
	titleSel.Find(".post_flair, small").Remove()
	d.Title = collapse(titleSel.Text())
	if flair := collapse(p.Find(".post_title .post_flair").First().Text()); flair != "" {
		d.Meta = append(d.Meta, doc.KV{Key: "flair", Value: flair})
	}
	d.Meta = append(d.Meta, doc.KV{Key: "", Value: byline(p)})
	for i := range d.Meta {
		if d.Meta[i].Key == "" {
			d.Meta[i].Key = "·"
		}
	}
	if img := p.Find(".post_media_content a").First(); img.Length() > 0 {
		d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: "▒▒ media: ", Style: doc.Italic},
			{Text: "open image/video", Link: addLink(d, abs(u, img.AttrOr("href", "")), "media")}}})
	}
	if link := p.Find("a.post_thumbnail").First(); link.Length() > 0 {
		href := link.AttrOr("href", "")
		if !strings.Contains(href, "/comments/") {
			dom := collapse(link.Find("span").Last().Text())
			d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: "→ linked page: ", Style: doc.Italic},
				{Text: dom, Link: addLink(d, abs(u, href), dom)}}})
		}
	}
	if body := p.Find(".post_body").First(); body.Length() > 0 {
		h, _ := body.Html()
		d.Blocks = append(d.Blocks, htmlconv.Fragment(d, h, u.String())...)
	}
	// Comments.
	count := collapse(gq.Find("#comment_count").Clone().Children().Remove().End().Text())
	threads := gq.Find("div.thread")
	if threads.Length() == 0 {
		return
	}
	if count == "" {
		count = strconv.Itoa(threads.Length()) + " threads"
	}
	d.Blocks = append(d.Blocks, doc.Rule{}, doc.Heading{Level: 2, Text: doc.Inline{{Text: count}}}, commentSort(d, u))
	threads.Each(func(_ int, t *goquery.Selection) {
		t.ChildrenFiltered("div.comment").Each(func(_ int, c *goquery.Selection) {
			d.Blocks = append(d.Blocks, comment(c, d, u, 0)...)
		})
		t.ChildrenFiltered("a.deeper_replies").Each(func(_ int, a *goquery.Selection) {
			d.Blocks = append(d.Blocks, moreLink(a, d, u))
		})
	})
}

func commentSort(d *doc.Document, u *url.URL) doc.Block {
	cur := u.Query().Get("sort")
	if cur == "" {
		cur = "confidence"
	}
	labels := map[string]string{"confidence": "best", "top": "top", "new": "new", "old": "old", "controversial": "controversial"}
	var in doc.Inline
	for _, s := range []string{"confidence", "top", "new", "old", "controversial"} {
		if len(in) > 0 {
			in = append(in, doc.Span{Text: " · "})
		}
		if s == cur {
			in = append(in, doc.Span{Text: labels[s], Style: doc.Bold})
			continue
		}
		n := *u
		q := n.Query()
		q.Set("sort", s)
		n.RawQuery = q.Encode()
		in = append(in, doc.Span{Text: labels[s], Link: addLink(d, n.String(), labels[s])})
	}
	return doc.Paragraph{Text: in}
}

// comment renders one comment and, folded, its replies.
func comment(c *goquery.Selection, d *doc.Document, u *url.URL, depth int) []doc.Block {
	right := c.ChildrenFiltered("details.comment_right").First()
	summary := right.ChildrenFiltered("summary").First()
	author := collapse(summary.Find(".comment_author").First().Text())
	when := collapse(summary.Find(".created").First().Text())
	score := collapse(c.ChildrenFiltered(".comment_left").Find(".comment_score").First().Text())
	head := doc.Inline{{Text: author, Style: doc.Bold}}
	if summary.Find(".comment_author.op").Length() > 0 {
		head = append(head, doc.Span{Text: " (OP)", Style: doc.Italic})
	}
	meta := []string{}
	if when != "" {
		meta = append(meta, when)
	}
	if score != "" && score != "•" {
		meta = append(meta, "▲"+score)
	}
	if len(meta) > 0 {
		head = append(head, doc.Span{Text: " · " + strings.Join(meta, " · "), Style: doc.Italic})
	}
	out := []doc.Block{doc.Paragraph{Text: head}}
	if body := right.ChildrenFiltered(".comment_body, .comment_body_filtered").First(); body.Length() > 0 {
		h, _ := body.Html()
		out = append(out, htmlconv.Fragment(d, h, u.String())...)
	}
	replies := right.ChildrenFiltered("blockquote.replies").First()
	var sub []doc.Block
	n := 0
	replies.Children().Each(func(_ int, r *goquery.Selection) {
		switch {
		case r.Is("div.comment"):
			n++
			if len(sub) > 0 {
				sub = append(sub, doc.Rule{})
			}
			sub = append(sub, comment(r, d, u, depth+1)...)
		case r.Is("a.deeper_replies"):
			sub = append(sub, moreLink(r, d, u))
		}
	})
	if len(sub) > 0 {
		label := fmt.Sprintf("%d replies", n)
		if n == 1 {
			label = "1 reply"
		} else if n == 0 {
			label = "more replies"
		}
		// Top-level threads show their first level of replies; deeper levels
		// start folded so long threads stay navigable.
		out = append(out, doc.Collapsible{Show: label, Hide: "hide " + label, Open: depth == 0,
			Blocks: []doc.Block{doc.Quote{Blocks: sub}}})
	}
	return out
}

func moreLink(a *goquery.Selection, d *doc.Document, u *url.URL) doc.Block {
	text := collapse(a.Text())
	text = strings.TrimSpace(strings.TrimPrefix(text, "→"))
	return doc.Paragraph{Text: doc.Inline{{Text: "→ " + text, Link: addLink(d, abs(u, a.AttrOr("href", "")), text)}}}
}

func pageTitle(gq *goquery.Document, u *url.URL) string {
	t := collapse(gq.Find("title").First().Text())
	t = strings.TrimSuffix(t, " - Redlib")
	if t == "" || strings.EqualFold(t, "redlib") {
		return strings.Trim(u.Path, "/")
	}
	return t
}

func communityOf(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 2 && parts[0] == "r" && !strings.Contains(parts[1], "+") {
		if len(parts) == 2 || parts[2] == "hot" || parts[2] == "new" || parts[2] == "top" || parts[2] == "rising" || parts[2] == "controversial" {
			return parts[1]
		}
	}
	return ""
}

// fixLinks turns Redlib-relative addresses back into Reddit ones and maps
// Redlib's media proxy paths to Reddit's media hosts.
func fixLinks(d *doc.Document, base string) {
	bu, _ := url.Parse(base)
	for i, l := range d.Links {
		lu, err := url.Parse(l.Href)
		if err != nil {
			continue
		}
		if (bu != nil && lu.Host == bu.Host) || (lu.Host != "www.reddit.com" && IsReddit(lu)) {
			lu.Scheme, lu.Host = "https", "www.reddit.com"
		}
		if lu.Host == "www.reddit.com" {
			switch {
			case strings.HasPrefix(lu.Path, "/img/"):
				lu.Host, lu.Path = "i.redd.it", strings.TrimPrefix(lu.Path, "/img")
			case strings.HasPrefix(lu.Path, "/preview/pre/"):
				lu.Host, lu.Path = "preview.redd.it", strings.TrimPrefix(lu.Path, "/preview/pre")
			case strings.HasPrefix(lu.Path, "/preview/external-pre/"):
				lu.Host, lu.Path = "external-preview.redd.it", strings.TrimPrefix(lu.Path, "/preview/external-pre")
			case strings.HasPrefix(lu.Path, "/vid/"):
				lu.Host, lu.Path = "v.redd.it", strings.TrimPrefix(lu.Path, "/vid")
			}
		}
		d.Links[i].Href = lu.String()
	}
}

func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }
