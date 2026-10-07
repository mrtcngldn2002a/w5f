package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"time"

	"w5f/internal/browser"
	"w5f/internal/comics"
	"w5f/internal/comics/suwayomi"
	"w5f/internal/comics/view"
	"w5f/internal/source"
	"w5f/internal/store"
)

// runView: w5f view [--comic ID] [--chapter ID] [--browser] [--bench N] [file]
func runView(args []string) int {
	fs := flag.NewFlagSet("view", flag.ContinueOnError)
	inBrowser := fs.Bool("browser", false, "show the viewer in a browser tab, not an X11 window")
	comicID := fs.Int64("comic", 0, "a comic of the library (keeps progress)")
	chapterID := fs.Int("chapter", 0, "a chapter from Suwayomi")
	bench := fs.Int("bench", 0, "turn N pages and report the time from key to picture")
	dwell := fs.Duration("dwell", 1500*time.Millisecond, "with --bench: reading time before each turn (0 = back to back)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	book, neighbor, err := viewBook(*comicID, *chapterID, fs.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, "w5f view:", err)
		return 1
	}
	var d view.Display
	if *inBrowser || !hasX11() {
		// A Mac, Windows, a Wayland desktop: the viewer in a browser tab.
		// Its frames are the screen's own pixels (twice the window on a
		// Retina screen), so the heap may be larger than on the laptop.
		debug.SetMemoryLimit(192 << 20)
		w, err := view.OpenWeb(book.Title)
		if err != nil {
			fmt.Fprintln(os.Stderr, "w5f view:", err)
			return 1
		}
		if err := browser.Open(w.URL); err != nil {
			fmt.Fprintln(os.Stderr, "w5f view:", err, "— open", w.URL, "yourself")
		}
		fmt.Println("The comics viewer is open in the browser:", w.URL)
		if err := w.WaitReady(time.Minute); err != nil {
			w.Close()
			fmt.Fprintln(os.Stderr, "w5f view:", err)
			return 1
		}
		d = w
	} else {
		// Pages are big when decoded; keep the heap near what is on screen
		// (budget: 80 MB on the W5F laptop).
		debug.SetMemoryLimit(48 << 20)
		x, err := view.OpenX11()
		if err != nil {
			fmt.Fprintln(os.Stderr, "w5f view:", err)
			return 1
		}
		d = x
	}
	defer d.Close()
	if *bench > 0 {
		return benchView(d, book, *bench, *dwell)
	}
	if _, err := view.New(d, book, neighbor).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "w5f view:", err)
		return 1
	}
	return 0
}

// hasX11 reports an X display the viewer's own window can use: on Linux
// and the BSDs with DISPLAY set (a Mac's XQuartz is not used: the browser
// is at home there).
func hasX11() bool {
	return runtime.GOOS != "darwin" && runtime.GOOS != "windows" && os.Getenv("DISPLAY") != ""
}

// viewBook finds what to show and how to keep its progress.
func viewBook(comicID int64, chapterID int, path string) (view.Book, func(int) (view.Book, error), error) {
	switch {
	case chapterID > 0:
		c := source.ComicsServer().Client()
		b, err := chapterBook(c, chapterID)
		if err != nil {
			return b, nil, err
		}
		cur := chapterID
		neighbor := func(dir int) (view.Book, error) {
			next, err := neighborChapter(c, cur, dir)
			if err != nil {
				return view.Book{}, err
			}
			b, err := chapterBook(c, next)
			if err == nil {
				cur = next
			}
			return b, err
		}
		return b, neighbor, nil
	case comicID > 0:
		db, err := store.Default()
		if err != nil {
			return view.Book{}, nil, err
		}
		cm, err := db.Comic(comicID)
		if err != nil {
			return view.Book{}, nil, err
		}
		b, err := comicBook(db, cm)
		if err != nil {
			return b, nil, err
		}
		cur := cm
		neighbor := func(dir int) (view.Book, error) {
			issues, err := db.ComicsInSeries(cur.Series)
			if err != nil {
				return view.Book{}, err
			}
			for i, is := range issues {
				if is.ID == cur.ID && i+dir >= 0 && i+dir < len(issues) {
					b, err := comicBook(db, issues[i+dir])
					if err == nil {
						cur = issues[i+dir]
					}
					return b, err
				}
			}
			if dir > 0 {
				return view.Book{}, errors.New("this is the last issue in " + cur.Series)
			}
			return view.Book{}, errors.New("this is the first issue in " + cur.Series)
		}
		return b, neighbor, nil
	case path != "" && comics.IsImage(path):
		// One picture: its folder, from that picture on.
		pages, info, start, err := comics.OpenImage(path)
		if err != nil {
			return view.Book{}, nil, err
		}
		return view.Book{Title: filepath.Base(filepath.Dir(path)), Pages: pages, RTL: info.RTL(), Start: start}, nil, nil
	case path != "":
		pages, info, err := comics.Open(path)
		if err != nil {
			return view.Book{}, nil, err
		}
		return view.Book{Title: filepath.Base(path), Pages: pages, RTL: info.RTL()}, nil, nil
	}
	return view.Book{}, nil, errors.New("usage: w5f view [--comic ID | --chapter ID | file]")
}

