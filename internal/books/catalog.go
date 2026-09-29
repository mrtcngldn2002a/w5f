package books

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"w5f/internal/fetch"
	"w5f/internal/store"
)

// Catalog endpoints (variables so tests can point at local servers).
var (
	GutenbergBase = "https://www.gutenberg.org"
	SEBase        = "https://standardebooks.org"
)

// Hit is one catalog search result.
type Hit struct {
	Source    string // "gutenberg:84" or "se:h-p-lovecraft/short-fiction"
	Title     string
	Author    string
	Extra     string // downloads, subjects…
	Available bool   // already in the local library
}

var reGutenbergID = regexp.MustCompile(`^/ebooks/(\d+)$`)

// SearchGutenberg queries Project Gutenberg's own search (fast, no JS). An
// empty query lists the most downloaded books.
func SearchGutenberg(ctx context.Context, f *fetch.Fetcher, q string, start int) ([]Hit, error) {
	u, _ := url.Parse(GutenbergBase + "/ebooks/search/")
	v := url.Values{}
	if q != "" {
		v.Set("query", q)
	} else {
		v.Set("sort_order", "downloads")
	}
	if start > 1 {
		v.Set("start_index", fmt.Sprint(start))
	}
	u.RawQuery = v.Encode()
	resp, err := f.Get(ctx, u, fetch.Options{})
	if err != nil {
		return nil, err
	}
	gq, err := goquery.NewDocumentFromReader(bytes.NewReader(resp.Body))
	if err != nil {
		return nil, err
	}
	var hits []Hit
	gq.Find("li.booklink").Each(func(_ int, li *goquery.Selection) {
		href := li.Find("a.link").AttrOr("href", "")
		m := reGutenbergID.FindStringSubmatch(href)
		if m == nil {
			return
		}
		hits = append(hits, Hit{
			Source: "gutenberg:" + m[1],
			Title:  clean(li.Find(".title").Text()),
			Author: clean(li.Find(".subtitle").Text()),
			Extra:  clean(li.Find(".extra").Text()),
		})
	})
	return hits, nil
}

// SearchSE queries Standard Ebooks (carefully produced public-domain EPUBs).
// An empty query lists the newest releases.
func SearchSE(ctx context.Context, f *fetch.Fetcher, q string, page int) ([]Hit, error) {
	u, _ := url.Parse(SEBase + "/ebooks")
	v := url.Values{}
	if q != "" {
		v.Set("query", q)
	} else {
		v.Set("sort", "newest")
	}
	if page > 1 {
		v.Set("page", fmt.Sprint(page))
	}
	u.RawQuery = v.Encode()
	resp, err := f.Get(ctx, u, fetch.Options{})
	if err != nil {
		return nil, err
	}
	gq, err := goquery.NewDocumentFromReader(bytes.NewReader(resp.Body))
	if err != nil {
		return nil, err
	}
	var hits []Hit
	gq.Find(`li[typeof="schema:Book"]`).Each(func(_ int, li *goquery.Selection) {
		about := strings.TrimPrefix(li.AttrOr("about", ""), "/ebooks/")
		if about == "" {
			return
		}
		hits = append(hits, Hit{
			Source: "se:" + about,
			Title:  clean(li.Find(`p > a > span[property="schema:name"]`).First().Text()),
			Author: clean(li.Find(`p.author span[property="schema:name"]`).First().Text()),
		})
	})
	return hits, nil
}

// Download fetches a catalog book into the library and records it. If the
// book is already there, it is returned without downloading again.
func Download(ctx context.Context, f *fetch.Fetcher, db *store.DB, src string) (store.Book, error) {
	if strings.HasPrefix(src, "libgen:") {
		src = strings.ToLower(src)
	}
	if b, ok := db.BookBySource(src); ok {
		if _, err := os.Stat(b.Path); err == nil {
			return b, nil
		}
	}
	var epubURL string
	switch {
	case strings.HasPrefix(src, "libgen:"):
		return downloadLibgen(ctx, Env{Fetcher: f, DB: db}, strings.TrimPrefix(src, "libgen:"))
	case strings.HasPrefix(src, "gutenberg:"):
		epubURL = GutenbergBase + "/ebooks/" + strings.TrimPrefix(src, "gutenberg:") + ".epub3.images"
	case strings.HasPrefix(src, "se:"):
		slug := strings.TrimPrefix(src, "se:")
		u, err := seEPUB(ctx, f, slug)
		if err != nil {
			return store.Book{}, err
		}
		epubURL = u
	default:
		return store.Book{}, errors.New("unknown catalog source " + src)
	}
	dir := LibraryDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return store.Book{}, err
	}
	tmp, err := os.CreateTemp(dir, ".download-*.epub")
	if err != nil {
		return store.Book{}, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := getTo(ctx, f.UserAgent, epubURL, tmp); err != nil {
		tmp.Close()
		return store.Book{}, err
	}
	tmp.Close()
	return AddFile(db, tmpName, "epub", "", "", src)
}

// seEPUB finds the standard (not "advanced", not Kobo) EPUB of an SE book.
func seEPUB(ctx context.Context, f *fetch.Fetcher, slug string) (string, error) {
	u, _ := url.Parse(SEBase + "/ebooks/" + slug)
	resp, err := f.Get(ctx, u, fetch.Options{})
	if err != nil {
		return "", err
	}
	gq, err := goquery.NewDocumentFromReader(bytes.NewReader(resp.Body))
	if err != nil {
		return "", err
	}
	var found string
	gq.Find(`a[href$=".epub"]`).EachWithBreak(func(_ int, a *goquery.Selection) bool {
		h := a.AttrOr("href", "")
		if strings.Contains(h, ".kepub.") || strings.Contains(h, "_advanced") {
			return true
		}
		if r, err := resp.URL.Parse(h); err == nil {
			// The plain link shows a "download started" page that forwards to
			// ?source=download; go to the file directly.
			q := r.Query()
			q.Set("source", "download")
			r.RawQuery = q.Encode()
			found = r.String()
			return false
		}
		return true
	})
	if found == "" {
		return "", errors.New("no EPUB download found on the Standard Ebooks page")
	}
	return found, nil
}

func getTo(ctx context.Context, ua, u string, w io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", ua)
	resp, err := (&http.Client{Timeout: 3 * time.Minute}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}
	_, err = io.Copy(w, io.LimitReader(resp.Body, 100<<20))
	return err
}

func clean(s string) string { return strings.Join(strings.Fields(s), " ") }
