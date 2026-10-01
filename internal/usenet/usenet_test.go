package usenet

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"w5f/internal/doc"
	"w5f/internal/store"
)

type fakeArticle struct {
	subject, from, date, id, refs string
	headers                       string // extra header lines
	body                          []string
}

// fakeServer answers the NNTP commands W5F sends, for one group.
type fakeServer struct {
	addr     string
	mu       sync.Mutex
	commands []string
}

var articles = map[int]fakeArticle{
	1: {subject: "Ghost lights", from: "Ann <ann@example.org>", date: "Mon, 28 Sep 2026 10:00:00 +0000", id: "<1@x>",
		body: []string{"Has anyone seen the lights over the marsh?", "They move against the wind.", "", "More at https://example.org/lights.", "", "-- ", "Ann, keeper of lanterns"}},
	2: {subject: "Re: Ghost lights", from: "Bob <bob@spam.example>", date: "Mon, 28 Sep 2026 11:00:00 +0000", id: "<2@x>", refs: "<1@x>",
		body: []string{"Ann wrote:", "> Has anyone seen the lights over the marsh?", "> They move against the wind.", "", "Marsh gas, surely."}},
	3: {subject: "[Spam] cheap lanterns", from: "Seller <s@shop.example>", date: "Mon, 28 Sep 2026 12:00:00 +0000", id: "<3@x>", body: []string{"buy"}},
	4: {subject: "=?UTF-8?B?w5Z5a8O8OiBhIHBvZW0=?=", from: "Cem <cem@example.org>", date: "Tue, 29 Sep 2026 09:00:00 +0000", id: "<4@x>",
		headers: "Content-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable",
		body:    []string{"The r=C3=BCzgar at the door,", "the lamp gone out,", ".. and the dark", "asks nothing more."}},
	5: {subject: "Re: Ghost lights", from: "Ann <ann@example.org>", date: "Wed, 30 Sep 2026 08:00:00 +0000", id: "<5@x>", refs: "<1@x> <2@x>",
		body: []string{"Not in September, Bob."}},
	6: {subject: "Re: Öykü: a poem", from: "Dee <dee@example.org>", date: "Wed, 30 Sep 2026 09:00:00 +0000", id: "<6@x>", refs: "<gone@x>",
		body: []string{"Lovely."}},
}

func startServer(t *testing.T) *fakeServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &fakeServer{addr: ln.Addr().String()}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serve(c)
		}
	}()
	return s
}

func (s *fakeServer) serve(c net.Conn) {
	defer c.Close()
	r := bufio.NewReader(c)
	fmt.Fprint(c, "200 fake news ready (posting ok)\r\n")
	group := ""
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		s.mu.Lock()
		s.commands = append(s.commands, line)
		s.mu.Unlock()
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		switch strings.ToUpper(f[0]) {
		case "QUIT":
			fmt.Fprint(c, "205 bye\r\n")
			return
		case "GROUP":
			switch f[1] {
			case "alt.test":
				group = f[1]
				fmt.Fprint(c, "211 6 1 6 alt.test\r\n")
			case "rec.empty":
				group = f[1]
				fmt.Fprint(c, "211 0 1 0 rec.empty\r\n")
			default:
				fmt.Fprint(c, "411 no such group\r\n")
			}
		case "XOVER":
			if group != "alt.test" {
				fmt.Fprint(c, "412 no group\r\n")
				continue
			}
			lo, hi, _ := strings.Cut(f[1], "-")
			a, _ := strconv.Atoi(lo)
			b, _ := strconv.Atoi(hi)
			fmt.Fprint(c, "224 overview\r\n")
			for n := a; n <= b; n++ {
				if x, ok := articles[n]; ok {
					fmt.Fprintf(c, "%d\t%s\t%s\t%s\t%s\t%s\t100\t%d\r\n", n, x.subject, x.from, x.date, x.id, x.refs, len(x.body))
				}
			}
			fmt.Fprint(c, ".\r\n")
		case "ARTICLE":
			n, _ := strconv.Atoi(f[1])
			x, ok := articles[n]
			if !ok {
				fmt.Fprint(c, "423 no such article\r\n")
				continue
			}
			fmt.Fprintf(c, "220 %d %s\r\n", n, x.id)
			fmt.Fprintf(c, "From: %s\r\nSubject: %s\r\nDate: %s\r\nMessage-ID: %s\r\n", x.from, x.subject, x.date, x.id)
			if x.refs != "" {
				fmt.Fprintf(c, "References: %s\r\n", x.refs)
			}
			if x.headers != "" {
				fmt.Fprintf(c, "%s\r\n", x.headers)
			}
			fmt.Fprint(c, "\r\n")
			for _, l := range x.body {
				fmt.Fprintf(c, "%s\r\n", l) // the bodies above are already dot-stuffed
			}
			fmt.Fprint(c, ".\r\n")
		case "LIST":
			fmt.Fprint(c, "215 active\r\n")
			for _, g := range []string{"alt.test 6 1 y", "alt.test.more 20 11 y", "rec.poems 3 1 y"} {
				if strings.Contains(g, strings.Trim(f[2], "*")) {
					fmt.Fprintf(c, "%s\r\n", g)
				}
			}
			fmt.Fprint(c, ".\r\n")
		default:
			fmt.Fprint(c, "500 what?\r\n")
		}
	}
}

