package sitecat

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"w5f/internal/fetch"
)

// Download is one downloadable file of a book.
type Download struct {
	URL    string
	Format string
	Label  string
}

// ErrRefused means the site refused the download (login, bot wall, limit).
var ErrRefused = errors.New("the site refused the download")

var extFormats = map[string]string{".epub": "epub", ".pdf": "pdf", ".txt": "txt", ".mobi": "mobi", ".azw3": "azw3",
	".azw": "azw3", ".fb2": "fb2", ".djvu": "djvu", ".cbz": "cbz", ".cbr": "cbr"}

var mimeFormats = map[string]string{"application/epub+zip": "epub", "application/pdf": "pdf", "text/plain": "txt",
	"application/x-mobipocket-ebook": "mobi", "application/vnd.amazon.ebook": "azw3", "image/vnd.djvu": "djvu",
	"application/x-fictionbook+xml": "fb2", "application/vnd.comicbook+zip": "cbz", "application/vnd.comicbook-rar": "cbr"}

var (
	reFormatWord = regexp.MustCompile(`(?i)\b(epub|pdf|mobi|azw3|fb2|djvu|cbz|cbr|kindle|plain text|txt)\b`)
	reDownload   = regexp.MustCompile(`(?i)download|indir|get\b|/dl/|/get/|/file`)
	reHopText    = regexp.MustCompile(`(?i)download|indir`)
	reOJSView    = regexp.MustCompile(`^(.*/article)/view/(\d+/\d+)/?$`)
)

func wordFormat(w string) string {
	switch strings.ToLower(w) {
	case "kindle":
		return "mobi"
	case "plain text", "txt":
		return "txt"
	}
	return strings.ToLower(w)
}

// FindIn lists download links on a page, ordered by prefer.
func FindIn(body []byte, pageURL string, prefer []string) []Download {
	gq, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil
	}
	base, _ := url.Parse(pageURL)
	seen := map[string]bool{}
	var out []Download
	gq.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		raw := strings.TrimSpace(a.AttrOr("href", ""))
		if raw == "" || strings.HasPrefix(raw, "#") || strings.HasPrefix(strings.ToLower(raw), "javascript:") {
			return
		}
		u, err := base.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			return
		}
		u.Fragment = ""
		label := collapse(a.Text())
		format := fileFormatOf(u)
		// OJS: a galley's "PDF" link opens a viewer; its download sits at
		// …/article/download/<id>/<galley>.
		if format == "" {
			if m := reOJSView.FindStringSubmatch(u.Path); m != nil {
				if w := reFormatWord.FindString(label); w != "" {
					u.Path = m[1] + "/download/" + m[2]
					format = wordFormat(w)
				}
			}
		}
		if format == "" {
			format = mimeFormats[strings.ToLower(strings.TrimSpace(strings.SplitN(a.AttrOr("type", ""), ";", 2)[0]))]
		}
		if format == "" {
			if m := reFormatWord.FindString(label); m != "" && (reDownload.MatchString(label) || reDownload.MatchString(u.Path)) {
				format = wordFormat(m)
			}
		}
		if format == "" || seen[u.String()] {
			return
		}
		seen[u.String()] = true
		out = append(out, Download{URL: u.String(), Format: format, Label: label})
	})
	rank := func(f string) int {
		for i, p := range prefer {
			if p == f {
				return i
			}
		}
		return len(prefer)
	}
	sortStable(out, func(a, b Download) bool { return rank(a.Format) < rank(b.Format) })
	return out
}

func sortStable(ds []Download, less func(a, b Download) bool) {
	for i := 1; i < len(ds); i++ {
		for j := i; j > 0 && less(ds[j], ds[j-1]); j-- {
			ds[j], ds[j-1] = ds[j-1], ds[j]
		}
	}
}

