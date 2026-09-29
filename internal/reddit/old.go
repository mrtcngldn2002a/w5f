package reddit

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"w5f/internal/doc"
	"w5f/internal/fetch"
	"w5f/internal/htmlconv"
)

// ErrSessionExpired means old.reddit sent us to the login page or a browser
// check: the stored cookie is missing, expired or rejected.
var ErrSessionExpired = errors.New("reddit session expired or rejected")

// oldBase is old.reddit's address (a variable so tests can use a local server).
var oldBase = "https://old.reddit.com"

// loadOld reads a Reddit address through old.reddit.com with the user's own
// session. old.reddit renders everything on the server — listings, posts and
// the full comment tree — so no JavaScript is involved.
func loadOld(ctx context.Context, f *fetch.Fetcher, u *url.URL, session string, reload bool) (*doc.Document, error) {
	d, _, err := loadOldInfo(ctx, f, u, session, reload)
	return d, err
}

// loadOldInfo is loadOld that also describes a post page.
func loadOldInfo(ctx context.Context, f *fetch.Fetcher, u *url.URL, session string, reload bool) (*doc.Document, *PostInfo, error) {
	ob, _ := url.Parse(oldBase)
	ou := *u
	ou.Scheme, ou.Host = ob.Scheme, ob.Host
	hdr := http.Header{}
	hdr.Set("Cookie", "reddit_session="+session)
	resp, err := f.Get(ctx, &ou, fetch.Options{Revalidate: reload, Header: hdr})
	if err != nil {
		return nil, nil, err
	}
	if strings.HasPrefix(resp.URL.Path, "/login") || bytes.Contains(resp.Body, []byte(`name="js_challenge"`)) {
		return nil, nil, ErrSessionExpired
	}
	gq, err := goquery.NewDocumentFromReader(bytes.NewReader(resp.Body))
	if err != nil {
		return nil, nil, err
	}
	d := &doc.Document{URL: u.String(), Origin: "live", Lang: "en"}
	if resp.FromCache {
		d.Origin = "cache"
	}
	var info *PostInfo
	switch {
	case gq.Find("div.commentarea").Length() > 0:
		info = postInfo(gq, u)
		oldPost(gq, d, u)
	case gq.Find("div.search-result").Length() > 0:
		oldSearch(gq, d, u)
	default:
		oldListing(gq, d, u)
	}
	if len(d.Blocks) == 0 {
		d.Blocks = []doc.Block{doc.Notice{Kind: "info", Text: "Nothing to show here (the page may be empty, private or banned)."}}
	}
	fixLinks(d, oldBase)
	d.Renumber()
	return d, info, nil
}

func oldListing(gq *goquery.Document, d *doc.Document, u *url.URL) {
	d.Title = oldTitle(gq, u)
	if sub := communityOf(u.Path); sub != "" {
		d.Blocks = append(d.Blocks, sortBar(d, sub, u))
	}
	var items [][]doc.Block
	gq.Find("#siteTable > div.thing").Each(func(_ int, t *goquery.Selection) {
		if t.HasClass("promoted") || t.HasClass("promotedlink") || t.AttrOr("data-promoted", "") == "true" {
			return
		}
		switch {
		case t.HasClass("link"):
			items = append(items, oldLinkSummary(t, d, u))
		case t.HasClass("comment"):
			items = append(items, oldCommentSummary(t, d, u))
		}
	})
	if len(items) > 0 {
		d.Blocks = append(d.Blocks, doc.List{Items: items})
	}
	oldPager(gq, d, u)
}

func oldLinkSummary(t *goquery.Selection, d *doc.Document, u *url.URL) []doc.Block {
	a := t.Find("a.title").First()
	title := collapse(a.Text())
	permalink := t.AttrOr("data-permalink", "")
	var head doc.Inline
	if flair := collapse(t.Find(".linkflairlabel").First().Text()); flair != "" {
		head = append(head, doc.Span{Text: "[" + flair + "] ", Style: doc.Italic})
	}
	head = append(head, doc.Span{Text: title, Style: doc.Bold, Link: addLink(d, abs(u, permalink), title)})
	if t.AttrOr("data-nsfw", "") == "true" {
		head = append(head, doc.Span{Text: " NSFW", Style: doc.Italic})
	}
	if t.AttrOr("data-spoiler", "") == "true" {
		head = append(head, doc.Span{Text: " spoiler", Style: doc.Italic})
	}
	parts := []string{}
	if s := t.AttrOr("data-subreddit-prefixed", ""); s != "" {
		parts = append(parts, s)
	}
	if s := t.AttrOr("data-author", ""); s != "" {
		parts = append(parts, "u/"+s)
	}
	if s := collapse(t.Find("time").First().Text()); s != "" {
		parts = append(parts, s)
	}
	if s := t.AttrOr("data-score", ""); s != "" {
		parts = append(parts, "▲"+s)
	}
	if s := t.AttrOr("data-comments-count", ""); s != "" {
		parts = append(parts, s+" comments")
	}
	if dom := t.AttrOr("data-domain", ""); dom != "" && !strings.HasPrefix(dom, "self.") {
		parts = append(parts, dom)
	}
	return []doc.Block{
		doc.Paragraph{Text: head},
		doc.Paragraph{Text: doc.Inline{{Text: strings.Join(parts, " · "), Style: doc.Italic}}},
	}
}