func testEnv(t *testing.T, s *fakeServer, groups ...string) Env {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	path := filepath.Join(t.TempDir(), "usenet.toml")
	if err := (Config{Server: s.addr, Groups: groups, Kill: Kill{Subjects: []string{"[spam]"}}}).Save(path); err != nil {
		t.Fatal(err)
	}
	return Env{DB: db, ConfigPath: path}
}

func text(d *doc.Document) string {
	var b strings.Builder
	doc.ReplaceBlocks(d.Blocks, func(x doc.Block) ([]doc.Block, bool) {
		switch p := x.(type) {
		case doc.Paragraph:
			b.WriteString(p.Text.PlainText() + "\n")
		case doc.Heading:
			b.WriteString("# " + p.Text.PlainText() + "\n")
		case doc.Pre:
			b.WriteString("[pre]" + p.Text + "\n")
		case doc.Notice:
			b.WriteString("! " + p.Text + "\n")
		case doc.Collapsible:
			b.WriteString("[fold " + p.Show + "]\n")
		}
		return nil, false
	})
	return b.String()
}

func TestRanges(t *testing.T) {
	r := parseRanges("1-5,7, 9-10,x,12-11")
	r = r.add(6, 6).add(20, 25).add(11, 11)
	if r.String() != "1-7,9-11,20-25" {
		t.Errorf("ranges: %s", r)
	}
	if !r.has(4) || r.has(8) || !r.has(25) || r.has(26) || r.countIn(5, 21) != 8 {
		t.Errorf("has/count: %v %d", r, r.countIn(5, 21))
	}
}

func TestGroupThreadsAndReading(t *testing.T) {
	s := startServer(t)
	env := testEnv(t, s, "alt.test", "no.such.group")
	ctx := context.Background()

	home, err := Route(ctx, "w5f:usenet", env)
	if err != nil {
		t.Fatal(err)
	}
	h := text(home)
	if !strings.Contains(h, "alt.test  6 new  · 6 posts") || !strings.Contains(h, "no.such.group  · the server does not carry that group") {
		t.Errorf("home:\n%s", h)
	}

	g, err := Route(ctx, "w5f:usenet/g/alt.test", env)
	if err != nil {
		t.Fatal(err)
	}
	gt := text(g)
	for _, want := range []string{"1 posts hidden by your kill file.", "Öykü: a poem  · Cem · 2 posts · 30 Sep 2026", "Ghost lights  · Ann · 3 posts · 30 Sep 2026"} {
		if !strings.Contains(gt, want) {
			t.Errorf("group page lacks %q:\n%s", want, gt)
		}
	}
	if strings.Index(gt, "Öykü") > strings.Index(gt, "Ghost lights") {
		t.Error("threads are listed newest first")
	}

	th, err := Route(ctx, "w5f:usenet/t/alt.test/1?from=1", env)
	if err != nil {
		t.Fatal(err)
	}
	tt := text(th)
	for _, want := range []string{"# Ann · 28 Sep 2026 10:00 · new", "# › Bob · 28 Sep 2026 11:00 · new", "# › › Ann · 30 Sep 2026 08:00 · new",
		"Has anyone seen the lights over the marsh? They move against the wind.", "[fold quoted, 2 lines]", "[fold signature]", "Marsh gas, surely."} {
		if !strings.Contains(tt, want) {
			t.Errorf("thread lacks %q:\n%s", want, tt)
		}
	}
	found := false
	for _, l := range th.Links {
		if l.Href == "https://example.org/lights" {
			found = true
		}
	}
	if !found {
		t.Error("web addresses in posts are links (without the full stop)")
	}
	home, _ = Route(ctx, "w5f:usenet", env)
	if h := text(home); !strings.Contains(h, "alt.test  3 new") {
		t.Errorf("after reading the thread:\n%s", h)
	}

	poem, err := Route(ctx, "w5f:usenet/t/alt.test/4", env)
	if err != nil {
		t.Fatal(err)
	}
	pt := text(poem)
	if !strings.Contains(pt, "[pre]The rüzgar at the door,\nthe lamp gone out,\n. and the dark\nasks nothing more.") || !strings.Contains(pt, "# › Dee") {
		t.Errorf("poem thread (quoted-printable, dot-stuffing, short lines kept, orphan reply by subject):\n%s", pt)
	}
}

