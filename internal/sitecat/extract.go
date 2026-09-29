package sitecat

import (
	"bytes"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
)

// Result is one entry of a site's result list.
type Result struct {
	Title string
	URL   string
	Extra string // author, year, format hints…
}

// sig is an element's structural signature: tag plus sorted classes.
func sig(n *html.Node) string {
	if n == nil || n.Type != html.ElementNode {
		return ""
	}
	var cls []string
	for _, a := range n.Attr {
		if a.Key == "class" {
			cls = strings.Fields(a.Val)
		}
	}
	sort.Strings(cls)
	if len(cls) == 0 {
		return n.Data
	}
	return n.Data + "." + strings.Join(cls, ".")
}

// pathSig is the signature of n and up to two ancestors, outermost first.
func pathSig(n *html.Node) string {
	var parts []string
	for i := 0; i < 3 && n != nil && n.Type == html.ElementNode; i++ {
		parts = append([]string{sig(n)}, parts...)
		n = n.Parent
	}
	return strings.Join(parts, " > ")
}

var chromeTags = map[string]bool{"nav": true, "header": true, "footer": true, "aside": true}

func inChrome(n *html.Node) bool {
	for p := n.Parent; p != nil; p = p.Parent {
		if p.Type == html.ElementNode && chromeTags[p.Data] {
			return true
		}
	}
	return false
}

func sameSite(a, b *url.URL) bool {
	ha := strings.TrimPrefix(strings.ToLower(a.Hostname()), "www.")
	hb := strings.TrimPrefix(strings.ToLower(b.Hostname()), "www.")
	return ha == hb || strings.HasSuffix(ha, "."+hb) || strings.HasSuffix(hb, "."+ha)
}

func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

// bestLink picks a member's main link: one inside a heading if any, else the
// one with the longest text. Links to other sites, fragments and javascript
// are ignored.
func bestLink(member *goquery.Selection, base *url.URL) (title, href string) {
	var best *goquery.Selection
	bestScore := -1
	member.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		raw := strings.TrimSpace(a.AttrOr("href", ""))
		if raw == "" || strings.HasPrefix(raw, "#") || strings.HasPrefix(strings.ToLower(raw), "javascript:") {
			return
		}
		u, err := base.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || !sameSite(u, base) {
			return
		}
		text := collapse(a.Text())
		if utf8.RuneCountInString(text) < 3 {
			return
		}
		score := utf8.RuneCountInString(text)
		if a.ParentsFiltered("h1,h2,h3,h4,h5,h6").Length() > 0 {
			score += 1000
		}
		if score > bestScore {
			best, bestScore = a, score
		}
	})
	if best == nil {
		return "", ""
	}
	u, _ := base.Parse(strings.TrimSpace(best.AttrOr("href", "")))
	u.Fragment = ""
	return collapse(best.Text()), u.String()
}

func resultOf(member *goquery.Selection, base *url.URL) (Result, bool) {
	title, href := bestLink(member, base)
	if href == "" {
		return Result{}, false
	}
	extra := collapse(strings.Replace(collapse(member.Text()), title, "", 1))
	extra = strings.Trim(extra, " ,;·-–—|")
	if r := []rune(extra); len(r) > 160 {
		extra = string(r[:157]) + "…"
	}
	return Result{Title: title, URL: href, Extra: extra}, true
}

type group struct {
	parent  *html.Node
	item    string
	members []*goquery.Selection
}

// LearnLayout finds the most likely result list on a page.
func LearnLayout(body []byte, pageURL string) (Layout, []Result) {
	gq, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return Layout{}, nil
	}
	base, _ := url.Parse(pageURL)
	var groups []group
	gq.Find("*").Each(func(_ int, s *goquery.Selection) {
		n := s.Get(0)
		bySig := map[string][]*goquery.Selection{}
		var order []string
		s.Children().Each(func(_ int, c *goquery.Selection) {
			k := sig(c.Get(0))
			if _, ok := bySig[k]; !ok {
				order = append(order, k)
			}
			bySig[k] = append(bySig[k], c)
		})
		for _, k := range order {
			if len(bySig[k]) >= 3 {
				groups = append(groups, group{parent: n, item: k, members: bySig[k]})
			}
		}
	})
	bestScore := 0.0
	bestChrome := false
	var best group
	var bestResults []Result
	for _, g := range groups {
		var rs []Result
		hrefs := map[string]bool{}
		textLen := 0
		itemLike := 0
		for _, m := range g.members {
			r, ok := resultOf(m, base)
			if !ok {
				continue
			}
			rs = append(rs, r)
			hrefs[r.URL] = true
			t := utf8.RuneCountInString(collapse(m.Text()))
			if t > 300 {
				t = 300
			}
			textLen += t
			if u, err := url.Parse(r.URL); err == nil && (strings.ContainsAny(u.Path+u.RawQuery, "0123456789") ||
				strings.Count(strings.Trim(u.Path, "/"), "/") >= strings.Count(strings.Trim(base.Path, "/"), "/")) {
				itemLike++
			}
		}
		if len(rs) < 3 || len(hrefs) < 3 || float64(len(rs)) < 0.6*float64(len(g.members)) {
			continue
		}
		avg := float64(textLen) / float64(len(rs))
		if avg < 8 {
			continue
		}
		score := float64(len(rs)) * avg
		if itemLike*2 >= len(rs) {
			score *= 1.5
		}
		// Lists inside page chrome (menus, footers, sidebars) are only a
		// fallback: a real result list elsewhere always wins.
		chrome := inChrome(g.members[0].Get(0)) || chromeTags[g.parent.Data]
		if chrome {
			score *= 0.3
		}
		if bestResults == nil || (bestChrome && !chrome) || (chrome == bestChrome && score > bestScore) {
			bestScore, best, bestResults, bestChrome = score, g, rs, chrome
		}
	}
	if bestResults == nil {
		return Layout{}, nil
	}
	return Layout{Parent: pathSig(best.parent), Item: best.item}, bestResults
}

