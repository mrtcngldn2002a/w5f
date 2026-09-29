package smallweb

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
)

func gopherServer(t *testing.T) (host string, port string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	host, port, _ = net.SplitHostPort(ln.Addr().String())
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				line, _ := bufio.NewReader(c).ReadString('\n')
				sel := strings.TrimRight(line, "\r\n")
				switch {
				case sel == "":
					fmt.Fprintf(c, "iWelcome to the hole\t\terror.host\t1\r\n"+
						"0About this server\t/about.txt\t%[1]s\t%[2]s\r\n"+
						"1Phlog\t/phlog\t%[1]s\t%[2]s\r\n"+
						"7Search the hole\t/search\t%[1]s\t%[2]s\r\n"+
						"hA web page\tURL:https://example.org/\t%[1]s\t%[2]s\r\n"+
						"hEvil\tURL:w5f:queue/add?u=x\t%[1]s\t%[2]s\r\n"+
						"9binary.zip\t/bin.zip\t%[1]s\t%[2]s\r\n.\r\n", host, port)
				case sel == "/about.txt":
					fmt.Fprint(c, "This is a plain text file\r\nwith two lines.\r\n.\r\n")
				case strings.HasPrefix(sel, "/search\t"):
					fmt.Fprintf(c, "0Found: %s\t/found.txt\t%s\t%s\r\n.\r\n", strings.TrimPrefix(sel, "/search\t"), host, port)
				default:
					fmt.Fprint(c, "3Not found\t\terror.host\t1\r\n.\r\n")
				}
			}(c)
		}
	}()
	return host, port
}

func TestGopherMenusTextAndSearch(t *testing.T) {
	env := testEnv(t)
	host, port := gopherServer(t)
	base := "gopher://" + net.JoinHostPort(host, port)
	ctx := context.Background()
	d, err := Load(ctx, env, base+"/")
	if err != nil {
		t.Fatal(err)
	}
	txt := flat(d)
	if !strings.Contains(txt, "Welcome to the hole") || !strings.Contains(txt, "binary.zip") {
		t.Errorf("menu:\n%s", txt)
	}
	hrefs := map[string]string{}
	for _, l := range d.Links {
		hrefs[l.Text] = l.Href
	}
	if hrefs["About this server"] != base+"/0/about.txt" || hrefs["Phlog"] != base+"/1/phlog" || hrefs["A web page"] != "https://example.org/" {
		t.Errorf("links: %v", hrefs)
	}
	if _, ok := hrefs["Evil"]; ok {
		t.Error("w5f: links from a gopher hole must be dropped")
	}
	if _, ok := hrefs["binary.zip"]; ok {
		t.Error("binary items are listed without a link")
	}
	td, err := Load(ctx, env, hrefs["About this server"])
	if err != nil || !strings.Contains(flat(td), "with two lines.") || strings.Contains(flat(td), "\n.\n") {
		t.Errorf("text: %v\n%s", err, flat(td))
	}
	in, err := Load(ctx, env, hrefs["Search the hole"])
	if err != nil || !strings.HasPrefix(in.URL, "w5f:smallweb/input?") {
		t.Fatalf("search prompt: %v %q", err, in.URL)
	}
	q := InputCommand(in.URL, "? lost signal")
	res, err := Load(ctx, env, q)
	if err != nil || !strings.Contains(flat(res), "Found: lost signal") {
		t.Errorf("search %q: %v\n%s", q, err, flat(res))
	}
}
