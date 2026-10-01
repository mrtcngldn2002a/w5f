package usenet

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"w5f/internal/doc"
	"w5f/internal/store"
)

// Env carries what the Usenet pages need.
type Env struct {
	DB         *store.DB // what has been read, per server and group
	ConfigPath string    // usenet.toml
	Timeout    time.Duration
}

// IsTarget reports Usenet addresses.
func IsTarget(t string) bool { return t == "w5f:usenet" || strings.HasPrefix(t, "w5f:usenet/") }

const (
	windowSize = 300 // articles listed per group page
	threadPage = 30  // posts shown per thread page
	newOnJoin  = 100 // a group read for the first time starts with this many new
)

// Route builds a Usenet page.
func Route(ctx context.Context, target string, env Env) (*doc.Document, error) {
	if env.Timeout == 0 {
		env.Timeout = 15 * time.Second
	}
	cfg, err := LoadConfig(env.ConfigPath)
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(target)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	parts := strings.Split(strings.TrimPrefix(u.Opaque, "usenet"), "/")[1:]
	arg := func(i int) string {
		if i < len(parts) {
			return strings.ToLower(parts[i])
		}
		return ""
	}
	num := func(s string) int { n, _ := strconv.Atoi(s); return n }
	switch arg(0) {
	case "":
		return homeDoc(env, cfg, "")
	case "find":
		return findDoc(env, cfg, q.Get("q"))
	case "g":
		return groupDoc(env, cfg, arg(1), num(q.Get("before")), "")
	case "t":
		return threadDoc(env, cfg, arg(1), num(arg(2)), num(q.Get("from")), num(q.Get("page")))
	case "sub", "unsub":
		g := arg(1)
		if !ValidGroup(g) {
			return nil, fmt.Errorf("not a newsgroup name: %q", g)
		}
		if arg(0) == "sub" {
			if !cfg.subscribed(g) {
				cfg.Groups = append(cfg.Groups, g)
				sort.Strings(cfg.Groups)
			}
			if err := cfg.Save(env.ConfigPath); err != nil {
				return nil, err
			}
			return groupDoc(env, cfg, g, 0, "Subscribed to "+g+".")
		}
		var keep []string
		for _, x := range cfg.Groups {
			if x != g {
				keep = append(keep, x)
			}
		}
		cfg.Groups = keep
		if err := cfg.Save(env.ConfigPath); err != nil {
			return nil, err
		}
		return homeDoc(env, cfg, "Unsubscribed from "+g+".")
	case "read":
		g := arg(1)
		c, err := dial(cfg.Server, env.Timeout)
		if err != nil {
			return nil, err
		}
		gr, err := c.group(g)
		c.Close()
		if err != nil {
			return nil, err
		}
		markAllRead(env, cfg, g, gr.Hi)
		return groupDoc(env, cfg, g, 0, "Marked everything in "+g+" as read.")
	case "kill":
		return killDoc(ctx, env, cfg, q)
	}
	return nil, errors.New("unknown Usenet address: " + target)
}

func readKey(cfg Config, group string) string { return "usenet:read:" + cfg.Server + ":" + group }

func loadRead(env Env, cfg Config, group string) (ranges, bool) {
	s := env.DB.Get(readKey(cfg, group))
	return parseRanges(s), s != ""
}

func saveRead(env Env, cfg Config, group string, r ranges) {
	_ = env.DB.Set(readKey(cfg, group), r.String())
}

func markRead(env Env, cfg Config, group string, nums ...int) {
	r, _ := loadRead(env, cfg, group)
	for _, n := range nums {
		r = r.add(n, n)
	}
	saveRead(env, cfg, group, r)
}

func markAllRead(env Env, cfg Config, group string, hi int) {
	r, _ := loadRead(env, cfg, group)
	saveRead(env, cfg, group, r.add(1, hi))
}

// readSet returns what has been read in a group; the first time, all but
// the last newOnJoin articles count as read.
func readSet(env Env, cfg Config, g Group) ranges {
	r, ok := loadRead(env, cfg, g.Name)
	if !ok {
		r = r.add(1, g.Hi-newOnJoin)
		saveRead(env, cfg, g.Name, r)
	}
	return r
}

func unreadIn(g Group, r ranges) int {
	if g.Hi < g.Lo || g.Hi == 0 {
		return 0
	}
	n := (g.Hi - g.Lo + 1) - r.countIn(g.Lo, g.Hi)
	return max(0, min(n, g.Count))
}

