package sitecat

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"w5f/internal/fetch"
	"w5f/internal/libgen"
)

// Finding is one ✓/✗ line on the check page.
type Finding struct {
	OK   bool
	Text string
}

// Report is the outcome of inspecting a site.
type Report struct {
	Profile  Profile
	Findings []Finding
	Sample   []Result
	Formats  []string
	CanAdd   bool
}

func (r *Report) add(ok bool, text string) { r.Findings = append(r.Findings, Finding{ok, text}) }

// Probe inspects a site: every candidate search method is tried with the
// test word until one returns results; then the downloads of the first
// result are listed (without downloading a book).
func Probe(ctx context.Context, f *fetch.Fetcher, raw, testWord string) (*Report, error) {
	f, err := f.ForCatalog()
	if err != nil {
		return nil, err
	}
	raw = strings.TrimSpace(raw)
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	if testWord == "" {
		testWord = "history"
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("catalog address must be an HTTP(S) URL")
	}
	if libgen.KnownHost(raw) {
		return probeLibgen(ctx, f, raw, testWord)
	}
	if isAnna(raw) {
		return probeAnna(ctx, f, raw, testWord)
	}
	resp, err := f.Get(ctx, u, fetch.Options{Revalidate: true})
	if err != nil {
		var challenge *fetch.ChallengeError
		if errors.As(err, &challenge) {
			p := Profile{Home: raw, Name: u.Hostname(), Added: time.Now().UTC()}
			p.ApplyDefaults()
			rep := &Report{Profile: p}
			rep.add(false, challenge.Error())
			return rep, nil
		}
		return nil, err
	}
	rep := &Report{}
	p := Profile{Home: resp.URL.String(), Added: time.Now().UTC(), Name: siteName(resp.Body, resp.URL)}
	p.ApplyDefaults()
	if wall := botWall(resp.Body); wall != "" {
		rep.add(false, wall)
		rep.Profile = p
		return rep, nil
	}
	s := newSite(ctx, f, resp)
	var cands []candidate
	for _, d := range detectors {
		cands = append(cands, d(ctx, s)...)
	}
	searchable := false
	for _, c := range cands {
		if c.Spec.Kind != "browse" {
			searchable = true
		}
	}
	if !searchable {
		rep.add(false, "search: no search box, OpenSearch or OPDS link found")
	}
	// Search methods first, then the search engine; a browse-only front
	// page is the last resort, since it cannot be searched.
	var searches, browse []candidate
	for _, c := range cands {
		if c.Spec.Kind == "browse" {
			browse = append(browse, c)
		} else {
			searches = append(searches, c)
		}
	}
	adopted := false
	for i, c := range searches {
		if i == maxAttempts {
			break
		}
		if tryCandidate(ctx, f, rep, &p, c, testWord, resp) {
			adopted = true
			break
		}
	}
	if !adopted && engineDetector != nil {
		for _, c := range engineDetector(ctx, s) {
			if tryCandidate(ctx, f, rep, &p, c, testWord, resp) {
				adopted = true
				break
			}
		}
	}
	for _, c := range browse {
		if adopted {
			break
		}
		adopted = tryCandidate(ctx, f, rep, &p, c, testWord, resp)
	}
	if !adopted {
		if needsJS(resp.Body) {
			rep.add(false, "this site builds its pages with JavaScript; W5F cannot search it")
		}
		rep.Profile = p
		return rep, nil
	}
	checkDownloads(ctx, f, rep, &p)
	rep.CanAdd = true
	rep.Profile = p
	return rep, nil
}

// layoutKind reports kinds whose results come from a learned HTML layout.
func layoutKind(kind string) bool {
	switch kind {
	case "opensearch", "form", "post", "wordpress", "browse":
		return true
	}
	return false
}

