package main

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"time"

	xterm "github.com/charmbracelet/x/term"

	"w5f/internal/browser"
	"w5f/internal/comics"
	"w5f/internal/config"
	"w5f/internal/dict"
	"w5f/internal/doctor"
	"w5f/internal/personal"
	"w5f/internal/source"
	"w5f/internal/store"
	"w5f/internal/sysdeps"
	"w5f/internal/update"
)

// runDoctor: w5f doctor [--live] [--bench]
func runDoctor(args []string) int {
	live, bench := false, false
	for _, a := range args {
		switch a {
		case "--live":
			live = true
		case "--bench":
			bench = true
		default:
			fmt.Fprintln(os.Stderr, "usage: w5f doctor [--live] [--bench]")
			return 2
		}
	}
	uc, err := updateConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "w5f:", err)
		return 1
	}
	e := doctor.Env{Version: version, Exe: uc.Exe, DataDir: store.DataDir(), CacheDir: cacheDir(), NotesDir: personal.Dir(),
		DictDir: dict.Dir(store.DataDir()), Repo: uc.Repo, HasKey: update.PublicKey() != nil, GOOS: runtime.GOOS, Getenv: os.Getenv,
		TermSize: func() (int, int, error) { return xterm.GetSize(os.Stdout.Fd()) }, FontList: doctor.FcList,
		ComicsDir: comics.Root(), SuwayomiJar: source.ComicsServer().Jar(), JavaPath: func() (string, error) { return sysdeps.FindJava(config.Load().Comics.Java) },
		Browser: func() (string, error) { l, err := browser.Choose(); return l.Name, err }}
	fmt.Println("W5F doctor")
	fails := doctor.Print(os.Stdout, doctor.Check(e))
	if live {
		fmt.Println("\nLive sources")
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		fails += doctor.Print(os.Stdout, doctor.Live(ctx, version, doctor.Probes(uc.Repo)))
		cancel()
	}
	if bench {
		fmt.Println("\nBench")
		fails += doctor.Print(os.Stdout, doctor.Bench(12000))
	}
	if fails > 0 {
		fmt.Printf("\n%d problem(s) found.\n", fails)
		return 1
	}
	return 0
}
