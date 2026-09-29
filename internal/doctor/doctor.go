// Package doctor checks a W5F install and says what is wrong with it. It
// only reads: the database is opened read-only, and writability is probed
// with a temporary file that is removed at once.
package doctor

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	_ "modernc.org/sqlite" // read-only look at the database
)

// Status of one check.
type Status int

const (
	OK Status = iota
	Warn
	Fail
)

func (s Status) String() string { return [...]string{"ok", "warn", "fail"}[s] }

// Result is one line of the report.
type Result struct {
	Status Status
	Name   string
	Detail string
}

// Env is what the checks look at (tests fill it with fakes).
type Env struct {
	Version  string
	Exe      string
	DataDir  string
	CacheDir string
	NotesDir string
	DictDir  string
	Repo     string // update source ("" = not set)
	HasKey   bool   // a release key is built in
	GOOS     string
	Getenv   func(string) string
	TermSize func() (w, h int, err error)
	FontList func() (string, error) // fc-list output (Linux)
}

// Check runs the offline checks.
func Check(e Env) []Result {
	var out []Result
	add := func(s Status, name, format string, a ...any) {
		out = append(out, Result{s, name, fmt.Sprintf(format, a...)})
	}
	add(OK, "version", "w5f %s at %s", e.Version, e.Exe)

	for _, d := range []struct{ name, path string }{{"data folder", e.DataDir}, {"cache folder", e.CacheDir}, {"notes folder", e.NotesDir}} {
		s, detail := dirState(d.path)
		add(s, d.name, "%s", detail)
	}

	cfg := filepath.Join(e.DataDir, "config.toml")
	if _, err := os.Stat(cfg); err != nil {
		add(OK, "config", "no config.toml (defaults are used)")
	} else {
		var v map[string]any
		if _, err := toml.DecodeFile(cfg, &v); err != nil {
			add(Fail, "config", "config.toml does not parse: %v", err)
		} else {
			add(OK, "config", "%s parses", cfg)
		}
	}

	out = append(out, database(filepath.Join(e.DataDir, "w5f.db")))

	if m, _ := filepath.Glob(filepath.Join(e.DictDir, "*.ifo")); len(m) > 0 {
		add(OK, "dictionary", "installed (%s)", filepath.Base(m[0]))
	} else {
		add(Warn, "dictionary", "not installed: g → dict-install (or w5f dict-install)")
	}

	out = append(out, terminal(e)...)
	if e.GOOS == "linux" {
		out = append(out, font(e))
	}

	switch {
	case !e.HasKey:
		add(Warn, "update", "this build has no release key (a development build): w5f update is off")
	case !strings.Contains(e.Repo, "/"):
		add(Warn, "update", `no release repo: add [update] repo = "owner/w5f" to config.toml`)
	default:
		if s, detail := dirState(filepath.Dir(e.Exe)); s != OK {
			add(Warn, "update", "the binary's folder is not writable, so w5f update cannot replace it (%s)", detail)
		} else {
			add(OK, "update", "releases from github.com/%s, signed", e.Repo)
		}
	}
	return out
}

// dirState: missing folders are fine (created on first use); existing ones
// must take a file.
func dirState(path string) (Status, string) {
	if path == "" {
		return Warn, "unknown location"
	}
	st, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return OK, path + " (created on first use)"
	}
	if err != nil {
		return Fail, err.Error()
	}
	if !st.IsDir() {
		return Fail, path + " is not a folder"
	}
	f, err := os.CreateTemp(path, ".w5f-doctor-*")
	if err != nil {
		return Fail, path + " is not writable"
	}
	f.Close()
	os.Remove(f.Name())
	return OK, path
}

func database(path string) Result {
	if _, err := os.Stat(path); err != nil {
		return Result{OK, "database", "not created yet (the first page you open makes it)"}
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?mode=ro")
	if err != nil {
		return Result{Fail, "database", err.Error()}
	}
	defer db.Close()
	var check string
	if err := db.QueryRow("PRAGMA quick_check").Scan(&check); err != nil || check != "ok" {
		if err != nil {
			check = err.Error()
		}
		return Result{Fail, "database", "damaged: " + check + " (w5f reindex rebuilds it from your files)"}
	}
	counts := []string{}
	for _, t := range []string{"items", "books", "serials"} {
		var n int
		if db.QueryRow("SELECT count(*) FROM "+t).Scan(&n) == nil {
			counts = append(counts, fmt.Sprintf("%d %s", n, t))
		}
	}
	if _, err := db.Exec("CREATE VIRTUAL TABLE temp.w5f_doctor USING fts5(x)"); err != nil {
		return Result{Fail, "database", "full-text search (FTS5) is missing: " + err.Error()}
	}
	return Result{OK, "database", "healthy, search ready · " + strings.Join(counts, ", ")}
}

func terminal(e Env) []Result {
	var out []Result
	if e.TermSize != nil {
		if w, h, err := e.TermSize(); err != nil {
			out = append(out, Result{Warn, "terminal", "not a terminal (size unknown)"})
		} else if w < 80 || h < 24 {
			out = append(out, Result{Warn, "terminal", fmt.Sprintf("%d×%d: smaller than 80×24, pages will be cramped", w, h)})
		} else {
			out = append(out, Result{OK, "terminal", fmt.Sprintf("%d×%d", w, h)})
		}
	}
	colors := "16 colours"
	if ct := e.Getenv("COLORTERM"); ct == "truecolor" || ct == "24bit" {
		colors = "true colour"
	} else if strings.Contains(e.Getenv("TERM"), "256") {
		colors = "256 colours"
	}
	out = append(out, Result{OK, "colours", colors + " (TERM=" + e.Getenv("TERM") + ")"})
	if e.GOOS == "linux" {
		loc := e.Getenv("LC_ALL")
		if loc == "" {
			loc = e.Getenv("LC_CTYPE")
		}
		if loc == "" {
			loc = e.Getenv("LANG")
		}
		if l := strings.ToLower(loc); strings.Contains(l, "utf-8") || strings.Contains(l, "utf8") {
			out = append(out, Result{OK, "locale", loc})
		} else {
			out = append(out, Result{Fail, "locale", fmt.Sprintf("%q is not UTF-8: Turkish letters and box lines will break (set LANG=en_US.UTF-8)", loc)})
		}
	}
	return out
}

// TestLine is the aesthetics plan's check: every glyph one cell wide.
const TestLine = "ğ Ğ ı İ ş Ş ç Ç ö Ö ü Ü  ─│┌┐└┘├┤"

func font(e Env) Result {
	if e.FontList == nil {
		return Result{Warn, "font", "cannot list fonts"}
	}
	list, err := e.FontList()
	if err != nil {
		return Result{Warn, "font", "fc-list is not available (apt install fontconfig); check by eye: " + TestLine}
	}
	if strings.Contains(strings.ToLower(list), "terminus") {
		return Result{OK, "font", "Terminus installed · check by eye: " + TestLine}
	}
	return Result{Warn, "font", "Terminus not found (apt install fonts-terminus xfonts-terminus) · check by eye: " + TestLine}
}

// FcList runs fc-list (Linux).
func FcList() (string, error) {
	out, err := exec.Command("fc-list").Output()
	return string(out), err
}

// Print writes the report and returns how many checks failed.
func Print(w io.Writer, rs []Result) int {
	fails := 0
	for _, r := range rs {
		if r.Status == Fail {
			fails++
		}
		fmt.Fprintf(w, "  %-5s %-13s %s\n", r.Status, r.Name, r.Detail)
	}
	return fails
}
