package fetch

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

var ddgID = regexp.MustCompile(`'/\.well-known/ddos-guard/id/([A-Za-z0-9_-]{1,128})'`)

// bootstrapDDG follows only the two cookie pixels in DDoS-Guard's public
// check.js. It does not evaluate downloaded JavaScript or fabricate challenge
// proofs. Interactive/JS fingerprint challenges remain explicit errors.
func (f *Fetcher) bootstrapDDG(ctx context.Context, site *url.URL) bool {
	if site.Scheme != "https" || f.Client.Jar == nil {
		return false
	}
	get := func(raw string) ([]byte, error) {
		req, err := http.NewRequestWithContext(ctx, "GET", raw, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", CatalogUserAgent)
		req.Header.Set("Referer", site.Scheme+"://"+site.Host+"/")
		f.wait(ctx, req.URL.Host)
		r, err := f.Client.Do(req)
		if err != nil {
			return nil, err
		}
		defer r.Body.Close()
		return io.ReadAll(io.LimitReader(r.Body, 64<<10))
	}
	b, err := get("https://check.ddos-guard.net/check.js")
	if err != nil {
		return false
	}
	m := ddgID.FindSubmatch(b)
	if m == nil {
		return false
	}
	id := string(m[1])
	if _, err = get(site.Scheme + "://" + site.Host + "/.well-known/ddos-guard/id/" + id); err != nil {
		return false
	}
	_, _ = get("https://check.ddos-guard.net/set/id/" + id)
	for _, c := range f.Client.Jar.Cookies(site) {
		if strings.HasPrefix(c.Name, "__ddg1_") {
			return true
		}
	}
	return false
}
