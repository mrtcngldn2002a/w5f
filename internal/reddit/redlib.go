// Package reddit reads Reddit through Redlib (github.com/redlib-org/redlib), a
// lightweight open-source Reddit front end. Reddit's own pages sit behind a
// JavaScript bot check for terminal readers, so W5F runs a local Redlib on
// demand and converts its plain HTML into documents. Addresses stay Reddit
// URLs everywhere in W5F; only the fetch goes through Redlib.
package reddit

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Config says where Redlib lives.
type Config struct {
	// URL of a running Redlib. Empty means "start the local binary".
	URL string
	// Bin is the redlib executable; empty searches PATH and the W5F data dir.
	Bin string
	// Port for the locally started instance.
	Port int
}

// ErrNoRedlib means no Redlib is running and no binary was found.
var ErrNoRedlib = errors.New("redlib not found")

var (
	mu      sync.Mutex
	cfg     = Config{Port: 8765}
	proc    *exec.Cmd
	baseURL string
)

// Configure sets the Redlib configuration (call before first use).
func Configure(c Config) {
	mu.Lock()
	defer mu.Unlock()
	if c.Port == 0 {
		c.Port = 8765
	}
	cfg = c
}

// Base returns the base URL of a usable Redlib, starting the local binary if
// needed. The instance keeps running until Shutdown.
func Base(ctx context.Context) (string, error) {
	mu.Lock()
	defer mu.Unlock()
	if baseURL != "" && alive(baseURL) {
		return baseURL, nil
	}
	if cfg.URL != "" {
		if !alive(cfg.URL) {
			return "", fmt.Errorf("redlib at %s is not responding", cfg.URL)
		}
		baseURL = strings.TrimRight(cfg.URL, "/")
		return baseURL, nil
	}
	local := fmt.Sprintf("http://127.0.0.1:%d", cfg.Port)
	if alive(local) {
		baseURL = local
		return baseURL, nil
	}
	bin := findBinary(cfg.Bin)
	if bin == "" {
		return "", ErrNoRedlib
	}
	cmd := exec.Command(bin, "--address", "127.0.0.1", "--port", fmt.Sprint(cfg.Port))
	cmd.Env = append(os.Environ(),
		"REDLIB_DEFAULT_SHOW_NSFW=on", "REDLIB_DEFAULT_BLUR_NSFW=off", // every kind of content (the owner, 2026-10-02)
		"REDLIB_DEFAULT_USE_HLS=off", "REDLIB_DEFAULT_HIDE_HLS_NOTIFICATION=on",
		"REDLIB_ROBOTS_DISABLE_INDEXING=on")
	hideWindow(cmd)
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("starting redlib: %w", err)
	}
	proc = cmd
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if alive(local) {
			baseURL = local
			return baseURL, nil
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(150 * time.Millisecond):
		}
	}
	return "", errors.New("redlib did not start in time")
}

// Shutdown stops a Redlib started by W5F.
func Shutdown() {
	mu.Lock()
	defer mu.Unlock()
	if proc != nil && proc.Process != nil {
		_ = proc.Process.Kill()
		_, _ = proc.Process.Wait()
	}
	proc, baseURL = nil, ""
}

func alive(base string) bool {
	u := strings.TrimRight(base, "/")
	host := strings.TrimPrefix(strings.TrimPrefix(u, "http://"), "https://")
	if c, err := net.DialTimeout("tcp", host, 300*time.Millisecond); err == nil {
		c.Close()
	} else {
		return false
	}
	cl := &http.Client{Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := cl.Get(u + "/settings")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode < 500
}

func findBinary(explicit string) string {
	if explicit != "" {
		if _, err := os.Stat(explicit); err == nil {
			return explicit
		}
		return ""
	}
	name := "redlib"
	if runtime.GOOS == "windows" {
		name = "redlib.exe"
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	var dirs []string
	if d, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(d, ".local", "share", "w5f", "bin"), filepath.Join(d, ".cargo", "bin"))
	}
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Dir(exe))
	}
	for _, d := range dirs {
		p := filepath.Join(d, name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}
