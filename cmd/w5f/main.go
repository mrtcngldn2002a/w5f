// Command w5f is the W5F // Archive Node terminal reader.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	xterm "github.com/charmbracelet/x/term"

	"w5f/internal/comics"
	"w5f/internal/dict"
	"w5f/internal/doc"
	"w5f/internal/feeds"
	"w5f/internal/fiction"
	"w5f/internal/index"
	"w5f/internal/personal"
	"w5f/internal/reddit"
	"w5f/internal/render"
	"w5f/internal/source"
	"w5f/internal/store"
	"w5f/internal/tui"
)

var version = "0.0.0-dev"

const usage = `W5F // ARCHIVE NODE  v%s

Usage:
  w5f [--offline] [target]    open the reader (no argument: welcome page)
                              target: url, domain, file, scp-173, w5f:random/scp
  w5f dump [-w N] [-open all] file|url
                              print the rendered page as plain text
  w5f sync                    refresh all periodicals and followed serials (for timers/cron)
  w5f reindex                 rebuild the search index (pages from the cache, feeds, books, notes)
  w5f dict-install [zip|url]  install the pop-up dictionary (default: Englishâ€“Turkish)
  w5f reddit-login            connect Reddit with your own session cookie
  w5f reddit-logout           remove the stored Reddit session
  w5f ao3-login / ao3-logout  connect AO3 with your own session cookie / remove it
  w5f view [--comic ID] [file] the comics viewer (X11; opened from Comics pages)
  w5f comics list|update       comics: local library and followed series (w5f comics for more)
  w5f doctor [--live] [--bench]
                              check this install (--live: sources, --bench: speed)
  w5f update [--check]        install the latest signed release (GitHub Releases)
  w5f update --rollback       go back to the previous version
  w5f version                 print the version
`

func main() {
	source.Version = version
	args := os.Args[1:]
	if len(args) > 0 {
		switch args[0] {
		case "version", "--version", "-v":
			fmt.Println("w5f", version)
			return
		case "help", "--help", "-h":
			fmt.Printf(usage, version)
			return
		case "dump":
			os.Exit(dump(args[1:]))
		case "update":
			os.Exit(runUpdate(args[1:]))
		case "selftest":
			os.Exit(selftest())
		case "doctor":
			os.Exit(runDoctor(args[1:]))
		case "comics":
			os.Exit(runComics(args[1:]))
		case "view":
			os.Exit(runView(args[1:]))
		case "sync":
			os.Exit(syncFeeds())
		case "reindex":
			os.Exit(reindex())
		case "dict-install":
			src := dict.DefaultURL
			if len(args) > 1 {
				src = args[1]
			}
			fmt.Println("Installing dictionary from", src)
			title, err := dict.Install(context.Background(), src, dict.Dir(store.DataDir()), source.Fetcher.UserAgent)
			if err != nil {
				fmt.Fprintln(os.Stderr, "w5f:", err)
				os.Exit(1)
			}
			fmt.Println("Installed:", title)
			return
		case "reddit-login":
			os.Exit(redditLogin())
		case "ao3-login":
			os.Exit(ao3Login())
		case "ao3-logout":
			if err := fiction.DeleteAO3Session(); err != nil {
				fmt.Fprintln(os.Stderr, "w5f:", err)
				os.Exit(1)
			}
			fmt.Println("AO3 session removed from this computer.")
			return
		case "reddit-logout":
			if err := reddit.DeleteSession(); err != nil {
				fmt.Fprintln(os.Stderr, "w5f:", err)
				os.Exit(1)
			}
			fmt.Println("Reddit session removed from this computer.")
			return
		}
	}
	offline := false
	target := ""
	for _, a := range args {
		switch a {
		case "--offline":
			offline = true
		default:
			if target == "" {
				target = source.Resolve(a)
			}
		}
	}
	source.Init(cacheDir(), offline)
	comics.AutoStart = true // the reader stops it again on exit
	p := tea.NewProgram(tui.New(target, version))
	_, err := p.Run()
	reddit.Shutdown()                      // stop a Redlib that W5F started
	comics.Shutdown(source.ComicsServer()) // and a Suwayomi
	if err != nil {
		fmt.Fprintln(os.Stderr, "w5f:", err)
		os.Exit(1)
	}
}

// redditLogin stores the user's own reddit_session cookie. The value is read
// without echo and written to a file only the user can read.
func redditLogin() int {
	fmt.Println("Paste the value of your reddit_session cookie and press enter.")
	fmt.Println("(Input stays hidden. It is stored only at " + reddit.SessionPath() + ")")
	fmt.Print("> ")
	var raw []byte
	var err error
	if xterm.IsTerminal(os.Stdin.Fd()) {
		raw, err = xterm.ReadPassword(os.Stdin.Fd())
		fmt.Println()
	} else {
		raw, err = bufio.NewReader(os.Stdin).ReadBytes('\n')
	}
	if err != nil && len(raw) == 0 {
		fmt.Fprintln(os.Stderr, "w5f:", err)
		return 1
	}
	if err := reddit.SaveSession(string(raw)); err != nil {
		fmt.Fprintln(os.Stderr, "w5f: could not save the session:", err)
		return 1
	}
	fmt.Println("Saved. Open Reddit in W5F with: g r/nosleep")
	return 0
}