func newDoc(title, href string) *doc.Document {
	return &doc.Document{Title: title, URL: href, Origin: "live", Lang: "en"}
}

func addLink(d *doc.Document, href, text string) int {
	d.Links = append(d.Links, doc.Link{Href: href, Text: text})
	return len(d.Links)
}

func notice(d *doc.Document, text string) {
	if text != "" {
		d.Blocks = append(d.Blocks, doc.Notice{Kind: "info", Text: text})
	}
}

func homeDoc(env Env, cfg Config, note string) (*doc.Document, error) {
	d := newDoc("Usenet", "w5f:usenet")
	d.Meta = []doc.KV{{Key: "server", Value: strings.TrimSuffix(cfg.Server, ":119")}}
	notice(d, note)
	d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: "The text newsgroups, read only. Find more groups: g → usenet <word>. New posts are counted since you last read a group.", Style: doc.Italic}}})
	var items [][]doc.Block
	c, err := dial(cfg.Server, env.Timeout)
	if err != nil {
		d.Blocks = append(d.Blocks, doc.Notice{Kind: "warn", Text: err.Error()})
	}
	var gs []Group
	var gerrs []error
	if c != nil {
		gs, gerrs = c.groups(cfg.Groups)
	}
	for i, g := range cfg.Groups {
		in := doc.Inline{{Text: g, Link: addLink(d, "w5f:usenet/g/"+g, g)}}
		if c != nil {
			if gr, err := gs[i], gerrs[i]; err != nil {
				in = append(in, doc.Span{Text: "  · " + err.Error(), Style: doc.Italic})
			} else {
				if n := unreadIn(gr, readSet(env, cfg, gr)); n > 0 {
					in = append(in, doc.Span{Text: fmt.Sprintf("  %d new", n), Style: doc.Bold})
				}
				in = append(in, doc.Span{Text: fmt.Sprintf("  · %d posts", gr.Count), Style: doc.Italic})
			}
		}
		in = append(in, doc.Span{Text: "   "}, doc.Span{Text: "unsubscribe", Link: addLink(d, "w5f:usenet/unsub/"+g, "unsubscribe "+g)})
		items = append(items, []doc.Block{doc.Paragraph{Text: in}})
	}
	if c != nil {
		c.Close()
	}
	if len(items) == 0 {
		d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: "No groups yet: g → usenet <word> finds some.", Style: doc.Italic}}})
	} else {
		d.Blocks = append(d.Blocks, doc.Heading{Level: 2, Text: doc.Inline{{Text: "Your groups"}}}, doc.List{Items: items})
	}
	d.Blocks = append(d.Blocks, doc.Rule{}, doc.Paragraph{Text: doc.Inline{{Text: fmt.Sprintf("Kill file: %d subject words, %d posters — %s", len(cfg.Kill.Subjects), len(cfg.Kill.From), env.ConfigPath), Style: doc.Italic}}})
	return d, nil
}

func findDoc(env Env, cfg Config, words string) (*doc.Document, error) {
	w := strings.ToLower(strings.TrimSpace(words))
	pattern := w
	if !strings.Contains(w, "*") {
		pattern = "*" + strings.ReplaceAll(w, " ", "*") + "*"
	}
	d := newDoc("Usenet groups: "+w, "w5f:usenet/find?"+url.Values{"q": {w}}.Encode())
	c, err := dial(cfg.Server, env.Timeout)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	gs, err := c.active(pattern)
	if err != nil {
		return nil, err
	}
	sort.Slice(gs, func(i, j int) bool { return gs[i].Name < gs[j].Name })
	if len(gs) > 300 {
		gs = gs[:300]
		notice(d, "More than 300 groups match; the first 300 are listed.")
	}
	var items [][]doc.Block
	for _, g := range gs {
		in := doc.Inline{{Text: g.Name, Link: addLink(d, "w5f:usenet/g/"+g.Name, g.Name)}, {Text: fmt.Sprintf("  · about %d posts", g.Count), Style: doc.Italic}, {Text: "   "}}
		if cfg.subscribed(g.Name) {
			in = append(in, doc.Span{Text: "subscribed", Style: doc.Italic})
		} else {
			in = append(in, doc.Span{Text: "subscribe", Link: addLink(d, "w5f:usenet/sub/"+g.Name, "subscribe "+g.Name)})
		}
		items = append(items, []doc.Block{doc.Paragraph{Text: in}})
	}
	if len(items) == 0 {
		d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: "No groups match " + pattern + " on " + cfg.Server + ".", Style: doc.Italic}}})
	} else {
		d.Blocks = append(d.Blocks, doc.List{Items: items})
	}
	return d, nil
}

