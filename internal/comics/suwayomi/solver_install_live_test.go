package suwayomi

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"w5f/internal/solver"
)

// Explicitly opt-in: starts a fresh server on another port, never the user's
// existing server. The optional helper has already been installed separately.
func TestLiveInstalledSolver(t *testing.T) {
	jar := os.Getenv("W5F_SOLVER_TEST_JAR")
	if jar == "" {
		t.Skip("set W5F_SOLVER_TEST_JAR and W5F_SOLVER_TEST_JAVA for the isolated server test")
	}
	helper := os.Getenv("W5F_SOLVER_URL")
	if helper == "" {
		t.Fatal("isolated helper URL missing")
	}
	dir := t.TempDir()
	src, e := os.Open(jar)
	if e != nil {
		t.Fatal(e)
	}
	defer src.Close()
	dst, e := os.Create(filepath.Join(dir, "Suwayomi-Server-isolated-test.jar"))
	if e != nil {
		t.Fatal(e)
	}
	_, e = io.Copy(dst, src)
	dst.Close()
	if e != nil {
		t.Fatal(e)
	}
	s := Server{Dir: dir, Java: os.Getenv("W5F_SOLVER_TEST_JAVA"), Port: 14567, Solver: helper, Downloads: filepath.Join(dir, "downloads"), Local: filepath.Join(dir, "local")}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	solver.AutoStart = true
	t.Cleanup(func() { solver.AutoStart = false; solver.Shutdown() })
	started, e := s.Start(ctx, 90*time.Second)
	if e != nil {
		t.Fatal(e)
	}
	if !started {
		t.Fatal("test port belongs to an existing server")
	}
	t.Cleanup(func() { _ = s.Stop() })
	settings, e := s.Client().ServerSettings(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if settings.Values["flareSolverrEnabled"] != true || settings.Values["flareSolverrUrl"] != helper {
		t.Fatal("helper was not connected", settings.Values["flareSolverrEnabled"], settings.Values["flareSolverrUrl"])
	}
	t.Log("new Suwayomi has FlareSolverr enabled at", helper)
	if e = s.Client().SetServerSettings(ctx, map[string]any{"flareSolverrEnabled": false}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Start(ctx, 90*time.Second); e != nil {
		t.Fatal(e)
	}
	settings, e = s.Client().ServerSettings(ctx)
	if e != nil || settings.Values["flareSolverrEnabled"] != false {
		t.Fatal("owner's disabled setting overwritten", e)
	}
}
