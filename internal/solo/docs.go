package solo

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"w5f/internal/discover"
	"w5f/internal/doc"
)

// IsTarget reports the table's addresses.
func IsTarget(t string) bool { return t == "w5f:solo" || strings.HasPrefix(t, "w5f:solo/") }

// Addresses (paths, not queries: an input page adds ?q= or ?d=).
const (
	askPrefix   = "w5f:solo/ask/"
	rollAddr    = "w5f:solo/roll"
	sparkPrefix = "w5f:solo/spark/"
)

// Route builds a page of the table.
func Route(ctx context.Context, target string, env Env) (*doc.Document, error) {
	u, err := url.Parse(target)
	if err != nil {
		return nil, err
	}
	path := strings.TrimSuffix(u.Opaque, "/")
	q := u.Query()
	switch {
	case path == "solo":
		return env.table(), nil
	case path == "solo/log":
		return env.logDoc(), nil
	case path == "solo/roll":
		return env.roll(q.Get("d"))
	case strings.HasPrefix(path, "solo/ask/"):
		return env.ask(ctx, strings.TrimPrefix(path, "solo/ask/"), q.Get("q"))
	case strings.HasPrefix(path, "solo/add/"):
		return env.add(strings.TrimPrefix(path, "solo/add/"), q.Get("q"))
	case strings.HasPrefix(path, "solo/pick/"):
		return env.pick(strings.TrimPrefix(path, "solo/pick/"))
	case strings.HasPrefix(path, "solo/character/"), strings.HasPrefix(path, "solo/thread/"), strings.HasPrefix(path, "solo/counter/"):
		list, action, _ := strings.Cut(strings.TrimPrefix(path, "solo/"), "/")
		i, err := strconv.Atoi(q.Get("i"))
		if err != nil {
			return nil, errors.New("which entry? (i=)")
		}
		return env.change(list, action, i, q.Get("n"))
	case strings.HasPrefix(path, "solo/spark/"):
		s, err := env.Draw(ctx, strings.TrimPrefix(path, "solo/spark/"))
		if err != nil {
			return nil, err
		}
		env.record(Entry{Kind: "spark", Input: s.Kind, Result: s.Text, Source: s.Source, Link: s.Link})
		d := env.table()
		d.Blocks = append(sparkBlocks(d, s, "Spark"), d.Blocks...)
		return d, nil
	}
	return nil, errors.New("unknown solo table address: " + target)
}

// splitOwn takes "… = 57" off the end: the owner's own dice.
func splitOwn(s string) (string, []int, error) {
	i := strings.LastIndex(s, "=")
	if i < 0 {
		return strings.TrimSpace(s), nil, nil
	}
	own, err := ParseOwn(s[i+1:])
	if err != nil {
		return "", nil, err
	}
	return strings.TrimSpace(s[:i]), own, nil
}

func (env Env) roll(text string) (*doc.Document, error) {
	expr, own, err := splitOwn(text)
	if err != nil {
		return nil, err
	}
	r, err := Throw(expr, own)
	if err != nil {
		return nil, err
	}
	env.record(Entry{Kind: "roll", Input: expr, Result: strconv.Itoa(r.Total), Detail: r.Detail(), Manual: r.Manual})
	d := env.table()
	head := []doc.Block{doc.Notice{Kind: "info", Text: fmt.Sprintf("%s = %d", expr, r.Total)}}
	detail := r.Detail()
	if r.Manual {
		detail += "  (your dice)"
	}
	head = append(head, doc.Paragraph{Text: doc.Inline{{Text: detail, Style: doc.Italic}}})
	d.Blocks = append(head, d.Blocks...)
	return d, nil
}

func (env Env) ask(ctx context.Context, odds, text string) (*doc.Document, error) {
	question, own, err := splitOwn(text)
	if err != nil {
		return nil, err
	}
	if len(own) > 1 {
		return nil, errors.New("the oracle takes one d100: = 57")
	}
	n := 0
	if len(own) == 1 {
		n = own[0]
	}
	a, err := Ask(question, odds, n)
	if err != nil {
		return nil, err
	}
	how := fmt.Sprintf("%s (%d) · d100 = %d", a.Odds, a.Chance, a.Roll)
	if a.Manual {
		how += " (your d100)"
	}
	d := env.table()
	head := []doc.Block{doc.Notice{Kind: "info", Text: a.Verdict()}}
	if a.Question != "" {
		head = append(head, doc.Paragraph{Text: doc.Inline{{Text: a.Question, Style: doc.Bold}}})
	}
	head = append(head, doc.Paragraph{Text: doc.Inline{{Text: how, Style: doc.Italic}}})
	en := Entry{Kind: "ask", Input: strings.TrimSpace(a.Odds + ": " + a.Question), Result: a.Verdict(), Detail: how, Manual: a.Manual}
	if a.Twist {
		// What the twist is about: two words, else a line from the reading.
		s, err := env.Draw(ctx, "words")
		if err != nil {
			s, err = env.Draw(ctx, "reading")
		}
		if err == nil {
			head = append(head, sparkBlocks(d, s, "The twist")...)
			en.Detail += " · twist: " + s.Text
		}
	}
	env.record(en)
	d.Blocks = append(head, d.Blocks...)
	return d, nil
}

func sparkBlocks(d *doc.Document, s Spark, label string) []doc.Block {
	in := doc.Inline{{Text: label + ": ", Style: doc.Bold}}
	if s.Link != "" {
		d.Links = append(d.Links, doc.Link{Href: s.Link, Text: s.Text})
		in = append(in, doc.Span{Text: s.Text, Link: len(d.Links)})
	} else {
		in = append(in, doc.Span{Text: s.Text})
	}
	bs := []doc.Block{doc.Paragraph{Text: in}}
	if len(s.Lines) == 6 {
		bs = append(bs, doc.Pre{Text: discover.HexagramText(s.Lines)})
	}
	return append(bs, doc.Paragraph{Text: doc.Inline{{Text: "from " + s.Source, Style: doc.Italic}}})
}

func logList(es []Entry) doc.Block {
	var items [][]doc.Block
	for _, e := range es {
		line := fmt.Sprintf("%s · %s · %s → %s", e.Time.Format("2 Jan 15:04"), e.Kind, e.Input, e.Result)
		if e.Manual {
			line += " (your dice)"
		}
		in := doc.Inline{{Text: line}}
		if e.Detail != "" && e.Kind != "ask" {
			in = append(in, doc.Span{Text: "  " + e.Detail, Style: doc.Italic})
		}
		if e.Source != "" {
			in = append(in, doc.Span{Text: "  · " + e.Source, Style: doc.Italic})
		}
		items = append(items, []doc.Block{doc.Paragraph{Text: in}})
	}
	return doc.List{Items: items}
}

func (env Env) logDoc() *doc.Document {
	d := &doc.Document{Title: "Solo RPG log", URL: "w5f:solo/log", Origin: "local", Lang: "en"}
	es := env.Recent(500)
	if len(es) == 0 {
		d.Blocks = []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: "Nothing thrown yet.", Style: doc.Italic}}}}
		return d
	}
	d.Blocks = []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: "Every throw, answer and spark, newest first; kept as JSON lines in " + env.LogDir, Style: doc.Italic}}}, logList(es)}
	return d
}