func groupDoc(env Env, cfg Config, group string, before int, note string) (*doc.Document, error) {
	c, err := dial(cfg.Server, env.Timeout)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	g, err := c.group(group)
	if err != nil {
		return nil, err
	}
	end := g.Hi
	if before > 0 && before-1 < end {
		end = before - 1
	}
	start := max(g.Lo, end-windowSize+1)
	ovs, err := c.over(start, end)
	if err != nil {
		return nil, err
	}
	ovs, killed := cfg.Kill.filter(ovs)
	read := readSet(env, cfg, g)
	d := newDoc(group, "w5f:usenet/g/"+group)
	if before > 0 {
		d.URL += fmt.Sprintf("?before=%d", before)
	}
	d.Meta = []doc.KV{{Key: "posts", Value: strconv.Itoa(g.Count)}, {Key: "new", Value: strconv.Itoa(unreadIn(g, read))}}
	notice(d, note)
	if killed > 0 {
		d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: fmt.Sprintf("%d posts hidden by your kill file.", killed), Style: doc.Italic}}})
	}
	var items [][]doc.Block
	for _, t := range buildThreads(ovs) {
		fresh := 0
		for _, p := range t.Posts {
			if !read.has(p.Num) {
				fresh++
			}
		}
		subj := t.Subject
		if subj == "" {
			subj = "(no subject)"
		}
		style := doc.Style(0)
		if fresh > 0 {
			style = doc.Bold
		}
		href := fmt.Sprintf("w5f:usenet/t/%s/%d?from=%d", group, t.Root.Num, start)
		in := doc.Inline{{Text: subj, Style: style, Link: addLink(d, href, subj)}}
		meta := fmt.Sprintf("  · %s", posterName(t.Root.From))
		if len(t.Posts) > 1 {
			meta += fmt.Sprintf(" · %d posts", len(t.Posts))
		}
		if fresh > 0 && fresh < len(t.Posts) {
			meta += fmt.Sprintf(", %d new", fresh)
		}
		if !t.Last.IsZero() {
			meta += " · " + t.Last.Format("2 Jan 2006")
		}
		in = append(in, doc.Span{Text: meta, Style: doc.Italic})
		items = append(items, []doc.Block{doc.Paragraph{Text: in}})
	}
	if len(items) == 0 {
		d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: "No posts here.", Style: doc.Italic}}})
	} else {
		d.Blocks = append(d.Blocks, doc.List{Items: items})
	}
	actions := doc.Inline{}
	if start > g.Lo {
		d.Next = fmt.Sprintf("w5f:usenet/g/%s?before=%d", group, start)
		actions = append(actions, doc.Span{Text: "older posts ›", Link: addLink(d, d.Next, "older")}, doc.Span{Text: "   "})
	}
	actions = append(actions, doc.Span{Text: "mark all read", Link: addLink(d, "w5f:usenet/read/"+group, "mark all read")}, doc.Span{Text: "   "})
	if cfg.subscribed(group) {
		actions = append(actions, doc.Span{Text: "unsubscribe", Link: addLink(d, "w5f:usenet/unsub/"+group, "unsubscribe")})
	} else {
		actions = append(actions, doc.Span{Text: "subscribe", Link: addLink(d, "w5f:usenet/sub/"+group, "subscribe")})
	}
	d.Blocks = append(d.Blocks, doc.Rule{}, doc.Paragraph{Text: actions})
	return d, nil
}

