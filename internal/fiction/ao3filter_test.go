package fiction

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"w5f/internal/doc"
)

func TestAO3SortAndFilter(t *testing.T) {
	srv := fixtureServer(t, map[string]string{"/tags/Cthulhu/works": "ao3-filters.html", "/works": "ao3-filters.html"})
	env := testEnv(t, ao3{})
	env.Fetcher.HostGap = 0
	oldRoot, oldHosts := ao3Root, ao3Hosts
	ao3Root, ao3Hosts = srv.URL, map[string]bool{strings.TrimPrefix(srv.URL, "http://"): true}
	defer func() { ao3Root, ao3Hosts = oldRoot, oldHosts }()
	ctx := context.Background()
	list := srv.URL + "/tags/Cthulhu/works"

	// The listing links to its filter page and says what is active.
	lu, _ := url.Parse(list)
	ld, err := AO3Page(ctx, env, lu)
	if err != nil || !strings.Contains(flat(ld), "Sort and filter") || !strings.Contains(flat(ld), "sorted by Kudos") {
		t.Fatalf("listing: %v\n%s", err, flat(ld))
	}

	d, err := Route(ctx, "w5f:fiction/ao3/filter?"+url.Values{"u": {list}}.Encode(), env)
	if err != nil {
		t.Fatal(err)
	}
	links := map[string]string{}
	var walk func(bs []doc.Block)
	walk = func(bs []doc.Block) {
		for _, b := range bs {
			switch x := b.(type) {
			case doc.Paragraph:
				for _, s := range x.Text {
					if _, seen := links[s.Text]; s.Link > 0 && !seen { // first = the Include one
						links[s.Text] = d.Links[s.Link-1].Href
					}
				}
			case doc.List:
				for _, it := range x.Items {
					walk(it)
				}
			case doc.Collapsible:
				walk(x.Blocks)
			}
		}
	}
	walk(d.Blocks)
	q := func(label string) url.Values {
		t.Helper()
		h, ok := links[label]
		if !ok {
			t.Fatalf("no link %q in %v", label, links)
		}
		u, _ := url.Parse(h)
		if !strings.HasSuffix(u.Path, "/works") {
			t.Errorf("%q goes to %s", label, h)
		}
		return u.Query()
	}
	// Sorting keeps the other filters.
	if v := q("Date Updated"); v.Get("work_search[sort_column]") != "revised_at" || v.Get("include_work_search[freeform_ids][]") != "587" ||
		v.Get("tag_id") != "Cthulhu" || v.Get("commit") != "Sort and Filter" || v.Get("work_search[words_from]") != "1000" {
		t.Errorf("sort: %v", v)
	}
	// A checked tag unchecks; an unchecked one adds.
	if v := q("[x] Horror (326)"); len(v["include_work_search[freeform_ids][]"]) != 0 {
		t.Errorf("uncheck: %v", v)
	}
	if v := q("[ ] Fluff (134)"); len(v["include_work_search[freeform_ids][]"]) != 2 {
		t.Errorf("check: %v", v)
	}
	if v := q("[ ] Mature (595)"); v.Get("include_work_search[rating_ids][]") != "12" {
		t.Errorf("rating radio: %v", v)
	}
	if v := q("Complete works only"); v.Get("work_search[complete]") != "T" {
		t.Errorf("complete: %v", v)
	}
	if v := q("English"); v.Get("work_search[language_id]") != "en" {
		t.Errorf("language: %v", v)
	}
	if !strings.Contains(flat(d), "g → f tag") {
		t.Error("typed filters need their g commands shown")
	}

	// Typed filters from the g prompt.
	ft := FilterCommand(d.URL, "f words 2000-9000")
	fu, _ := url.Parse(ft)
	if !strings.HasPrefix(ft, "w5f:fiction/ao3/filter/set?") || fu.Query().Get("u") != list {
		t.Fatalf("filter command target: %q", ft)
	}
	nu := applyTyped(t, env, list, "words 2000-9000")
	if nu.Get("work_search[words_from]") != "2000" || nu.Get("work_search[words_to]") != "9000" {
		t.Errorf("words: %v", nu)
	}
	if nu := applyTyped(t, env, list, "-tag Fluff, Angst"); nu.Get("work_search[excluded_tag_names]") != "Fluff, Angst" {
		t.Errorf("-tag: %v", nu)
	}
	if nu := applyTyped(t, env, list, "q cosmic dread"); nu.Get("work_search[query]") != "cosmic dread" {
		t.Errorf("q: %v", nu)
	}
	if FilterCommand("https://example.org/", "f tag x") != "" || FilterCommand(d.URL, "fandoms") != "" {
		t.Error("f commands only apply on AO3 lists")
	}
}

func applyTyped(t *testing.T, env Env, list, cmd string) url.Values {
	t.Helper()
	target, err := typedFilterURL(context.Background(), env, list, cmd)
	if err != nil {
		t.Fatalf("%s: %v", cmd, err)
	}
	u, _ := url.Parse(target)
	return u.Query()
}
