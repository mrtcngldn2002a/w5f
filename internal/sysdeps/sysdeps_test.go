package sysdeps

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHints(t *testing.T) {
	old, oldOS := osRelease, goos
	t.Cleanup(func() { osRelease, goos = old, oldOS })
	dir := t.TempDir()
	write := func(s string) {
		osRelease = filepath.Join(dir, "os-release")
		os.WriteFile(osRelease, []byte(s), 0o644)
	}
	goos = "linux"
	for release, want := range map[string]string{
		"ID=debian\n": "sudo apt install openjdk-21-jre-headless", // the W5F laptop (antiX reports debian)
		"ID=linuxmint\nID_LIKE=\"ubuntu debian\"\n": "sudo apt install openjdk-21-jre-headless",
		"ID=\"fedora\"\n":                "sudo dnf install java-21-openjdk-headless",
		"ID=endeavouros\nID_LIKE=arch\n": "sudo pacman -S jre21-openjdk-headless",
		"ID=nixos\n":                     "",
	} {
		write(release)
		if got := Hint(Java); got != want {
			t.Errorf("%q → %q, want %q", release, got, want)
		}
	}
	goos = "darwin"
	if got := Hint(Java); got != "brew install openjdk@21" {
		t.Errorf("macOS: %q", got)
	}
	goos = "windows"
	if got := With("Terminus not found", Terminus); got != "Terminus not found" {
		t.Errorf("no hint where there is none: %q", got)
	}
	if got := Hint(Java); !strings.HasPrefix(got, "winget install ") {
		t.Errorf("Windows: %q", got)
	}
}

func TestFindJava(t *testing.T) {
	self, _ := os.Executable()
	if p, err := FindJava(self); err != nil || p != self {
		t.Errorf("configured: %q %v", p, err)
	}
	if _, err := FindJava("/no/such/java"); err == nil || !strings.Contains(err.Error(), "config.toml") {
		t.Errorf("a missing configured java: %v", err)
	}
	home := t.TempDir()
	name := "java"
	if goos == "windows" {
		name = "java.exe"
	}
	os.MkdirAll(filepath.Join(home, "bin"), 0o755)
	os.WriteFile(filepath.Join(home, "bin", name), []byte("x"), 0o755)
	t.Setenv("JAVA_HOME", home)
	if p, err := FindJava(""); err != nil || p != filepath.Join(home, "bin", name) {
		t.Errorf("JAVA_HOME: %q %v", p, err)
	}
}
