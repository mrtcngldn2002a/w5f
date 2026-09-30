package main

import (
	"fmt"
	"os"

	"w5f/internal/feeds"
	"w5f/internal/source"
)

const feedsUsage = `Usage:
  w5f feeds import FILE.opml   add the feeds of another reader to your shelves
  w5f feeds export [FILE]      write your shelves as OPML (no FILE: to the screen)
`

func runFeeds(args []string) int {
	if len(args) == 0 || (args[0] == "import" && len(args) < 2) || (args[0] != "import" && args[0] != "export") {
		fmt.Fprint(os.Stderr, feedsUsage)
		return 2
	}
	source.Init(cacheDir(), false)
	env, err := source.FeedsEnv()
	if err != nil {
		fmt.Fprintln(os.Stderr, "w5f:", err)
		return 1
	}
	if args[0] == "import" {
		rep, err := feeds.Import(env.Catalog, args[1])
		if err != nil {
			fmt.Fprintln(os.Stderr, "w5f:", err)
			return 1
		}
		for _, s := range rep.Shelves {
			fmt.Printf("new shelf  %s (%s)\n", s.Label, s.ID)
		}
		for _, f := range rep.Added {
			fmt.Printf("added      %-24s %s\n", f.ID, f.URL[0])
		}
		for _, name := range rep.Duplicate {
			fmt.Printf("already in %s\n", name)
		}
		fmt.Printf("%d added, %d already on your shelves", len(rep.Added), len(rep.Duplicate))
		if len(rep.Added) > 0 {
			fmt.Printf(" (written to %s; w5f sync fetches them)", rep.File)
		}
		fmt.Println()
		return 0
	}
	out := os.Stdout
	if len(args) > 1 && args[1] != "-" {
		f, err := os.Create(args[1])
		if err != nil {
			fmt.Fprintln(os.Stderr, "w5f:", err)
			return 1
		}
		defer f.Close()
		out = f
	}
	n, err := feeds.Export(env.Catalog, env.DB, out)
	if err != nil {
		fmt.Fprintln(os.Stderr, "w5f:", err)
		return 1
	}
	if out != os.Stdout {
		fmt.Printf("%d feeds exported to %s\n", n, args[1])
	}
	return 0
}
