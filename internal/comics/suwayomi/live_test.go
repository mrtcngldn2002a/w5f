package suwayomi

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestLiveServer runs every call against a real server when W5F_SUWAYOMI_LIVE
// is set (e.g. http://127.0.0.1:4567). It never adds a repository or
// installs an extension.
func TestLiveServer(t *testing.T) {
	base := os.Getenv("W5F_SUWAYOMI_LIVE")
	if base == "" {
		t.Skip("set W5F_SUWAYOMI_LIVE to run against a server")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	c := New(base)
	v, err := c.Version(ctx)
	t.Logf("version %q %v", v, err)
	srcs, err := c.Sources(ctx)
	t.Logf("sources %d %v", len(srcs), err)
	ms, next, err := c.Browse(ctx, LocalSource, "POPULAR", "", 1)
	t.Logf("browse local %d next=%v %v", len(ms), next, err)
	if err != nil || len(ms) == 0 {
		t.Fatal("no local series")
	}
	_, found, err := c.Browse(ctx, LocalSource, "SEARCH", "Gamma", 1)
	t.Logf("search %v %v", found, err)
	if err := c.Follow(ctx, []int{ms[0].ID}, true); err != nil {
		t.Fatal(err)
	}
	lib, err := c.Library(ctx)
	t.Logf("library %d %v", len(lib), err)
	m, chs, err := c.Manga(ctx, ms[0].ID, true)
	t.Logf("manga %q chapters %d (refresh) %v", m.Title, len(chs), err)
	m, chs, err = c.Manga(ctx, ms[0].ID, false)
	t.Logf("manga %q chapters %d %v", m.Title, len(chs), err)
	if len(chs) == 0 {
		t.Fatal("no chapters")
	}
	t.Logf("first chapter %+v uploaded %v", chs[0], chs[0].Uploaded())
	t.Logf("mark read %v", c.MarkRead(ctx, []int{chs[0].ID}, true))
	t.Logf("mark unread %v", c.MarkRead(ctx, []int{chs[0].ID}, false))
	t.Logf("set page %v", c.SetPage(ctx, chs[0].ID, 3))
	t.Logf("update %v", c.UpdateLibrary(ctx))
	jobs, err := c.Updating(ctx)
	t.Logf("updating %+v %v", jobs, err)
	t.Logf("download %v", c.Download(ctx, []int{chs[len(chs)-1].ID}))
	on, q, err := c.Downloads(ctx)
	t.Logf("downloads running=%v queue=%d %v", on, len(q), err)
	exts, stores, err := c.Extensions(ctx, false)
	t.Logf("extensions %d stores %d %v", len(exts), len(stores), err)
	exts, stores, err = c.Extensions(ctx, true)
	t.Logf("extensions (refresh) %d stores %d %v", len(exts), len(stores), err)
	t.Logf("set extension on unknown package (schema check): %v", c.SetExtension(ctx, "w5f.nonexistent", "install"))
	t.Logf("remove unknown store (schema check): %v", c.RemoveStore(ctx, "https://example.invalid/index.min.json"))
	t.Logf("unfollow %v", c.Follow(ctx, []int{ms[0].ID}, false))
}