// tryCandidate runs one candidate with the test word and records the
// attempt. On success the profile takes the candidate's search.
func tryCandidate(ctx context.Context, f *fetch.Fetcher, rep *Report, p *Profile, c candidate, word string, home *fetch.Response) bool {
	trial := *p
	trial.Search = c.Spec
	trial.Layout = Layout{}
	trial.ApplyDefaults()
	var rs []Result
	more := false
	failure, note := "", ""
	b := backendFor(c.Spec.Kind)
	switch {
	case c.Spec.Kind == "browse":
		// Browse-only: the home page itself carries a book list. Short
		// lists are usually filters or menus, not books.
		l, found := LearnLayout(home.Body, home.URL.String())
		if len(found) < 5 {
			return false // a fallback, not worth a ✗ line
		}
		trial.Layout, rs = l, found
		note = fmt.Sprintf("browse-only: the front page lists %d items (check the samples); the catalog can be browsed, not searched", len(found))
	case b == nil:
		failure = "not supported"
	default:
		if lp, ok := b.(localProber); ok {
			var err error
			rs, note, err = lp.ProbeLocal(ctx, f, &trial, word)
			if err != nil {
				failure = err.Error()
			}
			break
		}
		req, _ := b.First(&trial, Query{Words: word})
		resp, err := doRequest(ctx, f, req)
		if err != nil {
			failure = err.Error()
			break
		}
		po, err := b.Page(ctx, f, &trial, 1, resp.Body, resp.URL.String())
		if err != nil {
			failure = err.Error()
			break
		}
		rs, more = po.Results, po.Next != nil
		if len(rs) == 0 && needsJS(resp.Body) {
			failure = "results are built with JavaScript"
		}
	}
	need := 1
	if layoutKind(c.Spec.Kind) {
		need = 3
	}
	if failure == "" && len(rs) < need {
		failure = fmt.Sprintf("no result list found for “%s”", word)
	}
	if failure != "" {
		rep.add(false, "search: "+c.Desc+" — "+failure)
		return false
	}
	switch {
	case c.Spec.Kind == "browse":
		rep.add(true, note)
	case note != "":
		rep.add(true, "search: "+c.Desc+" — "+note)
	default:
		rep.add(true, fmt.Sprintf("search: %s — %d results for “%s”", c.Desc, len(rs), word))
	}
	if trial.Layout.Item != "" && c.Spec.Kind != "browse" {
		rep.add(true, "results: in “"+trial.Layout.Parent+" > "+trial.Layout.Item+"”")
	}
	if more {
		rep.add(true, "pages: more result pages found — full search follows them (up to "+itoa(trial.Search.MaxPages)+")")
	}
	rep.Sample = rs[:min(5, len(rs))]
	*p = trial
	return true
}

// checkDownloads lists the formats of the first sample result.
func checkDownloads(ctx context.Context, f *fetch.Fetcher, rep *Report, p *Profile) {
	if len(rep.Sample) == 0 {
		return
	}
	ds, err := itemDownloads(ctx, f, p, rep.Sample[0].URL)
	seen := map[string]bool{}
	for _, d := range ds {
		if !seen[d.Format] {
			seen[d.Format] = true
			rep.Formats = append(rep.Formats, d.Format)
		}
	}
	switch {
	case err != nil:
		rep.add(false, "downloads: could not open the first result ("+err.Error()+")")
	case len(ds) == 0:
		rep.add(false, "downloads: no book files on the first result (other books may still have them)")
	default:
		rep.add(true, "downloads: "+strings.ToUpper(strings.Join(rep.Formats, ", "))+" on the first result")
	}
}

func siteName(body []byte, u *url.URL) string {
	gq, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err == nil {
		for _, sel := range []string{`meta[property="og:site_name"]`, `meta[name="application-name"]`} {
			if n := collapse(gq.Find(sel).First().AttrOr("content", "")); n != "" && len([]rune(n)) <= 50 {
				return n
			}
		}
		t := collapse(gq.Find("title").First().Text())
		for _, sep := range []string{" | ", " - ", " — ", " · ", ": "} {
			if i := strings.Index(t, sep); i > 0 {
				t = t[:i]
			}
		}
		if t != "" && len([]rune(t)) <= 50 {
			return t
		}
	}
	return strings.TrimPrefix(u.Hostname(), "www.")
}

// botWall recognises challenge and captcha pages. A captcha widget on an
// otherwise ordinary page (a contact form) is not a wall; a page that is
// little more than the captcha is.
func botWall(body []byte) string {
	if reason := fetch.ChallengeReason(body); reason != "" {
		return "bot check: " + reason
	}
	return ""
}

// visibleTextLen is the amount of readable text on a page.
func visibleTextLen(body []byte) int {
	gq, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return 0
	}
	gq.Find("script, style, noscript").Remove()
	return len([]rune(collapse(gq.Find("body").Text())))
}

func needsJS(body []byte) bool {
	l := strings.ToLower(string(body))
	return strings.Contains(l, "<noscript") || strings.Count(l, "<script") >= 3
}

var reSearchLink = regexp.MustCompile(`(?i)^(search|advanced search|search books|search the catalog(ue)?|catalog(ue)? search|find a book|find books|book search|ara|kitap ara|detaylı arama)$`)

// searchPageLink finds a same-site link to a separate search page.
func searchPageLink(body []byte, base *url.URL) *url.URL {
	gq, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil
	}
	var found *url.URL
	gq.Find("a[href]").EachWithBreak(func(_ int, a *goquery.Selection) bool {
		if !reSearchLink.MatchString(collapse(a.Text())) {
			return true
		}
		if u, err := base.Parse(strings.TrimSpace(a.AttrOr("href", ""))); err == nil && sameSite(u, base) && u.String() != base.String() {
			u.Fragment = ""
			found = u
			return false
		}
		return true
	})
	return found
}
