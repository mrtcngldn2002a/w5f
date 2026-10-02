package solver

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"w5f/internal/fetch"
)

func TestMain(m *testing.M) {
	if os.Getenv("W5F_TEST_BYParr_CHILD") == "yes" {
		mux := http.NewServeMux()
		mux.HandleFunc("/openapi.json", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{"paths":{"/v1":{}}}`) })
		if e := http.ListenAndServe("127.0.0.1:"+os.Getenv("PORT"), mux); e != nil {
			os.Exit(1)
		}
		return
	}
	os.Exit(m.Run())
}
func fakeProcessInstallation(t *testing.T, data, version string) string {
	t.Helper()
	dir := Dir(data, version)
	python := Python(dir, runtime.GOOS)
	os.MkdirAll(filepath.Dir(python), 0700)
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	src, e := os.Open(exe)
	if e != nil {
		t.Fatal(e)
	}
	defer src.Close()
	dst, e := os.OpenFile(python, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if e != nil {
		t.Fatal(e)
	}
	_, e = io.Copy(dst, src)
	dst.Close()
	if e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"main.py", "uv.lock"} {
		os.WriteFile(filepath.Join(dir, name), []byte("test fixture"), 0600)
	}
	os.MkdirAll(filepath.Join(data, "cloudflare-helper", "bin"), 0700)
	os.WriteFile(filepath.Join(data, "cloudflare-helper", "bin", "Xvfb"), []byte("fake browser does not need X"), 0600)
	return dir
}
func freeTestURL(t *testing.T) string {
	t.Helper()
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	u := "http://" + l.Addr().String()
	l.Close()
	return u
}
func TestOnlyStartingInstanceShutsItsProcessDown(t *testing.T) {
	t.Setenv("W5F_TEST_BYParr_CHILD", "yes")
	data := t.TempDir()
	fakeProcessInstallation(t, data, Version)
	m := &Manager{DataDir: data, URL: freeTestURL(t)}
	started, e := m.Start(context.Background(), m.URL)
	if e != nil || !started {
		t.Fatal(started, e)
	}
	t.Cleanup(m.Shutdown)
	other := &Manager{DataDir: data, URL: m.URL}
	started, e = other.Start(context.Background(), other.URL)
	if e != nil || started {
		t.Fatal("second instance launched a duplicate", started, e)
	}
	other.Shutdown()
	if _, e = fetch.ProbeSolver(context.Background(), m.URL); e != nil {
		t.Fatal("non-owner shutdown stopped helper", e)
	}
	m.Shutdown()
	if _, e = fetch.ProbeSolver(context.Background(), m.URL); !fetch.NoSolver(e) {
		t.Fatal("owner shutdown left helper running", e)
	}
}
func TestStaleProcessRecordNeverKillsReusedPID(t *testing.T) {
	m := &Manager{DataDir: t.TempDir(), URL: freeTestURL(t)}
	os.MkdirAll(filepath.Dir(m.recordPath(m.URL)), 0700)
	b, _ := json.Marshal(processRecord{PID: os.Getpid(), Stamp: "stale identity", Dir: Dir(m.DataDir, Version)})
	os.WriteFile(m.recordPath(m.URL), b, 0600)
	if e := m.Stop(); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(m.recordPath(m.URL)); !os.IsNotExist(e) {
		t.Fatal("stale record retained")
	}
}

func TestFailedCandidateKeepsPreviousVersion(t *testing.T) {
	i, _ := fakeInstaller(t)
	old := fakeProcessInstallation(t, i.DataDir, "3.0.3")
	m := &Manager{DataDir: i.DataDir, URL: freeTestURL(t)}
	if e := m.Update(context.Background(), &i); e == nil {
		t.Fatal("broken candidate was accepted")
	}
	if m.active() != old {
		t.Fatal("failed probe changed active version", m.active())
	}
}
func TestSuccessfulUpdateKeepsOnePreviousOwnedVersion(t *testing.T) {
	t.Setenv("W5F_TEST_BYParr_CHILD", "yes")
	i, _ := fakeInstaller(t)
	old := fakeProcessInstallation(t, i.DataDir, "3.0.3")
	older := Dir(i.DataDir, "3.0.2")
	os.MkdirAll(older, 0700)
	os.WriteFile(filepath.Join(older, "main.py"), []byte("older owned version"), 0600)
	if e := writeManifest(older, "3.0.2"); e != nil {
		t.Fatal(e)
	}
	manual := Dir(i.DataDir, "3.0.1")
	os.MkdirAll(manual, 0700)
	os.WriteFile(filepath.Join(manual, "main.py"), []byte("manual stays"), 0600)
	baseRun := i.Run
	i.Run = func(ctx context.Context, exe string, args, env []string, dir string, out io.Writer) error {
		if e := baseRun(ctx, exe, args, env, dir, out); e != nil {
			return e
		}
		if e := os.Remove(Python(dir, runtime.GOOS)); e != nil {
			return e
		}
		src, e := os.Open(os.Args[0])
		if e != nil {
			return e
		}
		dst, e := os.OpenFile(Python(dir, runtime.GOOS), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
		if e != nil {
			src.Close()
			return e
		}
		_, e = io.Copy(dst, src)
		src.Close()
		dst.Close()
		return e
	}
	m := &Manager{DataDir: i.DataDir, URL: freeTestURL(t)}
	if e := m.Update(context.Background(), &i); e != nil {
		t.Fatal(e)
	}
	if m.active() != Dir(i.DataDir, Version) {
		t.Fatal("candidate not selected")
	}
	if _, e := os.Stat(old); e != nil {
		t.Fatal("previous version lost")
	}
	if _, e := os.Stat(older); !os.IsNotExist(e) {
		t.Fatal("older owned version not pruned")
	}
	if _, e := os.Stat(manual); e != nil {
		t.Fatal("manual version removed")
	}
}
