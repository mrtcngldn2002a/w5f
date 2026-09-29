package htmlconv

import (
	"os"
	"strings"
	"testing"
)

func TestArticleArkeofili(t *testing.T) {
	body, err := os.ReadFile("../../testdata/pages/arkeofili-article.html")
	if err != nil {
		t.Fatal(err)
	}
	d, err := Article(body, "https://arkeofili.com/misirda-grekce-siirli-bir-roma-piramit-mezari-kesfedildi/")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(d.Title, "Piramit Mezarı") {
		t.Errorf("title = %q", d.Title)
	}
	if !strings.HasPrefix(d.Lang, "tr") {
		t.Errorf("lang = %q", d.Lang)
	}
	text := allText(d.Blocks)
	if len(text) < 1500 {
		t.Errorf("article body too short (%d chars):\n%s", len(text), text)
	}
	for _, junk := range []string{"Çerez", "cookie", "Abone ol", "Paylaş"} {
		if strings.Contains(text, junk) {
			t.Logf("possible chrome leaked: %q", junk)
		}
	}
}
