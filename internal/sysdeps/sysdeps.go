// Package sysdeps knows the programs W5F can use from the system (Java,
// fonts, FanFicFare, Poppler): how to find them and how each system
// installs them, so messages fit the computer W5F runs on rather than only
// the W5F laptop's Debian (2026-10-02).
package sysdeps

import (
	"bufio"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Pkg names a program in each packaging system; "" where it has none.
type Pkg struct{ Apt, Dnf, Pacman, Brew, Winget string }

// The programs W5F mentions.
var (
	Java       = Pkg{"openjdk-21-jre-headless", "java-21-openjdk-headless", "jre21-openjdk-headless", "openjdk@21", "EclipseAdoptium.Temurin.21.JRE"}
	Terminus   = Pkg{"fonts-terminus xfonts-terminus", "terminus-fonts terminus-fonts-console", "terminus-font", "", ""}
	Fontconfig = Pkg{"fontconfig", "fontconfig", "fontconfig", "", ""}
	Pipx       = Pkg{"pipx", "pipx", "python-pipx", "pipx", ""}
	Poppler    = Pkg{"poppler-utils", "poppler-utils", "poppler", "poppler", "oschwartz10612.Poppler"}
)

// osRelease is read to tell Linux distributions apart (a variable for tests).
var (
	osRelease = "/etc/os-release"
	goos      = runtime.GOOS
)

// Manager is the system's package manager: apt, dnf, pacman, brew,
// winget, or "" when not known.
func Manager() string {
	switch goos {
	case "windows":
		return "winget"
	case "darwin":
		return "brew"
	case "linux":
	default:
		return ""
	}
	ids := releaseIDs()
	for _, id := range ids {
		switch id {
		case "debian", "ubuntu", "linuxmint", "pop", "raspbian", "devuan", "antix", "mx":
			return "apt"
		case "fedora", "rhel", "centos", "rocky", "almalinux":
			return "dnf"
		case "arch", "manjaro", "endeavouros":
			return "pacman"
		}
	}
	return ""
}

// releaseIDs are ID then ID_LIKE's words from os-release.
func releaseIDs() []string {
	fh, err := os.Open(osRelease)
	if err != nil {
		return nil
	}
	defer fh.Close()
	var id, like []string
	sc := bufio.NewScanner(fh)
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), "=")
		if !ok {
			continue
		}
		v = strings.ToLower(strings.Trim(v, `"'`))
		switch k {
		case "ID":
			id = []string{v}
		case "ID_LIKE":
			like = strings.Fields(v)
		}
	}
	return append(id, like...)
}

// Hint is how this system installs p ("sudo apt install …"), or "" when
// W5F does not know.
func Hint(p Pkg) string {
	switch m := Manager(); {
	case m == "apt" && p.Apt != "":
		return "sudo apt install " + p.Apt
	case m == "dnf" && p.Dnf != "":
		return "sudo dnf install " + p.Dnf
	case m == "pacman" && p.Pacman != "":
		return "sudo pacman -S " + p.Pacman
	case m == "brew" && p.Brew != "":
		return "brew install " + p.Brew
	case m == "winget" && p.Winget != "":
		return "winget install " + p.Winget
	}
	return ""
}

// With adds the hint to a message: "Java is missing (sudo apt install …)".
func With(msg string, p Pkg) string {
	if h := Hint(p); h != "" {
		return msg + " (" + h + ")"
	}
	return msg
}

// FindJava finds the java program: the configured one ([comics] java in
// config.toml), else JAVA_HOME's, else java on the PATH.
func FindJava(configured string) (string, error) {
	if configured != "" {
		if p, err := exec.LookPath(configured); err == nil {
			return p, nil
		}
		return "", errors.New("[comics] java in config.toml is not found: " + configured)
	}
	if home := os.Getenv("JAVA_HOME"); home != "" {
		name := "java"
		if goos == "windows" {
			name = "java.exe"
		}
		if p := filepath.Join(home, "bin", name); isFile(p) {
			return p, nil
		}
	}
	if p, err := exec.LookPath("java"); err == nil {
		return p, nil
	}
	return "", errors.New(With("Java is not installed: Suwayomi needs Java 21", Java))
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}
