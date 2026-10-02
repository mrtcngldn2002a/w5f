package main

import (
	"bufio"
	"context"
	"fmt"
	xterm "github.com/charmbracelet/x/term"
	"io"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"w5f/internal/solver"
	"w5f/internal/store"
)

type crlfWriter struct{ io.Writer }

func (w crlfWriter) Write(b []byte) (int, error) {
	_, e := w.Writer.Write([]byte(strings.ReplaceAll(string(b), "\n", "\r\n")))
	return len(b), e
}
func runSolver(args []string) int {
	if len(args) == 0 {
		fmt.Println("usage: w5f solver install [--yes] | status | start | stop | update [--yes] | remove [--yes] | ask-again")
		return 2
	}
	action := args[0]
	yes := false
	for _, arg := range args[1:] {
		if arg != "--yes" {
			fmt.Fprintln(os.Stderr, "unknown solver option:", arg)
			return 2
		}
		yes = true
	}
	m := solver.Default()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	var e error
	switch action {
	case "status":
		s := m.Status(ctx)
		if s.Dir != "" {
			fmt.Printf("Byparr %s in %s (%.1f GB)\n", s.Version, s.Dir, float64(s.Bytes)/(1<<30))
			if !s.Managed {
				fmt.Println("Manual installation: adopted, never removed by W5F.")
			}
		} else {
			fmt.Println("Byparr is not installed.")
		}
		if s.Running {
			fmt.Println(s.Helper, "at", m.URL, "· started by", s.Owner)
		} else {
			fmt.Println(s.Problem)
		}
		if s.Update != "" {
			fmt.Println(s.Update)
		}
		if hint := solver.XvfbHint(m.DataDir); hint != "" {
			fmt.Println(hint)
		}
		return 0
	case "start":
		var started bool
		started, e = m.Start(ctx, m.URL)
		if e == nil {
			if started {
				fmt.Println("W5F started Byparr at", m.URL)
			} else {
				fmt.Println("Existing helper kept; no additional process started.")
			}
		}
	case "stop":
		e = m.Stop()
		if e == nil {
			fmt.Println("W5F-owned Byparr stopped; external helpers were kept.")
		}
	case "ask-again":
		var db *store.DB
		db, e = store.Default()
		if e == nil {
			e = solver.AskAgain(db)
		}
	case "install", "update", "remove":
		if action != "remove" {
			if err := solver.Supported(runtime.GOOS, runtime.GOARCH); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
		}
		if !yes {
			if !xterm.IsTerminal(os.Stdin.Fd()) {
				fmt.Fprintln(os.Stderr, "confirmation requires a terminal; use --yes for a script")
				return 2
			}
			prompt := "Install Byparr (~1.1 GB; at least 1.5 GB free)? [y/N] "
			if action == "update" {
				prompt = "Update Byparr separately from W5F? [y/N] "
			}
			if action == "remove" {
				prompt = "Remove only W5F's listed Byparr files? [y/N] "
			}
			fmt.Print(prompt)
			reply, _ := bufio.NewReader(os.Stdin).ReadString('\n')
			if strings.ToLower(strings.TrimSpace(reply)) != "y" && strings.ToLower(strings.TrimSpace(reply)) != "yes" {
				fmt.Println("Cancelled.")
				return 0
			}
		}
		out := io.Writer(os.Stdout)
		if xterm.IsTerminal(os.Stdin.Fd()) {
			if state, err := xterm.MakeRaw(os.Stdin.Fd()); err == nil {
				defer xterm.Restore(os.Stdin.Fd(), state)
				out = crlfWriter{os.Stdout}
				go func() {
					buf := make([]byte, 1)
					for {
						if _, err := os.Stdin.Read(buf); err != nil {
							return
						}
						if buf[0] == 27 || buf[0] == 3 {
							cancel()
							return
						}
					}
				}()
			}
		}
		switch action {
		case "install":
			_, e = (solver.Installer{DataDir: m.DataDir, Output: out}).Install(ctx)
		case "update":
			i := solver.Installer{DataDir: m.DataDir, Output: out}
			e = m.Update(ctx, &i)
			if e == nil {
				fmt.Fprintln(out, "Update complete. Restart any external Byparr service to use the selected version.")
			}
		case "remove":
			e = m.Remove()
		}
	default:
		fmt.Fprintln(os.Stderr, "unknown solver command:", action)
		return 2
	}
	if e != nil {
		fmt.Fprintln(os.Stderr, "w5f solver:", e)
		return 1
	}
	return 0
}
