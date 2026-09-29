// Package catalog gives what W5F shows a library call number in the style
// of the design (FIC·SCP·173, PER·ARKEOF·2026-09-24, BK·GUT·345).
package catalog

import (
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"

	"w5f/internal/doc"
)

var reSCPNum = regexp.MustCompile(`^scp-(\d{3,4})$`)

// Number returns the call number of a page; "" for W5F's own menus.
func Number(target string, d *doc.Document) string {
	if d != nil && d.Catalog != "" {
		return d.Catalog
	}
	u, err := url.Parse(target)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	slug := orHome(lastSegment(u.Path))
	switch {
	case host == "scp-wiki.wikidot.com":
		if m := reSCPNum.FindStringSubmatch(slug); m != nil {
			return "FIC·SCP·" + m[1]
		}
		return "FIC·SCP·" + cut(slug, 24)
	case host == "wanderers-library.wikidot.com":
		return "FIC·WL·" + cut(slug, 24)
	case host == "backrooms-wiki.wikidot.com":
		return "FIC·BR·" + cut(slug, 24)
	case strings.HasSuffix(host, ".wikidot.com"):
		return "FIC·" + code(strings.TrimSuffix(host, ".wikidot.com"), 8) + "·" + cut(slug, 24)
	case host == "reddit.com" || strings.HasSuffix(host, ".reddit.com"):
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) >= 2 && parts[0] == "r" {
			return "WEB·RDT·" + cut(strings.ToLower(parts[1]), 24)
		}
		return "WEB·RDT"
	}
	return "WEB·" + code(strings.Split(host, ".")[0], 8) + "·" + cut(slug, 24)
}

// Feed numbers a periodical item.
func Feed(feedID string, published time.Time) string {
	n := "PER·" + code(feedID, 6)
	if !published.IsZero() {
		n += "·" + published.Format("2006-01-02")
	}
	return n
}

// Book numbers a library book from its source ("gutenberg:345", "se:slug",
// "site:<catalog>:<url>"); local files get their library id.
func Book(source string, id int64) string {
	switch {
	case strings.HasPrefix(source, "libgen:"):
		return "BK·LG·" + strings.TrimPrefix(source, "libgen:")
	case strings.HasPrefix(source, "gutenberg:"):
		return "BK·GUT·" + strings.TrimPrefix(source, "gutenberg:")
	case strings.HasPrefix(source, "se:"):
		return "BK·SE·" + cut(lastSegment(strings.TrimPrefix(source, "se:")), 24)
	case strings.HasPrefix(source, "site:"):
		cat := strings.SplitN(strings.TrimPrefix(source, "site:"), ":", 2)[0]
		return fmt.Sprintf("BK·%s·%d", code(cat, 8), id)
	}
	return fmt.Sprintf("BK·LOC·%d", id)
}

var labels = map[string]string{"scp": "[SCP]", "wiki": "[WIKI]", "web": "[WEB]", "reddit": "[RDT]",
	"feed": "[RSS]", "book": "[BK]", "note": "[NOT]", "clip": "[KES]", "saved": "[SAV]"}

// Label is the short kind tag shown in lists ("[RSS]", "[BK]"…).
func Label(kind string) string {
	if l, ok := labels[kind]; ok {
		return l
	}
	return "[" + strings.ToUpper(kind) + "]"
}

func lastSegment(p string) string {
	p = strings.Trim(p, "/")
	if p == "" {
		return ""
	}
	s := path.Base(p)
	if un, err := url.PathUnescape(s); err == nil {
		s = un
	}
	return strings.ToLower(s)
}

func orHome(s string) string {
	if s == "" {
		return "home"
	}
	return s
}

func cut(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

func code(s string, n int) string {
	s = strings.Map(func(r rune) rune {
		if r == '-' || r == '_' || r == '.' {
			return -1
		}
		return r
	}, s)
	return cut(strings.ToUpper(s), n)
}
