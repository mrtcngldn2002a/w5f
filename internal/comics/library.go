package comics

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"w5f/internal/store"
)

// Root is the comics folder: W5F_COMICS, else ~/Archive/Comics. Suwayomi's
// downloads and its Local source live inside it, so they are scanned too.
func Root() string {
	if d := os.Getenv("W5F_COMICS"); d != "" {
		return d
	}
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Join(h, "Archive", "Comics")
	}
	return "Comics"
}

// External reports files the library lists but a system viewer opens.
func External(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".pdf", ".djvu":
		return true
	}
	return false
}

// Scan brings the library in step with the folder: new files are read once
// (pages, ComicInfo), known ones are kept with their progress, gone ones are
// hidden. Image folders count as one comic each.
func Scan(db *store.DB, root string) (int, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return 0, err
	}
	seen := map[string]bool{}
	n := 0
	add := func(p string, isDir bool) {
		seen[p] = true
		if old, ok := db.ComicByPath(p); ok && (old.Pages > 0 || External(p)) {
			n++
			return
		}
		c := store.Comic{Path: p}
		var info Info
		if !External(p) {
			pages, i, err := Open(p)
			if err != nil {
				return // unreadable files are not listed
			}
			c.Pages, info = pages.Len(), i
			pages.Close()
		}
		c.Series, c.Number, c.Title = describe(root, p, isDir, info)
		c.RTL = info.RTL()
		if _, err := db.UpsertComic(c); err == nil {
			n++
		}
	}
	err := filepath.WalkDir(root, func(p string, de os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := de.Name()
		if de.IsDir() {
			if p != root && (strings.HasPrefix(name, ".") || name == "data") {
				return filepath.SkipDir
			}
			if p != root && imageFolder(p) {
				add(p, true)
				return filepath.SkipDir
			}
			return nil
		}
		if IsComicFile(name) || External(name) {
			add(p, false)
		}
		return nil
	})
	if err != nil {
		return n, err
	}
	return n, db.MarkComicsMissingExcept(seen)
}

// imageFolder is a folder that holds pages and no further folders.
func imageFolder(dir string) bool {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	pages := 0
	for _, e := range ents {
		if e.IsDir() {
			return false
		}
		if IsImage(e.Name()) {
			pages++
		}
	}
	return pages >= 2
}

var reNumber = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(?:ch(?:apter)?|vol(?:ume)?|v|#|no\.?|issue)?\s*0*(\d+(?:\.\d+)?)(?:[^0-9]*)$`)

// describe names a comic: ComicInfo first, else the folder is the series and
// the last number in the name is the issue.
func describe(root, p string, isDir bool, info Info) (series string, number float64, title string) {
	base := filepath.Base(p)
	if !isDir {
		base = strings.TrimSuffix(base, filepath.Ext(base))
	}
	parent := filepath.Dir(p)
	series = strings.TrimSpace(info.Series)
	if series == "" {
		if parent == root || filepath.Clean(parent) == filepath.Clean(root) {
			series = strings.TrimSpace(reNumber.ReplaceAllString(base, ""))
			if series == "" {
				series = base
			}
		} else {
			series = filepath.Base(parent)
		}
	}
	title = strings.TrimSpace(info.Title)
	if title == "" {
		title = base
	}
	if f, err := strconv.ParseFloat(strings.TrimSpace(info.Number), 64); err == nil {
		number = f
	} else if m := reNumber.FindStringSubmatch(base); m != nil {
		number, _ = strconv.ParseFloat(m[1], 64)
	}
	return series, number, title
}