// FindDownloads looks on an item page and, if needed, one "download" page.
func FindDownloads(ctx context.Context, f *fetch.Fetcher, itemURL string, prefer []string) ([]Download, error) {
	u, err := url.Parse(itemURL)
	if err != nil {
		return nil, err
	}
	resp, err := f.Get(ctx, u, fetch.Options{})
	if err != nil {
		return nil, err
	}
	if ds := FindIn(resp.Body, resp.URL.String(), prefer); len(ds) > 0 {
		return ds, nil
	}
	gq, err := goquery.NewDocumentFromReader(bytes.NewReader(resp.Body))
	if err != nil {
		return nil, nil
	}
	var hop *url.URL
	gq.Find("a[href]").EachWithBreak(func(_ int, a *goquery.Selection) bool {
		if !reHopText.MatchString(collapse(a.Text())) {
			return true
		}
		if h, err := resp.URL.Parse(a.AttrOr("href", "")); err == nil && sameSite(h, resp.URL) && h.String() != resp.URL.String() {
			hop = h
			return false
		}
		return true
	})
	if hop == nil {
		return nil, nil
	}
	// The "Download" link may be a download page or the file itself. Look
	// at the response type first so a book is never fetched just to be
	// inspected.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, hop.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", f.UserAgent)
	client := *f.Client
	client.Timeout = time.Minute
	req.Header.Set("Referer", resp.URL.String())
	resp2, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp2.Body.Close()
	if resp2.StatusCode >= 400 {
		return nil, nil
	}
	ct := strings.ToLower(strings.TrimSpace(strings.SplitN(resp2.Header.Get("Content-Type"), ";", 2)[0]))
	if !strings.Contains(ct, "html") {
		format := mimeFormats[ct]
		if format == "" {
			format = extFormats[strings.ToLower(path.Ext(resp2.Request.URL.Path))]
		}
		if format == "" {
			return nil, nil
		}
		return []Download{{URL: hop.String(), Format: format, Label: "Download"}}, nil
	}
	body, err := io.ReadAll(io.LimitReader(resp2.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	return FindIn(body, resp2.Request.URL.String(), prefer), nil
}

var reRefresh = regexp.MustCompile(`(?i)<meta[^>]+http-equiv=["']?refresh["']?[^>]*content=["']?\s*\d+\s*;\s*url=([^"'>]+)`)

// hasMagic lists formats whose files always start with a known signature.
var hasMagic = map[string]bool{"pdf": true, "epub": true, "cbz": true, "cbr": true, "djvu": true, "mobi": true, "azw3": true}

// magicFormat identifies a file from its first bytes ("" if unknown).
func magicFormat(head []byte, fallback string) string {
	switch {
	case bytes.HasPrefix(head, []byte("%PDF")):
		return "pdf"
	case bytes.HasPrefix(head, []byte("PK\x03\x04")):
		if fallback == "cbz" {
			return "cbz"
		}
		return "epub"
	case bytes.HasPrefix(head, []byte("AT&TFORM")):
		return "djvu"
	case bytes.HasPrefix(head, []byte("Rar!")):
		return "cbr"
	case len(head) > 68 && string(head[60:68]) == "BOOKMOBI":
		if fallback == "azw3" {
			return "azw3"
		}
		return "mobi"
	case bytes.Contains(head, []byte("<FictionBook")):
		return "fb2"
	}
	return ""
}

func looksHTML(head []byte, ct string) bool {
	if strings.Contains(strings.ToLower(ct), "html") {
		return true
	}
	h := strings.ToLower(string(bytes.TrimSpace(head)))
	return strings.HasPrefix(h, "<!doctype html") || strings.HasPrefix(h, "<html")
}

// FetchFile downloads a book into dir (as a hidden temp file) and returns its
// path and the verified format. HTML "download started" pages are followed
// through their meta refresh (at most twice); any other HTML is an error.
func FetchFile(ctx context.Context, f *fetch.Fetcher, dl Download, dir string) (string, string, error) {
	if f.Offline {
		return "", "", fetch.ErrOffline
	}
	target := dl.URL
	client := *f.Client
	client.Timeout = 5 * time.Minute
	for hop := 0; hop < 3; hop++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			return "", "", err
		}
		req.Header.Set("User-Agent", f.UserAgent)
		resp, err := client.Do(req)
		if err != nil {
			return "", "", err
		}
		if resp.StatusCode == 401 || resp.StatusCode == 403 || resp.StatusCode == 429 {
			resp.Body.Close()
			return "", "", fmt.Errorf("%w (HTTP %d) — it may need a login or block readers", ErrRefused, resp.StatusCode)
		}
		if resp.StatusCode >= 400 {
			resp.Body.Close()
			return "", "", fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
		}
		br := bufio.NewReader(io.LimitReader(resp.Body, 100<<20+1))
		head, _ := br.Peek(512)
		if looksHTML(head, resp.Header.Get("Content-Type")) {
			page, _ := io.ReadAll(io.LimitReader(br, 1<<20))
			resp.Body.Close()
			m := reRefresh.FindSubmatch(append(append([]byte{}, head...), page...))
			if m == nil {
				return "", "", errors.New("the site served a web page, not a book file")
			}
			next, err := resp.Request.URL.Parse(strings.TrimSpace(string(m[1])))
			if err != nil {
				return "", "", err
			}
			target = next.String()
			continue
		}
		format := magicFormat(head, dl.Format)
		if format == "" {
			if hasMagic[dl.Format] {
				resp.Body.Close()
				return "", "", fmt.Errorf("the site did not send a valid %s file", strings.ToUpper(dl.Format))
			}
			format = dl.Format // plain text and FB2 have no fixed signature
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			resp.Body.Close()
			return "", "", err
		}
		tmp, err := os.CreateTemp(dir, ".download-*."+format)
		if err != nil {
			resp.Body.Close()
			return "", "", err
		}
		n, err := io.Copy(tmp, br)
		resp.Body.Close()
		tmp.Close()
		if err != nil {
			os.Remove(tmp.Name())
			return "", "", err
		}
		if n > 100<<20 {
			os.Remove(tmp.Name())
			return "", "", errors.New("the file is larger than 100 MB")
		}
		return tmp.Name(), format, nil
	}
	return "", "", errors.New("too many redirects through download pages")
}
