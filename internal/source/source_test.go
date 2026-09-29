package source

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"w5f/internal/doc"
)

func text(d *doc.Document) string {
	var b strings.Builder
	doc.ReplaceBlocks(d.Blocks, func(x doc.Block) ([]doc.Block, bool) {
		switch x := x.(type) {
		case doc.Paragraph:
			b.WriteString(x.Text.PlainText() + "\n")
		case doc.Notice:
			b.WriteString("[N] " + x.Text + "\n")
		}
		return nil, false
	})
	return b.String()
}

// A Wikidot-style page with one text-bearing embedded block and one that only
// carries scripts (like the Wanderers' Library analytics block).
func TestEmbedsInlinedOrDropped(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mux.HandleFunc("/page", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body><script>WIKIREQUEST={}</script><div id="page-content">
<p>Intro paragraph.</p>
<p><iframe class="html-block-iframe" src="/block-text"></iframe></p>
<p><iframe class="html-block-iframe" src="/block-empty"></iframe></p>
</div></body></html>`)
	})
	mux.HandleFunc("/block-text", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body><p>The embedded story continues here with enough words to count as text. <a href="/next">next part</a></p></body></html>`)
	})
	mux.HandleFunc("/block-empty", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body><script>gtag('config','x')</script></body></html>`)
	})

	d, err := Load(context.Background(), srv.URL+"/page", Options{})
	if err != nil {
		t.Fatal(err)
	}
	got := text(d)
	if !strings.Contains(got, "The embedded story continues here") {
		t.Errorf("text block not inlined:\n%s", got)
	}
	if strings.Contains(got, "[N]") || strings.Contains(got, "open embedded") {
		t.Errorf("empty block should vanish silently:\n%s", got)
	}
	// The inlined link must point at the embedded page's link, re-indexed.
	ok := false
	doc.ReplaceBlocks(d.Blocks, func(x doc.Block) ([]doc.Block, bool) {
		if p, isP := x.(doc.Paragraph); isP {
			for _, s := range p.Text {
				if s.Link > 0 && strings.HasSuffix(d.Links[s.Link-1].Href, "/next") && s.Text == "next part" {
					ok = true
				}
			}
		}
		return nil, false
	})
	if !ok {
		t.Error("link inside the inlined block was not re-indexed correctly")
	}
}

func TestEmptyPageGetsNotice(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body><script>gtag()</script></body></html>`)
	}))
	defer srv.Close()
	d, err := Load(context.Background(), srv.URL, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text(d), "No readable text") {
		t.Errorf("empty page shows nothing:\n%s", text(d))
	}
}

func TestSavedPageOffersEmbedLink(t *testing.T) {
	d, err := Load(context.Background(), "../../testdata/pages/scp-3125.html", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text(d), "open embedded content") {
		t.Errorf("saved page should offer the embed link:\n%s", text(d))
	}
}
