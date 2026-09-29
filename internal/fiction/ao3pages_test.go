package fiction

import (
	"context"
	"net/url"
	"strings"
	"testing"
)

func TestAO3NavigationPages(t *testing.T) {
	srv := fixtureServer(t, map[string]string{
		"/":                                   "ao3-home.html",
		"/media":                              "ao3-media.html",
		"/media/Books *a* Literature/fandoms": "ao3-fandoms.html",
		"/tags/Parahumans Series - Wildbow/works": "ao3-tagworks.html",
	})
	env := testEnv(t, ao3{})
	env.Fetcher.HostGap = 0
	oldRoot, oldHosts := ao3Root, ao3Hosts
	ao3Root, ao3Hosts = srv.URL, map[string]bool{strings.TrimPrefix(srv.URL, "http://"): true}
	defer func() { ao3Root, ao3Hosts = oldRoot, oldHosts }()
	ctx := context.Background()
	find := func(texts []string, want string) bool {
		for _, s := range texts {
			if strings.Contains(s, want) {
				return true
			}
		}
		return false
	}
	linkTexts := func(u string) ([]string, map[string]string) {
		t.Helper()
		pu, _ := url.Parse(u)
		d, err := AO3Page(ctx, env, pu)
		if err != nil {
			t.Fatalf("%s: %v", u, err)
		}
		var ts []string
		m := map[string]string{}
		for _, l := range d.Links {
			ts = append(ts, l.Text)
			m[l.Text] = l.Href
		}
		return ts, m
	}

	home, hm := linkTexts(srv.URL + "/")
	for _, want := range []string{"All Fandoms", "Books & Literature", "Uncategorized Fandoms", "Works search", "My AO3"} {
		if !find(home, want) {
			t.Errorf("home lacks %q: %v", want, home)
		}
	}
	hu, _ := url.Parse(srv.URL + "/")
	if hd, _ := AO3Page(ctx, env, hu); hd == nil || !strings.HasPrefix(hd.URL, "w5f:fiction/ao3/page?") {
		t.Error("AO3 menu pages carry a w5f: address so the reader trusts their W5F links")
	}
	if !strings.HasPrefix(hm["Books & Literature"], "w5f:fiction/ao3/fandoms?") {
		t.Errorf("category link: %q", hm["Books & Literature"])
	}

	media, mm := linkTexts(srv.URL + "/media")
	if !find(media, "Anime & Manga") || !find(media, "Haikyuu!!") || !strings.HasSuffix(mm["Haikyuu!!"], "/tags/Haikyuu!!/works") {
		t.Errorf("media: %v", media[:min(len(media), 12)])
	}

	// A category: the letter index, then one letter.
	d, err := Route(ctx, hm["Books & Literature"], env)
	if err != nil || !strings.Contains(flat(d), "Books & Literature") {
		t.Fatalf("letters: %v\n%s", err, flat(d))
	}
	var letterA string
	for _, l := range d.Links {
		if strings.HasPrefix(l.Text, "A ") || l.Text == "A" {
			letterA = l.Href
		}
	}
	if letterA == "" {
		t.Fatalf("no letter A in %+v", d.Links)
	}
	d, err = Route(ctx, letterA, env)
	if err != nil || len(d.Links) < 40 || !strings.Contains(d.Links[len(d.Links)-1].Href, "/tags/") {
		t.Fatalf("letter A: %v %d", err, len(d.Links))
	}

	// A tag's works: blurbs, next page.
	pu, _ := url.Parse(srv.URL + "/tags/Parahumans%20Series%20-%20Wildbow/works")
	d, err = AO3Page(ctx, env, pu)
	if err != nil || !strings.Contains(flat(d), "I, Simulacrum.") || !strings.Contains(flat(d), "A summary of the first work.") ||
		!strings.Contains(d.Next, "page=2") || !strings.Contains(d.Title, "Parahumans") {
		t.Errorf("tag works: %v title=%q next=%q\n%s", err, d.Title, d.Next, flat(d))
	}
}
