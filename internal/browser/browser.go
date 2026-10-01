// Package browser is W5F's way to a real browser when one is needed: it
// opens a page in Chromium (to sign in, to look at what W5F cannot show)
// and takes a site's session cookies from Chromium's profile when the owner
// asks (chosen with the owner, 2026-10-01).
package browser

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// candidates are the Chromium-family programs looked for, first found wins.
var candidates = []string{"chromium", "chromium-browser", "google-chrome-stable", "google-chrome"}

// Binary finds Chromium (W5F_BROWSER overrides).
func Binary() (string, error) {
	if b := os.Getenv("W5F_BROWSER"); b != "" {
		return exec.LookPath(b)
	}
	for _, c := range candidates {
		if p, err := exec.LookPath(c); err == nil {
			return p, nil
		}
	}
	if runtime.GOOS == "windows" {
		for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)", "LocalAppData"} {
			base := os.Getenv(env)
			if base == "" {
				continue
			}
			for _, rel := range []string{`Chromium\Application\chrome.exe`, `Google\Chrome\Application\chrome.exe`} {
				if p := filepath.Join(base, rel); fileExists(p) {
					return p, nil
				}
			}
		}
	}
	return "", errors.New("Chromium is not installed (antiX: sudo apt install chromium)")
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// Open shows a web page in Chromium, in its own window, without waiting.
func Open(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("only web pages open in Chromium (%q is not one)", raw)
	}
	if runtime.GOOS == "linux" && os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		return errors.New("no graphical display here (Chromium needs the X session)")
	}
	bin, err := Binary()
	if err != nil {
		return err
	}
	cmd := exec.Command(bin, u.String())
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting Chromium: %v", err)
	}
	go cmd.Wait() // reap it when it closes; W5F does not wait
	return nil
}
