package htmlconv

import (
	"bytes"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// stripBritannicaAI removes the generated Q&A module before extraction so its
// answers cannot outweigh the editorial article in Readability's scoring or
// reappear through the generic fallback. Editorial questions are left intact.
func stripBritannicaAI(body []byte, u *url.URL) []byte {
	if u == nil {
		return body
	}
	switch strings.ToLower(u.Hostname()) {
	case "britannica.com", "www.britannica.com":
	default:
		return body
	}
	d, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return body
	}
	modules := d.Find(".ai-qna-module, .ai-top-questions-module")
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
