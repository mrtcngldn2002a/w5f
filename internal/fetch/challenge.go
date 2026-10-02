package fetch

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// ChallengeError is a verification page, never usable or cacheable content.
type ChallengeError struct {
	URL, Reason string
	Helper      string // why the bot-check helper did not help, if it was asked
}

func (e *ChallengeError) Error() string {
	if e.Helper != "" {
		return fmt.Sprintf("%s: %s; %s", e.URL, e.Reason, e.Helper)
	}
	return fmt.Sprintf("%s: %s; the site still requires JavaScript or human verification, which this HTTP client cannot complete", e.URL, e.Reason)
}

// ChallengeReason avoids classifying an ordinary page's captcha widget or
// Cloudflare analytics script as a wall.
func ChallengeReason(body []byte) string {
	d, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return ""
	}
	title := strings.ToLower(strings.TrimSpace(d.Find("title").Text()))
	if title == "ddos-guard" {
		return "the site requires DDoS-Guard browser verification"
	}
	if title == "just a moment..." || title == "attention required! | cloudflare" || strings.Contains(title, "making sure you're not a bot") {
		return "the site requires browser verification"
	}
	l := strings.ToLower(string(body))
	d.Find("script,style,noscript").Remove()
	text := strings.ToLower(strings.Join(strings.Fields(d.Find("body").Text()), " "))
	content := d.Find("form input[type=search], form input[name=q], form input[name=query], .booklink, .books li, table tbody tr td a").Length() > 0
	if content {
		return ""
	}
	// Cloudflare also injects challenge-platform/scripts/jsd into ordinary
	// content and origin-error pages. Its presence alone is not a challenge.
	cloudflareWall := strings.Contains(l, "cf-challenge") || strings.Contains(l, "_cf_chl_opt") || strings.Contains(l, "/orchestrate/chl_page/")
	if len([]rune(text)) < 1200 && (strings.Contains(l, "anubis_challenge") || cloudflareWall || strings.Contains(l, `name="js_challenge"`) || strings.Contains(text, "making sure you're not a bot")) {
		return "the site requires browser verification"
	}
	if len([]rune(text)) < 400 && (strings.Contains(text, "captcha") || strings.Contains(text, "verify you are human") || strings.Contains(text, "verify that you are human")) {
		return "the site asks for a captcha"
	}
	return ""
}
