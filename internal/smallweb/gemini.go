package smallweb

import (
	"bufio"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"w5f/internal/doc"
)

var hostsMu sync.Mutex

// trust checks a server certificate against the known hosts (trust on first
// use): an unknown host is remembered; a known host must show the same
// certificate until the remembered one expires.
func (e Env) trust(hostport string, cert []byte, expires time.Time) error {
	sum := sha256.Sum256(cert)
	fp := "SHA256:" + hex.EncodeToString(sum[:])
	hostsMu.Lock()
	defer hostsMu.Unlock()
	path := filepath.Join(e.DataDir, "gemini_hosts")
	data, _ := os.ReadFile(path)
	var keep []string
	for _, ln := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		f := strings.Fields(ln)
		if len(f) != 3 {
			continue
		}
		if f[0] != hostport {
			keep = append(keep, ln)
			continue
		}
		if f[1] == fp {
			return nil
		}
		if until, err := time.Parse(time.RFC3339, f[2]); err == nil && time.Now().Before(until) {
			return fmt.Errorf("the certificate of %s changed (known %s, now %s) — refusing; if the capsule really renewed it, remove its line from %s", hostport, f[1], fp, path)
		}
	}
	keep = append(keep, hostport+" "+fp+" "+expires.UTC().Format(time.RFC3339))
	if err := os.MkdirAll(e.DataDir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strings.Join(keep, "\n")+"\n"), 0o600)
}

// gemini performs one request.
func (e Env) gemini(ctx context.Context, u *url.URL) (*response, error) {
	host := u.Host
	if u.Port() == "" {
		host = net.JoinHostPort(u.Hostname(), "1965")
	}
	ctx, cancel := context.WithTimeout(ctx, e.timeout())
	defer cancel()
	d := tls.Dialer{Config: &tls.Config{MinVersion: tls.VersionTLS12, ServerName: u.Hostname(),
		InsecureSkipVerify: true}} // capsules use self-signed certificates: checked by trust below
	conn, err := d.DialContext(ctx, "tcp", host)
	if err != nil {
		return nil, fmt.Errorf("Gemini: %w", err)
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		conn.SetDeadline(dl)
	}
	state := conn.(*tls.Conn).ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return nil, errors.New("Gemini: the capsule sent no certificate")
	}
	leaf := state.PeerCertificates[0]
	if err := e.trust(host, leaf.Raw, leaf.NotAfter); err != nil {
		return nil, fmt.Errorf("Gemini: %w", err)
	}
	req := *u
	req.Fragment = ""
	if _, err := conn.Write([]byte(req.String() + "\r\n")); err != nil {
		return nil, fmt.Errorf("Gemini: %w", err)
	}
	br := bufio.NewReader(io.LimitReader(conn, maxBody+1100))
	header, err := br.ReadString('\n')
	if err != nil && header == "" {
		return nil, fmt.Errorf("Gemini: no answer: %w", err)
	}
	header = strings.TrimRight(header, "\r\n")
	code, meta, _ := strings.Cut(header, " ")
	status, err := strconv.Atoi(code)
	if err != nil || len(code) != 2 {
		return nil, fmt.Errorf("Gemini: bad header %q", header)
	}
	r := &response{Status: status, Meta: strings.TrimSpace(meta)}
	if status/10 == 2 {
		r.Body, err = io.ReadAll(io.LimitReader(br, maxBody))
		if err != nil && len(r.Body) == 0 {
			return nil, fmt.Errorf("Gemini: %w", err)
		}
	}
	return r, nil
}

// geminiDoc converts a 2x answer.
func geminiDoc(u *url.URL, r *response) *doc.Document {
	mime := strings.ToLower(strings.TrimSpace(strings.SplitN(r.Meta, ";", 2)[0]))
	d := &doc.Document{URL: u.String(), Origin: "live"}
	if lang := metaParam(r.Meta, "lang"); lang != "" {
		d.Lang = strings.Split(lang, ",")[0]
	}
	switch {
	case mime == "" || mime == "text/gemini":
		gemtext(d, u, string(r.Body))
	case strings.HasPrefix(mime, "text/"):
		d.Blocks = []doc.Block{doc.Pre{Text: strings.ToValidUTF8(string(r.Body), "�")}}
	default:
		d.Blocks = []doc.Block{doc.Notice{Kind: "info", Text: "This capsule sent a " + mime + " file (" + strconv.Itoa(len(r.Body)) + " bytes), which W5F does not show."}}
	}
	if d.Title == "" {
		d.Title = u.Host + u.Path
	}
	return d
}

func metaParam(meta, key string) string {
	for _, p := range strings.Split(meta, ";") {
		k, v, ok := strings.Cut(strings.TrimSpace(p), "=")
		if ok && strings.EqualFold(k, key) {
			return strings.Trim(v, `"`)
		}
	}
	return ""
}

// gemtext converts text/gemini into blocks.
func gemtext(d *doc.Document, base *url.URL, text string) {
	var items [][]doc.Block
	flushList := func() {
		if len(items) > 0 {
			d.Blocks = append(d.Blocks, doc.List{Items: items})
			items = nil
		}
	}
	var pre []string
	inPre := false
	for _, ln := range strings.Split(strings.ReplaceAll(strings.ToValidUTF8(text, "�"), "\r", ""), "\n") {
		if strings.HasPrefix(ln, "```") {
			if inPre {
				d.Blocks = append(d.Blocks, doc.Pre{Text: strings.Join(pre, "\n")})
				pre, inPre = nil, false
			} else {
				flushList()
				inPre = true
			}
			continue
		}
		if inPre {
			pre = append(pre, ln)
			continue
		}
		switch {
		case strings.HasPrefix(ln, "* "):
			items = append(items, []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: strings.TrimSpace(ln[2:])}}}})
			continue
		}
		flushList()
		switch {
		case strings.TrimSpace(ln) == "":
		case strings.HasPrefix(ln, "=>"):
			f := strings.Fields(strings.TrimSpace(ln[2:]))
			if len(f) == 0 {
				continue
			}
			label := strings.Join(f[1:], " ")
			target, err := base.Parse(f[0])
			if label == "" {
				label = f[0]
			}
			if err != nil || strings.EqualFold(target.Scheme, "w5f") || strings.EqualFold(target.Scheme, "javascript") {
				d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: label}}}) // never a W5F action from a capsule
				continue
			}
			d.Links = append(d.Links, doc.Link{Href: target.String(), Text: label})
			d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: "→ ", Style: doc.Italic}, {Text: label, Link: len(d.Links)}}})
		case strings.HasPrefix(ln, "###"):
			d.Blocks = append(d.Blocks, doc.Heading{Level: 3, Text: doc.Inline{{Text: strings.TrimSpace(ln[3:])}}})
		case strings.HasPrefix(ln, "##"):
			d.Blocks = append(d.Blocks, doc.Heading{Level: 2, Text: doc.Inline{{Text: strings.TrimSpace(ln[2:])}}})
		case strings.HasPrefix(ln, "#"):
			t := strings.TrimSpace(ln[1:])
			if d.Title == "" {
				d.Title = t
			}
			d.Blocks = append(d.Blocks, doc.Heading{Level: 1, Text: doc.Inline{{Text: t}}})
		case strings.HasPrefix(ln, ">"):
			d.Blocks = append(d.Blocks, doc.Quote{Blocks: []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: strings.TrimSpace(ln[1:])}}}}})
		default:
			d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: ln}}})
		}
	}
	flushList()
	if inPre {
		d.Blocks = append(d.Blocks, doc.Pre{Text: strings.Join(pre, "\n")})
	}
}
