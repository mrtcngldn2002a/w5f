package solver

import (
	"context"
	"fmt"
	"net/url"
	"runtime"
	"strings"

	"w5f/internal/doc"
	"w5f/internal/fetch"
	"w5f/internal/store"
)

const Target = "w5f:solver"
const answerKey = "solver:asked"

func IsTarget(t string) bool {
	return t == Target || strings.HasPrefix(t, Target+"/") || strings.HasPrefix(t, Target+"?")
}

// Asking is persisted separately from installation: declining is never a gate.
func ShouldAsk(ctx context.Context, db *store.DB, m *Manager, goos, goarch string) bool {
	if Supported(goos, goarch) != nil || m.URL == "" || db.Get(answerKey) != "" || m.active() != "" {
		return false
	}
	_, e := fetch.ProbeSolver(ctx, m.URL)
	return fetch.NoSolver(e)
}
func Answer(db *store.DB, yes bool) error {
	v := "no"
	if yes {
		v = "yes"
	}
	return db.Set(answerKey, v)
}
func AskAgain(db *store.DB) error { return db.Set(answerKey, "") }

type page struct{ d *doc.Document }

func newPage(title, target string) *page {
	return &page{&doc.Document{Title: title, URL: target, Origin: "local", Lang: "en"}}
}
func (p *page) text(t string) {
	p.d.Blocks = append(p.d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: t}}})
}
func (p *page) link(href, label string) doc.Span {
	p.d.Links = append(p.d.Links, doc.Link{Href: href, Text: label})
	return doc.Span{Text: label, Link: len(p.d.Links)}
}
func (p *page) links(spans ...doc.Span) {
	p.d.Blocks = append(p.d.Blocks, doc.Paragraph{Text: doc.Inline(spans)})
}
func Route(ctx context.Context, target string, db *store.DB, m *Manager) (*doc.Document, error) {
	u, e := url.Parse(target)
	if e != nil {
		return nil, e
	}
	action := strings.TrimPrefix(u.Opaque, "solver")
	q := u.Query()
	sure := q.Get("sure") == "yes"
	switch action {
	case "/first":
		p := newPage("Install Byparr?", target)
		p.text("Install Byparr (~1.1 GB) so pages behind bot checks open? y/n")
		p.text("You can always install it later: g → solver, or w5f solver install.")
		next := q.Get("next")
		p.links(p.link(Target+"/decline?"+url.Values{"next": {next}}.Encode(), "No, later"), doc.Span{Text: " · "}, p.link(Target+"/install?"+url.Values{"sure": {"yes"}, "first": {"yes"}, "next": {next}}.Encode(), "Yes, install Byparr"))
		return p.d, nil
	case "/decline":
		if e = Answer(db, false); e != nil {
			return nil, e
		}
		p := newPage("Byparr: later", Target)
		p.text("The question is saved. Installation remains available at any time.")
		if next := q.Get("next"); next != "" {
			p.links(p.link(next, "Continue"))
		}
		p.links(p.link(Target, "Bot-check helper"))
		return p.d, nil
	case "/again":
		if e = AskAgain(db); e != nil {
			return nil, e
		}
		p := newPage("Bot-check helper", Target)
		p.text("W5F will ask again on the next launch if no helper is available.")
		p.links(p.link(Target, "back"))
		return p.d, nil
	case "/install", "/update", "/remove":
		verb := strings.TrimPrefix(action, "/")
		if !sure {
			p := newPage(verb+" Byparr?", target)
			if verb == "remove" {
				p.text("Only files in W5F's installation list are removed. Manual installations, external services and the page cache are kept.")
			} else {
				p.text("Byparr needs about 1.1 GB installed and at least 1.5 GB free during installation. Downloads are verified; GeoIP follows Byparr's own unpinned download.")
			}
			p.links(p.link(Target, "No, keep it"), doc.Span{Text: " · "}, p.link(Target+action+"?sure=yes", "Yes, "+verb+" Byparr"))
			return p.d, nil
		}
		if verb == "remove" {
			e = m.Remove()
		} else {
			if q.Get("first") == "yes" {
				if e = Answer(db, true); e != nil {
					return nil, e
				}
			}
			i := Installer{DataDir: m.DataDir}
			if verb == "update" {
				e = m.Update(ctx, &i)
			} else {
				_, e = i.Install(ctx)
			}
		}
		if e != nil {
			return nil, e
		}
		p := newPage("Bot-check helper", Target)
		p.text("Byparr: " + verb + " completed.")
		if verb == "update" {
			p.text("If an external service runs Byparr, restart it to use the new version.")
		}
		if next := q.Get("next"); next != "" {
			p.links(p.link(next, "Continue"))
		}
		p.links(p.link(Target, "Status"))
		return p.d, nil
	case "/start":
		_, e = m.Start(ctx, m.URL)
		if e != nil {
			return nil, e
		}
	case "/stop":
		if e = m.Stop(); e != nil {
			return nil, e
		}
	case "", "/":
	default:
		return nil, fmt.Errorf("unknown solver action: %s", action)
	}
	p := newPage("Bot-check helper", Target)
	s := m.Status(ctx)
	if s.Dir != "" {
		p.text(fmt.Sprintf("Byparr %s · %.1f GB · %s", s.Version, float64(s.Bytes)/(1<<30), s.Dir))
		if !s.Managed {
			p.text("Manual installation: reused as it is; W5F cannot remove it.")
		}
	} else {
		p.text("Byparr is not installed.")
	}
	if s.Running {
		p.text(s.Helper + " at " + m.URL + " · started by " + s.Owner)
	} else {
		p.text(s.Problem)
	}
	if s.Update != "" {
		p.text(s.Update)
	}
	if hint := XvfbHint(m.DataDir); hint != "" {
		p.text(hint)
	}
	if e := Supported(runtime.GOOS, runtime.GOARCH); e != nil {
		p.text(e.Error())
	} else {
		p.links(p.link(Target+"/install", "Install Byparr"))
	}
	if s.Dir != "" {
		p.links(p.link(Target+"/start", "Start"), doc.Span{Text: " · "}, p.link(Target+"/stop", "Stop W5F's Byparr"), doc.Span{Text: " · "}, p.link(Target+"/update", "Update"))
		if s.Managed {
			p.links(p.link(Target+"/remove", "Remove"))
		}
	}
	p.links(p.link(Target+"/again", "ask me again"))
	return p.d, nil
}
