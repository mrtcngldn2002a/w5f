package sitecat

import (
	"bytes"
	"context"
	"encoding/xml"
	"net/url"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"w5f/internal/fetch"
)

var (
	reAuthorWord = regexp.MustCompile(`(?i)author|creator|writer|yazar`)
	reTitleWord  = regexp.MustCompile(`(?i)title|book|eser|kitap|name`)
	generalNames = map[string]bool{"q": true, "query": true, "s": true, "search": true, "term": true,
		"keywords": true, "keyword": true, "text": true, "k": true}
	reOptParam  = regexp.MustCompile(`\{[^}]*\?\}`)
	reNotField  = regexp.MustCompile(`(?i)sort|order|lang|limit|per_?page|count|format|country|category`)
	reNotSearch = regexp.MustCompile(`(?i)e-?mail|login|user|pass|subscribe|newsletter|comment|contact|message`)
)

// discoverAll lists every search method a page offers, best first.
func discoverAll(ctx context.Context, f *fetch.Fetcher, body []byte, pageURL string) []candidate {
	base, _ := url.Parse(pageURL)
	gq, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil
	}
	var out []candidate
	// 1. OpenSearch description.
	if h, ok := gq.Find(`link[rel~="search"][type="application/opensearchdescription+xml"]`).First().Attr("href"); ok {
		if t, atom := openSearchTemplate(ctx, f, base, h); t != "" {
			if atom { // results come as an Atom (OPDS) feed
				out = append(out, candidate{SearchSpec{Kind: "opds", Template: t}, "OpenSearch with Atom results (" + t + ")"})
			} else {
				out = append(out, candidate{SearchSpec{Kind: "opensearch", Template: t}, "OpenSearch (" + t + ")"})
			}
		}
	}
	// 2. OPDS feed with a search link.
	if h, ok := gq.Find(`link[type*="opds"]`).First().Attr("href"); ok {
		if t := opdsTemplate(ctx, f, base, h); t != "" {
			out = append(out, candidate{SearchSpec{Kind: "opds", Template: t}, "OPDS catalog search (" + t + ")"})
		}
	}
	// 3. HTML forms (GET, then POST).
	if spec, desc, ok := formSearch(gq, base, "get"); ok {
		out = append(out, candidate{spec, desc})
	}
	if spec, desc, ok := formSearch(gq, base, "post"); ok {
		out = append(out, candidate{spec, desc})
	}
	// 4. WordPress.
	if strings.Contains(strings.ToLower(gq.Find(`meta[name="generator"]`).AttrOr("content", "")), "wordpress") {
		u := *base
		u.Path, u.RawQuery = "/", "s={q}"
		out = append(out, candidate{SearchSpec{Kind: "wordpress", Template: u.String()}, "WordPress search (?s=)"})
	}
	return out
}

// DiscoverSearch returns the best search method of a page (kind "none"
// when there is none). The description is for the check page.
func DiscoverSearch(ctx context.Context, f *fetch.Fetcher, body []byte, pageURL string) (SearchSpec, string) {
	if cs := discoverAll(ctx, f, body, pageURL); len(cs) > 0 {
		return cs[0].Spec, cs[0].Desc
	}
	gq, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err == nil {
		post := false
		gq.Find("form").Each(func(_ int, f *goquery.Selection) {
			if strings.EqualFold(strings.TrimSpace(f.AttrOr("method", "")), "post") {
				post = true
			}
		})
		if post {
			return SearchSpec{Kind: "none"}, "the site's search form uses POST, which W5F does not submit"
		}
	}
	return SearchSpec{Kind: "none"}, "no search box, OpenSearch or OPDS link found"
}

// openSearchTemplate reads an OpenSearch description. An HTML results
// template is preferred; atom reports that only an Atom one exists.
func openSearchTemplate(ctx context.Context, f *fetch.Fetcher, base *url.URL, href string) (tmpl string, atom bool) {
	u, err := base.Parse(href)
	if err != nil {
		return "", false
	}
	resp, err := f.Get(ctx, u, fetch.Options{})
	if err != nil {
		return "", false
	}
	var d struct {
		URLs []struct {
			Type     string `xml:"type,attr"`
			Template string `xml:"template,attr"`
		} `xml:"Url"`
	}
	if xml.Unmarshal(resp.Body, &d) != nil {
		return "", false
	}
	for _, x := range d.URLs {
		if strings.HasPrefix(x.Type, "text/html") {
			return cleanTemplate(x.Template), false
		}
	}
	for _, x := range d.URLs {
		if strings.Contains(x.Type, "atom") {
			return cleanTemplate(x.Template), true
		}
	}
	return "", false
}

