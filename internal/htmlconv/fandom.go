package htmlconv

import (
	"encoding/json"
	"errors"
	"html"
	"net/url"
	"strings"

	"w5f/internal/doc"
)

// IsFandom reports a page of a Fandom wiki ("/wiki/Title"). Fandom turns
// readers without a browser away from its pages, but its MediaWiki API answers.
func IsFandom(u *url.URL) bool {
	h := strings.ToLower(u.Hostname())
	return (h == "fandom.com" || strings.HasSuffix(h, ".fandom.com")) && strings.HasPrefix(u.Path, "/wiki/") &&
		!strings.Contains(strings.TrimPrefix(u.Path, "/wiki/"), ":")
}

// FandomAPI is the address that returns the page as HTML inside JSON.
func FandomAPI(u *url.URL) *url.URL {
	title, err := url.PathUnescape(strings.TrimPrefix(u.Path, "/wiki/"))
	if err != nil {
		title = strings.TrimPrefix(u.Path, "/wiki/")
	}
	q := url.Values{
		"action": {"parse"}, "page": {title}, "prop": {"text|displaytitle"},
		"redirects": {"1"}, "disableeditsection": {"1"}, "disablelimitreport": {"1"},
		"format": {"json"}, "formatversion": {"2"},
	}
	return &url.URL{Scheme: "https", Host: u.Host, Path: "/api.php", RawQuery: q.Encode()}
}

// Fandom converts the reply of FandomAPI. Links in the page text are relative
// to the wiki, so following one comes back here.
func Fandom(body []byte, pageURL string) (*doc.Document, error) {
	var r struct {
		Parse struct {
			Title        string `json:"title"`
			DisplayTitle string `json:"displaytitle"`
			Text         string `json:"text"`
		} `json:"parse"`
		Error struct {
			Info string `json:"info"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, err
	}
	if r.Error.Info != "" {
		return nil, errors.New("Fandom: " + r.Error.Info)
	}
	if strings.TrimSpace(r.Parse.Text) == "" {
		return nil, errors.New("Fandom: the page is empty")
	}
	d, err := Generic(strings.NewReader("<html><body>"+r.Parse.Text+"</body></html>"), pageURL)
	if err != nil {
		return nil, err
	}
	d.Title = collapseSpace(html.UnescapeString(r.Parse.Title))
	d.Origin = "live"
	if d.Lang == "" {
		d.Lang = "en"
	}
	dropRepeatedTitle(d)
	d.Renumber()
	return d, nil
}
