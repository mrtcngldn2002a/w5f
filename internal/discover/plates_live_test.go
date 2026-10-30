package discover

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"w5f/internal/doc"
	"w5f/internal/fetch"
	"w5f/internal/picture"
	"w5f/internal/store"
)

// TestPlatesLive draws from every source of the plates shelf against the
// real sites (W5F_PLATES=1; W5F_PLATES_N draws each, 3 by default), builds
// each page as the reader opens it, judges it and fetches and decodes its
// picture.
func TestPlatesLive(t *testing.T) {
	if os.Getenv("W5F_PLATES") == "" {
		t.Skip("set W5F_PLATES=1 to draw from the real sites")
	}
	n := 3
	if v, err := strconv.Atoi(os.Getenv("W5F_PLATES_N")); err == nil && v > 0 {
		n = v
	}
	f := fetch.New(t.TempDir(), "test")
	db, err := store.Open(filepath.Join(t.TempDir(), "live.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	env := Env{Fetcher: f, DB: db}
	good, all := 0, 0
	only := os.Getenv("W5F_PLATES_ONLY")
	for _, s := range plateSources {
		if only != "" && !strings.Contains(only, s.name) {
			continue
		}
		for range n {
			all++
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			start := time.Now()
			target, title, err := s.pick(ctx, env)
			if err != nil {
				cancel()
				t.Logf("%-26s DRAW FAILED %v", s.name, err)
				continue
			}
			page, err := Route(ctx, target, env)
			if err != nil {
				cancel()
				t.Logf("%-26s PAGE FAILED %s: %v", s.name, target, err)
				continue
			}
			v := Judge(page, "plates")
			pics, size, perr := 0, "", error(nil)
			for _, b := range page.Blocks {
				if im, ok := b.(doc.Image); ok {
					pics++
					if pics == 1 {
						u, _ := url.Parse(im.Src)
						resp, err := f.Get(ctx, u, fetch.Options{Header: PictureHeader(im.Src)})
						if err == nil {
							var p *picture.Picture
							if p, err = picture.Decode(resp.Body); err == nil {
								w, hh := p.Size()
								size = strconv.Itoa(len(resp.Body)/1024) + "KB " + strconv.Itoa(w) + "x" + strconv.Itoa(hh)
							}
						}
						perr = err
					}
				}
			}
			cancel()
			ok := v.OK() && perr == nil
			if ok {
				good++
			}
			t.Logf("%-26s ok=%v %4.1fs words=%d pics=%d %s %v | %s | %s | %s", s.name, ok, time.Since(start).Seconds(),
				v.Words, pics, size, perr, title, page.Title, strings.Join(v.Reasons, ","))
		}
	}
	t.Logf("plates: %d/%d whole", good, all)
}
