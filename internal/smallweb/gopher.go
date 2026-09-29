package smallweb

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"

	"w5f/internal/doc"
)

// gopherParts splits gopher://host[:port]/<type><selector>[%09query].
func gopherParts(u *url.URL) (hostport string, typ byte, selector, query string) {
	hostport = u.Host
	if u.Port() == "" {
		hostport = net.JoinHostPort(u.Hostname(), "70")
	}
	p := u.Path
	if raw, err := url.PathUnescape(u.EscapedPath()); err == nil {
		p = raw
	}
	p = strings.TrimPrefix(p, "/")
	typ = '1'
	if p != "" {
		typ, p = p[0], p[1:]
	}
	selector = p
	if i := strings.Index(selector, "\t"); i >= 0 {
		selector, query = selector[:i], selector[i+1:]
	}
	return
}

// gopher performs one request; Meta holds the item type.
func (e Env) gopher(ctx context.Context, u *url.URL) (*response, error) {
	hostport, typ, selector, query := gopherParts(u)
	ctx, cancel := context.WithTimeout(ctx, e.timeout())
	defer cancel()
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", hostport)
	if err != nil {
		return nil, fmt.Errorf("Gopher: %w", err)
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		conn.SetDeadline(dl)
	}
	req := selector
	if query != "" {
		req += "\t" + query
	}
	if _, err := conn.Write([]byte(req + "\r\n")); err != nil {
		return nil, fmt.Errorf("Gopher: %w", err)
	}
	body, err := io.ReadAll(io.LimitReader(conn, maxBody))
	if err != nil && len(body) == 0 {
		return nil, fmt.Errorf("Gopher: %w", err)
	}
	return &response{Status: 20, Meta: string(typ), Body: body}, nil
}

// gopherDoc opens a Gopher address: a menu, a text file or a search.
func (e Env) gopherDoc(ctx context.Context, u *url.URL) (*doc.Document, error) {
	_, typ, _, query := gopherParts(u)
	if typ == '7' && query == "" {
		return inputDoc(u, "This Gopher search asks for words.", false), nil
	}
	switch typ {
	case '0', '1', '7':
	default:
		return nil, fmt.Errorf("Gopher item type %q is a file W5F does not show", string(typ))
	}
	r, err := e.fetch(ctx, u)
	if err != nil {
		return nil, err
	}
	d := &doc.Document{URL: u.String(), Title: u.Host + u.Path, Origin: "live"}
	text := strings.ReplaceAll(strings.ToValidUTF8(string(r.Body), "�"), "\r\n", "\n")
	if typ == '0' {
		text = strings.TrimRight(text, "\n")
		text = strings.TrimRight(strings.TrimSuffix(text, "\n."), "\n")
		d.Blocks = []doc.Block{doc.Pre{Text: text}}
		return d, nil
	}
	gophermap(d, text)
	return d, nil
}

var gopherMark = map[byte]string{'0': "¶ ", '1': "▸ ", '7': "? ", 'h': "→ "}

// gophermap converts a Gopher menu; only text, menu, search and web items
// get links (never a w5f: address).
func gophermap(d *doc.Document, text string) {
	var info []string
	flush := func() {
		if len(info) > 0 {
			d.Blocks = append(d.Blocks, doc.Pre{Text: strings.Join(info, "\n")})
			info = nil
		}
	}
	for _, ln := range strings.Split(text, "\n") {
		if ln == "." || ln == "" {
			continue
		}
		t, rest := ln[0], ln[1:]
		f := strings.Split(rest, "\t")
		display := f[0]
		if t == 'i' || t == '3' || len(f) < 4 {
			info = append(info, display)
			continue
		}
		flush()
		selector, host, port := f[1], f[2], strings.TrimSpace(f[3])
		var href string
		switch t {
		case '0', '1', '7':
			href = "gopher://" + net.JoinHostPort(host, port) + "/" + string(t) + (&url.URL{Path: selector}).EscapedPath()
		case 'h':
			if s, ok := strings.CutPrefix(selector, "URL:"); ok {
				if lu, err := url.Parse(s); err == nil && (lu.Scheme == "http" || lu.Scheme == "https" || lu.Scheme == "gemini" || lu.Scheme == "gopher") {
					href = lu.String()
				}
			}
		}
		if href == "" {
			label := "[file] " + display
			if t == 'h' {
				label = display
			}
			d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: label, Style: doc.Italic}}})
			continue
		}
		d.Links = append(d.Links, doc.Link{Href: href, Text: display})
		d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: gopherMark[t], Style: doc.Italic}, {Text: display, Link: len(d.Links)}}})
	}
	flush()
}
