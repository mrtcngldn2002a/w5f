package fiction

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"w5f/internal/doc"
	"w5f/internal/fetch"
	"w5f/internal/htmlconv"
)

// errVerification is returned when a site answers with a bot check.
type errVerification struct{ site, reason string }

func (e errVerification) Error() string {
	return fmt.Sprintf("%s asks for browser verification (%s) — W5F cannot pass it. Try again later, open it in a browser, or save it with FanFicFare (g → ffr <address>)", e.site, e.reason)
}

// getPage fetches and parses a page; bot-check pages become errVerification.
func getPage(ctx context.Context, f *fetch.Fetcher, raw, site string, fresh bool) (*goquery.Document, *url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, nil, err
	}
	resp, err := f.Get(ctx, u, fetch.Options{Revalidate: fresh})
	if err != nil {
		var ce *fetch.ChallengeError
		if errors.As(err, &ce) {
			return nil, nil, errVerification{site, ce.Reason}
		}
		return nil, nil, fmt.Errorf("%s: %w", site, err)
	}
	if r := fetch.ChallengeReason(resp.Body); r != "" {
		return nil, nil, errVerification{site, r}
	}
	gq, err := goquery.NewDocumentFromReader(bytes.NewReader(resp.Body))
	if err != nil {
		return nil, nil, err
	}
	final := resp.URL
	if final == nil {
		final = u
	}
	return gq, final, nil
}

// blocksOf converts an element's inner HTML into blocks inside d.
func blocksOf(d *doc.Document, sel *goquery.Selection, base *url.URL) []doc.Block {
	h, err := sel.Html()
	if err != nil {
		return nil
	}
	return htmlconv.Fragment(d, h, base.String())
}

// resolve makes href absolute against base.
func resolve(base *url.URL, href string) string {
	r, err := base.Parse(strings.TrimSpace(href))
	if err != nil {
		return ""
	}
	r.Fragment = ""
	return r.String()
}

func text(sel *goquery.Selection) string { return strings.Join(strings.Fields(sel.Text()), " ") }

// folded wraps blocks into a closed section.
func folded(label string, bs []doc.Block) doc.Block {
	return doc.Collapsible{Show: label, Hide: "hide " + strings.ToLower(label), Blocks: bs}
}

func unixTime(s string) time.Time {
	var n int64
	if _, err := fmt.Sscan(strings.TrimSpace(s), &n); err != nil || n <= 0 {
		return time.Time{}
	}
	return time.Unix(n, 0)
}
