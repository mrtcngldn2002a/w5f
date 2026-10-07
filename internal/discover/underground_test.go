package discover

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestTotseLinkPaths(t *testing.T) {
	for in, want := range map[string]string{
		"/web/20070101101745/http://www.totse.com/en/conspiracy/index.html":                    "/en/conspiracy/index.html",
		"/web/20061205232500/http://www.totse.com/en/conspiracy/institutional_analysis/1.html": "/en/conspiracy/institutional_analysis/1.html",
		"/web/20070101101745im_/http://totse.com/en/fringe/x.html":                             "/en/fringe/x.html",
	} {
		m := reTotse.FindStringSubmatch(in)
		if m == nil || m[1] != want {
			t.Errorf("%s: got %v, want %s", in, m, want)
		}
	}
	for _, in := range []string{
		"/web/20070101101745/http://www.adbrite.com/mb/commerce/purchase_form.php",
		"/web/20070101101745/http://www.totse.com/bin/bbs/Ultimate.cgi",
		"/web/20070101101745/http://www.totse.com/en/search/index.html?x=1",
	} {
		if reTotse.MatchString(in) {
			t.Errorf("%s should not match", in)
		}
	}
}

func TestPhrackTitle(t *testing.T) {
	if m := rePhrack.FindStringSubmatch("https://archives.phrack.org/issues/36/8.txt"); m == nil || m[1] != "36" || m[2] != "8" {
		t.Errorf("phrack path: %v", m)
	}
}

func TestUndergroundDraw(t *testing.T) {
	old := undergroundSources
	t.Cleanup(func() { undergroundSources = old })
	undergroundSources = []undergroundSource{
		{"down", func(context.Context, Env) (string, string, error) { return "", "", errors.New("closed") }},
		{"up", func(context.Context, Env) (string, string, error) { return "https://example.org/x", "a title", nil }},
	}
	for range 10 {
		d, err := underground{}.Draw(context.Background(), Env{})
		if err != nil {
			t.Fatal(err)
		}
		if d.Target != "https://example.org/x" || d.Why != "underground/up · a title" {
			t.Errorf("draw: %+v", d)
		}
	}
	undergroundSources = undergroundSources[:1]
	if _, err := (underground{}).Draw(context.Background(), Env{}); err == nil || !strings.Contains(err.Error(), "down: closed") {
		t.Errorf("all down: %v", err)
	}
}

func TestUndergroundIsAFamily(t *testing.T) {
	for _, f := range families {
		if f.Name() == "underground" {
			if sourcesOf(f) != len(undergroundSources) {
				t.Errorf("sources: %d", sourcesOf(f))
			}
			return
		}
	}
	t.Error("underground is not among the families")
}
