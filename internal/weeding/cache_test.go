package weeding

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, p string, size int, mod time.Time) {
	t.Helper()
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(strings.Repeat("x", size)), 0o644); err != nil {
		t.Fatal(err)
	}
	os.Chtimes(p, mod, mod)
}

// Other programs' folders beside W5F's cache (the laptop's uv,
// flaresolverr-install, byparr-install) are never measured, emptied or
// trimmed.
func cacheWithNeighbours(t *testing.T) (string, []string) {
	dir := t.TempDir()
	old := time.Now().Add(-48 * time.Hour)
	var others []string
	for _, o := range []string{"uv/python/bin/python3", "flaresolverr-install/flaresolverr", "byparr-install/byparr.py", "keep-me.txt"} {
		p := filepath.Join(dir, filepath.FromSlash(o))
		write(t, p, 5000, old.Add(-time.Hour)) // older than anything of W5F's
		others = append(others, p)
	}
	return dir, others
}

func intact(t *testing.T, others []string) {
	t.Helper()
	for _, p := range others {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s was touched: %v", p, err)
		}
	}
}

func TestClearCacheTouchesOnlyW5F(t *testing.T) {
	dir, others := cacheWithNeighbours(t)
	now := time.Now()
	write(t, filepath.Join(dir, "http", "aa", "aa1.json"), 100, now)
	write(t, filepath.Join(dir, "http", "aa", "aa1.body"), 900, now)
	write(t, filepath.Join(dir, "smallweb", "s.json"), 300, now)
	write(t, filepath.Join(dir, "images", "0102", "1.jpg"), 400, now)
	write(t, filepath.Join(dir, "pdftext", "b.txt"), 200, now)
	write(t, filepath.Join(dir, "internet-fiction", "c.html"), 100, now) // v1's leftover cache
	if size, n := CacheSize(dir); size != 2000 || n != 6 {
		t.Errorf("size %d, files %d: other folders were counted", size, n)
	}
	freed, n, err := ClearCache(dir)
	if err != nil || freed != 2000 || n != 6 {
		t.Errorf("cleared %d bytes, %d files, %v", freed, n, err)
	}
	if size, _ := CacheSize(dir); size != 0 {
		t.Errorf("left %d", size)
	}
	if _, err := os.Stat(filepath.Join(dir, "http")); err != nil {
		t.Error("the http folder itself is kept")
	}
	intact(t, others)
}

func TestTrimCacheOldestReadFirst(t *testing.T) {
	dir, others := cacheWithNeighbours(t)
	now := time.Now()
	for i, age := range []time.Duration{5, 4, 3, 2, 1} { // hours since read
		base := filepath.Join(dir, "http", "ab", string(rune('a'+i)))
		write(t, base+".json", 100, now.Add(-age*time.Hour))
		write(t, base+".body", 900, now.Add(-age*time.Hour))
	}
	if freed, _ := TrimCache(dir, 10000); freed != 0 {
		t.Error("trimmed under the limit")
	}
	freed, n := TrimCache(dir, 3500) // 5000 → at most 3150: two pages go
	if freed != 2000 || n != 4 {
		t.Errorf("freed %d, %d files", freed, n)
	}
	for _, gone := range []string{"a", "b"} {
		if _, err := os.Stat(filepath.Join(dir, "http", "ab", gone+".body")); err == nil {
			t.Errorf("%s (read longest ago) kept", gone)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "http", "ab", "e.json")); err != nil {
		t.Error("the page read last went")
	}
	intact(t, others)
}

func TestSymlinkedPartNotFollowed(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	write(t, filepath.Join(outside, "precious.txt"), 10, time.Now())
	if err := os.Symlink(outside, filepath.Join(dir, "http")); err != nil {
		t.Skip("no symlinks here:", err)
	}
	ClearCache(dir)
	if _, err := os.Stat(filepath.Join(outside, "precious.txt")); err != nil {
		t.Error("a linked folder was followed")
	}
}
