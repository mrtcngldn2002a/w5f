package solver

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"w5f/internal/store"
)

func tarFixture(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if e := tw.WriteHeader(&tar.Header{Name: name, Mode: 0755, Size: int64(len(body))}); e != nil {
			t.Fatal(e)
		}
		tw.Write([]byte(body))
	}
	tw.Close()
	gz.Close()
	return b.Bytes()
}
func hashOf(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func fakeInstaller(t *testing.T) (Installer, *int) {
	t.Helper()
	uv := tarFixture(t, map[string]string{"uv-root/uv": "uv"})
	byparr := tarFixture(t, map[string]string{"byparr/main.py": "pass", "byparr/uv.lock": "locked", "byparr/pyproject.toml": "project"})
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path == "/uv" {
			w.Write(uv)
		} else {
			w.Write(byparr)
		}
	}))
	t.Cleanup(s.Close)
	// The data folder as W5F resolves it (cleanData): a Mac's temporary
	// folder is behind /var -> /private/var, a Windows runner's is a short
	// name (RUNNER~1), and the paths the tests compare must be the same.
	data, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	i := Installer{DataDir: data, GOOS: "linux", GOARCH: "amd64", UV: Artifact{s.URL + "/uv", hashOf(uv)}, Byparr: Artifact{s.URL + "/byparr", hashOf(byparr)}, Free: func(string) (uint64, error) { return MinFreeBytes + 1, nil }}
	i.Run = func(ctx context.Context, exe string, args, env []string, dir string, out io.Writer) error {
		if e := ctx.Err(); e != nil {
			return e
		}
		for _, p := range []string{Python(dir, "linux"), Python(dir, "windows"), filepath.Join(dir, "browser-cache", "firefox"), filepath.Join(dir, "browser-cache", "geoip", "database.mmdb")} {
			os.MkdirAll(filepath.Dir(p), 0700)
			os.WriteFile(p, []byte("fixture"), 0600)
		}
		if strings.Contains(strings.Join(args, " "), "cached_font_manifest_path(FONT_MANIFEST)") {
			p := filepath.Join(dir, "browser-cache", "fonts", "bundle-fonts-fixture.list")
			os.MkdirAll(filepath.Dir(p), 0700)
			os.WriteFile(p, []byte("fonts"), 0600)
		}
		for _, entry := range env {
			if strings.HasPrefix(entry, "UV_CACHE_DIR=") && strings.Contains(entry, ".cache/w5f") {
				t.Fatal("protected cache used")
			}
		}
		return nil
	}
	return i, &calls
}
func TestInstallHashMismatchStopsAndCleans(t *testing.T) {
	i, calls := fakeInstaller(t)
	i.UV.SHA256 = strings.Repeat("0", 64)
	_, e := i.Install(context.Background())
	if e == nil || !strings.Contains(e.Error(), "SHA-256") {
		t.Fatal(e)
	}
	if *calls != 1 {
		t.Fatal(*calls)
	}
	entries, _ := os.ReadDir(i.DataDir)
	if len(entries) != 0 {
		t.Fatal("partial install retained", entries)
	}
}

