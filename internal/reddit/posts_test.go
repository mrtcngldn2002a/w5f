package reddit

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

const userPageHTML = `<html><body><div id="siteTable" class="sitetable linklisting">
<div class="thing link" data-author="writer1" data-subreddit-prefixed="r/nosleep" data-permalink="/r/nosleep/comments/c3/part_3/">
  <p class="title"><a class="title" href="/r/nosleep/comments/c3/part_3/">The Keeper's Log (Part 3)</a></p>
  <p class="tagline"><time datetime="2019-10-20T12:00:00+00:00">6 years ago</time></p></div>
<div class="thing link" data-author="writer1" data-subreddit-prefixed="r/nosleep" data-permalink="/r/nosleep/comments/c2/part_2/">
  <p class="title"><a class="title" href="/r/nosleep/comments/c2/part_2/">The Keeper's Log (Part 2)</a></p>
  <p class="tagline"><time datetime="2019-10-12T12:00:00+00:00">6 years ago</time></p></div>
</div>
<div class="nav-buttons"><span class="next-button"><a href="%s/user/writer1/submitted/?sort=new&count=25&after=t3_c2">next ›</a></span></div>
</body></html>`

func TestSubmittedAndPostInfo(t *testing.T) {
	t.Setenv("W5F_REDDIT_SESSION", "test-session")
	var base string
	pages := 0
	f := setup(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Cookie"), "reddit_session=test-session") {
			t.Error("the owner's session must be sent")
		}
		switch {
		case strings.HasPrefix(r.URL.Path, "/user/writer1/submitted"):
			pages++
			fmt.Fprintf(w, userPageHTML, base)
		default:
			w.Write([]byte(strings.Replace(oldPostHTML, `<p class="tagline"><time>3 hours ago</time></p>`,
				`<p class="tagline"><time datetime="2019-10-12T12:00:00+00:00">3 hours ago</time></p>`, 1)))
		}
	})
	base = oldBase
	ps, err := Submitted(context.Background(), f, "writer1", 3, func(oldest PostInfo) bool { return true })
	if err != nil || len(ps) != 2 || pages != 1 {
		t.Fatalf("submitted: %v %d pages=%d", err, len(ps), pages)
	}
	if ps[0].Title != "The Keeper's Log (Part 3)" || ps[0].Sub != "r/nosleep" || ps[0].Posted.Day() != 20 ||
		ps[0].URL != "https://www.reddit.com/r/nosleep/comments/c3/part_3/" {
		t.Errorf("post: %+v", ps[0])
	}
	u, _ := url.Parse("https://www.reddit.com/r/nosleep/comments/abc123/the_lighthouse/")
	d, info, err := LoadPost(context.Background(), f, u, false)
	if err != nil || info == nil || d == nil || info.Author != "writer1" || info.Posted.Year() != 2019 {
		t.Fatalf("post info: %v %+v", err, info)
	}
}
