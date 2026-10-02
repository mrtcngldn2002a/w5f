package doctor

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func baseEnv(t *testing.T) Env {
	data := t.TempDir()
	return Env{Version: "0.7.0", Exe: filepath.Join(t.TempDir(), "w5f"), DataDir: data, CacheDir: t.TempDir(),
		NotesDir: filepath.Join(data, "notes-not-yet"), DictDir: filepath.Join(data, "dict"), Repo: "me/w5f", HasKey: true,
		GOOS: "linux", Getenv: func(k string) string {
			return map[string]string{"LANG": "en_US.UTF-8", "TERM": "xterm-256color"}[k]
		},
		TermSize: func() (int, int, error) { return 100, 30, nil },
		FontList: func() (string, error) {
			return "/usr/share/fonts/X11/misc/ter-u16n.pcf.gz: Terminus:style=Regular", nil
		}}
}

func find(rs []Result, name string) Result {
	for _, r := range rs {
		if r.Name == name {
			return r
		}
	}
	return Result{Status: -1, Name: "(missing " + name + ")"}
}

func TestHealthyInstall(t *testing.T) {
	e := baseEnv(t)
	os.MkdirAll(e.DictDir, 0o755)
	os.WriteFile(filepath.Join(e.DictDir, "en-tr.ifo"), []byte("x"), 0o644)
	db, _ := sql.Open("sqlite", filepath.Join(e.DataDir, "w5f.db"))
	db.Exec("CREATE TABLE items(x); CREATE TABLE books(x); INSERT INTO books VALUES (1)")
	db.Close()
	before, _ := os.ReadDir(e.DataDir)
	rs := Check(e)
	for _, r := range rs {
		if r.Status != OK {
			t.Errorf("%s: %s %s", r.Name, r.Status, r.Detail)
		}
	}
	if d := find(rs, "database").Detail; !strings.Contains(d, "1 books") {
		t.Errorf("database: %s", d)
	}
	after, _ := os.ReadDir(e.DataDir)
	if len(after) != len(before) {
		t.Errorf("doctor wrote into the data folder: %d → %d entries", len(before), len(after))
	}
}

func TestProblemsAreReported(t *testing.T) {
	e := baseEnv(t)
	os.WriteFile(filepath.Join(e.DataDir, "config.toml"), []byte("[update\nrepo = 1"), 0o644)
	os.WriteFile(filepath.Join(e.DataDir, "w5f.db"), []byte("this is not a database, just text that is long enough"), 0o644)
	e.Getenv = func(k string) string { return map[string]string{"LANG": "C"}[k] }
	e.TermSize = func() (int, int, error) { return 70, 20, nil }
	e.FontList = func() (string, error) { return "", errors.New("no fc-list") }
	e.HasKey = false
	notDir := filepath.Join(t.TempDir(), "file")
	os.WriteFile(notDir, nil, 0o644)
	e.CacheDir = notDir
	e.ComicsDir = t.TempDir()
	e.SuwayomiJar = "/x/Suwayomi-Server-v2.jar"
	e.JavaPath = func() (string, error) { return "", errors.New("not found") }
	rs := Check(e)
	want := map[string]Status{"suwayomi": Warn, "viewer": Warn, "comics folder": OK, "config": Fail, "database": Fail, "locale": Fail, "terminal": Warn, "font": Warn,
		"dictionary": Warn, "update": Warn, "cache folder": Fail, "notes folder": OK}
	for name, s := range want {
		if r := find(rs, name); r.Status != s {
			t.Errorf("%s: %v %q, want %v", name, r.Status, r.Detail, s)
		}
	}
	var b strings.Builder
	if n := Print(&b, rs); n != 4 {
		t.Errorf("fails: %d\n%s", n, b.String())
	}
}

func TestBench(t *testing.T) {
	rs := Bench(2000)
	if len(rs) != 2 || !strings.Contains(rs[0].Detail, "2000-word page") {
		t.Errorf("bench: %+v", rs)
	}
}

func TestSolverCheck(t *testing.T) {
	probe := func(name string, err error) func(string) (string, error) {
		return func(string) (string, error) { return name, err }
	}
	for _, tc := range []struct {
		e      Env
		status Status
		want   string
	}{
		{Env{Solver: "", ProbeSolver: probe("", nil)}, OK, "off"},
		{Env{Solver: "http://127.0.0.1:8191", ProbeSolver: probe("Byparr", nil)}, OK, "Byparr answers at http://127.0.0.1:8191"},
		{Env{Solver: "http://127.0.0.1:8191", ProbeSolver: probe("", errors.New("no bot-check helper is running"))}, Warn, "w5f solver install"},
	} {
		r, ok := solverCheck(tc.e)
		if !ok || r.Status != tc.status || !strings.Contains(r.Detail, tc.want) {
			t.Fatalf("%+v", r)
		}
	}
	if _, ok := solverCheck(Env{}); ok {
		t.Fatal("reported without a probe")
	}
}
