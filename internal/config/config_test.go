package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSolverURL(t *testing.T) {
	for _, env := range []string{"W5F_SOLVER_URL", "W5F_HERMETIC_SOLVER_URL"} {
		t.Setenv(env, "")
		os.Unsetenv(env)
	}
	data := t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)
	t.Setenv("APPDATA", data)
	os.MkdirAll(filepath.Dir(Path()), 0755)
	write := func(s string) { os.WriteFile(Path(), []byte(s), 0644) }
	for _, tc := range []struct{ file, want string }{
		{"", DefaultSolverURL}, // on by default, on every computer
		{"[discovery]\nhermetic_solver_url = \"http://127.0.0.1:8193\"\n", "http://127.0.0.1:8193"},
		{"[fetch]\nsolver_url = \"http://localhost:8192\"\n[discovery]\nhermetic_solver_url = \"http://127.0.0.1:8193\"\n", "http://localhost:8192"},
		{"[fetch]\nsolver_url = \"\"\n", ""}, // turned off
	} {
		write(tc.file)
		if got := SolverURL(); got != tc.want {
			t.Fatalf("%q: %q, want %q", tc.file, got, tc.want)
		}
	}
	write("")
	t.Setenv("W5F_HERMETIC_SOLVER_URL", "http://localhost:8194")
	if got := SolverURL(); got != "http://localhost:8194" {
		t.Fatal(got)
	}
	t.Setenv("W5F_SOLVER_URL", "")
	if got := SolverURL(); got != "" {
		t.Fatal("an empty W5F_SOLVER_URL did not turn the helper off:", got)
	}
}

func TestUserDirs(t *testing.T) {
	home := t.TempDir()
	p := filepath.Join(t.TempDir(), "user-dirs.dirs")
	for body, want := range map[string]string{
		"# comment\nXDG_DESKTOP_DIR=\"$HOME/Masaüstü\"\nXDG_DOWNLOAD_DIR=\"$HOME/İndirilenler\"\n": filepath.Join(home, "İndirilenler"),
		"XDG_DOWNLOAD_DIR=\"$HOME/Downloads\"\n":                                                   filepath.Join(home, "Downloads"),
		"XDG_DOWNLOAD_DIR=\"$HOME/\"\n":                                                            "",
		"XDG_DOWNLOAD_DIR=\"$HOME\"\n":                                                             "",
		"XDG_MUSIC_DIR=\"$HOME/Music\"\n":                                                          "",
	} {
		os.WriteFile(p, []byte(body), 0o644)
		if got := parseUserDirs(p, home); got != want {
			t.Errorf("%q → %q, want %q", body, got, want)
		}
	}
}

// What is not set keeps the laptop's folders; the environment wins over
// the file, the file over the default.
func TestFolders(t *testing.T) {
	data := t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)
	t.Setenv("APPDATA", data)
	t.Setenv("W5F_BOOKS", "")
	home, _ := os.UserHomeDir()
	if got := Folder("W5F_BOOKS", Load().Folders.Books, "Archive", "Books"); got != filepath.Join(home, "Archive", "Books") {
		t.Errorf("default: %q", got)
	}
	os.MkdirAll(filepath.Dir(Path()), 0o755)
	os.WriteFile(Path(), []byte("[folders]\nbooks = \"~/Kitaplar\"\ndownloads = \"/srv/in\"\n[browser]\ncommand = \"firefox --new-window\"\n"), 0o644)
	f := Load()
	if got := Folder("W5F_BOOKS", f.Folders.Books, "Archive", "Books"); got != filepath.Join(home, "Kitaplar") {
		t.Errorf("file: %q", got)
	}
	if f.Browser.Command != "firefox --new-window" {
		t.Errorf("browser: %q", f.Browser.Command)
	}
	t.Setenv("W5F_BOOKS", "/elsewhere")
	if got := Folder("W5F_BOOKS", f.Folders.Books, "Archive", "Books"); got != "/elsewhere" {
		t.Errorf("environment: %q", got)
	}
	t.Setenv("W5F_DOWNLOADS", "")
	if got := Downloads(); got != "/srv/in" {
		t.Errorf("downloads: %q", got)
	}
	// The older [fiction] downloads still counts.
	os.WriteFile(Path(), []byte("[fiction]\ndownloads = \"/old/place\"\n"), 0o644)
	if got := Downloads(); got != "/old/place" {
		t.Errorf("[fiction] downloads: %q", got)
	}
}
