package fiction

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"w5f/internal/doc"
)

// AO3's "Sort and Filter" form, made usable in a terminal: every choice is a
// link to the list as the form would submit it with that choice changed;
// the typed fields (tags, word count, dates, words) come from the g prompt
// ("f tag Horror").

type filterOpt struct {
	value, label string
	on           bool
}

type filterGroup struct {
	title, name string
	radio       bool // one choice at a time (radio buttons, selects)
	opts        []filterOpt
}

type ao3Form struct {
	action *url.URL
	vals   url.Values // what a browser would submit now
	sort   *filterGroup
	lang   *filterGroup
	groups []*filterGroup // radio/checkbox groups in page order
}

var filterKinds = map[string]string{"rating_ids": "Ratings", "archive_warning_ids": "Warnings", "category_ids": "Categories",
	"fandom_ids": "Fandoms", "character_ids": "Characters", "relationship_ids": "Relationships", "freeform_ids": "Additional tags",
	"crossover": "Crossovers", "complete": "Completion status"}

func filterTitle(name string) string {
	prefix := ""
	switch {
	case strings.HasPrefix(name, "include_"):
		prefix = "Include "
	case strings.HasPrefix(name, "exclude_"):
		prefix = "Exclude "
	}
	key := name
	if i := strings.Index(name, "["); i >= 0 {
		key = strings.Trim(strings.SplitN(name[i+1:], "]", 2)[0], "[]")
	}
	if t, ok := filterKinds[key]; ok {
		return prefix + strings.ToLower(t[:1]) + t[1:]
	}
	return prefix + key
}

// parseAO3Form reads the filter form of a work list ("" form → nil).
func parseAO3Form(gq *goquery.Document, base *url.URL) *ao3Form {
	form := gq.Find("form#work-filters").First()
	if form.Length() == 0 {
		return nil
	}
	action, _ := form.Attr("action")
	if action == "" {
		action = "/works"
	}
	au, err := url.Parse(resolve(base, action))
	if err != nil {
		return nil
	}
	f := &ao3Form{action: au, vals: url.Values{}}
	byName := map[string]*filterGroup{}
	label := func(in *goquery.Selection) string {
		if l := in.Closest("label"); l.Length() > 0 {
			return text(l)
		}
		if id, ok := in.Attr("id"); ok && id != "" {
			return text(form.Find(`label[for="` + id + `"]`).First())
		}
		return ""
	}
	form.Find("input, select").Each(func(_ int, in *goquery.Selection) {
		name, _ := in.Attr("name")
		if name == "" || name == "commit" {
			return
		}
		if goquery.NodeName(in) == "select" {
			g := &filterGroup{title: filterTitle(name), name: name, radio: true}
			sel := ""
			in.Find("option").Each(func(i int, o *goquery.Selection) {
				v, _ := o.Attr("value")
				_, on := o.Attr("selected")
				if i == 0 || on {
					sel = v
				}
				g.opts = append(g.opts, filterOpt{value: v, label: text(o), on: on})
			})
			f.vals.Set(name, sel)
			switch name {
			case "work_search[sort_column]":
				g.title = "Sort by"
				f.sort = g
			case "work_search[language_id]":
				g.title = "Language"
				f.lang = g
			}
			return
		}
		typ := strings.ToLower(in.AttrOr("type", "text"))
		v := in.AttrOr("value", "")
		switch typ {
		case "checkbox", "radio":
			_, on := in.Attr("checked")
			g := byName[name]
			if g == nil {
				g = &filterGroup{title: filterTitle(name), name: name, radio: typ == "radio"}
				byName[name] = g
				f.groups = append(f.groups, g)
			}
			g.opts = append(g.opts, filterOpt{value: v, label: label(in), on: on})
			if on {
				f.vals.Add(name, v)
			}
		case "submit", "button":
		default: // text, hidden
			if v != "" || typ == "hidden" {
				f.vals.Set(name, v)
			}
		}
	})
	return f
}

