package suwayomi

import (
	"context"
	"os"
	"testing"

	"w5f/internal/comics/suwayomi/suwayomitest"
)

// The helper is set once; after that Suwayomi's setting is the owner's.
func TestSolverDefaultOnce(t *testing.T) {
	f := suwayomitest.New(t)
	s := Server{Dir: t.TempDir(), Port: f.Port, Solver: "http://127.0.0.1:8191"}
	s.solverDefault(context.Background())
	if f.Values["flareSolverrEnabled"] != true || f.Values["flareSolverrUrl"] != "http://127.0.0.1:8191" || f.Sets != 1 {
		t.Fatalf("not set: %v %v (%d sets)", f.Values["flareSolverrEnabled"], f.Values["flareSolverrUrl"], f.Sets)
	}
	if _, err := os.Stat(s.solverMark()); err != nil {
		t.Fatal("no mark:", err)
	}
	f.Values["flareSolverrEnabled"] = false // the owner turns it off
	s.solverDefault(context.Background())
	if f.Values["flareSolverrEnabled"] != false || f.Sets != 1 {
		t.Fatal("turned on again over the owner's choice")
	}
}

func TestSolverDefaultOff(t *testing.T) {
	f := suwayomitest.New(t)
	s := Server{Dir: t.TempDir(), Port: f.Port}
	s.solverDefault(context.Background())
	if f.Sets != 0 || f.Values["flareSolverrEnabled"] != false {
		t.Fatal("an empty solver changed Suwayomi")
	}
	// A server that cannot be reached is tried again, not marked.
	s = Server{Dir: t.TempDir(), Port: 1, Solver: "http://127.0.0.1:8191"}
	s.solverDefault(context.Background())
	if _, err := os.Stat(s.solverMark()); err == nil {
		t.Fatal("marked without setting")
	}
}
