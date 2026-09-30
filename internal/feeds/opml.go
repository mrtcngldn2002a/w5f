package feeds

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/BurntSushi/toml"
	"golang.org/x/net/html/charset"

	"w5f/internal/store"
)

// OPML is how feed readers move subscription lists: a tree of outlines,
// feeds carry xmlUrl, the outlines around them are folders.
type outline struct {
	Text     string    `xml:"text,attr"`
	Title    string    `xml:"title,attr,omitempty"`
	Type     string    `xml:"type,attr,omitempty"`
	XMLURL   string    `xml:"xmlUrl,attr,omitempty"`
	HTMLURL  string    `xml:"htmlUrl,attr,omitempty"`
	Language string    `xml:"language,attr,omitempty"`
	Outlines []outline `xml:"outline"`
}

type opml struct {
	XMLName xml.Name `xml:"opml"`
	Version string   `xml:"version,attr"`
	Head    struct {
		Title       string `xml:"title"`
		DateCreated string `xml:"dateCreated,omitempty"`
	} `xml:"head"`
	Body struct {
		Outlines []outline `xml:"outline"`
	} `xml:"body"`
}

// OPMLFeed is one subscription read from an OPML file.
type OPMLFeed struct {
	Name, URL, Site, Lang string
	Folder                string // the folder it was in ("" at the top)
}

// ParseOPML reads the subscriptions of an OPML file.
func ParseOPML(r io.Reader) ([]OPMLFeed, error) {
	var o opml
	dec := xml.NewDecoder(r)
	dec.CharsetReader = charset.NewReaderLabel
	dec.Strict = false // hand-edited files often carry bare & in titles
	if err := dec.Decode(&o); err != nil {
		return nil, fmt.Errorf("not an OPML file (%v)", err)
	}
	var out []OPMLFeed
	var walk func(xs []outline, folder string)
	walk = func(xs []outline, folder string) {
		for _, x := range xs {
			name := strings.TrimSpace(firstNonEmpty(x.Title, x.Text))
			if u := strings.TrimSpace(x.XMLURL); u != "" {
				out = append(out, OPMLFeed{Name: firstNonEmpty(name, u), URL: u, Site: strings.TrimSpace(x.HTMLURL),
					Lang: strings.TrimSpace(x.Language), Folder: folder})
			}
			if len(x.Outlines) > 0 {
				walk(x.Outlines, firstNonEmpty(name, folder)) // the innermost folder names the shelf
			}
		}
	}
	walk(o.Body.Outlines, "")
	if len(out) == 0 {
		return nil, errors.New("no feeds in this OPML file")
	}
	return out, nil
}

// ImportReport says what an import did.
type ImportReport struct {
	Added     []Feed
	Shelves   []Shelf // new shelves
	Duplicate []string
	File      string // the user catalog written
}

// sameFeed compares feed addresses loosely: scheme, www. and a closing
// slash do not make a different feed.
func sameFeed(a, b string) bool {
	norm := func(s string) string {
		u, err := url.Parse(strings.TrimSpace(s))
		if err != nil || u.Host == "" {
			return strings.ToLower(strings.TrimSpace(s))
		}
		return strings.TrimPrefix(strings.ToLower(u.Host), "www.") + strings.TrimSuffix(u.EscapedPath(), "/") + "?" + u.RawQuery
	}
	return norm(a) == norm(b)
}

func slug(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		if b.Len() >= 32 {
			break
		}
		switch {
		case r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			b.WriteRune(r)
			dash = false
		case b.Len() > 0 && !dash:
			b.WriteByte('-')
			dash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "feed"
	}
	return out
}

func unique(base string, taken map[string]bool) string {
	id := base
	for n := 2; taken[id]; n++ {
		id = fmt.Sprintf("%s-%d", base, n)
	}
	taken[id] = true
	return id
}

