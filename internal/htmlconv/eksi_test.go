package htmlconv

import (
	"os"
	"strings"
	"testing"

	"w5f/internal/doc"
)

func TestEksiHomeShowsGundem(t *testing.T) {
	body, err := os.ReadFile("../../testdata/pages/eksi-home.html")
	if err != nil {
		t.Fatal(err)
	}
	d, err := Eksi(body, "https://eksisozluk.com/")
	if err != nil {
		t.Fatal(err)
	}
	var list *doc.List
	for _, b := range d.Blocks {
		if l, ok := b.(doc.List); ok {
			list = &l
			break
		}
	}
	if list == nil || len(list.Items) < 20 {
		t.Fatalf("gündem list missing or short")
	}
	if len(d.Links) == 0 || !strings.Contains(d.Links[0].Href, "eksisozluk.com/") {
		t.Errorf("topic links not resolved: %+v", d.Links[:1])
	}
}

func TestEksiTopicEntries(t *testing.T) {
	body, err := os.ReadFile("../../testdata/pages/eksi-topic.html")
	if err != nil {
		t.Fatal(err)
	}
	d, err := Eksi(body, "https://eksisozluk.com/bu-saatte-hala-uyumama-sebebi--6166390")
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "bu saatte hala uyumama sebebi" {
		t.Errorf("title = %q", d.Title)
	}
	text := allText(d.Blocks)
	if strings.Count(text, "— ") < 5 {
		t.Errorf("expected entry signatures (— author · date):\n%s", text)
	}
	if !strings.Contains(text, "sonraki") {
		t.Error("pager missing")
	}
}