// Results extracts results using a stored layout.
func Results(body []byte, pageURL string, l Layout) []Result {
	if l.Item == "" {
		return nil
	}
	gq, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil
	}
	base, _ := url.Parse(pageURL)
	var out []Result
	gq.Find("*").Each(func(_ int, s *goquery.Selection) {
		if pathSig(s.Get(0)) != l.Parent {
			return
		}
		s.Children().Each(func(_ int, c *goquery.Selection) {
			if sig(c.Get(0)) != l.Item {
				return
			}
			if r, ok := resultOf(c, base); ok {
				out = append(out, r)
			}
		})
	})
	return out
}

// Extract uses the stored layout and re-learns when it yields nothing.
func Extract(body []byte, pageURL string, l Layout) ([]Result, Layout) {
	if rs := Results(body, pageURL, l); len(rs) > 0 {
		return rs, l
	}
	nl, rs := LearnLayout(body, pageURL)
	if len(rs) == 0 {
		return nil, l
	}
	return rs, nl
}

var nextWords = map[string]bool{"next": true, "next page": true, "›": true, "»": true, ">": true, "→": true,
	"sonraki": true, "next ›": true, "next »": true, "next >": true, "next →": true, "older": true}

var (
	pageKeys   = []string{"page", "p", "pg"}
	offsetKeys = []string{"start", "offset", "start_index"}
	reDigits   = regexp.MustCompile(`^\d+$`)
)

// NextPage finds the address of the next result page.
func NextPage(body []byte, pageURL string) string {
	gq, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return ""
	}
	base, err := url.Parse(pageURL)
	if err != nil {
		return ""
	}
	abs := func(h string) string {
		u, err := base.Parse(strings.TrimSpace(h))
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.String() == base.String() {
			return ""
		}
		u.Fragment = ""
		return u.String()
	}
	if h, ok := gq.Find(`link[rel~="next"], a[rel~="next"]`).First().Attr("href"); ok {
		if u := abs(h); u != "" {
			return u
		}
	}
	var found string
	gq.Find("a[href]").EachWithBreak(func(_ int, a *goquery.Selection) bool {
		t := strings.ToLower(collapse(a.Text()))
		if nextWords[t] || strings.HasPrefix(t, "next ") {
			if u := abs(a.AttrOr("href", "")); u != "" {
				found = u
				return false
			}
		}
		return true
	})
	if found != "" {
		return found
	}
	q := base.Query()
	// A page number in the current address: increment it.
	for _, k := range pageKeys {
		if v := q.Get(k); reDigits.MatchString(v) {
			n, _ := strconv.Atoi(v)
			q.Set(k, strconv.Itoa(n+1))
			u := *base
			u.RawQuery = q.Encode()
			return u.String()
		}
	}
	// Links on the page that differ only in a page/offset parameter: take the
	// smallest value above the current one.
	cur := map[string]int{}
	for _, k := range append(append([]string{}, pageKeys...), offsetKeys...) {
		if v, err := strconv.Atoi(q.Get(k)); err == nil {
			cur[k] = v
		}
	}
	bestVal, best := -1, ""
	gq.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		u, err := base.Parse(strings.TrimSpace(a.AttrOr("href", "")))
		if err != nil || u.Path != base.Path || !sameSite(u, base) {
			return
		}
		uq := u.Query()
		for _, k := range append(append([]string{}, pageKeys...), offsetKeys...) {
			v, err := strconv.Atoi(uq.Get(k))
			if err != nil || v <= cur[k] {
				continue
			}
			other := u.Query()
			other.Del(k)
			mine := base.Query()
			mine.Del(k)
			if other.Encode() != mine.Encode() {
				continue
			}
			if bestVal == -1 || v < bestVal {
				u.Fragment = ""
				bestVal, best = v, u.String()
			}
		}
	})
	return best
}
