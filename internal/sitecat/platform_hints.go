package sitecat

import (
	"context"
	"net/url"
	"path"
	"regexp"
	"strings"
)

var reOJSJournal = regexp.MustCompile(`^(/index\.php/[^/]+)`)

// detectHints adds the known search addresses of common platforms, for
// when their own search form is not found on the page.
func detectHints(ctx context.Context, s *site) []candidate {
	origin := s.Home.Scheme + "://" + s.Home.Host
	var out []candidate
	add := func(t, desc string) {
		out = append(out, candidate{Spec: SearchSpec{Kind: "form", Template: t}, Desc: desc})
	}
	switch g := s.Generator; {
	case strings.Contains(g, "open journal systems"):
		if m := reOJSJournal.FindStringSubmatch(s.Home.Path); m != nil {
			add(origin+m[1]+"/search/search?query={q}", "OJS journal search")
		}
	case strings.Contains(g, "eprints"):
		add(origin+"/cgi/search/simple?q={q}", "EPrints search")
	case strings.Contains(g, "mediawiki"):
		add(origin+mediawikiScript(s)+"/index.php?search={q}&title=Special:Search&fulltext=1&ns0=1&ns6=1", "MediaWiki search (pages and files)")
	case strings.Contains(g, "blogger"):
		add(origin+"/search?q={q}", "Blogger search")
	}
	return out
}

// mediawikiScript finds the wiki's script path ("/w") from its EditURI link.
func mediawikiScript(s *site) string {
	if s.Doc == nil {
		return ""
	}
	if h, ok := s.Doc.Find(`link[rel="EditURI"]`).First().Attr("href"); ok {
		if u, err := s.Home.Parse(h); err == nil {
			return strings.TrimSuffix(path.Dir(u.Path), "/")
		}
	}
	return ""
}

// detectOPDSPath probes the /opds address used by Calibre's content server
// and Calibre-Web, only on pages that mention calibre or OPDS.
func detectOPDSPath(ctx context.Context, s *site) []candidate {
	l := strings.ToLower(string(s.Body))
	if !strings.Contains(l, "calibre") && !strings.Contains(l, "opds") {
		return nil
	}
	u := &url.URL{Scheme: s.Home.Scheme, Host: s.Home.Host, Path: "/opds"}
	if t := opdsTemplate(ctx, s.F, s.Home, u.String()); t != "" {
		return []candidate{{Spec: SearchSpec{Kind: "opds", Template: t}, Desc: "OPDS catalog at " + u.String()}}
	}
	return nil
}