// ao3Login stores the owner's own AO3 session cookie, read without echo.
func ao3Login() int {
	fmt.Println("Paste the value of your AO3 _otwarchive_session cookie (or the whole Cookie header) and press enter.")
	fmt.Println("(Input stays hidden. It is stored only at " + fiction.AO3SessionPath() + " and sent only to archiveofourown.org)")
	fmt.Print("> ")
	var raw []byte
	var err error
	if xterm.IsTerminal(os.Stdin.Fd()) {
		raw, err = xterm.ReadPassword(os.Stdin.Fd())
		fmt.Println()
	} else {
		raw, err = bufio.NewReader(os.Stdin).ReadBytes('\n')
	}
	if err != nil && len(raw) == 0 {
		fmt.Fprintln(os.Stderr, "w5f:", err)
		return 1
	}
	if err := fiction.SaveAO3Session(string(raw)); err != nil {
		fmt.Fprintln(os.Stderr, "w5f: could not save the AO3 session:", err)
		return 1
	}
	fmt.Println("Saved. In W5F: g → fiction → My AO3")
	return 0
}

// syncFeeds refreshes every periodical (for cron / runit timers).
func syncFeeds() int {
	source.Init(cacheDir(), false)
	env, err := source.FeedsEnv()
	if err != nil {
		fmt.Fprintln(os.Stderr, "w5f:", err)
		return 1
	}
	rep := feeds.Sync(context.Background(), env.Fetcher, env.DB, env.Catalog, nil, func(done, total int) {
		fmt.Fprintf(os.Stderr, "\rsyncing %d/%d", done, total)
	})
	fmt.Fprintf(os.Stderr, "\r")
	fmt.Printf("%d feeds, %d new items, %d failed, %s\n", len(rep.Results), rep.New(), len(rep.Failed()), rep.Took.Round(time.Second))
	for _, r := range rep.Failed() {
		fmt.Printf("  %-22s %v\n", r.Feed, r.Err)
	}
	if n, err := index.Feeds(env.DB); err == nil && n > 0 {
		fmt.Printf("%d new items indexed for search\n", n)
	}
	if fenv, err := source.FictionEnv(); err == nil {
		frep := fiction.SyncFollowed(context.Background(), fenv, func(done, total int) {
			fmt.Fprintf(os.Stderr, "\rchecking serials %d/%d", done, total)
		})
		fmt.Fprintf(os.Stderr, "\r")
		if frep.Checked > 0 {
			fmt.Println(frep.String())
		}
		if n, err := fiction.ImportAO3Downloads(fenv); err == nil && n > 0 {
			fmt.Printf("%d AO3 books imported from your Downloads folder\n", n)
		}
	}
	return 0
}

func reindex() int {
	source.Init(cacheDir(), true) // pages come from the cache only
	db, err := store.Default()
	if err != nil {
		fmt.Fprintln(os.Stderr, "w5f:", err)
		return 1
	}
	ctx := context.Background()
	load := func(t string) (*doc.Document, error) { return source.Load(ctx, t, source.Options{}) }
	fmt.Println("Rebuilding the search indexâ€¦")
	r, err := index.Rebuild(ctx, db, personal.Dir(), load)
	if err != nil {
		fmt.Fprintln(os.Stderr, "w5f:", err)
		return 1
	}
	fmt.Printf("%d pages, %d feed items, %d books, %d notes indexed; %d pages are no longer in the cache\n",
		r.Pages, r.Feeds, r.Books, r.Notes, r.Skipped)
	return 0
}

// cacheDir is where fetched pages are kept for offline reading.
func cacheDir() string {
	if d, err := os.UserCacheDir(); err == nil {
		return filepath.Join(d, "w5f")
	}
	return ""
}

func dump(args []string) int {
	fs := flag.NewFlagSet("dump", flag.ContinueOnError)
	width := fs.Int("w", 72, "text column width")
	open := fs.String("open", "", `"all" to expand every collapsible`)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintf(os.Stderr, usage, version)
		return 2
	}
	d, err := source.Load(context.Background(), source.Resolve(fs.Arg(0)), source.Options{})
	reddit.Shutdown()
	comics.Shutdown(source.ComicsServer())
	if err != nil {
		fmt.Fprintln(os.Stderr, "w5f:", err)
		return 1
	}
	opts := render.Options{Width: *width, Open: map[int]bool{}}
	if *open == "all" {
		for i := 1; i <= d.Collapsibles; i++ {
			opts.Open[i] = true
		}
	}
	l := render.Render(d, opts)
	var b strings.Builder
	for _, ln := range l.Lines {
		b.WriteString(strings.TrimRight(ln.Text(), " "))
		b.WriteByte('\n')
	}
	fmt.Print(b.String())
	return 0
}
