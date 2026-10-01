package browser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The order: W5F_BROWSER, then [browser] command, then Chromium, then the
// system's default (the laptop has no setting and keeps Chromium).
func TestChooseOrder(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	old := configured
	t.Cleanup(func() { configured = old })

	configured = func() string { return "" }
	t.Setenv("W5F_BROWSER", self)
	if l, err := Choose(); err != nil || l.Argv[0] != self || l.Name != filepath.Base(self) {
		t.Errorf("W5F_BROWSER: %+v %v", l, err)
	}

	t.Setenv("W5F_BROWSER", "")
	configured = func() string { return self + " --new-window" }
	if l, err := Choose(); err != nil || len(l.Argv) != 2 || l.Argv[1] != "--new-window" {
		t.Errorf("config: %+v %v", l, err)
	}
	configured = func() string { return "no-such-browser-w5f" }
	if _, err := Choose(); err == nil || !strings.Contains(err.Error(), "config.toml") {
		t.Errorf("a missing configured browser is said plainly: %v", err)
	}

	// Nothing set: Chromium if installed, else the system's default.
	configured = func() string { return "" }
	if l, err := Choose(); err == nil && l.Name != "Chromium" && l.Name != "the default browser" {
		t.Errorf("default: %+v", l)
	}
}
