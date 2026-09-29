package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"w5f/internal/comics"
	"w5f/internal/comics/suwayomi"
	"w5f/internal/source"
	"w5f/internal/store"
)

const comicsUsage = `usage:
  w5f comics list                   local series and the series Suwayomi follows
  w5f comics update                 check followed series for new chapters
  w5f comics server install         download Suwayomi-Server (official release, checksum checked)
  w5f comics server start|stop|status`

// runComics: w5f comics …
func runComics(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, comicsUsage)
		return 2
	}
	srv := source.ComicsServer()
	ctx := context.Background()
	fail := func(err error) int {
		fmt.Fprintln(os.Stderr, "w5f:", err)
		return 1
	}
	switch args[0] {
	case "server":
		action := ""
		if len(args) > 1 {
			action = args[1]
		}
		switch action {
		case "install":
			fmt.Println("Downloading Suwayomi-Server into", srv.Dir)
			name, err := suwayomi.Install(ctx, srv.Dir, func(done, total int64) {
				if total > 0 {
					fmt.Fprintf(os.Stderr, "\r%d%%", done*100/total)
				}
			})
			fmt.Fprintln(os.Stderr)
			if err != nil {
				return fail(err)
			}
			fmt.Println("Installed", name, "(checksum OK). It needs Java 21 or newer.")
		case "start":
			fmt.Println("Starting Suwayomi (about 15 seconds)…")
			if _, err := srv.Start(ctx, 90*time.Second); err != nil {
				return fail(err)
			}
			fmt.Println("Suwayomi is running at", srv.Addr(), "(stop it with w5f comics server stop)")
		case "stop":
			if err := srv.Stop(); err != nil {
				return fail(err)
			}
			fmt.Println("Suwayomi stopped.")
		case "status":
			v, err := suwayomi.New(srv.Addr()).Version(ctx)
			switch {
			case err == nil:
				fmt.Println("running:", v, "at", srv.Addr())
			case srv.Jar() == "":
				fmt.Println("not installed (w5f comics server install)")
			default:
				fmt.Println("installed, not running:", srv.Jar())
			}
		default:
			fmt.Fprintln(os.Stderr, comicsUsage)
			return 2
		}
		return 0
	case "list", "update":
		db, err := store.Default()
		if err != nil {
			return fail(err)
		}
		if args[0] == "list" {
			if _, err := comics.Scan(db, comics.Root()); err != nil {
				fmt.Fprintln(os.Stderr, "w5f: comics folder:", err)
			}
			series, _ := db.ComicSeriesList()
			fmt.Printf("Local library (%s): %d series\n", comics.Root(), len(series))
			for _, s := range series {
				fmt.Printf("  %-40s %3d issues, %d unread\n", s.Name, s.Issues, s.Unread)
			}
		}
		started, err := srv.Start(ctx, 90*time.Second)
		if err != nil {
			if args[0] == "list" {
				fmt.Println("Suwayomi:", err)
				return 0
			}
			return fail(err)
		}
		if started {
			defer srv.Stop()
		}
		c := suwayomi.New(srv.Addr())
		if args[0] == "update" {
			if err := c.UpdateLibrary(ctx); err != nil {
				return fail(err)
			}
			fmt.Print("Checking followed series")
			for i := 0; i < 180; i++ {
				time.Sleep(2 * time.Second)
				j, err := c.Updating(ctx)
				if err != nil || !j.IsRunning {
					break
				}
				fmt.Printf("\rChecking followed series %d/%d", j.FinishedJobs, j.TotalJobs)
			}
			fmt.Println()
		}
		lib, err := c.Library(ctx)
		if err != nil {
			return fail(err)
		}
		fmt.Printf("Following on Suwayomi: %d series\n", len(lib))
		for _, m := range lib {
			fmt.Printf("  %-40s %3d unread  (%s)\n", m.Title, m.UnreadCount, m.SourceName())
		}
		return 0
	}
	fmt.Fprintln(os.Stderr, comicsUsage)
	return 2
}
