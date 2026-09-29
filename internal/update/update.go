// Package update replaces the running W5F binary with a signed release from
// GitHub Releases, and puts the previous one back on request. Only the binary
// changes: data, config, notes and cache are never touched.
package update

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// DefaultRepo is the GitHub repo ("owner/name"), set at build time with
// -X w5f/internal/update.DefaultRepo=…; config.toml [update] repo wins.
var DefaultRepo = ""

// Config says where releases come from and what is running.
type Config struct {
	Repo     string                            // "owner/name"
	API      string                            // GitHub API base ("" = https://api.github.com)
	Exe      string                            // the running binary
	Current  string                            // the running version
	Key      ed25519.PublicKey                 // release key (nil = PublicKey())
	Client   *http.Client                      // nil = a client with a timeout
	Selftest func(path string) (string, error) // nil = run "<path> selftest"
}

// Release is a checked, newer release for this platform.
type Release struct {
	Manifest *Manifest
	Asset    Asset
	url      string
}

// Paths of the new and the previous binary next to exe ("w5f.new",
// "w5f.prev"; on Windows "w5f.new.exe", "w5f.prev.exe" so they can run).
func Paths(exe string) (next, prev string) {
	ext := filepath.Ext(exe)
	base := strings.TrimSuffix(exe, ext)
	if ext != ".exe" {
		base, ext = exe, ""
	}
	return base + ".new" + ext, base + ".prev" + ext
}

func (c Config) client() *http.Client {
	if c.Client != nil {
		return c.Client
	}
	return &http.Client{Timeout: 10 * time.Minute}
}

func (c Config) key() ed25519.PublicKey {
	if c.Key != nil {
		return c.Key
	}
	return PublicKey()
}

type ghRelease struct {
	Tag    string `json:"tag_name"`
	Assets []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
		Size int64  `json:"size"`
	} `json:"assets"`
}

func (c Config) get(ctx context.Context, u string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "W5F/"+c.Current+" (update)")
	resp, err := c.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d", u, resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("%s: larger than expected", u)
	}
	return b, nil
}

// Check finds the latest release and verifies its manifest. It returns nil
// (and no error) when the running version is up to date. A half-finished
// earlier download is removed.
func (c Config) Check(ctx context.Context) (*Release, error) {
	next, _ := Paths(c.Exe)
	_ = os.Remove(next)
	if len(c.key()) != ed25519.PublicKeySize {
		return nil, errors.New("this build has no release key, so it cannot update itself (only release builds carry the key)")
	}
	if !strings.Contains(c.Repo, "/") {
		return nil, errors.New(`no release repo set: add [update] repo = "owner/w5f" to config.toml`)
	}
	api := c.API
	if api == "" {
		api = "https://api.github.com"
	}
	b, err := c.get(ctx, api+"/repos/"+c.Repo+"/releases/latest", 1<<20)
	if err != nil {
		return nil, fmt.Errorf("could not reach the releases: %w", err)
	}
	var gr ghRelease
	if err := json.Unmarshal(b, &gr); err != nil {
		return nil, fmt.Errorf("unreadable release list: %w", err)
	}
	urls := map[string]string{}
	for _, a := range gr.Assets {
		urls[a.Name] = a.URL
	}
	if urls[ManifestName] == "" || urls[SignatureName] == "" {
		return nil, fmt.Errorf("release %s has no signed manifest", gr.Tag)
	}
	mb, err := c.get(ctx, urls[ManifestName], 1<<20)
	if err != nil {
		return nil, err
	}
	sb, err := c.get(ctx, urls[SignatureName], 4<<10)
	if err != nil {
		return nil, err
	}
	m, err := Verify(mb, sb, c.key())
	if err != nil {
		return nil, err
	}
	if !Newer(m.Version, c.Current) {
		return nil, nil
	}
	a, ok := m.Assets[Platform()]
	if !ok || urls[a.Name] == "" {
		return nil, fmt.Errorf("release %s has no binary for %s", m.Version, Platform())
	}
	return &Release{Manifest: m, Asset: a, url: urls[a.Name]}, nil
}

// Apply downloads the release next to the running binary, checks its hash
// and that it runs, then swaps it in; the old binary stays as the ".prev".
func (c Config) Apply(ctx context.Context, r *Release) error {
	next, prev := Paths(c.Exe)
	if err := c.download(ctx, r, next); err != nil {
		_ = os.Remove(next)
		return err
	}
	selftest := c.Selftest
	if selftest == nil {
		selftest = runSelftest
	}
	out, err := selftest(next)
	if err != nil || !strings.Contains(out, "w5f "+strings.TrimPrefix(r.Manifest.Version, "v")) {
		_ = os.Remove(next)
		if err == nil {
			err = fmt.Errorf("it reports %q", strings.TrimSpace(out))
		}
		return fmt.Errorf("the new binary failed its self-test, nothing changed: %w", err)
	}
	_ = os.Remove(prev)
	if err := os.Rename(c.Exe, prev); err != nil {
		_ = os.Remove(next)
		return fmt.Errorf("could not move the running binary aside: %w", err)
	}
	if err := os.Rename(next, c.Exe); err != nil {
		_ = os.Rename(prev, c.Exe)
		return fmt.Errorf("could not put the new binary in place (old one restored): %w", err)
	}
	return nil
}

func (c Config) download(ctx context.Context, r *Release, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "W5F/"+c.Current+" (update)")
	resp, err := c.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download: HTTP %d", resp.StatusCode)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, r.Asset.Size+1))
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	if n != r.Asset.Size {
		return fmt.Errorf("download: %d bytes, the manifest says %d", n, r.Asset.Size)
	}
	if hex.EncodeToString(h.Sum(nil)) != strings.ToLower(r.Asset.SHA256) {
		return errors.New("download: checksum does not match the signed manifest")
	}
	return nil
}

func runSelftest(path string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "selftest").CombinedOutput()
	return string(out), err
}

// Rollback swaps the running binary and the previous one (twice = back).
func Rollback(exe string) error {
	_, prev := Paths(exe)
	if _, err := os.Stat(prev); err != nil {
		return errors.New("there is no previous version to go back to")
	}
	tmp := exe + ".swap"
	_ = os.Remove(tmp)
	if err := os.Rename(exe, tmp); err != nil {
		return err
	}
	if err := os.Rename(prev, exe); err != nil {
		_ = os.Rename(tmp, exe)
		return err
	}
	return os.Rename(tmp, prev)
}

// Executable is the running binary's real path.
func Executable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	return exe, nil
}
