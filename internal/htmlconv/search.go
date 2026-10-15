package htmlconv

import (
	"bytes"
	"net/url"
	"sort"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"w5f/internal/doc"
)

// maxSearches is how many search boxes of one page are kept.
const maxSearches = 3

// searchNames are the usual names of a search box (q, WordPress's s …).
var searchNames = map[string]bool{
	"q": true, "query": true, "s": true, "search": true, "search_query": true, "searchterm": true,
	"searchtext": true, "keywords": true, "keyword": true, "k": true, "kw": true, "term": true,
	"terms": true, "text": true, "find": true, "qt": true,
}

// SearchForms finds a page's search boxes: GET forms with one box to type
// words in, looked for in the whole page (Readability drops headers and
// sidebars, where they usually sit). Forms that ask for more (a login, a
// comment, a newsletter address), send their answer by POST or work only
// with JavaScript are left out.
func SearchForms(body []byte, pageURL string) []doc.Search {
	base, err := url.Parse(pageURL)
	if err != nil {
		return nil
	}
	root, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil
	}
	type found struct {
		s     doc.Search
		score int
	}
	var all []found
	seen := map[string]bool{}
	var visit func(n *html.Node, inSearch bool)
	visit = func(n *html.Node, inSearch bool) {
		if n.Type == html.ElementNode {
			inSearch = inSearch || strings.EqualFold(attr(n, "role"), "search") || n.DataAtom == atom.Search
			if n.DataAtom == atom.Form {
				if s, score, ok := searchForm(n, base); ok {
					if inSearch {
						score += 2
					}
					if key := s.URL(""); !seen[key] {
						seen[key] = true // a page often has a desktop and a mobile copy
						all = append(all, found{s, score})
					}
				}
				return
			}
		}
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			visit(ch, inSearch)
		}
	}
	visit(root, false)
	sort.SliceStable(all, func(i, j int) bool { return all[i].score > all[j].score })
	var out []doc.Search
	for _, f := range all[:min(len(all), maxSearches)] {
		out = append(out, f.s)
	}
	return out
}

// searchForm reads one form. score is how much it looks like a search
// (0: it does not, though it could be sent).
func searchForm(form *html.Node, base *url.URL) (s doc.Search, score int, ok bool) {
	if m := strings.ToLower(strings.TrimSpace(attr(form, "method"))); m != "" && m != "get" {
		return s, 0, false
	}
	action := strings.TrimSpace(attr(form, "action"))
	low := strings.ToLower(action)
	// Wikidot's search says action="dummy" and is sent by its script.
	if strings.HasPrefix(low, "javascript:") || low == "dummy" {
		return s, 0, false
	}
	au, err := url.Parse(action)
	if err != nil {
		return s, 0, false
	}
	target := base.ResolveReference(au)
	if target.Scheme != "http" && target.Scheme != "https" {
		return s, 0, false
	}
	target.RawQuery, target.Fragment = "", "" // a GET form replaces the query

	var boxes []*html.Node
	var params []doc.KV
	var submit *html.Node
	bad := false
	walk(form, func(x *html.Node) bool {
		if x.Type != html.ElementNode || x == form {
			return true
		}
		if hasAttr(x, "disabled") {
			return false
		}
		name := attr(x, "name")
		switch x.DataAtom {
		case atom.Input:
			switch strings.ToLower(attr(x, "type")) {
			case "", "text", "search":
				if name != "" {
					boxes = append(boxes, x)
				}
			case "password", "email", "file":
				bad = true
			case "checkbox", "radio":
				if name != "" && hasAttr(x, "checked") {
					v := attr(x, "value")
					if !hasAttr(x, "value") {
						v = "on"
					}
					params = append(params, doc.KV{Key: name, Value: v})
				}
			case "submit":
				if submit == nil {
					submit = x
				}
			case "image", "reset", "button":
			default: // hidden, number, date …: sent as they are
				if name != "" {
					params = append(params, doc.KV{Key: name, Value: attr(x, "value")})
				}
			}
		case atom.Textarea:
			bad = true
		case atom.Select:
			if name != "" {
				for _, v := range selected(x) {
					params = append(params, doc.KV{Key: name, Value: v})
				}
			}
			return false
		case atom.Button:
			if t := strings.ToLower(attr(x, "type")); (t == "" || t == "submit") && submit == nil {
				submit = x
			}
			return false
		}
		return true
	})
	if bad || len(boxes) == 0 {
		return s, 0, false
	}
	box := boxes[0]
	if len(boxes) > 1 {
		box = nil
		for _, b := range boxes {
			if strings.EqualFold(attr(b, "type"), "search") || searchNames[strings.ToLower(attr(b, "name"))] {
				box = b
				break
			}
		}
		if box == nil {
			return s, 0, false
		}
		for _, b := range boxes {
			if b != box {
				params = append(params, doc.KV{Key: attr(b, "name"), Value: attr(b, "value")})
			}
		}
	}
	// Pressing enter sends the form's first button along, as a browser does.
	if submit != nil && attr(submit, "name") != "" {
		params = append(params, doc.KV{Key: attr(submit, "name"), Value: attr(submit, "value")})
	}

	if strings.EqualFold(attr(box, "type"), "search") {
		score += 2
	}
	if searchNames[strings.ToLower(attr(box, "name"))] {
		score++
	}
	if p := strings.ToLower(target.Path); strings.Contains(p, "search") || strings.Contains(p, "find") {
		score++
	}
	for _, n := range []*html.Node{form, box} {
		if strings.Contains(strings.ToLower(attr(n, "id")+" "+attr(n, "class")+" "+attr(n, "aria-label")), "search") {
			score++
			break
		}
	}
	if score == 0 {
		return s, 0, false
	}
	return doc.Search{Action: target.String(), Field: attr(box, "name"), Params: params, Label: boxLabel(form, box, submit)}, score, true
}