func TestByparrHashMismatchAlsoCleansPython(t *testing.T) {
	i, calls := fakeInstaller(t)
	i.Byparr.SHA256 = strings.Repeat("0", 64)
	if _, e := i.Install(context.Background()); e == nil || !strings.Contains(e.Error(), "SHA-256") {
		t.Fatal(e)
	}
	if *calls != 2 {
		t.Fatal(*calls)
	}
	entries, _ := os.ReadDir(i.DataDir)
	if len(entries) != 0 {
		t.Fatal("Python or downloads retained after source checksum failure")
	}
}
func TestRemovalRejectsEscapingListBeforeDeletingAnyFile(t *testing.T) {
	i, _ := fakeInstaller(t)
	dir, e := i.Install(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	m, e := readManifest(dir)
	if e != nil {
		t.Fatal(e)
	}
	m.Files = append(m.Files, "../keep.txt")
	outside := filepath.Join(i.DataDir, "keep.txt")
	os.WriteFile(outside, []byte("keep"), 0600)
	b, _ := json.Marshal(m)
	os.WriteFile(filepath.Join(dir, manifestName), b, 0600)
	if e = removeListed(dir); e == nil {
		t.Fatal("escaping list accepted")
	}
	if _, e = os.Stat(filepath.Join(dir, "main.py")); e != nil {
		t.Fatal("partial removal before validating entire list")
	}
	if b, e = os.ReadFile(outside); e != nil || string(b) != "keep" {
		t.Fatal("outside file touched")
	}
}
func TestArchiveLinksCannotEscape(t *testing.T) {
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Name: "root/link", Typeflag: tar.TypeSymlink, Linkname: "../../outside", Mode: 0777})
	tw.Close()
	gz.Close()
	archive := filepath.Join(t.TempDir(), "bad.tar.gz")
	os.WriteFile(archive, b.Bytes(), 0600)
	if e := extract(archive, t.TempDir(), false, true); e == nil {
		t.Fatal("escaping link accepted")
	}
}
func TestShutdownCancelsAndWaitsForInstallCleanup(t *testing.T) {
	i, _ := fakeInstaller(t)
	entered := make(chan struct{})
	done := make(chan error, 1)
	i.Run = func(ctx context.Context, _ string, _ []string, _ []string, _ string, _ io.Writer) error {
		close(entered)
		<-ctx.Done()
		return ctx.Err()
	}
	go func() { _, e := i.Install(context.Background()); done <- e }()
	<-entered
	Shutdown()
	if e := <-done; !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	entries, _ := os.ReadDir(i.DataDir)
	if len(entries) != 0 {
		t.Fatal("shutdown returned before cleanup")
	}
}
func TestInsufficientDiskDoesNotDownload(t *testing.T) {
	i, calls := fakeInstaller(t)
	i.Free = func(string) (uint64, error) { return MinFreeBytes - 1, nil }
	_, e := i.Install(context.Background())
	if e == nil || *calls != 0 {
		t.Fatal(e, *calls)
	}
}
func TestCancelledInstallCleansOwnedFiles(t *testing.T) {
	i, _ := fakeInstaller(t)
	ctx, cancel := context.WithCancel(context.Background())
	i.Run = func(context.Context, string, []string, []string, string, io.Writer) error { cancel(); return ctx.Err() }
	_, e := i.Install(ctx)
	if !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	entries, _ := os.ReadDir(i.DataDir)
	if len(entries) != 0 {
		t.Fatal("partial install retained")
	}
}
func TestManifestRemovalKeepsUnlistedAndManualFiles(t *testing.T) {
	i, _ := fakeInstaller(t)
	dir, e := i.Install(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	extra := filepath.Join(dir, "user-note.txt")
	os.WriteFile(extra, []byte("keep"), 0600)
	if e = removeListed(dir); e == nil {
		t.Fatal("unlisted file was not reported")
	}
	if b, e := os.ReadFile(extra); e != nil || string(b) != "keep" {
		t.Fatal("unlisted file removed")
	}
	if _, e = os.Stat(filepath.Join(dir, manifestName)); e != nil {
		t.Fatal("list lost")
	}
	manual := filepath.Join(i.DataDir, "byparr-v0.1.0")
	os.Mkdir(manual, 0700)
	os.WriteFile(filepath.Join(manual, "main.py"), []byte("keep"), 0600)
	if e = removeListed(manual); e == nil {
		t.Fatal("manual install removable")
	}
}
func TestCleanManifestRemoval(t *testing.T) {
	i, _ := fakeInstaller(t)
	dir, e := i.Install(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	if b, e := os.ReadFile(filepath.Join(dir, "browser-cache", "fonts", "bundle-fonts-fixture.list")); e != nil || string(b) != "fonts" {
		t.Fatal("runtime font list was not prepared before recording the installation", e)
	}
	if e = removeListed(dir); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(dir); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("installation retained")
	}
}
func TestDecliningOnlySuppressesQuestion(t *testing.T) {
	i, calls := fakeInstaller(t)
	db, e := store.Open(":memory:")
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	m := &Manager{DataDir: i.DataDir, URL: "http://127.0.0.1:1"}
	if !ShouldAsk(context.Background(), db, m, "linux", "amd64") {
		t.Fatal("first question missing")
	}
	if e = Answer(db, false); e != nil {
		t.Fatal(e)
	}
	if ShouldAsk(context.Background(), db, m, "linux", "amd64") {
		t.Fatal("decline not kept")
	}
	if _, e = i.Install(context.Background()); e != nil || *calls != 2 {
		t.Fatal("decline blocked explicit install", e)
	}
	m.DataDir = t.TempDir()
	if e = AskAgain(db); e != nil {
		t.Fatal(e)
	}
	if !ShouldAsk(context.Background(), db, m, "linux", "amd64") {
		t.Fatal("ask again did not reset")
	}
}
func TestArchiveTraversalRejected(t *testing.T) {
	for _, name := range []string{"root/../../escape", "/absolute", "root/C:/escape"} {
		archive := filepath.Join(t.TempDir(), "bad.tar.gz")
		os.WriteFile(archive, tarFixture(t, map[string]string{name: "bad"}), 0600)
		if e := extract(archive, t.TempDir(), false, true); e == nil {
			t.Fatal("unsafe archive accepted", name)
		}
	}
}
func TestManualInstallationAdoptedWithoutDownloads(t *testing.T) {
	i, calls := fakeInstaller(t)
	dir := Dir(i.DataDir, Version)
	for _, p := range []string{filepath.Join(dir, "main.py"), filepath.Join(dir, "uv.lock"), Python(dir, "linux"), Python(dir, "windows")} {
		os.MkdirAll(filepath.Dir(p), 0700)
		os.WriteFile(p, []byte("manual"), 0600)
	}
	if _, e := i.Install(context.Background()); e != nil || *calls != 0 {
		t.Fatal(e, *calls)
	}
	if _, e := readManifest(dir); e == nil {
		t.Fatal("manual install claimed by installer")
	}
}
func TestExternalHelperIsNeverStopped(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/openapi.json" {
			io.WriteString(w, `{"paths":{"/v1":{}}}`)
		} else {
			http.NotFound(w, r)
		}
	}))
	defer s.Close()
	m := &Manager{DataDir: t.TempDir(), URL: s.URL}
	started, e := m.Start(context.Background(), m.URL)
	if e != nil || started {
		t.Fatal(started, e)
	}
	m.Shutdown()
	if e = m.Stop(); e != nil {
		t.Fatal(e)
	}
	resp, e := http.Get(s.URL + "/openapi.json")
	if e != nil {
		t.Fatal("external helper stopped", e)
	}
	resp.Body.Close()
}
func TestUnknownServiceReceivesNoSolverRequest(t *testing.T) {
	posts := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			posts++
		}
		io.WriteString(w, `{"hello":"other service"}`)
	}))
	defer s.Close()
	m := &Manager{DataDir: t.TempDir(), URL: s.URL}
	if _, e := m.Start(context.Background(), m.URL); e == nil {
		t.Fatal("unknown service accepted")
	}
	if posts != 0 {
		t.Fatal("unknown service got a page")
	}
}
func TestFirstQuestionAndRemovalDefaultToNo(t *testing.T) {
	db, _ := store.Open(":memory:")
	defer db.Close()
	m := &Manager{DataDir: t.TempDir(), URL: "http://127.0.0.1:1"}
	for _, target := range []string{Target + "/first", Target + "/remove"} {
		d, e := Route(context.Background(), target, db, m)
		if e != nil {
			t.Fatal(e)
		}
		if len(d.Links) < 2 || !strings.HasPrefix(d.Links[0].Text, "No") {
			t.Fatal("confirmation does not default to no")
		}
	}
}

