// Package places keeps the owner's own places in the Reading Room
// (2026-10-15, asked for with "any site anywhere"): sites and pages pinned
// with g → add, shown on the Reading Room under "Your places".
package places

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"w5f/internal/doc"
	"w5f/internal/store"
)

// Place is a pinned site or page.
type Place struct {
	Name  string `toml:"name"`
	URL   string `toml:"url"`
	Added string `toml:"added,omitempty"`
}

// Path is places.toml in the data folder.
func Path() string { return filepath.Join(store.DataDir(), "places.toml") }

// Load reads the places; no file is none.
func Load() ([]Place, error) {
	var f struct {
		Place []Place `toml:"place"`
	}
	if _, err := toml.DecodeFile(Path(), &f); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("%s: %w", Path(), err)
	}
	var out []Place
	for _, p := range f.Place {
		if strings.TrimSpace(p.URL) != "" {
			if strings.TrimSpace(p.Name) == "" {
				p.Name = p.URL
			}
			out = append(out, p)
		}
	}
	return out, nil
}

const header = `# Your places in the Reading Room. W5F writes this file when you pin a
# page (g → add on it); it can be edited by hand too: name, url, in the
# order they are shown.

`

func save(ps []Place) error {
	var b bytes.Buffer
	b.WriteString(header)
	if err := toml.NewEncoder(&b).Encode(struct {
		Place []Place `toml:"place"`
	}{ps}); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(Path()), 0o755); err != nil {
		return err
	}
	tmp := Path() + ".tmp"
	if err := os.WriteFile(tmp, b.Bytes(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, Path())
}

// IsTarget reports the places' addresses.
func IsTarget(t string) bool { return t == "w5f:places" || strings.HasPrefix(t, "w5f:places/") }

// Route pins (w5f:places/add?url=…&name=…), unpins, asking first
// (w5f:places/remove?url=…), moves up (w5f:places/up?url=…) and lists.
func Route(target string) (*doc.Document, error) {
	u, err := url.Parse(target)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	ps, err := Load()
	if err != nil {
		return nil, err
	}
	addr := strings.TrimSpace(q.Get("url"))
	i := slices.IndexFunc(ps, func(p Place) bool { return p.URL == addr })
	switch u.Opaque {
	case "places":
		return listDoc(ps, ""), nil
	case "places/add":
		if addr == "" {
			return nil, errors.New("which page? g → add <address>")
		}
		name := strings.Join(strings.Fields(q.Get("name")), " ")
		if name == "" {
			name = hostOf(addr)
		}
		if i >= 0 {
			ps[i].Name = name
			if err := save(ps); err != nil {
				return nil, err
			}
			return listDoc(ps, "“"+name+"” is already in the Reading Room."), nil
		}
		ps = append(ps, Place{Name: name, URL: addr, Added: time.Now().Format("2006-01-02")})
		if err := save(ps); err != nil {
			return nil, err
		}
		return listDoc(ps, "Pinned “"+name+"” to the Reading Room (1), under Your places."), nil
	case "places/up":
		if i > 0 {
			ps[i-1], ps[i] = ps[i], ps[i-1]
			if err := save(ps); err != nil {
				return nil, err
			}
		}
		return listDoc(ps, ""), nil
	case "places/remove":
		if i < 0 {
			return listDoc(ps, "That page is not pinned."), nil
		}
		if q.Get("sure") != "yes" { // every removal asks first, as in the Weeding Room
			v := url.Values{"url": {addr}}
			d := &doc.Document{Title: "Unpin this place?", URL: "w5f:places/remove?" + v.Encode(), Origin: "local", Lang: "en"}
			v.Set("sure", "yes")
			d.Blocks = []doc.Block{
				doc.Paragraph{Text: doc.Inline{{Text: ps[i].Name + " · " + addr}}},
				doc.Paragraph{Text: doc.Inline{{Text: "It leaves the Reading Room; the page itself is not touched.", Style: doc.Italic}}},
				doc.Paragraph{Text: doc.Inline{{Text: "No, keep it", Link: link(d, "w5f:places", "keep")}, {Text: " · "},
					{Text: "Yes, unpin it", Style: doc.Bold, Link: link(d, "w5f:places/remove?"+v.Encode(), "unpin")}}},
			}
			return d, nil
		}
		name := ps[i].Name
		ps = slices.Delete(ps, i, i+1)
		if err := save(ps); err != nil {
			return nil, err
		}
		return listDoc(ps, "Unpinned “"+name+"”."), nil
	}
	return nil, errors.New("unknown address: " + target)
}

func link(d *doc.Document, href, text string) int {
	d.Links = append(d.Links, doc.Link{Href: href, Text: text})
	return len(d.Links)
}

func listDoc(ps []Place, note string) *doc.Document {
	d := &doc.Document{Title: "Your places", URL: "w5f:places", Origin: "local", Lang: "en"}
	if note != "" {
		d.Blocks = append(d.Blocks, doc.Notice{Kind: "info", Text: note})
	}
	d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: "Pages pinned to the Reading Room. Pin one with g → add on it (or g → add <address>).", Style: doc.Italic}}})
	if len(ps) == 0 {
		d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: "Nothing pinned yet.", Style: doc.Italic}}})
	}
	var items [][]doc.Block
	for i, p := range ps {
		v := url.Values{"url": {p.URL}}.Encode()
		in := doc.Inline{{Text: p.Name, Style: doc.Bold, Link: link(d, p.URL, p.Name)}, {Text: "  · ", Style: doc.Italic}}
		if i > 0 {
			in = append(in, doc.Span{Text: "move up", Link: link(d, "w5f:places/up?"+v, "move up "+p.Name)}, doc.Span{Text: " · "})
		}
		in = append(in, doc.Span{Text: "unpin", Link: link(d, "w5f:places/remove?"+v, "unpin "+p.Name)})
		items = append(items, []doc.Block{doc.Paragraph{Text: in}})
	}
	if len(items) > 0 {
		d.Blocks = append(d.Blocks, doc.List{Items: items})
	}
	d.Blocks = append(d.Blocks, doc.Rule{}, doc.Paragraph{Text: doc.Inline{{Text: "Kept in ", Style: doc.Italic}, {Text: Path(), Style: doc.Code},
		{Text: " — it can be edited by hand.", Style: doc.Italic}}})
	return d
}

func hostOf(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return strings.TrimPrefix(u.Hostname(), "www.")
	}
	return raw
}