// boxLabel is what the page calls its box: its placeholder, its label or
// its button.
func boxLabel(form, box, submit *html.Node) string {
	for _, k := range []string{"placeholder", "aria-label", "title"} {
		if v := collapseSpace(attr(box, k)); v != "" {
			return clip(v)
		}
	}
	if id := attr(box, "id"); id != "" {
		if l := find(form.Parent, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.DataAtom == atom.Label && attr(n, "for") == id
		}); l != nil {
			if v := collapseSpace(textContent(l)); v != "" {
				return clip(v)
			}
		}
	}
	if submit != nil {
		v := attr(submit, "value")
		if submit.DataAtom == atom.Button {
			v = textContent(submit)
		}
		if v = collapseSpace(v); v != "" && !strings.EqualFold(v, "go") && !strings.EqualFold(v, "submit") {
			return clip(v)
		}
	}
	return ""
}

func clip(s string) string {
	if r := []rune(s); len(r) > 60 {
		return string(r[:59]) + "…"
	}
	return s
}

// selected is what a select sends: its chosen option, or else the first;
// a list that allows several sends each chosen one, and nothing when none is.
func selected(sel *html.Node) []string {
	multiple := hasAttr(sel, "multiple")
	var first *html.Node
	var chosen []*html.Node
	walk(sel, func(n *html.Node) bool {
		if n.Type == html.ElementNode && n.DataAtom == atom.Option && !hasAttr(n, "disabled") {
			if first == nil {
				first = n
			}
			if hasAttr(n, "selected") && (multiple || len(chosen) == 0) {
				chosen = append(chosen, n)
			}
		}
		return true
	})
	if len(chosen) == 0 && !multiple && first != nil {
		chosen = append(chosen, first)
	}
	var out []string
	for _, o := range chosen {
		if hasAttr(o, "value") {
			out = append(out, attr(o, "value"))
		} else {
			out = append(out, collapseSpace(textContent(o)))
		}
	}
	return out
}

func hasAttr(n *html.Node, key string) bool {
	for _, a := range n.Attr {
		if a.Key == key {
			return true
		}
	}
	return false
}
