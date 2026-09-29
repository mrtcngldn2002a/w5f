package sitecat

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"net/url"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"

	"w5f/internal/fetch"
)

// indexFile is one book file of a directory catalog.
type indexFile struct {
	Path string `json:"path"` // absolute URL
	Size string `json:"size,omitempty"`
}

// dirIndex is the stored file list of a directory catalog.
type dirIndex struct {
	Root    string      `json:"root"`
	Built   time.Time   `json:"built"`
	Partial bool        `json:"partial"`
	Folders int         `json:"folders"`
	Failed  int         `json:"failed"`
	Files   []indexFile `json:"files"`
}

// crawlGap is the pause between folder requests while indexing.
var crawlGap = time.Second

var (
	reIndexTitle = regexp.MustCompile(`(?i)^(index of|directory listing for) /`)
	reSizeCell   = regexp.MustCompile(`(?i)^\d+(\.\d+)?\s?[KMGT]i?B?$|^\d+$`)
	reSizeTail   = regexp.MustCompile(`(\d+(?:\.\d+)?[KMGT]?)\s*$`)
)

// readListing reads a directory listing: sub-folders below the current
// page and book files below root. ok is false for other pages.
func readListing(body []byte, pageURL string, root *url.URL) (folders []string, files []indexFile, ok bool) {
	gq, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil, nil, false
	}
	parent := gq.Find(`a[href="../"], a[href=".."]`).Length() > 0
	if !reIndexTitle.MatchString(collapse(gq.Find("title").First().Text())) &&
		!(parent && gq.Find("pre a[href], table a[href]").Length() >= 3) {
		return nil, nil, false
	}
	base, err := url.Parse(pageURL)
	if err != nil {
		return nil, nil, false
	}
	seen := map[string]bool{}
	gq.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		raw := strings.TrimSpace(a.AttrOr("href", ""))
		if raw == "" || strings.HasPrefix(raw, "?") || strings.HasPrefix(raw, "#") {
			return // sort links and anchors
		}
		u, err := base.Parse(raw)
		if err != nil || u.Host != root.Host || !strings.HasPrefix(u.Path, root.Path) {
			return // other hosts, or outside the catalog's tree
		}
		u.RawQuery, u.Fragment = "", ""
		if seen[u.String()] {
			return
		}
		seen[u.String()] = true
		if strings.HasSuffix(u.Path, "/") {
			if len(u.Path) > len(base.Path) && strings.HasPrefix(u.Path, base.Path) {
				folders = append(folders, u.String()) // strictly deeper only
			}
			return
		}
		if fileFormatOf(u) != "" {
			files = append(files, indexFile{Path: u.String(), Size: sizeNear(a)})
		}
	})
	return folders, files, true
}

// sizeNear reads a file's size from its table row or pre line.
func sizeNear(a *goquery.Selection) string {
	if tr := a.Closest("tr"); tr.Length() > 0 {
		size := ""
		tr.Find("td").Each(func(_ int, td *goquery.Selection) {
			if t := collapse(td.Text()); reSizeCell.MatchString(t) {
				size = t
			}
		})
		return size
	}
	if n := a.Get(0).NextSibling; n != nil && n.Type == html.TextNode {
		line := strings.TrimSpace(strings.SplitN(n.Data, "\n", 2)[0])
		if m := reSizeTail.FindStringSubmatch(line); m != nil {
			return m[1]
		}
	}
	return ""
}

// crawl indexes a listing tree breadth-first within the limits. On
// cancellation the index so far is returned, marked partial.
func crawl(ctx context.Context, f *fetch.Fetcher, root string, maxFolders, maxDepth int, progress func(folders, files int)) (*dirIndex, error) {
	ru, err := url.Parse(root)
	if err != nil {
		return nil, err
	}
	if !strings.HasSuffix(ru.Path, "/") {
		ru.Path = ru.Path[:strings.LastIndex(ru.Path, "/")+1]
	}
	ix := &dirIndex{Root: ru.String(), Built: time.Now().UTC()}
	type folder struct {
		u     string
		depth int
	}
	queue := []folder{{ru.String(), 0}}
	seen := map[string]bool{ru.String(): true}
	for len(queue) > 0 {
		if ix.Folders >= maxFolders {
			ix.Partial = true
			break
		}
		if ix.Folders > 0 && crawlGap > 0 {
			select {
			case <-ctx.Done():
			case <-time.After(crawlGap):
			}
		}
		if err := ctx.Err(); err != nil {
			ix.Partial = true
			return ix, err
		}
		it := queue[0]
		queue = queue[1:]
		u, _ := url.Parse(it.u)
		resp, err := f.Get(ctx, u, fetch.Options{})
		ix.Folders++
		if err != nil {
			if ctx.Err() != nil {
				ix.Partial = true
				return ix, ctx.Err()
			}
			ix.Failed++
			continue
		}
		folders, files, ok := readListing(resp.Body, resp.URL.String(), ru)
		if !ok {
			ix.Failed++
			continue
		}
		ix.Files = append(ix.Files, files...)
		for _, sub := range folders {
			if !seen[sub] && it.depth+1 <= maxDepth {
				seen[sub] = true
				queue = append(queue, folder{sub, it.depth + 1})
			}
		}
		if progress != nil {
			progress(ix.Folders, len(ix.Files))
		}
	}
	return ix, nil
}

func saveIndex(path string, ix *dirIndex) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(ix)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func loadIndex(path string) (*dirIndex, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var ix dirIndex
	if err := json.Unmarshal(b, &ix); err != nil {
		return nil, err
	}
	return &ix, nil
}

var foldReplacer = strings.NewReplacer(
	"_", " ", "-", " ", ".", " ", "İ", "i", "I", "i", "ı", "i", "ş", "s", "ğ", "g", "ü", "u", "ö", "o", "ç", "c",
	"á", "a", "à", "a", "â", "a", "ä", "a", "ã", "a", "å", "a", "é", "e", "è", "e", "ê", "e", "ë", "e",
	"í", "i", "ì", "i", "î", "i", "ï", "i", "ó", "o", "ò", "o", "ô", "o", "õ", "o", "ú", "u", "ù", "u", "û", "u",
	"ñ", "n", "ý", "y", "ÿ", "y", "æ", "ae", "œ", "oe", "ß", "ss")

// fold makes file names and queries comparable: lower case, no accents,
// separators as spaces.
func fold(s string) string {
	return foldReplacer.Replace(strings.ToLower(foldReplacer.Replace(s)))
}
