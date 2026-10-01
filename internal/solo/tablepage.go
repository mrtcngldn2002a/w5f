package solo

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"w5f/internal/doc"
	"w5f/internal/smallweb"
)

// table is the table's page, in two columns: what is thrown (the oracle,
// the dice, the sparks) on the left, what is kept (characters, threads,
// counters) on the right; the log below.
func (env Env) table() *doc.Document {
	d := &doc.Document{Title: "The Gaming Table", URL: "w5f:solo", Origin: "local", Lang: "en"}
	link := func(href, text string) doc.Span {
		d.Links = append(d.Links, doc.Link{Href: href, Text: text})
		return doc.Span{Text: text, Link: len(d.Links)}
	}
	heading := func(s string) doc.Block { return doc.Heading{Level: 2, Text: doc.Inline{{Text: s}}} }
	italic := func(s string) doc.Block { return doc.Paragraph{Text: doc.Inline{{Text: s, Style: doc.Italic}}} }
	gap := doc.Span{Text: "   "}

	var left []doc.Block
	left = append(left, heading("Ask the oracle"))
	var odds [][]doc.Block
	for _, o := range Odds {
		prompt := fmt.Sprintf("Ask the oracle (%s, %d in 100): your yes/no question. Rolling your own d100? End with = and the number, like: Is the door locked? = 57", o.Label, o.Chance)
		odds = append(odds, []doc.Block{doc.Paragraph{Text: doc.Inline{link(smallweb.WebSearchPage(askPrefix+o.Key, "q", prompt), o.Label),
			{Text: fmt.Sprintf("  %d in 100", o.Chance), Style: doc.Italic}}}})
	}
	left = append(left, doc.List{Items: odds},
		italic("A d100 at or under the chance is yes; matching digits (11, 22 … 100) make it extreme, or a twist."))
	left = append(left, heading("Dice"))
	dice := doc.Inline{}
	for _, x := range []string{"d4", "d6", "d8", "d10", "d12", "d20", "d100", "2d6", "3d6", "2d20kh1", "2d20kl1"} {
		dice = append(dice, link(rollAddr+"?"+url.Values{"d": {x}}.Encode(), x), gap)
	}
	dice = append(dice, link(smallweb.WebSearchPage(rollAddr, "d", "Dice, like 2d6+1, d100, 4d6kh3 (keep the 3 highest). Your own dice? Add = and the total (2d6 = 9) or each die (2d6 = 3 6)."), "any dice…"))
	left = append(left, doc.Paragraph{Text: dice})
	left = append(left, heading("Sparks"))
	sparks := doc.Inline{}
	for _, k := range SparkKinds {
		sparks = append(sparks, link(sparkPrefix+k.Key, k.Label), gap)
	}
	left = append(left, doc.Paragraph{Text: sparks})

	t, err := env.LoadTable()
	var right []doc.Block
	if err != nil {
		right = append(right, doc.Notice{Kind: "warn", Text: "table.json could not be read: " + err.Error()})
	}
	act := func(list, action string, i int, name, label string) doc.Span {
		return link("w5f:solo/"+list+"/"+action+"?"+url.Values{"i": {strconv.Itoa(i)}, "n": {name}}.Encode(), label)
	}
	tools := func(list, prompt string, pick bool) doc.Block {
		in := doc.Inline{link(smallweb.WebSearchPage("w5f:solo/add/"+list, "q", prompt), "add…")}
		if pick {
			in = append(in, gap, link("w5f:solo/pick/"+list, "pick one"))
		}
		return doc.Paragraph{Text: in}
	}

	right = append(right, heading("Characters"))
	var chars [][]doc.Block
	for i, c := range t.Characters {
		in := doc.Inline{{Text: c.Name, Style: doc.Bold}}
		if c.Note != "" {
			in = append(in, doc.Span{Text: " — " + c.Note})
		}
		in = append(in, doc.Span{Text: "  "}, act("character", "remove", i, c.Name, "x"))
		chars = append(chars, []doc.Block{doc.Paragraph{Text: in}})
	}
	if len(chars) > 0 {
		right = append(right, doc.List{Items: chars})
	} else {
		right = append(right, italic("No one met yet."))
	}
	right = append(right, tools("character", "A character: the name, then a few words after a dash, like: Severian — a torturer, exiled", true))

	right = append(right, heading("Threads"))
	var open, closed [][]doc.Block
	for i, th := range t.Threads {
		if th.Closed {
			closed = append(closed, []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: th.Text, Style: doc.Strike}, {Text: "  "},
				act("thread", "open", i, th.Text, "reopen"), {Text: " "}, act("thread", "remove", i, th.Text, "x")}}})
			continue
		}
		open = append(open, []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: th.Text}, {Text: "  "},
			act("thread", "close", i, th.Text, "close"), {Text: " "}, act("thread", "remove", i, th.Text, "x")}}})
	}
	if len(open) > 0 {
		right = append(right, doc.List{Items: open})
	} else {
		right = append(right, italic("No thread left open."))
	}
	if len(closed) > 0 {
		right = append(right, doc.List{Items: closed})
	}
	right = append(right, tools("thread", "A thread: something left to be resolved, like: Find who burned the archive", true))

	right = append(right, heading("Counters"))
	var counters [][]doc.Block
	for i, c := range t.Counters {
		counters = append(counters, []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: c.Name + "  ", Style: doc.Bold}, {Text: c.Bar() + "  "},
			act("counter", "down", i, c.Name, "-"), {Text: " "}, act("counter", "up", i, c.Name, "+"), {Text: "  "}, act("counter", "remove", i, c.Name, "x")}}})
	}
	if len(counters) > 0 {
		right = append(right, doc.List{Items: counters})
	} else {
		right = append(right, italic("No counters kept."))
	}
	right = append(right, tools("counter", "A counter: a name and a number, with a maximum after a slash for a track or a clock, like: Health 5/5, Supply 3, Doom 0/6", false))

	d.Blocks = append(d.Blocks, doc.Columns{Cols: [][]doc.Block{left, right}})
	if recent := env.Recent(12); len(recent) > 0 {
		d.Blocks = append(d.Blocks, doc.Rule{}, heading("Log"), logList(recent),
			doc.Paragraph{Text: doc.Inline{link("w5f:solo/log", "the whole log")}})
	}
	d.Blocks = append(d.Blocks, doc.Rule{}, italic("From the prompt: g → roll 2d6+1, g → ask likely Is it guarded?, g → spark words, g → npc Name — a note, g → thread …, g → counter Health 5/5, g → pick npc. The odds follow Ironsworn's (Shawn Tomkin, CC BY 4.0)."))
	return d
}