// oldCommentSummary is a comment shown in a user's history.
func oldCommentSummary(t *goquery.Selection, d *doc.Document, u *url.URL) []doc.Block {
	parent := t.Find("a.title").First()
	var out []doc.Block
	if pt := collapse(parent.Text()); pt != "" {
		out = append(out, doc.Paragraph{Text: doc.Inline{{Text: "on: ", Style: doc.Italic},
			{Text: pt, Style: doc.Bold, Link: addLink(d, abs(u, parent.AttrOr("href", "")), pt)}}})
	}
	if body := t.Find("div.entry div.usertext-body div.md").First(); body.Length() > 0 {
		text := collapse(body.Text())
		if r := []rune(text); len(r) > 280 {
			text = string(r[:277]) + "…"
		}
		out = append(out, doc.Paragraph{Text: doc.Inline{{Text: text}}})
	}
	if pl := t.AttrOr("data-permalink", ""); pl != "" {
		out = append(out, doc.Paragraph{Text: doc.Inline{{Text: "→ in context", Link: addLink(d, abs(u, pl), "context")}}})
	}
	return out
}

func oldPager(gq *goquery.Document, d *doc.Document, u *url.URL) {
	var nav doc.Inline
	if a := gq.Find(".nav-buttons .prev-button a").First(); a.Length() > 0 {
		nav = append(nav, doc.Span{Text: "‹ previous", Link: addLink(d, abs(u, a.AttrOr("href", "")), "previous")}, doc.Span{Text: "   "})
	}
	if a := gq.Find(".nav-buttons .next-button a").First(); a.Length() > 0 {
		nav = append(nav, doc.Span{Text: "next page ›", Link: addLink(d, abs(u, a.AttrOr("href", "")), "next")})
	}
	if len(nav) > 0 {
		d.Blocks = append(d.Blocks, doc.Rule{}, doc.Paragraph{Text: nav})
	}
}

func oldSearch(gq *goquery.Document, d *doc.Document, u *url.URL) {
	d.Title = "Reddit search: " + u.Query().Get("q")
	var items [][]doc.Block
	gq.Find("div.search-result-link").Each(func(_ int, r *goquery.Selection) {
		a := r.Find("a.search-title").First()
		title := collapse(a.Text())
		if title == "" {
			return
		}
		parts := []string{}
		for _, sel := range []string{".search-subreddit-link", ".search-author a", ".search-time time", ".search-score", ".search-comments"} {
			if s := collapse(r.Find(sel).First().Text()); s != "" {
				parts = append(parts, s)
			}
		}
		item := []doc.Block{
			doc.Paragraph{Text: doc.Inline{{Text: title, Style: doc.Bold, Link: addLink(d, abs(u, a.AttrOr("href", "")), title)}}},
			doc.Paragraph{Text: doc.Inline{{Text: strings.Join(parts, " · "), Style: doc.Italic}}},
		}
		if snip := collapse(r.Find(".search-result-body").First().Text()); snip != "" {
			if rs := []rune(snip); len(rs) > 240 {
				snip = string(rs[:237]) + "…"
			}
			item = append(item, doc.Paragraph{Text: doc.Inline{{Text: snip}}})
		}
		items = append(items, item)
	})
	if len(items) > 0 {
		d.Blocks = append(d.Blocks, doc.List{Items: items})
	}
	if a := gq.Find("a[rel~=next]").First(); a.Length() > 0 {
		d.Blocks = append(d.Blocks, doc.Rule{}, doc.Paragraph{Text: doc.Inline{{Text: "next page ›", Link: addLink(d, abs(u, a.AttrOr("href", "")), "next")}}})
	}
}

