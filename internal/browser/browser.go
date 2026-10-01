// Package browser is W5F's way to a real browser when one is needed: it
// opens a page (to sign in, to look at what W5F cannot show) and takes a
// site's session cookies from a browser's profile when the owner asks
// (chosen with the owner, 2026-10-01).
//
// Which browser (2026-10-02, so W5F works on computers other than the
// laptop): W5F_BROWSER, else [browser] command in config.toml, else
// Chromium or Chrome when installed (the laptop's way, unchanged), else
// the system's default browser.
package browser

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"w5f/internal/config"
)

// candidates are the Chromium-family programs looked for, first found wins.
var candidates = []string{"chromium", "chromium-browser", "google-chrome-stable", "google-chrome"}

// Launcher is a way to open a page: a program, its arguments (the page's
// address goes last) and its name for messages.
type Launcher struct {
	Name string
	Argv []string
}

// configured is [browser] command from config.toml (a variable for tests).
var configured = func() string { return config.Load().Browser.Command }

// Choose finds the browser pages open in.
func Choose() (Launcher, error) {
	if b := os.Getenv("W5F_BROWSER"); b != "" {
		return command(b, "W5F_BROWSER")
	}
	if c := strings.TrimSpace(configured()); c != "" {
		return command(c, "[browser] command in config.toml")
	}
	if p, err := Binary(); err == nil {
		return Launcher{Name: "Chromium", Argv: []string{p}}, nil
	}
	if l, ok := systemDefault(); ok {
		return l, nil
	}
	return Launcher{}, errors.New("no browser found: install one, or name one in config.toml under [browser] command")
}

// command reads a configured command line ("firefox --new-window").
func command(line, from string) (Launcher, error) {
	f := strings.Fields(line)
	if len(f) == 0 {
		return Launcher{}, fmt.Errorf("%s is empty", from)
	}
	p, err := exec.LookPath(f[0])
	if err != nil {
		return Launcher{}, fmt.Errorf("%s: %s is not found", from, f[0])
	}
	return Launcher{Name: filepath.Base(f[0]), Argv: append([]string{p}, f[1:]...)}, nil
}

// systemDefault is the system's own way to open an address in the
// default browser.
func systemDefault() (Launcher, bool) {
	var argv []string
	switch runtime.GOOS {
	case "windows":
		argv = []string{"rundll32", "url.dll,FileProtocolHandler"}
	case "darwin":
		argv = []string{"open"}
	default:
		argv = []string{"xdg-open"}
	}
	p, err := exec.LookPath(argv[0])
	if err != nil {
		return Launcher{}, false
	}
	return Launcher{Name: "the default browser", Argv: append([]string{p}, argv[1:]...)}, true
}

// Binary finds Chromium or Chrome (Suwayomi's WebView page and the
// laptop's sign-ins use it when it is there).
func Binary() (string, error) {
	for _, c := range candidates {
		if p, err := exec.LookPath(c); err == nil {
			return p, nil
		}
	}
	var places []string
	switch runtime.GOOS {
	case "windows":
		for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)", "LocalAppData"} {
			if base := os.Getenv(env); base != "" {
				places = append(places, filepath.Join(base, `Chromium\Application\chrome.exe`), filepath.Join(base, `Google\Chrome\Application\chrome.exe`))
			}
		}
	case "darwin":
		places = []string{"/Applications/Chromium.app/Contents/MacOS/Chromium", "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"}
	}
	for _, p := range places {
		if fileExists(p) {
			return p, nil
		}
	}
	return "", errors.New("Chromium is not installed")
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// Name is the browser pages open in, for messages ("Chromium", "firefox",
// "the default browser").
func Name() string {
	if l, err := Choose(); err == nil {
		return l.Name
	}
	return "the browser"
}

// Open shows a web page in the browser, in its own window, without waiting.
func Open(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("only web pages open in the browser (%q is not one)", raw)
	}
	if NoDisplay() {
		return errors.New("no graphical display here: a browser needs a desktop (not SSH or the console)")
	}
	l, err := Choose()
	if err != nil {
		return err
	}
	cmd := exec.Command(l.Argv[0], append(l.Argv[1:], u.String())...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting %s: %v", l.Name, err)
	}
	go cmd.Wait() // reap it when it closes; W5F does not wait
	return nil
}

// NoDisplay reports a Unix without a graphical session (SSH, the console);
// Windows and macOS always have one.
func NoDisplay() bool {
	switch runtime.GOOS {
	case "windows", "darwin":
		return false
	}
	return os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == ""
}