// add puts a character, a thread or a counter on the table.
func (env Env) add(list, text string) (*doc.Document, error) {
	t, err := env.LoadTable()
	if err != nil {
		return nil, err
	}
	var note string
	switch list {
	case "character":
		c, err := ParseCharacter(text)
		if err != nil {
			return nil, err
		}
		t.Characters = append(t.Characters, c)
		note = "At the table: " + c.Name
	case "thread":
		text = strings.TrimSpace(text)
		if text == "" {
			return nil, errors.New("a thread needs a few words")
		}
		t.Threads = append(t.Threads, Thread{Text: text})
		note = "A new thread: " + text
	case "counter":
		c, err := ParseCounter(text)
		if err != nil {
			return nil, err
		}
		t.Counters = append(t.Counters, c)
		note = "Kept: " + c.Name + " " + c.Bar()
	default:
		return nil, errors.New("the table keeps characters, threads and counters, not " + list)
	}
	if err := env.SaveTable(t); err != nil {
		return nil, err
	}
	if list == "thread" {
		env.record(Entry{Kind: "thread", Input: "opened", Result: strings.TrimSpace(text)})
	}
	return env.noted(note), nil
}

// change applies an action to one entry of the table.
func (env Env) change(list, action string, i int, name string) (*doc.Document, error) {
	t, err := env.LoadTable()
	if err != nil {
		return nil, err
	}
	note, err := t.Change(list, action, i, name)
	if err != nil {
		return nil, err
	}
	if err := env.SaveTable(t); err != nil {
		return nil, err
	}
	if list == "thread" && (action == "close" || action == "open") {
		env.record(Entry{Kind: "thread", Input: map[string]string{"close": "closed", "open": "reopened"}[action], Result: name})
	}
	return env.noted(note), nil
}

// pick draws a character or an open thread at random, and logs it.
func (env Env) pick(list string) (*doc.Document, error) {
	t, err := env.LoadTable()
	if err != nil {
		return nil, err
	}
	s, err := t.Pick(list)
	if err != nil {
		return nil, err
	}
	env.record(Entry{Kind: "pick", Input: list, Result: s})
	return env.noted("Picked (" + list + "): " + s), nil
}

// noted is the table with a word on what was just done.
func (env Env) noted(note string) *doc.Document {
	d := env.table()
	d.Blocks = append([]doc.Block{doc.Notice{Kind: "info", Text: note}}, d.Blocks...)
	return d
}
