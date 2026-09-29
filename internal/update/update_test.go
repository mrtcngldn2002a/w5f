package update

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The test binary doubles as a "w5f" for the self-test: run with "selftest"
// and W5F_FAKE_VERSION it prints that version (or fails when asked to).
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "selftest" && os.Getenv("W5F_FAKE_VERSION") != "" {
		if os.Getenv("W5F_FAKE_FAIL") != "" {
			fmt.Println("broken")
			os.Exit(3)
		}
		fmt.Println("w5f " + os.Getenv("W5F_FAKE_VERSION"))
		os.Exit(0)
	}
	os.Exit(m.Run())
}

type fakeRelease struct {
	srv      *httptest.Server
	key      ed25519.PrivateKey
	pub      ed25519.PublicKey
	binary   []byte
	manifest []byte
	sig      []byte
	cut      bool // serve a truncated binary
}

func newRelease(t *testing.T, version string, tamper func(*Manifest)) *fakeRelease {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(nil)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bin, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(bin)
	name := "w5f-" + Platform()
	m := Manifest{Version: version, Date: "2026-09-30", Assets: map[string]Asset{Platform(): {Name: name, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(bin))}}}
	if tamper != nil {
		tamper(&m)
	}
	mb, _ := json.Marshal(m)
	r := &fakeRelease{key: priv, pub: pub, binary: bin, manifest: mb, sig: Sign(mb, priv)}
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/repos/me/w5f/releases/latest":
			base := r.srv.URL + "/dl/"
			fmt.Fprintf(w, `{"tag_name":"v%s","assets":[{"name":%q,"browser_download_url":%q},{"name":"manifest.json","browser_download_url":%q},{"name":"manifest.json.sig","browser_download_url":%q}]}`,
				version, name, base+name, base+"manifest.json", base+"manifest.json.sig")
		case "/dl/manifest.json":
			w.Write(r.manifest)
		case "/dl/manifest.json.sig":
			w.Write(r.sig)
		case "/dl/" + name:
			if r.cut {
				w.Write(r.binary[:len(r.binary)/2])
				return
			}
			w.Write(r.binary)
		default:
			http.NotFound(w, req)
		}
	}))
	t.Cleanup(r.srv.Close)
	return r
}

// install puts a fake "current" binary in a temp dir.
func install(t *testing.T) string {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "w5f")
	if Platform()[:7] == "windows" {
		exe += ".exe"
	}
	if err := os.WriteFile(exe, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	return exe
}

func (r *fakeRelease) config(exe string) Config {
	return Config{Repo: "me/w5f", API: r.srv.URL, Exe: exe, Current: "0.6.4", Key: r.pub}
}

func content(t *testing.T, path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return "(missing)"
	}
	if len(b) > 20 {
		return "(new binary)"
	}
	return string(b)
}

func TestUpdateInstallsAndRollsBack(t *testing.T) {
	t.Setenv("W5F_FAKE_VERSION", "0.7.0")
	r := newRelease(t, "0.7.0", nil)
	exe := install(t)
	c := r.config(exe)
	rel, err := c.Check(context.Background())
	if err != nil || rel == nil || rel.Manifest.Version != "0.7.0" {
		t.Fatalf("check: %+v %v", rel, err)
	}
	if err := c.Apply(context.Background(), rel); err != nil {
		t.Fatal(err)
	}
	next, prev := Paths(exe)
	if content(t, exe) != "(new binary)" || content(t, prev) != "old binary" || content(t, next) != "(missing)" {
		t.Fatalf("after update: exe=%s prev=%s next=%s", content(t, exe), content(t, prev), content(t, next))
	}
	// The same release again: nothing to do.
	c.Current = "0.7.0"
	if rel, err := c.Check(context.Background()); rel != nil || err != nil {
		t.Errorf("up to date: %+v %v", rel, err)
	}
	// Rollback twice returns to where it started.
	if err := Rollback(exe); err != nil || content(t, exe) != "old binary" || content(t, prev) != "(new binary)" {
		t.Fatalf("rollback: %v exe=%s", err, content(t, exe))
	}
	if err := Rollback(exe); err != nil || content(t, exe) != "(new binary)" {
		t.Fatalf("rollback again: %v exe=%s", err, content(t, exe))
	}
}

