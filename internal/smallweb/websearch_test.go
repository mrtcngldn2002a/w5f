package smallweb

import (
	"context"
	"strings"
	"testing"
)

func TestWebSearchPageAsksAndAnswers(t *testing.T) {
	addr := WebSearchPage("https://search.example/search", "query", "Search the small web.")
	d, err := Load(context.Background(), Env{}, addr)
	if err != nil || d.URL != addr || !IsInputPage(d.URL) || !strings.Contains(flat(d), "Search the small web.") {
		t.Fatalf("search page: %v %+v", err, d)
	}
	if got := InputCommand(d.URL, "? gene wolfe"); got != "https://search.example/search?query=gene+wolfe" {
		t.Errorf("answer: %q", got)
	}
	if got := InputCommand(d.URL, "?  "); got != "" {
		t.Errorf("an empty answer goes nowhere: %q", got)
	}
	// Only web addresses are searched this way.
	if _, err := Load(context.Background(), Env{}, WebSearchPage("file:///etc/passwd", "q", "")); err == nil {
		t.Error("a non-web search address must be refused")
	}
}

func TestMenuOpensWibyAndMarginaliaThroughW5F(t *testing.T) {
	d := menuDoc()
	var wiby, wibySearch, marginalia bool
	for _, l := range d.Links {
		wiby = wiby || l.Href == "w5f:discover/wiby"
		wibySearch = wibySearch || (IsInputPage(l.Href) && strings.Contains(l.Href, "wiby.me"))
		if IsInputPage(l.Href) && strings.Contains(l.Href, "marginalia") {
			marginalia = InputCommand(l.Href, "? linear b") == "https://old-search.marginalia.nu/search?query=linear+b"
		}
	}
	if !wiby || !wibySearch || !marginalia {
		t.Errorf("menu links: %+v", d.Links)
	}
}
