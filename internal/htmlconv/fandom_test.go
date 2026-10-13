package htmlconv

import (
	"net/url"
	"testing"

	"w5f/internal/doc"
)

func TestFandomAddress(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://middlerooms.fandom.com/wiki/Level_0":              true,
		"https://middlerooms.fandom.com/wiki/The_Middlerooms_Wiki": true,
		"https://middlerooms.fandom.com/wiki/Special:Search":       false,
		"https://middlerooms.fandom.com/wiki/Category:Levels":      false,
		"https://middlerooms.fandom.com/f/p/123":                   false,
		"https://example.org/wiki/Level_0":                         false,
		"https://notfandom.com/wiki/Level_0":                       false,
	} {
		u, _ := url.Parse(raw)
		if got := IsFandom(u); got != want {
			t.Errorf("IsFandom(%s) = %v", raw, got)
		}
	}
	u, _ := url.Parse("https://middlerooms.fandom.com/wiki/Level_0.2")
	api := FandomAPI(u)
	if api.Host != "middlerooms.fandom.com" || api.Path != "/api.php" || api.Query().Get("page") != "Level_0.2" || api.Query().Get("action") != "parse" {
		t.Errorf("FandomAPI = %s", api)
	}
}

func TestFandomPage(t *testing.T) {
	body := `{"parse":{"title":"Level 0","displaytitle":"Level 0","text":"<div class=\"mw-parser-output\"><p>The first level of the Middlerooms is a <a href=\"/wiki/Entity_1\">hall</a> without end, lit by a flat grey light that never changes.</p><h2>Description</h2><p>Walls of damp wallpaper, a carpet that keeps the sound of steps, and a hum that is felt more than heard.</p></div>"}}`
	d, err := Fandom([]byte(body), "https://middlerooms.fandom.com/wiki/Level_0")
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "Level 0" || d.Origin != "live" {
		t.Errorf("title %q origin %q", d.Title, d.Origin)
	}
	if len(d.Links) != 1 || d.Links[0].Href != "https://middlerooms.fandom.com/wiki/Entity_1" {
		t.Errorf("links = %+v", d.Links)
	}
	if doc.TextLength(d.Blocks) < 150 {
		t.Errorf("the text is missing: %d characters", doc.TextLength(d.Blocks))
	}
	if _, err := Fandom([]byte(`{"error":{"code":"missingtitle","info":"The page you specified doesn't exist."}}`), "https://x.fandom.com/wiki/Nope"); err == nil {
		t.Error("a missing page should be an error")
	}
}
