package doctor

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"

	"w5f/internal/doc"
	"w5f/internal/fetch"
	"w5f/internal/render"
	"w5f/internal/smallweb"
)

// Probe is one live check: a web address or a small web address.
type Probe struct {
	Name, Target string
}

// Probes are one source of each kind W5F reads.
func Probes(repo string) []Probe {
	gh := "https://api.github.com/"
	if strings.Contains(repo, "/") {
		gh = "https://api.github.com/repos/" + repo + "/releases/latest"
	}
	return []Probe{
		{"feed", "https://arkeofili.com/feed/"},
		{"SCP wiki", "https://scp-wiki.wikidot.com/scp-173"},
		{"Gemini", "gemini://geminiprotocol.net/"},
		{"Gopher", "gopher://gopher.floodgap.com/1/"},
		{"releases", gh},
	}
}

// slow is when a live source counts as slow.
const slow = 5 * time.Second

// Live fetches each probe once (never from the cache). Gemini's trust store
// is a temporary folder, so nothing is written into the data folder.
func Live(ctx context.Context, version string, probes []Probe) []Result {
	f := fetch.New("", version)
	tmp, _ := os.MkdirTemp("", "w5f-doctor-")
	defer os.RemoveAll(tmp)
	sw := smallweb.Env{DataDir: tmp, Timeout: 20 * time.Second}
	var out []Result
	for _, p := range probes {
		start := time.Now()
		var err error
		if smallweb.IsTarget(p.Target) {
			_, err = smallweb.Load(ctx, sw, p.Target)
		} else {
			var u *url.URL
			if u, err = url.Parse(p.Target); err == nil {
				_, err = f.Get(ctx, u, fetch.Options{NoStore: true})
			}
		}
		took := time.Since(start).Round(10 * time.Millisecond)
		switch {
		case err != nil:
			out = append(out, Result{Fail, p.Name, fmt.Sprintf("%s: %v", p.Target, err)})
		case took > slow:
			out = append(out, Result{Warn, p.Name, fmt.Sprintf("slow, %s (%s)", took, p.Target)})
		default:
			out = append(out, Result{OK, p.Name, fmt.Sprintf("%s (%s)", took, p.Target)})
		}
	}
	return out
}

// Budget is the render budget for a long page (03 Teknik Plan §6).
const Budget = 150 * time.Millisecond

// Bench lays out a long synthetic page (about words long) at 72 columns
// and reports the time against the budget, and the memory in use.
func Bench(words int) []Result {
	d := LongPage(words)
	render.Render(d, render.Options{Width: 72}) // warm up
	const runs = 5
	start := time.Now()
	lines := 0
	for i := 0; i < runs; i++ {
		lines = len(render.Render(d, render.Options{Width: 72}).Lines)
	}
	per := time.Since(start) / runs
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	s := OK
	if per > Budget {
		s = Warn
	}
	return []Result{
		{s, "render", fmt.Sprintf("%d-word page, %d lines: %s per layout (budget %s)", words, lines, per.Round(100*time.Microsecond), Budget)},
		{OK, "memory", fmt.Sprintf("%.1f MB from the system, %.1f MB in use", float64(ms.Sys)/(1<<20), float64(ms.HeapAlloc)/(1<<20))},
	}
}

// LongPage is an SCP-like page: headings, paragraphs with links, lists and
// folded blocks.
func LongPage(words int) *doc.Document {
	d := &doc.Document{Title: "Bench"}
	para := strings.Fields(strings.Repeat("The archive keeps what the world forgets, and the keeper counts the shelves by lamplight. ", 3))
	n := 0
	for sec := 1; n < words; sec++ {
		d.Links = append(d.Links, doc.Link{Href: fmt.Sprintf("https://example.org/%d", sec), Text: "source"})
		body := []doc.Block{}
		for i := 0; i < 6 && n < words; i++ {
			body = append(body, doc.Paragraph{Text: doc.Inline{{Text: strings.Join(para, " ") + " "}, {Text: "source", Link: len(d.Links)}}})
			n += len(para)
		}
		body = append(body, doc.List{Items: [][]doc.Block{{doc.Paragraph{Text: doc.Inline{{Text: "Item: Safe"}}}}, {doc.Paragraph{Text: doc.Inline{{Text: "Item: Euclid"}}}}}})
		d.Blocks = append(d.Blocks, doc.Heading{Level: 2, Text: doc.Inline{{Text: fmt.Sprintf("Addendum %d", sec)}}})
		if sec%3 == 0 {
			d.Blocks = append(d.Blocks, doc.Collapsible{Show: "+ Show log", Hide: "- Hide log", Blocks: body, Open: true})
		} else {
			d.Blocks = append(d.Blocks, body...)
		}
	}
	d.Renumber()
	return d
}