// Import adds the feeds of an OPML file to the user catalog (feeds.toml)
// under shelves named after their folders ("Imported" for loose ones).
// Feeds already in the catalog are skipped; the user's file is appended to,
// never rewritten, and restored if the result does not load.
func Import(c *Catalog, opmlPath string) (ImportReport, error) {
	rep := ImportReport{File: UserCatalogPath()}
	f, err := os.Open(opmlPath)
	if err != nil {
		return rep, err
	}
	subs, err := ParseOPML(f)
	f.Close()
	if err != nil {
		return rep, err
	}
	feedIDs, shelfIDs := map[string]bool{}, map[string]bool{}
	for _, fd := range c.Feeds {
		feedIDs[fd.ID] = true
	}
	for _, s := range c.Shelves {
		shelfIDs[s.ID] = true
	}
	shelfFor := func(label string) string {
		if label == "" {
			label = "Imported"
		}
		for _, s := range append(c.Shelves, rep.Shelves...) {
			if strings.EqualFold(s.Label, label) {
				return s.ID
			}
		}
		s := Shelf{ID: unique(slug(label), shelfIDs), Label: label}
		rep.Shelves = append(rep.Shelves, s)
		return s.ID
	}
	have := func(u string) bool {
		for _, fd := range append(c.Feeds, rep.Added...) {
			for _, x := range fd.URL {
				if sameFeed(x, u) {
					return true
				}
			}
		}
		return false
	}
	for _, s := range subs {
		if have(s.URL) {
			rep.Duplicate = append(rep.Duplicate, s.Name)
			continue
		}
		rep.Added = append(rep.Added, Feed{ID: unique(slug(s.Name), feedIDs), Name: s.Name,
			Shelf: shelfFor(s.Folder), Lang: s.Lang, URL: []string{s.URL}, Site: s.Site})
	}
	if len(rep.Added) == 0 {
		return rep, nil
	}
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "\n# Imported from %s on %s\n", filepath.Base(opmlPath), time.Now().Format("2006-01-02"))
	if err := toml.NewEncoder(&buf).Encode(Catalog{Shelves: rep.Shelves, Feeds: rep.Added}); err != nil {
		return rep, err
	}
	old, err := os.ReadFile(rep.File)
	if err != nil && !os.IsNotExist(err) {
		return rep, err
	}
	if err := os.MkdirAll(filepath.Dir(rep.File), 0o755); err != nil {
		return rep, err
	}
	if err := os.WriteFile(rep.File, append(append([]byte{}, old...), buf.Bytes()...), 0o644); err != nil {
		return rep, err
	}
	if _, err := LoadCatalog(); err != nil {
		if old == nil {
			os.Remove(rep.File)
		} else {
			os.WriteFile(rep.File, old, 0o644)
		}
		return rep, fmt.Errorf("the catalog would not load after the import, nothing was changed: %v", err)
	}
	return rep, nil
}

// Export writes the catalog as OPML 2.0, one folder per shelf. A feed is
// given the address that last worked when the database knows it. It returns
// how many feeds it wrote.
func Export(c *Catalog, db *store.DB, w io.Writer) (int, error) {
	n := 0
	var o opml
	o.Version = "2.0"
	o.Head.Title = "W5F periodicals"
	o.Head.DateCreated = time.Now().UTC().Format(time.RFC1123Z)
	for _, s := range c.Shelves {
		folder := outline{Text: s.Label}
		for _, fd := range c.Feeds {
			if fd.Shelf != s.ID {
				continue
			}
			u := ""
			if db != nil {
				u = db.Feed(fd.ID).URL
			}
			if u == "" && len(fd.URL) > 0 {
				u = fd.URL[0]
			}
			if u == "" {
				continue // found by discovery only and never synced
			}
			n++
			folder.Outlines = append(folder.Outlines, outline{Text: fd.Name, Title: fd.Name, Type: "rss",
				XMLURL: u, HTMLURL: fd.Site, Language: fd.Lang})
		}
		if len(folder.Outlines) > 0 {
			o.Body.Outlines = append(o.Body.Outlines, folder)
		}
	}
	io.WriteString(w, xml.Header)
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(o); err != nil {
		return n, err
	}
	_, err := io.WriteString(w, "\n")
	return n, err
}
