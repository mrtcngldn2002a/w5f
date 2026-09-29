// Package personal keeps the owner's own data — reading queue, notes,
// clippings and saved pages — as Markdown files Obsidian can open.
package personal

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"

	"w5f/internal/store"
)

// Dir is the notes folder: W5F_NOTES, else `notes` in <DataDir>/config.toml,
// else ~/Archive/Notes.
func Dir() string {
	if d := os.Getenv("W5F_NOTES"); d != "" {
		return d
	}
	var cfg struct {
		Notes string `toml:"notes"`
	}
	if _, err := toml.DecodeFile(filepath.Join(store.DataDir(), "config.toml"), &cfg); err == nil && cfg.Notes != "" {
		return cfg.Notes
	}
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Join(h, "Archive", "Notes")
	}
	return "Notes"
}

// Rel shows a path relative to the notes folder ("Notes/SCP-173.md").
func Rel(p string) string {
	if r, err := filepath.Rel(Dir(), p); err == nil {
		return filepath.ToSlash(r)
	}
	return p
}

// FileURL is a file:// address the reader opens.
func FileURL(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		abs = p
	}
	return (&url.URL{Scheme: "file", Path: "/" + strings.TrimPrefix(filepath.ToSlash(abs), "/")}).String()
}

// Source is what a note, clipping or saved page is about.
type Source struct {
	Title, URL, Catalog, Kind string
}

var reUnsafe = regexp.MustCompile(`[<>:"/\\|?*#^\[\]\x00-\x1f]+`)

// FileName makes a title safe as a file name on every OS and in Obsidian
// links; at most 80 characters.
func FileName(title string) string {
	name := strings.Join(strings.Fields(reUnsafe.ReplaceAllString(title, " ")), " ")
	name = strings.Trim(name, ". ")
	if r := []rune(name); len(r) > 80 {
		name = strings.TrimSpace(string(r[:80]))
	}
	if name == "" {
		name = "Untitled"
	}
	return name
}

// pathFor is the file for a source in folder: "<Title>.md", or
// "<Title> (2).md" … when that name belongs to another source.
func pathFor(folder string, s Source) string {
	base := FileName(s.Title)
	for i := 1; ; i++ {
		name := base
		if i > 1 {
			name = fmt.Sprintf("%s (%d)", base, i)
		}
		p := filepath.Join(folder, name+".md")
		data, err := os.ReadFile(p)
		if err != nil {
			return p
		}
		if front, _ := SplitFront(string(data)); front["url"] == s.URL {
			return p
		}
	}
}

// SplitFront separates YAML frontmatter (simple "key: value" lines) from
// the body.
func SplitFront(data string) (front map[string]string, body string) {
	data = strings.ReplaceAll(data, "\r\n", "\n")
	front = map[string]string{}
	if !strings.HasPrefix(data, "---\n") {
		return front, data
	}
	end := strings.Index(data[4:], "\n---")
	if end < 0 {
		return front, data
	}
	for _, ln := range strings.Split(data[4:4+end], "\n") {
		k, v, ok := strings.Cut(ln, ":")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		if strings.HasPrefix(v, `"`) {
			if u, err := strconv.Unquote(v); err == nil {
				v = u
			}
		}
		front[strings.TrimSpace(k)] = v
	}
	rest := data[4+end+4:]
	return front, strings.TrimPrefix(rest, "\n")
}

type kv struct{ k, v string }

func frontmatter(kvs []kv, tags string) string {
	var b strings.Builder
	b.WriteString("---\n")
	for _, x := range kvs {
		if x.v != "" {
			b.WriteString(x.k + ": " + strconv.Quote(x.v) + "\n")
		}
	}
	b.WriteString("tags: [" + tags + "]\n---\n")
	return b.String()
}

func appendFile(p, s string) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(s); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