// Byparr installs on Linux, Windows and a Mac (Apple silicon and Intel),
// each with its own pinned uv.
func TestPlatformsAndTheirUV(t *testing.T) {
	for _, p := range []struct {
		goos, goarch, uv string
		ok               bool
	}{
		{"linux", "amd64", "uv-x86_64-unknown-linux-gnu.tar.gz", true},
		{"windows", "amd64", "uv-x86_64-pc-windows-msvc.zip", true},
		{"darwin", "arm64", "uv-aarch64-apple-darwin.tar.gz", true},
		{"darwin", "amd64", "uv-x86_64-apple-darwin.tar.gz", true},
		{"linux", "386", "", false},
		{"linux", "arm64", "", false},
		{"freebsd", "amd64", "", false},
	} {
		if err := Supported(p.goos, p.goarch); (err == nil) != p.ok {
			t.Errorf("%s/%s supported: %v", p.goos, p.goarch, err)
		}
		if !p.ok {
			continue
		}
		uv := Installer{GOOS: p.goos, GOARCH: p.goarch}.defaults().UV
		if !strings.HasSuffix(uv.URL, "/"+UVVersion+"/"+p.uv) || len(uv.SHA256) != 64 {
			t.Errorf("%s/%s uv: %+v", p.goos, p.goarch, uv)
		}
	}
	old := DarwinEnabled
	DarwinEnabled = "no"
	defer func() { DarwinEnabled = old }()
	if Supported("darwin", "arm64") == nil {
		t.Error("DarwinEnabled = no still offers Byparr on a Mac")
	}
}

// A Mac installs the same way: verified uv, Python, Byparr, its browser.
func TestMacInstall(t *testing.T) {
	i, _ := fakeInstaller(t)
	i.GOOS, i.GOARCH = "darwin", "arm64"
	dir, e := i.Install(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(filepath.Join(dir, "tools", "uv")); e != nil {
		t.Errorf("uv: %v", e)
	}
	if !present(dir) {
		t.Error("the installation is not complete")
	}
}
