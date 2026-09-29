package htmlconv

import (
	"os"
	"strings"
	"testing"
)

// Fixtures: testdata/marginalia, saved from old-search.marginalia.nu on 2026-09-29.
func TestMarginaliaResults(t *testing.T) {
	body, err := os.ReadFile("../../testdata/marginalia/results.html")
	if err != nil {
		t.Fatal(err)
	}
	d, err := Marginalia(body, "https://old-search.marginalia.nu/search?query=gene+wolfe&sst=S-4a0ec25298439b16")
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "Marginalia: gene wolfe" || len(d.Links) < 20 || d.Links[0].Href != "https://en.wikipedia.org/wiki/Gene_Wolfe" || d.Links[0].Text != "Gene Wolfe" {
		t.Fatalf("results: %q %d %+v", d.Title, len(d.Links), d.Links[:1])
	}
	if !strings.Contains(d.Next, "page=2") {
		t.Errorf("next page: %q", d.Next)
	}
	for _, l := range d.Links {
		if strings.Contains(l.Href, "/site/") || strings.Contains(l.Href, "/site-search/") {
			t.Errorf("site tools are not results: %s", l.Href)
		}
	}
}

func TestMarginaliaWaitPageKeepsItsLinkForTheReader(t *testing.T) {
	body, err := os.ReadFile("../../testdata/marginalia/wait.html")
	if err != nil {
		t.Fatal(err)
	}
	d, err := Marginalia(body, "https://old-search.marginalia.nu/search?query=gene+wolfe")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Links) != 1 || !strings.HasPrefix(d.Links[0].Href, "https://old-search.marginalia.nu/search?query=gene+wolfe&sst=") || d.Next != "" {
		t.Errorf("wait page: %+v next=%q", d.Links, d.Next)
	}
}

func TestMarginaliaRandomSites(t *testing.T) {
	body, err := os.ReadFile("../../testdata/marginalia/random.html")
	if err != nil {
		t.Fatal(err)
	}
	d, err := Marginalia(body, "https://old-search.marginalia.nu/search?query=browse:random")
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "Marginalia: random sites" || len(d.Links) != 50 {
		t.Fatalf("random: %q %d links", d.Title, len(d.Links))
	}
	if d.Links[0].Href != "http://alt-h.net/" || d.Links[1].Href != "https://old-search.marginalia.nu/explore/alt-h.net" {
		t.Errorf("site and its similar link: %+v", d.Links[:2])
	}
}