// cleanTemplate turns an OpenSearch template into a {q} template: optional
// parameters are dropped, common required ones get their first value.
func cleanTemplate(t string) string {
	t = strings.ReplaceAll(t, "{searchTerms}", "{q}")
	t = reOptParam.ReplaceAllString(t, "")
	for k, v := range map[string]string{"{startPage}": "1", "{startIndex}": "1", "{count}": "20", "{language}": "*", "{inputEncoding}": "UTF-8", "{outputEncoding}": "UTF-8"} {
		t = strings.ReplaceAll(t, k, v)
	}
	u, err := url.Parse(t)
	if err != nil {
		return t
	}
	q := u.Query()
	for k, vs := range q {
		if len(vs) == 0 || vs[0] == "" {
			q.Del(k)
		}
	}
	u.RawQuery = q.Encode()
	// {q} may sit in the path or the query; keep it literal in both.
	return strings.ReplaceAll(u.String(), "%7Bq%7D", "{q}")
}

func opdsTemplate(ctx context.Context, f *fetch.Fetcher, base *url.URL, href string) string {
	u, err := base.Parse(href)
	if err != nil {
		return ""
	}
	resp, err := f.Get(ctx, u, fetch.Options{})
	if err != nil {
		return ""
	}
	gq, err := goquery.NewDocumentFromReader(bytes.NewReader(resp.Body))
	if err != nil {
		return ""
	}
	link := gq.Find(`link[rel="search"]`).First()
	h, ok := link.Attr("href")
	if !ok {
		return ""
	}
	if strings.Contains(h, "{searchTerms}") {
		su, err := resp.URL.Parse(h)
		if err != nil {
			return ""
		}
		return cleanTemplate(strings.ReplaceAll(su.String(), "%7BsearchTerms%7D", "{searchTerms}"))
	}
	t, _ := openSearchTemplate(ctx, f, resp.URL, h)
	return t
}

type formInput struct {
	name, typ, value, hint string
}

