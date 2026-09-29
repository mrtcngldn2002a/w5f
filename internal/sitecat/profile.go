// Package sitecat turns any book website into a Library catalog: it detects
// how the site is searched, learns how its result lists look, searches all
// result pages and finds download links. What it learns is kept as an
// editable TOML profile per site.
package sitecat

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"w5f/internal/store"
)

// Profile is everything W5F knows about one site catalog.
type Profile struct {
	ID       string       `toml:"id"`
	Name     string       `toml:"name"`
	Home     string       `toml:"home"`
	Added    time.Time    `toml:"added"`
	Search   SearchSpec   `toml:"search"`
	Layout   Layout       `toml:"layout"`
	Download DownloadSpec `toml:"download"`
}

// / SearchSpec says how to search the site. Templates contain {q}.
type SearchSpec struct {
	// Kind picks the backend: opensearch | opds | form | post | wordpress |
	// archive | dspace | index | browse | engine | libgen | anna.
	Kind     string `toml:"kind"`
	Template string `toml:"template"`
	// Fields holds "author"/"title" templates (for POST: body templates).
	Fields     map[string]string `toml:"fields,omitempty"`
	MaxPages   int               `toml:"max_pages"`
	MaxResults int               `toml:"max_results"`
	Method     string            `toml:"method,omitempty"`      // "POST" for post forms; default GET
	Body       string            `toml:"body,omitempty"`        // POST body template with {q}
	API        string            `toml:"api,omitempty"`         // dspace: REST base
	Collection string            `toml:"collection,omitempty"`  // archive: restrict to a collection
	Index      string            `toml:"index,omitempty"`       // index: local file list
	MaxFolders int               `toml:"max_folders,omitempty"` // index: folders read when indexing
}

// Layout is the learned shape of a result list: the container's signature
// path and the signature of one result element.
type Layout struct {
	Parent string `toml:"parent"`
	Item   string `toml:"item"`
}

// DownloadSpec orders the formats to fetch.
type DownloadSpec struct {
	Prefer []string `toml:"prefer"`
}

// DefaultPrefer is the default format order.
var DefaultPrefer = []string{"epub", "pdf", "txt", "mobi", "azw3", "fb2", "djvu", "cbz", "cbr"}

type file struct {
	Catalogs []Profile `toml:"catalog"`
}

// Path is the profile file in the W5F data directory.
func Path() string { return filepath.Join(store.DataDir(), "catalogs.toml") }

// / ApplyDefaults fills unset limits and preferences.
func (p *Profile) ApplyDefaults() {
	if p.Search.Kind == "none" { // round-1 name
		p.Search.Kind = "browse"
	}
	pages, results := 10, 300
	if p.Search.Kind == "engine" { // a search engine's index is shallow
		pages, results = 3, 60
	}
	if p.Search.MaxPages <= 0 {
		p.Search.MaxPages = pages
	}
	if p.Search.MaxResults <= 0 {
		p.Search.MaxResults = results
	}
	if p.Search.Kind == "index" && p.Search.MaxFolders <= 0 {
		p.Search.MaxFolders = 150
	}
	if len(p.Download.Prefer) == 0 {
		p.Download.Prefer = append([]string{}, DefaultPrefer...)
	}
}

// LoadAll reads all profiles; a missing file means none.
func LoadAll(path string) ([]Profile, error) {
	var f file
	if _, err := toml.DecodeFile(path, &f); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	for i := range f.Catalogs {
		f.Catalogs[i].ApplyDefaults()
	}
	return f.Catalogs, nil
}

// SaveAll writes all profiles atomically.
func SaveAll(path string, ps []Profile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	w, err := os.Create(tmp)
	if err != nil {
		return err
	}
	w.WriteString("# W5F site catalogs — added with g → catalog-add <url>. Safe to edit.\n\n")
	if err := toml.NewEncoder(w).Encode(file{Catalogs: ps}); err != nil {
		w.Close()
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Find returns the index of a profile id, or -1.
func Find(ps []Profile, id string) int {
	for i := range ps {
		if ps[i].ID == id {
			return i
		}
	}
	return -1
}

// IDFor derives a short unique id from a site address ("books-example-org").
func IDFor(home string, existing []Profile) string {
	base := "catalog"
	if u, err := url.Parse(home); err == nil && u.Hostname() != "" {
		base = strings.ReplaceAll(strings.TrimPrefix(strings.ToLower(u.Hostname()), "www."), ".", "-")
	}
	id := base
	for i := 2; Find(existing, id) >= 0; i++ {
		id = fmt.Sprintf("%s-%d", base, i)
	}
	return id
}
