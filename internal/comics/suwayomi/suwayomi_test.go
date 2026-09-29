package suwayomi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The test binary doubles as "java": with W5F_FAKE_JAVA it serves a tiny
// Suwayomi on the port given in the -D arguments until it is stopped.
func TestMain(m *testing.M) {
	if os.Getenv("W5F_FAKE_JAVA") == "serve" {
		port := ""
		for _, a := range os.Args {
			if v, ok := strings.CutPrefix(a, "-Dsuwayomi.tachidesk.config.server.port="); ok {
				port = v
			}
		}
		ln, err := net.Listen("tcp", "127.0.0.1:"+port)
		if err != nil {
			os.Exit(3)
		}
		go http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"data":{"aboutServer":{"version":"v-fake"}}}`)
		}))
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
		select {
		case <-stop:
		case <-time.After(30 * time.Second):
		}
		os.Exit(0)
	}
	if os.Getenv("W5F_FAKE_JAVA") == "crash" {
		fmt.Println("Error: Unable to access jarfile")
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// fakeGraphQL answers by the first matching word of the query.
func fakeGraphQL(t *testing.T, answers map[string]string, seen *[]map[string]any) *Client {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var req map[string]any
		json.Unmarshal(b, &req)
		if seen != nil {
			*seen = append(*seen, req)
		}
		q, _ := req["query"].(string)
		for key, a := range answers {
			if strings.Contains(q, key) {
				fmt.Fprint(w, a)
				return
			}
		}
		fmt.Fprint(w, `{"errors":[{"message":"unexpected query"}]}`)
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL)
}

func TestLibraryMangaAndErrors(t *testing.T) {
	c := fakeGraphQL(t, map[string]string{
		"mangas(": `{"data":{"mangas":{"nodes":[{"id":7,"title":"Berserk","inLibrary":true,"unreadCount":2,"source":{"displayName":"Local source"}}]}}}`,
		"manga(id": `{"data":{"manga":{"id":7,"title":"Berserk","chapters":{"nodes":[
			{"id":1,"name":"Ch 1","sourceOrder":1,"uploadDate":"1700000000000"},{"id":2,"name":"Ch 2","sourceOrder":2,"uploadDate":"0"}]}}}}`,
		"fetchExtensions": `{"errors":[{"message":"no network"}]}`,
	}, nil)
	ctx := context.Background()
	lib, err := c.Library(ctx)
	if err != nil || len(lib) != 1 || lib[0].SourceName() != "Local source" || lib[0].UnreadCount != 2 {
		t.Fatalf("library: %+v %v", lib, err)
	}
	m, chs, err := c.Manga(ctx, 7, false)
	if err != nil || m.Title != "Berserk" || len(chs) != 2 || chs[0].ID != 2 {
		t.Fatalf("manga (newest chapter first): %+v %+v %v", m, chs, err)
	}
	if chs[1].Uploaded().Year() != 2023 || !chs[0].Uploaded().IsZero() {
		t.Errorf("upload dates: %v %v", chs[1].Uploaded(), chs[0].Uploaded())
	}
	if _, _, err := c.Extensions(ctx, true); err == nil || !strings.Contains(err.Error(), "no network") {
		t.Errorf("GraphQL errors surface: %v", err)
	}
}

func TestDownloadStartsTheDownloaderOnlyWhenStopped(t *testing.T) {
	var seen []map[string]any
	c := fakeGraphQL(t, map[string]string{
		"enqueueChapterDownloads": `{"data":{"enqueueChapterDownloads":{"downloadStatus":{"state":"STARTED"}}}}`,
		"startDownloader":         `{"data":{}}`,
	}, &seen)
	if err := c.Download(context.Background(), []int{4, 5}); err != nil || len(seen) != 1 {
		t.Fatalf("running downloader is not restarted: %v (%d calls)", err, len(seen))
	}
	if ids := seen[0]["variables"].(map[string]any)["ids"].([]any); len(ids) != 2 {
		t.Errorf("chapter ids: %v", ids)
	}
}

func TestExtensionsGuardAndStores(t *testing.T) {
	var seen []map[string]any
	c := fakeGraphQL(t, map[string]string{"updateExtension": `{"data":{}}`, "addExtensionStore": `{"data":{}}`}, &seen)
	ctx := context.Background()
	if err := c.SetExtension(ctx, "x", "delete-everything"); err == nil {
		t.Error("unknown action refused")
	}
	if err := c.SetExtension(ctx, "eu.kanade.sample", "install"); err != nil {
		t.Fatal(err)
	}
	if p := seen[0]["variables"].(map[string]any)["p"].(map[string]any); p["install"] != true || len(p) != 1 {
		t.Errorf("patch: %v", p)
	}
	if err := c.AddStore(ctx, "not a url"); err == nil {
		t.Error("a repository needs a web address")
	}
	if err := c.AddStore(ctx, " https://example.org/index.min.json "); err != nil || len(seen) != 2 {
		t.Errorf("add store: %v", err)
	}
}

func TestNotRunning(t *testing.T) {
	c := New("http://127.0.0.1:1")
	if _, err := c.Version(context.Background()); err == nil || !strings.Contains(err.Error(), "not running") {
		t.Errorf("a missing server says so: %v", err)
	}
}

func freePort(t *testing.T) int {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func TestServerStartStop(t *testing.T) {
	self, _ := os.Executable()
	dir := t.TempDir()
	s := Server{Dir: dir, Java: self, Port: freePort(t), Downloads: filepath.Join(dir, "dl"), Local: filepath.Join(dir, "local")}
	ctx := context.Background()
	if _, err := s.Start(ctx, 5*time.Second); err == nil || !strings.Contains(err.Error(), "no Suwayomi-Server jar") {
		t.Fatalf("no jar: %v", err)
	}
	os.WriteFile(filepath.Join(dir, "Suwayomi-Server-v2.3.2243.jar"), []byte("jar"), 0o644)
	if a := strings.Join(s.Args(), " "); !strings.Contains(a, "webUIEnabled=false") || !strings.Contains(a, "ip=127.0.0.1") || !strings.HasSuffix(a, "Suwayomi-Server-v2.3.2243.jar") {
		t.Errorf("args: %s", a)
	}

	t.Setenv("W5F_FAKE_JAVA", "crash")
	if _, err := s.Start(ctx, 10*time.Second); err == nil || !strings.Contains(err.Error(), "stopped while starting") {
		t.Fatalf("a crashing server is reported: %v", err)
	}

	t.Setenv("W5F_FAKE_JAVA", "serve")
	started, err := s.Start(ctx, 20*time.Second)
	if err != nil || !started || !s.Running(ctx) {
		t.Fatalf("start: %v %v", started, err)
	}
	if again, err := s.Start(ctx, time.Second); again || err != nil {
		t.Errorf("a running server is not started twice: %v %v", again, err)
	}
	if err := s.Stop(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50 && s.Running(ctx); i++ {
		time.Sleep(100 * time.Millisecond)
	}
	if s.Running(ctx) {
		t.Error("server still answers after Stop")
	}
	if err := s.Stop(); err == nil {
		t.Error("stop without a started server says so")
	}
}

func TestStrayServerIsAdoptedAndStopped(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("strays are found through /proc")
	}
	self, _ := os.Executable()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "Suwayomi-Server-v1.jar"), []byte("jar"), 0o644)
	s := Server{Dir: dir, Java: self, Port: freePort(t)}
	ctx := context.Background()
	t.Setenv("W5F_FAKE_JAVA", "serve")
	if _, err := s.Start(ctx, 20*time.Second); err != nil {
		t.Fatal(err)
	}
	os.Remove(s.pidFile()) // as if W5F had exited before writing it
	if again, err := s.Start(ctx, 5*time.Second); again || err != nil {
		t.Errorf("a running stray is used, not doubled: %v %v", again, err)
	}
	os.Remove(s.pidFile())
	if err := s.Stop(); err != nil {
		t.Fatalf("stop finds the stray: %v", err)
	}
	if len(s.strays()) != 0 || s.Running(ctx) {
		t.Error("stray still running")
	}
}
