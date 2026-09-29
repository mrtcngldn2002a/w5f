package reddit

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"w5f/internal/doc"
	"w5f/internal/fetch"
)

// PostInfo describes a Reddit post, for finding the series it belongs to.
type PostInfo struct {
	Sub    string // "r/nosleep"
	Author string
	Title  string
	URL    string // https://www.reddit.com/r/…/comments/…/
	Posted time.Time
	Links  []doc.Link // links in the post body
}

// LoadPost is Load that also describes the post when the address is one
// (read through old.reddit with the owner's session; nil otherwise).
func LoadPost(ctx context.Context, f *fetch.Fetcher, u *url.URL, reload bool) (*doc.Document, *PostInfo, error) {
	u = Canonical(u)
	session := LoadSession()
	if session == "" || u.Path == "" || u.Path == "/" || !strings.Contains(u.Path, "/comments/") {
		d, err := Load(ctx, f, u, reload)
		return d, nil, err
	}
	d, info, err := loadOldInfo(ctx, f, u, session, reload)
	if errors.Is(err, ErrSessionExpired) {
		return setupPage(true), nil, nil
	}
	return d, info, err
}

// postInfo reads the post's description from an old.reddit post page.
func postInfo(gq *goquery.Document, u *url.URL) *PostInfo {
	t := gq.Find("#siteTable > div.thing.link").First()
	if t.Length() == 0 {
		return nil
	}
	p := thingInfo(t, u)
	gq.Find("#siteTable > div.thing.link div.expando div.usertext-body div.md a[href]").Each(func(_ int, a *goquery.Selection) {
		p.Links = append(p.Links, doc.Link{Href: abs(u, a.AttrOr("href", "")), Text: collapse(a.Text())})
	})
	return p
}

func thingInfo(t *goquery.Selection, u *url.URL) *PostInfo {
	p := &PostInfo{Sub: t.AttrOr("data-subreddit-prefixed", ""), Author: t.AttrOr("data-author", ""),
		Title: collapse(t.Find("a.title").First().Text())}
	if pl := t.AttrOr("data-permalink", ""); pl != "" {
		p.URL = abs(u, pl)
	} else {
		p.URL = u.String()
	}
	p.URL = strings.Replace(strings.Replace(p.URL, "old.reddit.com", "www.reddit.com", 1), "://reddit.com", "://www.reddit.com", 1)
	if dt, ok := t.Find("p.tagline time").First().Attr("datetime"); ok {
		p.Posted, _ = time.Parse(time.RFC3339, dt)
	}
	return p
}

// Submitted lists an author's posts, newest first, up to maxPages pages of
// old.reddit's user page, read with the owner's session. It stops early
// when stop returns true for a page's oldest post.
func Submitted(ctx context.Context, f *fetch.Fetcher, author string, maxPages int, stop func(oldest PostInfo) bool) ([]PostInfo, error) {
	session := LoadSession()
	if session == "" {
		return nil, errors.New("Reddit is not connected (g → reddit-login)")
	}
	ob, _ := url.Parse(oldBase)
	next := oldBase + "/user/" + url.PathEscape(author) + "/submitted/?sort=new"
	var out []PostInfo
	for page := 0; next != "" && page < maxPages; page++ {
		u, err := url.Parse(next)
		if err != nil {
			return out, err
		}
		u.Scheme, u.Host = ob.Scheme, ob.Host
		hdr := http.Header{}
		hdr.Set("Cookie", "reddit_session="+session)
		resp, err := f.Get(ctx, u, fetch.Options{Header: hdr})
		if err != nil {
			return out, err
		}
		if strings.HasPrefix(resp.URL.Path, "/login") || bytes.Contains(resp.Body, []byte(`name="js_challenge"`)) {
			return out, ErrSessionExpired
		}
		gq, err := goquery.NewDocumentFromReader(bytes.NewReader(resp.Body))
		if err != nil {
			return out, err
		}
		var last PostInfo
		gq.Find("#siteTable > div.thing.link").Each(func(_ int, t *goquery.Selection) {
			if t.HasClass("promoted") {
				return
			}
			p := thingInfo(t, u)
			out = append(out, *p)
			last = *p
		})
		next = gq.Find(".nav-buttons .next-button a").First().AttrOr("href", "")
		if stop != nil && last.URL != "" && stop(last) {
			break
		}
	}
	return out, nil
}