func comicBook(db *store.DB, c store.Comic) (view.Book, error) {
	pages, info, err := comics.Open(c.Path)
	if err != nil {
		return view.Book{}, err
	}
	start := c.Page
	if c.Finished {
		start = 0
	}
	id := c.ID
	return view.Book{Title: c.Series + " · " + c.Title, Pages: pages, RTL: c.RTL || info.RTL(), Start: start,
		OnPage: func(p, n int) { _ = db.SaveComicPage(id, p, n) }}, nil
}

func chapterBook(c *suwayomi.Client, id int) (view.Book, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	info, err := c.ChapterInfo(ctx, id)
	if err != nil {
		return view.Book{}, err
	}
	pages, err := c.Pages(ctx, id)
	if err != nil {
		return view.Book{}, err
	}
	start := info.LastPageRead
	if info.IsRead || start >= pages.Len() {
		start = 0
	}
	return view.Book{Title: info.Manga.Title + " · " + info.Name, Pages: pages, Start: start,
		OnPage: func(p, n int) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = c.SetPage(ctx, id, p)
			if p >= n-1 {
				_ = c.MarkRead(ctx, []int{id}, true)
			}
		}}, nil
}

// neighborChapter is the chapter after (dir 1) or before (-1) in its series.
func neighborChapter(c *suwayomi.Client, id, dir int) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	info, err := c.ChapterInfo(ctx, id)
	if err != nil {
		return 0, err
	}
	_, chs, err := c.Manga(ctx, info.Manga.ID, false)
	if err != nil {
		return 0, err
	}
	best, bestD := 0, 0
	for _, ch := range chs {
		if d := (ch.SourceOrder - info.SourceOrder) * dir; d > 0 && (best == 0 || d < bestD) {
			best, bestD = ch.ID, d
		}
	}
	if best == 0 {
		return 0, errors.New("no further chapter")
	}
	return best, nil
}

// benchView turns n pages, waiting dwell before each turn as a reader
// would, and reports the time from the key to the picture on screen.
func benchView(d view.Display, b view.Book, n int, dwell time.Duration) int {
	b.OnPage = nil // a measurement is not reading
	keys := make(chan view.Key)
	fd := &benchDisplay{Display: d, keys: keys, shown: make(chan struct{}, 1)}
	v := view.New(fd, b, nil)
	type result struct{ avg, worst time.Duration }
	done := make(chan result)
	turns := min(n, b.Pages.Len()) - 1
	go func() {
		<-fd.shown // the first page
		var total, worst time.Duration
		for i := 0; i < turns; i++ {
			time.Sleep(dwell)
			for len(fd.shown) > 0 { // an overlay redraw is not a turn
				<-fd.shown
			}
			start := time.Now()
			keys <- "space"
			<-fd.shown
			took := time.Since(start)
			total += took
			worst = max(worst, took)
		}
		keys <- "q"
		done <- result{total / time.Duration(max(1, turns)), worst}
	}()
	v.Run()
	r := <-done
	sz := d.Size()
	fmt.Printf("%d page turns on a %dx%d screen, %s reading before each: %s average, %s worst (budget 300ms)\n",
		turns, sz.X, sz.Y, dwell, r.avg.Round(time.Millisecond), r.worst.Round(time.Millisecond))
	return 0
}

type benchDisplay struct {
	view.Display
	keys  chan view.Key
	shown chan struct{}
}

func (b *benchDisplay) Keys() <-chan view.Key { return b.keys }
func (b *benchDisplay) Show(f *image.RGBA) error {
	err := b.Display.Show(f)
	select {
	case b.shown <- struct{}{}:
	default:
	}
	return err
}