func TestUpdateRefusals(t *testing.T) {
	t.Setenv("W5F_FAKE_VERSION", "0.7.0")
	ctx := context.Background()
	cases := []struct {
		name  string
		setup func(r *fakeRelease, c *Config)
		want  string
	}{
		{"foreign key", func(r *fakeRelease, c *Config) {
			_, other, _ := ed25519.GenerateKey(nil)
			r.sig = Sign(r.manifest, other)
		}, "signature"},
		{"edited manifest", func(r *fakeRelease, c *Config) {
			r.manifest = []byte(strings.Replace(string(r.manifest), "0.7.0", "0.7.1", 1))
		}, "signature"},
		{"no key in this build", func(r *fakeRelease, c *Config) { c.Key = ed25519.PublicKey{} }, "no release key"},
		{"no repo", func(r *fakeRelease, c *Config) { c.Repo = "" }, "no release repo"},
		{"no network", func(r *fakeRelease, c *Config) { c.API = "http://127.0.0.1:1" }, "could not reach"},
	}
	for _, tc := range cases {
		r := newRelease(t, "0.7.0", nil)
		exe := install(t)
		c := r.config(exe)
		tc.setup(r, &c)
		rel, err := c.Check(ctx)
		if err == nil || !strings.Contains(err.Error(), tc.want) || rel != nil {
			t.Errorf("%s: %v", tc.name, err)
		}
		if content(t, exe) != "old binary" {
			t.Errorf("%s: the binary changed", tc.name)
		}
	}
}

func TestApplyRefusesABadBinary(t *testing.T) {
	ctx := context.Background()
	check := func(name string, r *fakeRelease, want string) {
		t.Helper()
		exe := install(t)
		c := r.config(exe)
		rel, err := c.Check(ctx)
		if err != nil || rel == nil {
			t.Fatalf("%s: check %v", name, err)
		}
		err = c.Apply(ctx, rel)
		next, prev := Paths(exe)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", name, err)
		}
		if content(t, exe) != "old binary" || content(t, next) != "(missing)" || content(t, prev) != "(missing)" {
			t.Errorf("%s: exe=%s next=%s prev=%s", name, content(t, exe), content(t, next), content(t, prev))
		}
	}
	t.Setenv("W5F_FAKE_VERSION", "0.7.0")
	bad := newRelease(t, "0.7.0", func(m *Manifest) {
		a := m.Assets[Platform()]
		a.SHA256 = strings.Repeat("0", 64)
		m.Assets[Platform()] = a
	})
	check("wrong checksum", bad, "checksum")

	cut := newRelease(t, "0.7.0", nil)
	cut.cut = true
	check("interrupted download", cut, "bytes")

	t.Setenv("W5F_FAKE_FAIL", "1")
	check("failing self-test", newRelease(t, "0.7.0", nil), "self-test")
	os.Unsetenv("W5F_FAKE_FAIL")

	t.Setenv("W5F_FAKE_VERSION", "0.6.9") // runs, but is not the version it claims
	check("wrong version inside", newRelease(t, "0.7.0", nil), "self-test")
}

func TestNoDowngradeAndLeftovers(t *testing.T) {
	r := newRelease(t, "0.6.0", nil)
	exe := install(t)
	next, _ := Paths(exe)
	os.WriteFile(next, []byte("half a download"), 0o755)
	rel, err := r.config(exe).Check(context.Background())
	if rel != nil || err != nil {
		t.Errorf("an older release is not installed: %+v %v", rel, err)
	}
	if content(t, next) != "(missing)" {
		t.Error("a half-finished download is cleaned up")
	}
	if err := Rollback(exe); err == nil || content(t, exe) != "old binary" {
		t.Errorf("rollback without a previous version: %v", err)
	}
}

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{{"0.7.0", "0.6.4", true}, {"v0.10.0", "0.9.9", true}, {"0.6.4", "0.6.4", false}, {"0.6.3", "0.6.4", false}, {"0.1.0", "0.0.0-dev", true}, {"garbage", "0.1.0", false}} {
		if Newer(c.a, c.b) != c.want {
			t.Errorf("Newer(%q, %q) != %v", c.a, c.b, c.want)
		}
	}
}