// formSearch scores GET forms and builds general and field templates.
func formSearch(gq *goquery.Document, base *url.URL, method string) (SearchSpec, string, bool) {
	bestScore := 0
	var best SearchSpec
	fields := map[string]string{}
	gq.Find("form").Each(func(_ int, form *goquery.Selection) {
		m := strings.ToLower(strings.TrimSpace(form.AttrOr("method", "get")))
		if m == "" {
			m = "get"
		}
		if m != method {
			return
		}
		// Login and newsletter forms are never search forms.
		if notSearchForm(form) {
			return
		}
		action, err := base.Parse(form.AttrOr("action", ""))
		if err != nil {
			return
		}
		var inputs []formInput
		form.Find("input[name]").Each(func(_ int, in *goquery.Selection) {
			typ := strings.ToLower(in.AttrOr("type", "text"))
			inputs = append(inputs, formInput{name: in.AttrOr("name", ""), typ: typ, value: in.AttrOr("value", ""),
				hint: strings.ToLower(in.AttrOr("id", "") + " " + in.AttrOr("placeholder", "") + " " + in.AttrOr("aria-label", ""))})
		})
		var textIdx []int
		for i, in := range inputs {
			if in.typ == "text" || in.typ == "search" || in.typ == "" {
				textIdx = append(textIdx, i)
			}
		}
		if len(textIdx) == 0 {
			return
		}
		score := 0
		general := textIdx[0]
		for _, i := range textIdx {
			in := inputs[i]
			if in.typ == "search" {
				score += 3
				general = i
			}
			if generalNames[strings.ToLower(in.name)] {
				score += 2
				general = i
			}
		}
		meta := strings.ToLower(form.AttrOr("action", "") + " " + form.AttrOr("id", "") + " " + form.AttrOr("class", "") + " " + form.AttrOr("role", ""))
		if strings.Contains(meta, "search") {
			score += 2
		}
		if form.ParentsFiltered("nav,header").Length() > 0 {
			score++
		}
		// Submitting a POST form sends something to the site; only a form
		// that clearly says "search" is tried.
		if method == "post" && score < 2 {
			return
		}
		build := func(textInput int, set map[string]string) string {
			parts := []string{}
			for i, in := range inputs {
				switch {
				case i == textInput:
					parts = append(parts, url.QueryEscape(in.name)+"={q}")
				case in.typ == "hidden":
					parts = append(parts, url.QueryEscape(in.name)+"="+url.QueryEscape(in.value))
				}
			}
			for k, v := range set {
				parts = append(parts, url.QueryEscape(k)+"="+url.QueryEscape(v))
			}
			if method == "post" {
				return strings.Join(parts, "&") // a POST body template
			}
			u := *action
			u.RawQuery = strings.Join(parts, "&")
			return u.String()
		}
		// Field choices: a <select> with author/title options…
		selFields := map[string]string{}
		var selName, allValue string
		form.Find("select[name]").Each(func(_ int, sel *goquery.Selection) {
			// "Sort by author/title" menus are not a choice of search field.
			if reNotField.MatchString(sel.AttrOr("name", "") + " " + sel.AttrOr("id", "")) {
				return
			}
			sel.Find("option").Each(func(_ int, o *goquery.Selection) {
				v, t := o.AttrOr("value", collapse(o.Text())), collapse(o.Text())
				switch {
				case reAuthorWord.MatchString(v + " " + t):
					selFields["author"], selName = v, sel.AttrOr("name", "")
				case reTitleWord.MatchString(v + " " + t):
					selFields["title"], selName = v, sel.AttrOr("name", "")
				case allValue == "":
					allValue = v
				}
			})
		})
		spec := SearchSpec{Kind: "form"}
		if method == "post" {
			spec = SearchSpec{Kind: "post", Method: "POST"}
		}
		localFields := map[string]string{}
		if selName != "" {
			set := map[string]string{}
			if allValue != "" {
				set[selName] = allValue
			}
			spec.Template = build(general, set)
			for field, v := range selFields {
				localFields[field] = build(general, map[string]string{selName: v})
			}
		} else {
			spec.Template = build(general, nil)
			// …or separate author/title inputs in the same form.
			for _, i := range textIdx {
				label := inputs[i].name + " " + inputs[i].hint
				if reAuthorWord.MatchString(label) {
					localFields["author"] = build(i, nil)
				} else if reTitleWord.MatchString(label) && !generalNames[strings.ToLower(inputs[i].name)] {
					localFields["title"] = build(i, nil)
				}
			}
		}
		if method == "post" { // the address is the action; bodies carry {q}
			spec.Body, spec.Template = spec.Template, action.String()
		}
		// Separate author/title forms on the page contribute fields too.
		if len(textIdx) == 1 && method == "get" {
			label := inputs[textIdx[0]].name + " " + inputs[textIdx[0]].hint + " " + meta
			if reAuthorWord.MatchString(label) {
				fields["author"] = spec.Template
			} else if reTitleWord.MatchString(label) && !generalNames[strings.ToLower(inputs[textIdx[0]].name)] {
				fields["title"] = spec.Template
			}
		}
		if score > bestScore || (score == bestScore && best.Template == "") {
			bestScore, best = score, spec
			if len(localFields) > 0 {
				best.Fields = localFields
			}
		}
	})
	if best.Template == "" {
		return SearchSpec{}, "", false
	}
	for k, v := range fields {
		if best.Fields == nil {
			best.Fields = map[string]string{}
		}
		if _, ok := best.Fields[k]; !ok && v != best.Template {
			best.Fields[k] = v
		}
	}
	if method == "post" {
		desc := "POST search form (" + best.Template + ")"
		if len(best.Fields) > 0 {
			desc += " with fields"
		}
		return best, desc, true
	}
	desc := "search form (" + best.Template + ")"
	if len(best.Fields) > 0 {
		desc += " with fields:"
		for _, k := range []string{"author", "title"} {
			if best.Fields[k] != "" {
				desc += " " + k
			}
		}
	}
	return best, desc, true
}

// notSearchForm recognises login, newsletter, contact and comment forms,
// which are never submitted.
func notSearchForm(form *goquery.Selection) bool {
	if form.Find(`input[type="password"], input[type="email"], textarea`).Length() > 0 {
		return true
	}
	names := form.AttrOr("action", "") + " " + form.AttrOr("id", "") + " " + form.AttrOr("class", "")
	form.Find("input[name]").Each(func(_ int, in *goquery.Selection) {
		if !strings.EqualFold(in.AttrOr("type", ""), "hidden") {
			names += " " + in.AttrOr("name", "") + " " + in.AttrOr("id", "")
		}
	})
	return reNotSearch.MatchString(names)
}
