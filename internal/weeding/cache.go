// Package weeding is The Weeding Room (asked for by the owner, 2026-10-02):
// what W5F keeps on disk, how large it has grown, and the removal of what
// is no longer wanted — the page cache emptied, a note, a book or a comic
// removed one at a time, each after asking. Nothing here removes in bulk
// what the owner made or gathered.
package weeding

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// CacheParts are the only folders inside the cache folder that W5F ever
// measures, empties or trims: its own four, and internet-fiction, the page
// cache W5F v1's Internet Fiction tool left (pages of 2026-09-27; nothing
// on the laptop uses it any more — checked 2026-10-02, the owner agreed).
// Anything else there belongs to other programs set up beside W5F (uv,
// flaresolverr-install, byparr-install on the laptop) and is never listed,
// measured or touched (the owner's rule, 2026-10-02).
var CacheParts = []string{"http", "smallweb", "images", "pdftext", "internet-fiction"}

// cacheFiles lists the files of W5F's own cache parts.
func cacheFiles(cacheDir string) []fileInfo {
	if cacheDir == "" {
		return nil
	}
	var out []fileInfo
	for _, part := range CacheParts {
		root := filepath.Join(cacheDir, part)
		filepath.WalkDir(root, func(p string, de fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if de.Type()&fs.ModeSymlink != 0 {
				return nil // a link could lead out of the cache: never followed, never removed
			}
			if de.IsDir() {
				return nil
			}
			if info, err := de.Info(); err == nil {
				out = append(out, fileInfo{path: p, size: info.Size(), mod: info.ModTime()})
			}
			return nil
		})
	}
	return out
}

type fileInfo struct {
	path string
	size int64
	mod  time.Time
}

// CacheSize is the size and number of files of W5F's own cache.
func CacheSize(cacheDir string) (int64, int) {
	var n int64
	fs := cacheFiles(cacheDir)
	for _, f := range fs {
		n += f.size
	}
	return n, len(fs)
}

// ClearCache removes every file of W5F's own cache parts, one by one (the
// folders themselves stay); it returns what was freed.
func ClearCache(cacheDir string) (int64, int, error) {
	var freed int64
	n := 0
	for _, f := range cacheFiles(cacheDir) {
		if err := os.Remove(f.path); err != nil {
			continue
		}
		freed += f.size
		n++
	}
	return freed, n, nil
}

// TrimCache brings W5F's own cache under limit bytes (≤ 0: no limit),
// removing the pages opened longest ago first; a page's .json and .body
// go together. Pages are touched when read from the cache, so "opened" is
// read, not only fetched.
func TrimCache(cacheDir string, limit int64) (int64, int) {
	if limit <= 0 {
		return 0, 0
	}
	files := cacheFiles(cacheDir)
	var total int64
	groups := map[string]*group{}
	for _, f := range files {
		total += f.size
		key := f.path[:len(f.path)-len(filepath.Ext(f.path))]
		g := groups[key]
		if g == nil {
			g = &group{}
			groups[key] = g
		}
		g.files = append(g.files, f)
		g.size += f.size
		if f.mod.After(g.mod) {
			g.mod = f.mod
		}
	}
	if total <= limit {
		return 0, 0
	}
	var gs []*group
	for _, g := range groups {
		gs = append(gs, g)
	}
	sort.Slice(gs, func(i, j int) bool { return gs[i].mod.Before(gs[j].mod) })
	target := limit * 9 / 10 // a little room, so trimming is not needed again at once
	var freed int64
	n := 0
	for _, g := range gs {
		if total <= target {
			break
		}
		for _, f := range g.files {
			if os.Remove(f.path) == nil {
				total -= f.size
				freed += f.size
				n++
			}
		}
	}
	return freed, n
}

type group struct {
	files []fileInfo
	size  int64
	mod   time.Time
}
