package theme

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// The console palettes in platform/antix are written by hand from Console();
// this test keeps them in step with the theme.
var kmsconNames = []string{"black", "red", "green", "yellow", "blue", "magenta", "cyan", "light-grey",
	"dark-grey", "light-red", "light-green", "light-yellow", "light-blue", "light-magenta", "light-cyan", "white"}

func expectedKmscon() []string {
	c := AmberP3.Console()
	var lines []string
	for i, n := range kmsconNames {
		r, g, b := RGB(c[i])
		lines = append(lines, fmt.Sprintf("palette-%s=%d,%d,%d", n, r, g, b))
	}
	r, g, b := RGB(AmberP3.FG)
	lines = append(lines, fmt.Sprintf("palette-foreground=%d,%d,%d", r, g, b))
	r, g, b = RGB(AmberP3.BG)
	return append(lines, fmt.Sprintf("palette-background=%d,%d,%d", r, g, b))
}

func expectedXresources() []string {
	c := AmberP3.Console()
	lines := []string{"XTerm*foreground: " + Hex(AmberP3.FG), "XTerm*background: " + Hex(AmberP3.BG), "XTerm*cursorColor: " + Hex(AmberP3.Accent)}
	for i := range c {
		lines = append(lines, fmt.Sprintf("XTerm*color%d: %s", i, Hex(c[i])))
	}
	return lines
}

func TestPlatformPalettesMatchTheTheme(t *testing.T) {
	for file, want := range map[string][]string{
		"../../platform/antix/kmscon.conf": expectedKmscon(),
		"../../platform/antix/Xresources":  expectedXresources(),
	} {
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range want {
			if !strings.Contains(string(b), line+"\n") {
				t.Errorf("%s lacks %q; the palette lines should be:\n%s", file, line, strings.Join(want, "\n"))
				break
			}
		}
	}
}