// threadDoc shows a thread's posts (threadPage at a time) and marks them
// read. from is where the group page's window started, so the thread is
// rebuilt from the same articles on.
func threadDoc(env Env, cfg Config, group string, root, from, page int) (*doc.Document, error) {
	c, err := dial(cfg.Server, env.Timeout)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	g, err := c.group(group)
	if err != nil {
		return nil, err
	}
	if from <= 0 || from > root {
		from = root
	}
	ovs, err := c.over(max(from, g.Lo), min(g.Hi, root+2000))
	if err != nil {
		return nil, err
	}
	ovs, _ = cfg.Kill.filter(ovs)
	var th *Thread
	for _, t := range buildThreads(ovs) {
		for _, p := range t.Posts {
			if p.Num == root {
				t := t
				th = &t
			}
		}
	}
	if th == nil {
		return nil, fmt.Errorf("article %d of %s is not on the server any more (or your kill file hides it)", root, group)
	}
	read := readSet(env, cfg, g)
	href := fmt.Sprintf("w5f:usenet/t/%s/%d?from=%d", group, root, from)
	d := newDoc(th.Subject, href)
	if page > 0 {
		d.URL += fmt.Sprintf("&page=%d", page)
	}
	d.Meta = []doc.KV{{Key: "group", Value: group}, {Key: "posts", Value: strconv.Itoa(len(th.Posts))}}
	d.Byline = posterName(th.Root.From)
	lo := page * threadPage
	if lo >= len(th.Posts) {
		lo = 0
	}
	hi := min(len(th.Posts), lo+threadPage)
	var nums []int
	for _, p := range th.Posts[lo:hi] {
		nums = append(nums, p.Num)
	}
	bodies, errs := c.articles(nums)
	var shown []int
	for i, p := range th.Posts[lo:hi] {
		if i > 0 {
			d.Blocks = append(d.Blocks, doc.Rule{})
		}
		head := strings.Repeat("› ", p.Depth) + posterName(p.From)
		if !p.Date.IsZero() {
			head += " · " + p.Date.Format("2 Jan 2006 15:04")
		}
		if !read.has(p.Num) {
			head += " · new"
		}
		d.Blocks = append(d.Blocks, doc.Heading{Level: 3, Text: doc.Inline{{Text: head}}})
		if baseSubject(p.Subject) != baseSubject(th.Subject) {
			d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: p.Subject, Style: doc.Italic}}})
		}
		if errs[i] != nil {
			d.Blocks = append(d.Blocks, doc.Notice{Kind: "warn", Text: errs[i].Error()})
			continue
		}
		a, err := parseArticle(p.Num, bodies[i])
		if err != nil {
			d.Blocks = append(d.Blocks, doc.Notice{Kind: "warn", Text: "this post could not be read: " + err.Error()})
			continue
		}
		d.Blocks = append(d.Blocks, bodyBlocks(d, a.Body)...)
		shown = append(shown, p.Num)
		if addr := posterAddress(p.From); addr != "" {
			kill := "w5f:usenet/kill?" + url.Values{"from": {addr}, "back": {d.URL}}.Encode()
			d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: "hide this poster", Style: doc.Italic, Link: addLink(d, kill, "hide "+addr)}}})
		}
	}
	markRead(env, cfg, group, shown...)
	nav := doc.Inline{{Text: "‹ " + group, Link: addLink(d, "w5f:usenet/g/"+group, group)}}
	if hi < len(th.Posts) {
		d.Next = fmt.Sprintf("%s&page=%d", href, page+1)
		nav = append(nav, doc.Span{Text: "   "}, doc.Span{Text: fmt.Sprintf("more of this thread (%d–%d of %d) ›", hi+1, min(len(th.Posts), hi+threadPage), len(th.Posts)), Link: addLink(d, d.Next, "more")})
	}
	if lo > 0 {
		d.Prev = fmt.Sprintf("%s&page=%d", href, page-1)
	}
	kill := "w5f:usenet/kill?" + url.Values{"subject": {baseSubject(th.Subject)}, "back": {"w5f:usenet/g/" + group}}.Encode()
	nav = append(nav, doc.Span{Text: "   "}, doc.Span{Text: "hide this thread", Link: addLink(d, kill, "hide this thread")})
	d.Blocks = append(d.Blocks, doc.Rule{}, doc.Paragraph{Text: nav})
	d.Renumber()
	return d, nil
}

func posterAddress(from string) string {
	if a, err := mail.ParseAddress(from); err == nil {
		return strings.ToLower(a.Address)
	}
	if f := strings.Fields(from); len(f) > 0 && strings.Contains(f[0], "@") {
		return strings.ToLower(strings.Trim(f[0], "<>"))
	}
	return ""
}

func killDoc(ctx context.Context, env Env, cfg Config, q url.Values) (*doc.Document, error) {
	var note string
	if s := strings.TrimSpace(q.Get("subject")); s != "" {
		cfg.Kill.Subjects = appendNew(cfg.Kill.Subjects, s)
		note = "Hidden from now on: posts whose subject contains “" + s + "”."
	}
	if f := strings.TrimSpace(q.Get("from")); f != "" {
		cfg.Kill.From = appendNew(cfg.Kill.From, f)
		note = "Hidden from now on: posts from " + f + "."
	}
	if note == "" {
		return nil, errors.New("nothing to hide")
	}
	if err := cfg.Save(env.ConfigPath); err != nil {
		return nil, err
	}
	back := q.Get("back")
	if !strings.HasPrefix(back, "w5f:usenet/g/") && !strings.HasPrefix(back, "w5f:usenet/t/") {
		back = "w5f:usenet"
	}
	d, err := Route(ctx, back, env)
	if err != nil {
		d = newDoc("Usenet", "w5f:usenet")
	}
	d.Blocks = append([]doc.Block{doc.Notice{Kind: "info", Text: note + " Edit the list in " + env.ConfigPath + "."}}, d.Blocks...)
	return d, nil
}