func TestKillSubscribeAndSafety(t *testing.T) {
	s := startServer(t)
	env := testEnv(t, s, "alt.test")
	ctx := context.Background()

	d, err := Route(ctx, "w5f:usenet/kill?from=bob%40spam.example&back=w5f%3Ausenet%2Ft%2Falt.test%2F1%3Ffrom%3D1", env)
	if err != nil {
		t.Fatal(err)
	}
	if dt := text(d); strings.Contains(dt, "# › Bob") || !strings.Contains(dt, "# › Ann · 30 Sep") || !strings.Contains(dt, "Hidden from now on: posts from bob@spam.example.") {
		t.Errorf("after killing Bob:\n%s", dt)
	}
	cfg, _ := LoadConfig(env.ConfigPath)
	if len(cfg.Kill.From) != 1 || cfg.Kill.From[0] != "bob@spam.example" {
		t.Errorf("kill file: %+v", cfg.Kill)
	}
	if d, _ := Route(ctx, "w5f:usenet/kill?subject=x&back=https%3A%2F%2Fevil.example", env); d.URL != "w5f:usenet" {
		t.Errorf("back goes only to Usenet pages: %s", d.URL)
	}

	f, err := Route(ctx, "w5f:usenet/find?q=test", env)
	if err != nil || !strings.Contains(text(f), "alt.test.more  · about 10 posts   subscribe") || !strings.Contains(text(f), "alt.test  · about 6 posts   subscribed") {
		t.Fatalf("find: %v\n%s", err, text(f))
	}
	if _, err := Route(ctx, "w5f:usenet/sub/alt.test.more", env); err == nil {
		t.Log("subscribed page opened (the fake server does not carry it, so an error is also fine)")
	}
	cfg, _ = LoadConfig(env.ConfigPath)
	if !cfg.subscribed("alt.test.more") {
		t.Errorf("subscribe: %v", cfg.Groups)
	}
	Route(ctx, "w5f:usenet/unsub/alt.test", env)
	cfg, _ = LoadConfig(env.ConfigPath)
	if cfg.subscribed("alt.test") {
		t.Errorf("unsubscribe: %v", cfg.Groups)
	}

	// Nothing typed or found on a page smuggles a command to the server.
	for _, bad := range []string{"w5f:usenet/g/alt.test%0D%0AQUIT", "w5f:usenet/find?q=a%0D%0ALIST", "w5f:usenet/sub/Bad%20Group"} {
		Route(ctx, bad, env)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.commands {
		if strings.Contains(strings.ToUpper(c), "QUIT") && c != "QUIT" || strings.Contains(c, "Bad") {
			t.Errorf("a smuggled command reached the server: %q", c)
		}
	}
}

func TestFirstConfigHasTheOwnersGroups(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usenet.toml")
	c, err := LoadConfig(path)
	if err != nil || c.Server != DefaultServer || !c.subscribed("alt.magick") || !c.subscribed("rec.arts.sf.written") || len(c.Groups) != 13 {
		t.Fatalf("%+v %v", c, err)
	}
	b, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(b), "# W5F Usenet") {
		t.Errorf("file:\n%s", b)
	}
}
