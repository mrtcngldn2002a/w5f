package discover

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"w5f/internal/fetch"
)

func TestRSSPick(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<?xml version="1.0"?><rss version="2.0"><channel><title>Aeon</title>
<item><title><![CDATA[On the phenomenology of boredom]]></title><link>https://aeon.co/essays/boredom</link></item>
<item><title></title><link>https://aeon.co/empty</link></item></channel></rss>`)
	}))
	defer srv.Close()
	f := fetch.New("", "test")
	f.HostGap = 0
	link, title, err := rssPick(context.Background(), f, srv.URL)
	if err != nil || link != "https://aeon.co/essays/boredom" || title != "On the phenomenology of boredom" {
		t.Errorf("rss: %q %q %v", link, title, err)
	}
}

func TestKnowledgeFamilyTriesAnotherSource(t *testing.T) {
	old := knowledgeSources
	defer func() { knowledgeSources = old }()
	knowledgeSources = []knowSource{
		{"Broken", func(context.Context, Env) (string, string, error) { return "", "", errors.New("down") }},
		{"Aeon", func(context.Context, Env) (string, string, error) { return "https://aeon.co/x", "An essay", nil }},
	}
	for i := 0; i < 10; i++ {
		d, err := knowledge{}.Draw(context.Background(), Env{})
		if err != nil || d.Target != "https://aeon.co/x" || d.Why != "knowledge/Aeon · An essay" {
			t.Fatalf("knowledge: %+v %v", d, err)
		}
	}
	names := map[string]bool{}
	for _, s := range old {
		names[s.name] = true
	}
	for _, want := range []string{"Aeon", "JSTOR Daily", "Quanta Magazine", "Wikipedia featured articles", "Wikisource",
		"World History Encyclopedia", "Internet Classics Archive", "Catholic Encyclopedia (1913)"} {
		if !names[want] {
			t.Errorf("missing source %q", want)
		}
	}
}
