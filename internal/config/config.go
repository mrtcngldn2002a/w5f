// Package config reads config.toml in the data folder: the settings a
// computer other than the W5F laptop may need (where its folders are,
// which browser and which Java to use). Every setting is optional; what is
// not set keeps the laptop's behaviour. An environment variable, where
// there is one, wins over the file.
package config

import (
	"bufio"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/BurntSushi/toml"

	"w5f/internal/store"
)

// File is config.toml as W5F reads it.
type File struct {
	Notes   string `toml:"notes"`
	Folders struct {
		Books     string `toml:"books"`
		Comics    string `toml:"comics"`
		Downloads string `toml:"downloads"`
	} `toml:"folders"`
	Browser struct {
		// Command opens a web page: a program and its arguments, the
		// address added last ("firefox", "open -a Safari").
		Command string `toml:"command"`
	} `toml:"browser"`
	Comics struct {
		Java string `toml:"java"` // the java program Suwayomi starts with
	} `toml:"comics"`
	Fiction struct {
		Mature    bool   `toml:"mature"`
		Downloads string `toml:"downloads"` // the older place of [folders] downloads
	} `toml:"fiction"`
	Update struct {
		Repo string `toml:"repo"`
	} `toml:"update"`
	Cache struct {
		LimitMB *int `toml:"limit_mb"` // W5F's page cache; unset: 500, 0: no limit
	} `toml:"cache"`
	Fetch struct {
		// SolverURL is the local bot-check helper (a FlareSolverr-style
		// /v1 API); unset: DefaultSolverURL, "": none.
		SolverURL *string `toml:"solver_url"`
	} `toml:"fetch"`
	Discovery struct {
		HermeticSolverURL string `toml:"hermetic_solver_url"` // the older place of [fetch] solver_url
	} `toml:"discovery"`
}

// DefaultSolverURL is where Byparr and FlareSolverr listen by default.
const DefaultSolverURL = "http://127.0.0.1:8191"

// SolverURL is the local helper W5F (and the Suwayomi it starts) asks when a
// site answers with a verification page: W5F_SOLVER_URL, the older
// W5F_HERMETIC_SOLVER_URL, [fetch] solver_url, [discovery]
// hermetic_solver_url, else DefaultSolverURL. An empty value turns it off.
func SolverURL() string {
	for _, env := range []string{"W5F_SOLVER_URL", "W5F_HERMETIC_SOLVER_URL"} {
		if value, set := os.LookupEnv(env); set {
			return strings.TrimSpace(value)
		}
	}
	f := Load()
	if f.Fetch.SolverURL != nil {
		return strings.TrimSpace(*f.Fetch.SolverURL)
	}
	if u := strings.TrimSpace(f.Discovery.HermeticSolverURL); u != "" {
		return u
	}
	return DefaultSolverURL
}

// CacheLimit is the most W5F's own page cache may hold, in bytes (0: no
// limit).
func CacheLimit() int64 {
	if p := Load().Cache.LimitMB; p != nil {
		return int64(max(*p, 0)) << 20
	}
	return 500 << 20
}

// Path is config.toml's place.
func Path() string { return filepath.Join(store.DataDir(), "config.toml") }

// Load reads config.toml; a missing or broken file gives the defaults
// (w5f doctor reports a broken one).
func Load() File {
	var f File
	_, _ = toml.DecodeFile(Path(), &f)
	return f
}

// Expand turns a leading ~ into the home folder.
func Expand(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, p[1:])
		}
	}
	return p
}

// Folder is a folder setting: the environment variable, else the file's
// value, else def under the home folder.
func Folder(env, file string, def ...string) string {
	if d := os.Getenv(env); d != "" {
		return d
	}
	if file != "" {
		return Expand(file)
	}
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Join(append([]string{h}, def...)...)
	}
	return filepath.Join(def...)
}

// Downloads is the browser's download folder: W5F_DOWNLOADS, [folders]
// downloads, [fiction] downloads, the desktop's own (XDG user-dirs on
// Linux, which a Turkish desktop names İndirilenler), else ~/Downloads.
func Downloads() string {
	f := Load()
	if d := os.Getenv("W5F_DOWNLOADS"); d != "" {
		return d
	}
	for _, d := range []string{f.Folders.Downloads, f.Fiction.Downloads} {
		if d != "" {
			return Expand(d)
		}
	}
	if d := xdgDownloads(); d != "" {
		return d
	}
	return Folder("", "", "Downloads")
}

// xdgDownloads reads XDG_DOWNLOAD_DIR from ~/.config/user-dirs.dirs.
func xdgDownloads() string {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	cfg := os.Getenv("XDG_CONFIG_HOME")
	if cfg == "" {
		cfg = filepath.Join(home, ".config")
	}
	return parseUserDirs(filepath.Join(cfg, "user-dirs.dirs"), home)
}

func parseUserDirs(path, home string) string {
	fh, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer fh.Close()
	sc := bufio.NewScanner(fh)
	for sc.Scan() {
		k, v, ok := strings.Cut(strings.TrimSpace(sc.Text()), "=")
		if !ok || k != "XDG_DOWNLOAD_DIR" {
			continue
		}
		v = strings.Trim(v, `"`)
		v = filepath.Clean(strings.Replace(v, "$HOME", home, 1))
		if v == filepath.Clean(home) || !filepath.IsAbs(v) {
			return "" // "$HOME" alone means the desktop has no such folder
		}
		return v
	}
	return ""
}
