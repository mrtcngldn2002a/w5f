package smallweb

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"w5f/internal/doc"
)

func selfSigned(t *testing.T) tls.Certificate {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: "localhost"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour), DNSNames: []string{"localhost"}}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// geminiServer answers by path.
func geminiServer(t *testing.T, cert tls.Certificate) string {
	t.Helper()
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				line, _ := bufio.NewReader(c).ReadString('\n')
				req := strings.TrimSpace(line)
				switch {
				case strings.HasSuffix(req, "/"):
					c.Write([]byte("20 text/gemini\r\n# Capsule\n\nWelcome to the capsule.\n=> /log.gmi Gemlog\n=> w5f:queue/add?u=x Evil\n* one\n* two\n> quoted\n```\n  ascii  art\n```\n"))
				case strings.HasSuffix(req, "/search"):
					c.Write([]byte("10 Search for what?\r\n"))
				case strings.HasSuffix(req, "/search?cosmic%20horror"):
					c.Write([]byte("20 text/gemini\r\nResults for cosmic horror\n"))
				case strings.HasSuffix(req, "/old"):
					c.Write([]byte("31 /\r\n"))
				case strings.HasSuffix(req, "/gone"):
					c.Write([]byte("51 Not found\r\n"))
				default:
					c.Write([]byte("20 text/plain\r\nplain text\n"))
				}
			}(c)
		}
	}()
	return "gemini://" + ln.Addr().String()
}

func testEnv(t *testing.T) Env {
	dir := t.TempDir()
	return Env{DataDir: dir, CacheDir: filepath.Join(dir, "cache"), Timeout: 5 * time.Second}
}

func flat(d *doc.Document) string {
	var b strings.Builder
	doc.ReplaceBlocks(d.Blocks, func(x doc.Block) ([]doc.Block, bool) {
		switch p := x.(type) {
		case doc.Paragraph:
			b.WriteString(p.Text.PlainText() + "\n")
		case doc.Heading:
			b.WriteString("# " + p.Text.PlainText() + "\n")
		case doc.Pre:
			b.WriteString(p.Text + "\n")
		case doc.Notice:
			b.WriteString("! " + p.Text + "\n")
		}
		return nil, false
	})
	return b.String()
}

func TestGeminiPagesInputRedirectsAndErrors(t *testing.T) {
	env := testEnv(t)
	base := geminiServer(t, selfSigned(t))
	ctx := context.Background()
	d, err := Load(ctx, env, base+"/")
	if err != nil {
		t.Fatal(err)
	}
	txt := flat(d)
	if !strings.Contains(txt, "# Capsule") || !strings.Contains(txt, "Welcome to the capsule.") || !strings.Contains(txt, "ascii  art") {
		t.Errorf("gemtext:\n%s", txt)
	}
	if len(d.Links) != 1 || d.Links[0].Href != base+"/log.gmi" {
		t.Errorf("links (w5f: links from capsules are dropped): %+v", d.Links)
	}
	// Trust on first use: the fingerprint is now stored.
	if b, _ := os.ReadFile(filepath.Join(env.DataDir, "gemini_hosts")); !strings.Contains(string(b), strings.TrimPrefix(base, "gemini://")) {
		t.Errorf("known hosts: %q", b)
	}
	in, err := Load(ctx, env, base+"/search")
	if err != nil || !strings.Contains(flat(in), "Search for what?") || !strings.HasPrefix(in.URL, "w5f:smallweb/input?") {
		t.Fatalf("input: %v %q\n%s", err, in.URL, flat(in))
	}
	if got := InputCommand(in.URL, "? cosmic horror"); got != base+"/search?cosmic%20horror" {
		t.Errorf("input command: %q", got)
	}
	if r, _ := Load(ctx, env, base+"/search?cosmic%20horror"); !strings.Contains(flat(r), "Results for cosmic horror") {
		t.Error("answered input")
	}
	if r, err := Load(ctx, env, base+"/old"); err != nil || !strings.Contains(flat(r), "Welcome to the capsule.") {
		t.Errorf("redirect: %v", err)
	}
	if _, err := Load(ctx, env, base+"/gone"); err == nil || !strings.Contains(err.Error(), "Not found") {
		t.Errorf("51: %v", err)
	}
}

func TestGeminiRefusesAChangedCertificate(t *testing.T) {
	env := testEnv(t)
	base := geminiServer(t, selfSigned(t))
	host := strings.TrimPrefix(base, "gemini://")
	os.WriteFile(filepath.Join(env.DataDir, "gemini_hosts"), []byte(host+" SHA256:deadbeef "+time.Now().Add(time.Hour).Format(time.RFC3339)+"\n"), 0o600)
	if _, err := Load(context.Background(), env, base+"/"); err == nil || !strings.Contains(err.Error(), "certificate") {
		t.Errorf("changed certificate: %v", err)
	}
}