// with is the list address with the form values changed by edit.
func (f *ao3Form) with(edit func(v url.Values)) string {
	v := url.Values{}
	for k, vs := range f.vals {
		v[k] = append([]string(nil), vs...)
	}
	edit(v)
	v.Set("commit", "Sort and Filter")
	u := *f.action
	u.RawQuery = v.Encode()
	return u.String()
}

// toggle is the address with one choice switched.
func (f *ao3Form) toggle(g *filterGroup, o filterOpt) string {
	return f.with(func(v url.Values) {
		switch {
		case g.radio && o.on && strings.HasPrefix(g.name, "include_"):
			v.Del(g.name) // a chosen rating can be cleared again
		case g.radio:
			v.Set(g.name, o.value)
		case o.on:
			var keep []string
			for _, x := range v[g.name] {
				if x != o.value {
					keep = append(keep, x)
				}
			}
			v[g.name] = keep
		default:
			v.Add(g.name, o.value)
		}
	})
}

func (f *ao3Form) chosen(g *filterGroup) string {
	for _, o := range g.opts {
		if o.on || (g.radio && f.vals.Get(g.name) == o.value && o.value != "") {
			return o.label
		}
	}
	return ""
}

// summary says what is active ("sorted by Kudos · 2 tags included · …").
func (f *ao3Form) summary() string {
	var s []string
	if f.sort != nil {
		for _, o := range f.sort.opts {
			if o.value == f.vals.Get(f.sort.name) {
				s = append(s, "sorted by "+o.label)
			}
		}
	}
	inc, exc := 0, 0
	for _, g := range f.groups {
		for _, o := range g.opts {
			if !o.on {
				continue
			}
			switch {
			case strings.HasPrefix(g.name, "include_"):
				inc++
			case strings.HasPrefix(g.name, "exclude_"):
				exc++
			case o.value != "":
				s = append(s, strings.ToLower(o.label))
			}
		}
	}
	if inc > 0 {
		s = append(s, fmt.Sprintf("%d included", inc))
	}
	if exc > 0 {
		s = append(s, fmt.Sprintf("%d excluded", exc))
	}
	for _, t := range []struct{ name, label string }{{"work_search[other_tag_names]", "tags"}, {"work_search[excluded_tag_names]", "not"},
		{"work_search[query]", "search"}} {
		if v := f.vals.Get(t.name); v != "" {
			s = append(s, t.label+": "+v)
		}
	}
	if a, b := f.vals.Get("work_search[words_from]"), f.vals.Get("work_search[words_to]"); a != "" || b != "" {
		s = append(s, "words "+a+"–"+b)
	}
	if a, b := f.vals.Get("work_search[date_from]"), f.vals.Get("work_search[date_to]"); a != "" || b != "" {
		s = append(s, "updated "+a+"–"+b)
	}
	if f.lang != nil {
		if l := f.chosen(f.lang); l != "" {
			s = append(s, l)
		}
	}
	return joinDot(s)
}

func filterHref(list string) string {
	return "w5f:fiction/ao3/filter?" + url.Values{"u": {list}}.Encode()
}

// clearURL is the list without filters (the tag's own list).
func (f *ao3Form) clearURL(list string) string {
	if tag := f.vals.Get("tag_id"); tag != "" {
		return ao3Root + "/tags/" + url.PathEscape(tag) + "/works"
	}
	return list
}

func init() {
	extraRoutes["fiction/ao3/filter"] = func(ctx context.Context, q url.Values, env Env) (*doc.Document, error) {
		return ao3FilterDoc(ctx, env, q.Get("u"))
	}
	extraRoutes["fiction/ao3/filter/set"] = func(ctx context.Context, q url.Values, env Env) (*doc.Document, error) {
		target, err := typedFilterURL(ctx, env, q.Get("u"), q.Get("f"))
		if err != nil {
			return nil, err
		}
		u, err := url.Parse(target)
		if err != nil {
			return nil, err
		}
		return AO3Page(ctx, env, u)
	}
}

