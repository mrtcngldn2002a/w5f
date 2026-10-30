package htmlconv

import (
	"bytes"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// siteChrome is what a site puts around its text that is not part of it, by
// host (without "www."). Britannica's generated Q&A module would outweigh the
// editorial article in Readability's scoring or come back through the generic
// fallback (its editorial questions are left intact); gnosis.org opens and
// closes every text with its banner and a bar of links to the archive's other
// rooms.
var siteChrome = map[string]string{
	"britannica.com": ".ai-qna-module, .ai-top-questions-module",
	"gnosis.org":     `img[src*="GnosticSocietyLibrary"], h6:contains("Gnosis Archive"), h5:contains("GNOSIS ARCHIVE")`,
}

// stripSiteChrome removes the site's chrome before extraction.
func stripSiteChrome(body []byte, u *url.URL) []byte {
	if u == nil {
		return body
	}
	sel, ok := siteChrome[strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")]
	if !ok {
		return body
	}
	d, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return body
	}
	modules := d.Find(sel)
	if modules.Length() == 0 {
		return body
	}
	modules.Remove()
	clean, err := d.Html()
	if err != nil {
		return body
	}
	return []byte(clean)
}