func appendNew(xs []string, s string) []string {
	for _, x := range xs {
		if strings.EqualFold(x, s) {
			return xs
		}
	}
	return append(xs, s)
}

var reURL = regexp.MustCompile(`https?://[^\s<>"'()\[\]]+[^\s<>"'()\[\].,;:!?]`)

// linked turns a line of text into spans, with web addresses as links.
func linked(d *doc.Document, text string) doc.Inline {
	var in doc.Inline
	last := 0
	for _, m := range reURL.FindAllStringIndex(text, -1) {
		if m[0] > last {
			in = append(in, doc.Span{Text: text[last:m[0]]})
		}
		u := text[m[0]:m[1]]
		in = append(in, doc.Span{Text: u, Link: addLink(d, u, u)})
		last = m[1]
	}
	if last < len(text) {
		in = append(in, doc.Span{Text: text[last:]})
	}
	return in
}

// bodyBlocks lays out a post: paragraphs reflow, short-lined or spaced-out
// passages (poems, tables, ASCII art) keep their lines, quoted text and the
// signature fold away.
func bodyBlocks(d *doc.Document, body string) []doc.Block {
	lines := strings.Split(strings.TrimRight(body, "\n "), "\n")
	var sig []string
	for i := len(lines) - 1; i >= 0 && i >= len(lines)-12; i-- {
		if lines[i] == "-- " || lines[i] == "--" {
			sig = lines[i+1:]
			lines = lines[:i]
			break
		}
	}
	var out []doc.Block
	var para []string
	flush := func() {
		if len(para) == 0 {
			return
		}
		if preformatted(para) {
			out = append(out, doc.Pre{Text: strings.Join(para, "\n")})
		} else {
			var words []string
			for _, l := range para {
				words = append(words, strings.TrimSpace(l))
			}
			out = append(out, doc.Paragraph{Text: linked(d, strings.Join(words, " "))})
		}
		para = nil
	}
	for i := 0; i < len(lines); {
		l := lines[i]
		if isQuote(l) {
			flush()
			j := i
			var q []string
			for j < len(lines) && (isQuote(lines[j]) || strings.TrimSpace(lines[j]) == "" && j+1 < len(lines) && isQuote(lines[j+1])) {
				q = append(q, strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(lines[j]), ">")))
				j++
			}
			out = append(out, doc.Collapsible{Show: fmt.Sprintf("quoted, %d lines", len(q)), Hide: "hide the quote",
				Blocks: []doc.Block{doc.Quote{Blocks: []doc.Block{doc.Pre{Text: strings.TrimSpace(strings.Join(q, "\n"))}}}}})
			i = j
			continue
		}
		if strings.TrimSpace(l) == "" {
			flush()
		} else {
			para = append(para, strings.TrimRight(l, " \t"))
		}
		i++
	}
	flush()
	if s := strings.TrimSpace(strings.Join(sig, "\n")); s != "" {
		out = append(out, doc.Collapsible{Show: "signature", Hide: "hide the signature", Blocks: []doc.Block{doc.Pre{Text: s}}})
	}
	if len(out) == 0 {
		out = append(out, doc.Paragraph{Text: doc.Inline{{Text: "(empty post)", Style: doc.Italic}}})
	}
	return out
}

func isQuote(l string) bool { return strings.HasPrefix(strings.TrimLeft(l, " "), ">") }

var reInnerSpaces = regexp.MustCompile(`\S {3,}\S`)

// preformatted: poems and tables keep their lines — a passage of short
// lines, lines indented alike or with columns of spaces.
func preformatted(ls []string) bool {
	if len(ls) < 2 {
		return strings.HasPrefix(ls[0], "  ") || reInnerSpaces.MatchString(ls[0])
	}
	short, indented, spaced := 0, 0, 0
	for _, l := range ls {
		if len([]rune(l)) < 50 {
			short++
		}
		if strings.HasPrefix(l, "  ") || strings.HasPrefix(l, "\t") {
			indented++
		}
		if reInnerSpaces.MatchString(l) {
			spaced++
		}
	}
	n := len(ls)
	return short == n && n >= 3 || indented*2 > n || spaced*3 > n
}