func listForm(ctx context.Context, env Env, list string) (*ao3Form, *goquery.Document, error) {
	u, err := url.Parse(list)
	if err != nil || !IsAO3(u) {
		return nil, nil, errors.New("not an AO3 work list")
	}
	gq, base, err := ao3Page(ctx, env.Fetcher, list, false)
	if err != nil {
		return nil, nil, err
	}
	f := parseAO3Form(gq, base)
	if f == nil {
		return nil, gq, errors.New("this AO3 list has no sort and filter options")
	}
	return f, gq, nil
}

// ao3FilterDoc is the Sort and Filter page of a work list.
func ao3FilterDoc(ctx context.Context, env Env, list string) (*doc.Document, error) {
	f, gq, err := listForm(ctx, env, list)
	if err != nil {
		return nil, err
	}
	d := &doc.Document{Title: "Sort and filter — " + strings.TrimSuffix(text(gq.Find("#main h2.heading").First()), " "), URL: filterHref(list), Origin: "live", Lang: "en"}
	d.Blocks = append(d.Blocks, ao3Nav(d))
	back := doc.Inline{{Text: "‹ back to the list", Link: link(d, list, "list")}, plain("   ", 0),
		doc.Span{Text: "clear all filters", Link: link(d, f.clearURL(list), "clear")}}
	if s := f.summary(); s != "" {
		back = append(back, plain("   now: "+s, doc.Italic))
	}
	d.Blocks = append(d.Blocks, para(back...))

	inline := func(g *filterGroup) doc.Block {
		var in doc.Inline
		in = append(in, plain(g.title+": ", doc.Bold))
		for i, o := range g.opts {
			if o.label == "" {
				continue
			}
			if i > 0 {
				in = append(in, plain(" · ", 0))
			}
			if o.on || (g == f.sort && f.vals.Get(g.name) == o.value) {
				in = append(in, plain("● "+o.label, doc.Bold))
				continue
			}
			in = append(in, doc.Span{Text: o.label, Link: link(d, f.toggle(g, o), o.label)})
		}
		return para(in...)
	}
	if f.sort != nil {
		d.Blocks = append(d.Blocks, inline(f.sort))
	}
	var tagGroups []*filterGroup
	for _, g := range f.groups {
		if strings.HasPrefix(g.name, "include_") || strings.HasPrefix(g.name, "exclude_") {
			tagGroups = append(tagGroups, g)
			continue
		}
		d.Blocks = append(d.Blocks, inline(g)) // crossovers, completion
	}
	section := ""
	for _, g := range tagGroups {
		head := "Include"
		if strings.HasPrefix(g.name, "exclude_") {
			head = "Exclude"
		}
		if head != section {
			section = head
			d.Blocks = append(d.Blocks, doc.Heading{Level: 2, Text: doc.Inline{{Text: head}}})
		}
		var items [][]doc.Block
		n := 0
		for _, o := range g.opts {
			mark := "[ ] "
			if o.on {
				mark, n = "[x] ", n+1
			}
			items = append(items, []doc.Block{para(doc.Span{Text: mark + o.label, Link: link(d, f.toggle(g, o), o.label)})})
		}
		show := strings.TrimPrefix(strings.TrimPrefix(g.title, "Include "), "Exclude ")
		show = strings.ToUpper(show[:1]) + show[1:]
		if n > 0 {
			show += fmt.Sprintf(" (%d chosen)", n)
		}
		d.Blocks = append(d.Blocks, doc.Collapsible{Show: show, Hide: "hide", Blocks: []doc.Block{doc.List{Items: items}}, Open: n > 0})
	}
	if f.lang != nil {
		var items [][]doc.Block
		for _, o := range f.lang.opts {
			if o.value == "" {
				continue
			}
			if f.vals.Get(f.lang.name) == o.value {
				items = append(items, []doc.Block{para(plain("● "+o.label, doc.Bold))})
				continue
			}
			items = append(items, []doc.Block{para(doc.Span{Text: o.label, Link: link(d, f.toggle(f.lang, o), o.label)})})
		}
		label := "Language"
		if l := f.chosen(f.lang); l != "" {
			label += " (" + l + ")"
		}
		d.Blocks = append(d.Blocks, doc.Heading{Level: 2, Text: doc.Inline{{Text: "More"}}},
			doc.Collapsible{Show: label, Hide: "hide", Blocks: []doc.Block{doc.List{Items: items}}})
	}
	d.Blocks = append(d.Blocks, doc.Heading{Level: 2, Text: doc.Inline{{Text: "Typed filters"}}},
		doc.List{Items: [][]doc.Block{
			{para(plain("g → f tag Horror, Cosmic Horror", doc.Code), plain("   other tags to include (now: "+orNone(f.vals.Get("work_search[other_tag_names]"))+")", doc.Italic))},
			{para(plain("g → f -tag Fluff", doc.Code), plain("   tags to exclude (now: "+orNone(f.vals.Get("work_search[excluded_tag_names]"))+")", doc.Italic))},
			{para(plain("g → f words 1000-50000", doc.Code), plain("   word count, either side may be empty", doc.Italic))},
			{para(plain("g → f date 2024-01-01..2025-06-30", doc.Code), plain("   date updated", doc.Italic))},
			{para(plain("g → f q cosmic dread", doc.Code), plain("   search within results", doc.Italic))},
			{para(plain("g → f clear", doc.Code), plain("   remove every filter", doc.Italic))},
		}})
	d.Renumber()
	return d, nil
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

// FilterCommand turns "f …" typed at the g prompt on an AO3 list (or its
// filter page) into the address that applies it; "" when it does not apply.
func FilterCommand(docURL, input string) string {
	in := strings.TrimSpace(input)
	if len(in) < 2 || !strings.EqualFold(in[:2], "f ") {
		return ""
	}
	var list string
	for _, p := range []string{"w5f:fiction/ao3/page?", "w5f:fiction/ao3/filter?"} {
		if strings.HasPrefix(docURL, p) {
			if q, err := url.ParseQuery(strings.TrimPrefix(docURL, p)); err == nil {
				list = q.Get("u")
			}
		}
	}
	if list == "" {
		return ""
	}
	return "w5f:fiction/ao3/filter/set?" + url.Values{"u": {list}, "f": {strings.TrimSpace(in[2:])}}.Encode()
}

// typedFilterURL applies a typed filter ("tag X", "-tag X", "words A-B",
// "date A..B", "q X", "clear") to a list.
func typedFilterURL(ctx context.Context, env Env, list, cmd string) (string, error) {
	f, _, err := listForm(ctx, env, list)
	if err != nil {
		return "", err
	}
	verb, arg, _ := strings.Cut(strings.TrimSpace(cmd), " ")
	arg = strings.TrimSpace(arg)
	set := func(pairs ...string) string {
		return f.with(func(v url.Values) {
			for i := 0; i+1 < len(pairs); i += 2 {
				if pairs[i+1] == "" {
					v.Del(pairs[i])
				} else {
					v.Set(pairs[i], pairs[i+1])
				}
			}
		})
	}
	switch strings.ToLower(verb) {
	case "tag", "tags", "+tag":
		return set("work_search[other_tag_names]", arg), nil
	case "-tag", "-tags", "not":
		return set("work_search[excluded_tag_names]", arg), nil
	case "words", "w":
		a, b, _ := strings.Cut(arg, "-")
		return set("work_search[words_from]", strings.TrimSpace(a), "work_search[words_to]", strings.TrimSpace(b)), nil
	case "date", "updated":
		a, b, _ := strings.Cut(arg, "..")
		return set("work_search[date_from]", strings.TrimSpace(a), "work_search[date_to]", strings.TrimSpace(b)), nil
	case "q", "search":
		return set("work_search[query]", arg), nil
	case "clear":
		return f.clearURL(list), nil
	}
	return "", fmt.Errorf("unknown filter %q — use: f tag …, f -tag …, f words A-B, f date A..B, f q …, f clear", verb)
}