// oldPost renders the post and its full comment tree.
func oldPost(gq *goquery.Document, d *doc.Document, u *url.URL) {
	t := gq.Find("#siteTable > div.thing.link").First()
	d.Title = collapse(t.Find("a.title").First().Text())
	parts := []string{}
	if s := t.AttrOr("data-subreddit-prefixed", ""); s != "" {
		parts = append(parts, s)
	}
	if s := t.AttrOr("data-author", ""); s != "" {
		parts = append(parts, "u/"+s)
	}
	if s := collapse(t.Find("p.tagline time").First().Text()); s != "" {
		parts = append(parts, s)
	}
	if s := t.AttrOr("data-score", ""); s != "" {
		parts = append(parts, "▲"+s)
	}
	if len(parts) > 0 {
		d.Meta = append(d.Meta, doc.KV{Key: "·", Value: strings.Join(parts, " · ")})
	}
	if flair := collapse(t.Find(".linkflairlabel").First().Text()); flair != "" {
		d.Meta = append(d.Meta, doc.KV{Key: "flair", Value: flair})
	}
	if dom := t.AttrOr("data-domain", ""); dom != "" && !strings.HasPrefix(dom, "self.") {
		target := t.AttrOr("data-url", "")
		d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: "→ linked page: ", Style: doc.Italic},
			{Text: dom, Link: addLink(d, abs(u, target), dom)}}})
	}
	if body := t.Find("div.expando div.usertext-body div.md").First(); body.Length() > 0 {
		h, _ := body.Html()
		d.Blocks = append(d.Blocks, htmlconv.Fragment(d, h, u.String())...)
	}
	top := gq.Find("div.commentarea > div.sitetable > div.thing.comment")
	count := collapse(gq.Find("div.commentarea .panestack-title .title").First().Text())
	if count == "" {
		count = fmt.Sprintf("%d comments", top.Length())
	}
	d.Blocks = append(d.Blocks, doc.Rule{}, doc.Heading{Level: 2, Text: doc.Inline{{Text: count}}}, commentSort(d, u))
	if top.Length() == 0 {
		d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: "No comments yet.", Style: doc.Italic}}})
		return
	}
	top.Each(func(i int, c *goquery.Selection) {
		if i > 0 {
			d.Blocks = append(d.Blocks, doc.Rule{})
		}
		d.Blocks = append(d.Blocks, oldComment(c, d, u, 0)...)
	})
	gq.Find("div.commentarea > div.sitetable > div.thing.morechildren").Each(func(_ int, m *goquery.Selection) {
		d.Blocks = append(d.Blocks, moreComments(m, d, u))
	})
}

// oldComment renders a comment; replies go into a collapsible quote.
func oldComment(c *goquery.Selection, d *doc.Document, u *url.URL, depth int) []doc.Block {
	entry := c.ChildrenFiltered("div.entry").First()
	tag := entry.Find("p.tagline").First()
	author := collapse(tag.Find("a.author").First().Text())
	if author == "" {
		author = "[deleted]"
	}
	head := doc.Inline{{Text: "u/" + author, Style: doc.Bold}}
	if tag.Find("a.author.submitter").Length() > 0 {
		head = append(head, doc.Span{Text: " (OP)", Style: doc.Italic})
	}
	meta := []string{}
	if s := tag.Find("span.score.unvoted").First().AttrOr("title", ""); s != "" {
		meta = append(meta, "▲"+s)
	}
	if s := collapse(tag.Find("time").First().Text()); s != "" {
		meta = append(meta, s)
	}
	if len(meta) > 0 {
		head = append(head, doc.Span{Text: " · " + strings.Join(meta, " · "), Style: doc.Italic})
	}
	out := []doc.Block{doc.Paragraph{Text: head}}
	if body := entry.Find("div.usertext-body div.md").First(); body.Length() > 0 {
		h, _ := body.Html()
		out = append(out, htmlconv.Fragment(d, h, u.String())...)
	}
	var sub []doc.Block
	n := 0
	c.ChildrenFiltered("div.child").ChildrenFiltered("div.sitetable").ChildrenFiltered("div.thing").Each(func(_ int, r *goquery.Selection) {
		switch {
		case r.HasClass("comment"):
			n++
			if len(sub) > 0 {
				sub = append(sub, doc.Rule{})
			}
			sub = append(sub, oldComment(r, d, u, depth+1)...)
		case r.HasClass("morechildren"):
			sub = append(sub, moreComments(r, d, u))
		}
	})
	if deep := c.ChildrenFiltered("div.child").Find("span.deepthread a").First(); deep.Length() > 0 {
		sub = append(sub, doc.Paragraph{Text: doc.Inline{{Text: "→ continue this thread", Link: addLink(d, abs(u, deep.AttrOr("href", "")), "continue")}}})
	}
	if len(sub) > 0 {
		label := fmt.Sprintf("%d replies", n)
		switch n {
		case 0:
			label = "more replies"
		case 1:
			label = "1 reply"
		}
		out = append(out, doc.Collapsible{Show: label, Hide: "hide " + label, Open: depth == 0,
			Blocks: []doc.Block{doc.Quote{Blocks: sub}}})
	}
	return out
}

// moreComments turns old.reddit's JavaScript "load more comments" into a link
// to the parent comment's permalink, which lists those replies.
func moreComments(m *goquery.Selection, d *doc.Document, u *url.URL) doc.Block {
	text := collapse(m.Find("a").First().Text())
	if text == "" {
		text = "load more comments"
	}
	parent := m.ParentsFiltered("div.thing.comment").First()
	href := parent.AttrOr("data-permalink", "")
	if href == "" {
		return doc.Paragraph{Text: doc.Inline{{Text: text + " (open the thread on its own page)", Style: doc.Italic}}}
	}
	return doc.Paragraph{Text: doc.Inline{{Text: "→ " + text, Link: addLink(d, abs(u, href), text)}}}
}

func oldTitle(gq *goquery.Document, u *url.URL) string {
	if t := collapse(gq.Find("span.pagename a, span.pagename").First().Text()); t != "" {
		if sub := communityOf(u.Path); sub != "" {
			return "r/" + sub
		}
		return t
	}
	return strings.Trim(u.Path, "/")
}
