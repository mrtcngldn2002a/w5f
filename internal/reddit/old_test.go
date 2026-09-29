package reddit

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"w5f/internal/doc"
	"w5f/internal/fetch"
)

// Markup mirrors old.reddit.com's server-rendered HTML.
const oldListingHTML = `<html><body><span class="pagename redditname"><a href="/r/nosleep/">nosleep</a></span>
<div id="siteTable" class="sitetable linklisting">
<div class="thing link promoted" data-promoted="true" data-permalink="/ad"><a class="title" href="/ad">Buy stuff</a></div>
<div class="thing link" data-author="writer1" data-subreddit-prefixed="r/nosleep" data-score="1234" data-comments-count="56"
  data-permalink="/r/nosleep/comments/abc123/the_lighthouse/" data-domain="self.nosleep">
  <p class="title"><span class="linkflairlabel">Series</span><a class="title" href="/r/nosleep/comments/abc123/the_lighthouse/">The Lighthouse Keeper's Log</a></p>
  <p class="tagline"><time>3 hours ago</time></p></div>
</div>
<div class="nav-buttons"><span class="next-button"><a href="https://old.reddit.com/r/nosleep/?count=25&after=t3_abc123" rel="nofollow next">next ›</a></span></div>
</body></html>`

const oldPostHTML = `<html><body>
<div id="siteTable"><div class="thing link" data-author="writer1" data-subreddit-prefixed="r/nosleep" data-score="1234" data-domain="self.nosleep">
<p class="title"><a class="title" href="/r/nosleep/comments/abc123/the_lighthouse/">The Lighthouse Keeper's Log</a></p>
<p class="tagline"><time>3 hours ago</time></p>
<div class="expando"><div class="usertext-body"><div class="md"><p>The lamp went out at <em>midnight</em>.</p></div></div></div>
</div></div>
<div class="commentarea"><div class="panestack-title"><span class="title">all 3 comments</span></div>
<div class="sitetable nestedlisting">
  <div class="thing comment" data-permalink="/r/nosleep/comments/abc123/the_lighthouse/c1/">
    <div class="entry"><p class="tagline"><a class="author">reader1</a><span class="score unvoted" title="42">42 points</span><time>2h</time></p>
      <div class="usertext-body"><div class="md"><p>Top comment text.</p></div></div></div>
    <div class="child"><div class="sitetable listing">
      <div class="thing comment" data-permalink="/r/nosleep/comments/abc123/the_lighthouse/c2/">
        <div class="entry"><p class="tagline"><a class="author submitter">writer1</a><span class="score unvoted" title="30">30 points</span></p>
          <div class="usertext-body"><div class="md"><p>Reply from OP.</p></div></div></div>
        <div class="child"><div class="sitetable listing">
          <div class="thing comment"><div class="entry"><p class="tagline"><a class="author">reader2</a></p>
            <div class="usertext-body"><div class="md"><p>Deep reply.</p></div></div></div></div>
        </div></div>
      </div>
      <div class="thing morechildren"><a href="javascript:void(0)">load more comments (7 replies)</a></div>
    </div></div>
  </div>
</div></div></body></html>`

func setup(t *testing.T, handler http.HandlerFunc) *fetch.Fetcher {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	old := oldBase
	oldBase = srv.URL
	t.Cleanup(func() { oldBase = old })
	f := fetch.New("", "test")
	f.HostGap = 0
	return f
}

func flatten(bs []doc.Block, open bool) string {
	var b strings.Builder
	var visit func([]doc.Block)
	visit = func(bs []doc.Block) {
		for _, x := range bs {
			switch x := x.(type) {
			case doc.Paragraph:
				b.WriteString(x.Text.PlainText() + "\n")
			case doc.Heading:
				b.WriteString("# " + x.Text.PlainText() + "\n")
			case doc.List:
				for _, it := range x.Items {
					visit(it)
				}
			case doc.Quote:
				visit(x.Blocks)
			case doc.Collapsible:
				fmt.Fprintf(&b, "[C:%s open=%v]\n", x.Show, x.Open)
				visit(x.Blocks)
			case doc.Notice:
				b.WriteString("[N] " + x.Text + "\n")
			}
		}
	}
	visit(bs)
	return b.String()
}

func TestOldListingSendsCookieAndSkipsAds(t *testing.T) {
	f := setup(t, func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("reddit_session"); err != nil || c.Value != "SECRET" {
			t.Errorf("session cookie not sent: %v", err)
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, oldListingHTML)
	})
	u, _ := url.Parse("https://www.reddit.com/r/nosleep/")
	d, err := loadOld(context.Background(), f, u, "SECRET", false)
	if err != nil {
		t.Fatal(err)
	}
	got := flatten(d.Blocks, true)
	if strings.Contains(got, "Buy stuff") {
		t.Error("promoted post leaked")
	}
	for _, want := range []string{"[Series] The Lighthouse Keeper's Log", "r/nosleep · u/writer1 · 3 hours ago · ▲1234 · 56 comments", "next page ›", "top all"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	for _, l := range d.Links {
		if strings.Contains(l.Href, "127.0.0.1") || strings.Contains(l.Href, "old.reddit.com") {
			t.Errorf("link not mapped back to www.reddit.com: %s", l.Href)
		}
	}
}

func TestOldPostCommentTree(t *testing.T) {
	f := setup(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, oldPostHTML)
	})
	u, _ := url.Parse("https://www.reddit.com/r/nosleep/comments/abc123/the_lighthouse/")
	d, err := loadOld(context.Background(), f, u, "SECRET", false)
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "The Lighthouse Keeper's Log" {
		t.Errorf("title = %q", d.Title)
	}
	got := flatten(d.Blocks, true)
	for _, want := range []string{
		"The lamp went out at midnight.",
		"# all 3 comments",
		"u/reader1 · ▲42 · 2h",
		"[C:1 reply open=true]",  // first-level replies shown
		"u/writer1 (OP) · ▲30",   // nested reply with OP marker
		"[C:1 reply open=false]", // deeper levels start folded
		"Deep reply.",
		"→ load more comments (7 replies)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestExpiredSessionShowsSetup(t *testing.T) {
	f := setup(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/login") {
			fmt.Fprint(w, "<html>login</html>")
			return
		}
		http.Redirect(w, r, "/login/?dest=x", http.StatusFound)
	})
	u, _ := url.Parse("https://www.reddit.com/r/nosleep/")
	if _, err := loadOld(context.Background(), f, u, "OLD", false); err != ErrSessionExpired {
		t.Errorf("err = %v, want ErrSessionExpired", err)
	}
}

func TestCleanCookie(t *testing.T) {
	cases := map[string]string{
		"abc123":                             "abc123",
		"  abc123\n":                         "abc123",
		"reddit_session=abc123; token_v2=xx": "abc123",
		"not a cookie":                       "",
	}
	for in, want := range cases {
		if got := cleanCookie(in); got != want {
			t.Errorf("cleanCookie(%q) = %q, want %q", in, got, want)
		}
	}
}
