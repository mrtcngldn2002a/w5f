// Package libgen adapts the halfurness/libgen-cli libgen.li protocol to W5F.
// Protocol reference: https://github.com/halfurness/libgen-cli (Apache-2.0).
// Unlike the CLI it uses W5F's cancellable client, keeps TLS verification,
// and never writes progress bars to the terminal.
package libgen

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"w5f/internal/fetch"
	"w5f/internal/store"
)

const UserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"

var DefaultMirrors = []string{"https://libgen.li", "https://libgen.vg", "https://libgen.la", "https://libgen.bz", "https://libgen.gl"}
var hashRE = regexp.MustCompile(`(?i)^[a-f0-9]{32}$`)
var anyHashRE = regexp.MustCompile(`(?i)[a-f0-9]{32}`)
var yearRE = regexp.MustCompile(`\d{4}`)

type Book struct{ MD5, Title, Author, Publisher, Year, Language, Pages, Size, Extension string }
type Config struct {
	SearchMirrors   []string `json:"search_mirrors,omitempty"`
	DownloadMirrors []string `json:"download_mirrors,omitempty"`
	Extension       []string `json:"extension,omitempty"`
}

func ConfigPath() string { return filepath.Join(store.DataDir(), "libgen.json") }
func LoadConfig() (Config, error) {
	var c Config
	b, err := os.ReadFile(ConfigPath())
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return c, fmt.Errorf("%s: %w", ConfigPath(), err)
	}
	return c, nil
}

type Client struct {
	F                              *fetch.Fetcher
	SearchMirrors, DownloadMirrors []string
	Extensions                     []string
}

func New(f *fetch.Fetcher) (*Client, error) {
	f, err := f.ForCatalog()
	if err != nil {
		return nil, err
	}
	c, err := LoadConfig()
	if err != nil {
		return nil, err
	}
	sm, err := mirrors(c.SearchMirrors)
	if err != nil {
		return nil, err
	}
	dm, err := mirrors(c.DownloadMirrors)
	if err != nil {
		return nil, err
	}
	return &Client{f, sm, dm, c.Extension}, nil
}
func mirrors(in []string) ([]string, error) {
	if len(in) == 0 {
		in = DefaultMirrors
	}
	var out []string
	for _, s := range in {
		if !strings.Contains(s, "://") {
			s = "https://" + s
		}
		u, err := url.Parse(s)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
			return nil, fmt.Errorf("invalid LibGen mirror %q", s)
		}
		u.Path, u.RawPath, u.RawQuery, u.Fragment = "", "", "", ""
		out = append(out, strings.TrimSuffix(u.String(), "/"))
	}
	return out, nil
}
func KnownHost(raw string) bool {
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	for _, m := range DefaultMirrors {
		v, _ := url.Parse(m)
		if strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.") == v.Hostname() {
			return true
		}
	}
	return false
}
func ValidHash(s string) bool { return hashRE.MatchString(s) }
func (c *Client) get(ctx context.Context, base, endpoint string, q url.Values, fresh bool) (*fetch.Response, error) {
	u, err := url.Parse(base + "/" + endpoint)
	if err != nil {
		return nil, err
	}
	u.RawQuery = q.Encode()
	r, err := c.F.Get(ctx, u, fetch.Options{NoStore: fresh, Header: http.Header{"User-Agent": {UserAgent}}})
	if err == nil && strings.Contains(strings.ToLower(string(r.Body)), "<title>welcome to nginx!</title>") {
		return nil, errors.New("mirror returned an nginx placeholder")
	}
	return r, err
}

type Options struct {
	Query, Extension, Language, Publisher, Year, Sort, Author, Title string
	Page, Results                                                    int
	Desc, RequireAuthor                                              bool
}

// ParseQuery accepts normal words and optional field:value filters. A quoted
// value may contain spaces, e.g. publisher:"Project Gutenberg".
func ParseQuery(raw string) (Options, error) {
	o := Options{Page: 1, Results: 25}
	re := regexp.MustCompile(`(?:[^\s"]+|"[^"]*")+`)
	var words []string
	for _, tok := range re.FindAllString(raw, -1) {
		k, v, ok := strings.Cut(tok, ":")
		v = strings.Trim(v, `"`)
		if !ok {
			words = append(words, strings.Trim(tok, `"`))
			continue
		}
		switch strings.ToLower(k) {
		case "ext", "format":
			o.Extension = v
		case "lang", "language":
			o.Language = v
		case "publisher", "pub":
			o.Publisher = v
		case "year":
			o.Year = v
		case "author":
			o.Author = v
			words = append(words, v)
		case "title":
			o.Title = v
			words = append(words, v)
		case "sort":
			o.Sort = strings.TrimPrefix(v, "-")
			o.Desc = strings.HasPrefix(v, "-")
		case "limit":
			n, e := strconv.Atoi(v)
			if e != nil || (n != 25 && n != 50 && n != 100) {
				return o, errors.New("LibGen page size must be 25, 50 or 100")
			}
			o.Results = n
		default:
			words = append(words, tok)
		}
	}
	o.Query = strings.Join(words, " ")
	if o.Query == "" {
		return o, errors.New("type some words to search LibGen")
	}
	return o, nil
}

type Results struct {
	Books  []Book
	More   bool
	Mirror string
}

func (c *Client) Search(ctx context.Context, o Options) (Results, error) {
	size := o.Results
	if size != 25 && size != 50 && size != 100 {
		size = 25
	}
	q := url.Values{"req": {o.Query}, "res": {strconv.Itoa(size)}, "page": {strconv.Itoa(max(o.Page, 1))}}
	if o.Sort != "" {
		key := map[string]string{"id": "id", "title": "title", "author": "author", "pub": "publisher", "publisher": "publisher", "year": "year", "lang": "language", "size": "filesize", "ext": "extension"}[o.Sort]
		if key == "" {
			return Results{}, fmt.Errorf("unsupported LibGen sort %q", o.Sort)
		}
		q.Set("sort", key)
		q.Set("sortmode", "ASC")
		if o.Desc {
			q.Set("sortmode", "DESC")
		}
	}
	var errs []error
	for _, m := range c.SearchMirrors {
		if err := ctx.Err(); err != nil {
			return Results{}, err
		}
		r, err := c.get(ctx, m, "index.php", q, false)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		bs, more, err := ParseResults(r.Body, r.URL)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		out := Results{More: more, Mirror: m}
		formats := c.Extensions
		if o.Extension != "" {
			formats = strings.Split(o.Extension, ",")
		}
		for _, b := range bs {
			if len(formats) > 0 {
				match := false
				for _, e := range formats {
					if strings.EqualFold(strings.TrimPrefix(strings.TrimSpace(e), "."), b.Extension) {
						match = true
					}
				}
				if !match {
					continue
				}
			}
			if o.Language != "" && !strings.EqualFold(o.Language, b.Language) {
				continue
			}
			if o.Year != "" && o.Year != b.Year {
				continue
			}
			if o.RequireAuthor && b.Author == "" {
				continue
			}
			if !contains(b.Publisher, o.Publisher) || !contains(b.Author, o.Author) || !contains(b.Title, o.Title) {
				continue
			}
			out.Books = append(out.Books, b)
		}
		// Keep all matches on a page: truncating here would silently skip the
		// remaining rows when the reader follows the next-page link.
		return out, nil
	}
	return Results{}, fmt.Errorf("LibGen search failed: %w", errors.Join(errs...))
}
func contains(s, sub string) bool      { return strings.Contains(strings.ToLower(s), strings.ToLower(sub)) }
func text(s *goquery.Selection) string { return strings.Join(strings.Fields(s.Text()), " ") }
func ParseResults(body []byte, base *url.URL) ([]Book, bool, error) {
	d, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil, false, err
	}
	var out []Book
	d.Find("tr").Each(func(_ int, row *goquery.Selection) {
		td := row.ChildrenFiltered("td")
		if td.Length() < 9 || row.Find(`a[href*="file.php?id="]`).Length() == 0 {
			return
		}
		h := row.Find(`a[href*="ads.php?md5="]`).First().AttrOr("href", "")
		if h == "" {
			h, _ = row.Html()
		}
		hash := anyHashRE.FindString(h)
		if hash == "" {
			return
		}
		title := text(td.Eq(0).Find(`a[href*="edition.php?id="]`).First())
		if title == "" {
			title = text(td.Eq(0).Find("b").First())
		}
		if title == "" {
			title = text(td.Eq(0))
		}
		out = append(out, Book{MD5: strings.ToLower(hash), Title: title, Author: text(td.Eq(1)), Publisher: text(td.Eq(2)), Year: yearRE.FindString(text(td.Eq(3))), Language: text(td.Eq(4)), Pages: text(td.Eq(5)), Size: text(td.Eq(6)), Extension: strings.ToLower(text(td.Eq(7)))})
	})
	more := false
	page, _ := strconv.Atoi(base.Query().Get("page"))
	page = max(page, 1)
	d.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		u, e := base.Parse(a.AttrOr("href", ""))
		if e != nil || u.Host != base.Host {
			return
		}
		n, _ := strconv.Atoi(u.Query().Get("page"))
		if n > page && u.Query().Get("req") == base.Query().Get("req") {
			more = true
		}
	})
	if len(out) == 0 {
		s := strings.ToLower(text(d.Selection))
		if !strings.Contains(s, "no results") && !strings.Contains(s, "0 files") && !strings.Contains(s, "0 results") && !strings.Contains(s, "nothing found") && !strings.Contains(s, "no files") {
			return nil, false, errors.New("LibGen returned an unrecognized results page (possibly a verification page)")
		}
	}
	return out, more, nil
}
func (c *Client) jsonMap(ctx context.Context, endpoint url.Values) (map[string]json.RawMessage, error) {
	var errs []error
	for _, m := range c.SearchMirrors {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		r, err := c.get(ctx, m, "json.php", endpoint, false)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		var records map[string]json.RawMessage
		if err = json.Unmarshal(r.Body, &records); err != nil {
			errs = append(errs, err)
			continue
		}
		if len(records) == 0 || records["error"] != nil {
			errs = append(errs, errors.New("LibGen metadata not found"))
			continue
		}
		return records, nil
	}
	return nil, errors.Join(errs...)
}
func (c *Client) Details(ctx context.Context, hash string) (Book, error) {
	b := Book{MD5: strings.ToLower(hash)}
	if !ValidHash(hash) {
		return b, errors.New("LibGen requires a 32-character MD5")
	}
	records, err := c.jsonMap(ctx, url.Values{"object": {"f"}, "md5": {hash}, "addkeys": {"*"}})
	if err != nil {
		return b, err
	}
	var file struct {
		MD5       string `json:"md5"`
		Extension string `json:"extension"`
		Size      string `json:"filesize"`
		Pages     string `json:"pages"`
		Editions  map[string]struct {
			ID string `json:"e_id"`
		} `json:"editions"`
	}
	keys := make([]string, 0, len(records))
	for k := range records {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if err = json.Unmarshal(records[keys[0]], &file); err != nil {
		return b, err
	}
	if file.MD5 != "" && !strings.EqualFold(file.MD5, hash) {
		return b, errors.New("LibGen metadata hash mismatch")
	}
	b.Extension, b.Size, b.Pages = strings.ToLower(file.Extension), file.Size, file.Pages
	var ids []string
	for _, e := range file.Editions {
		if e.ID != "" {
			ids = append(ids, e.ID)
		}
	}
	sort.Strings(ids)
	if len(ids) > 0 {
		es, e := c.jsonMap(ctx, url.Values{"object": {"e"}, "ids": {ids[0]}, "addkeys": {"*"}})
		if e == nil {
			for _, raw := range es {
				var v struct {
					Title, Author, Publisher, Year string
					Add                            map[string]struct {
						Name  string `json:"name_en"`
						Value string `json:"value"`
					}
				}
				if json.Unmarshal(raw, &v) == nil {
					b.Title, b.Author, b.Publisher, b.Year = v.Title, v.Author, v.Publisher, v.Year
					for _, a := range v.Add {
						if strings.EqualFold(a.Name, "Language") {
							b.Language = a.Value
						}
					}
				}
				break
			}
		}
	}
	if b.Title == "" {
		b.Title = b.MD5
	}
	return b, nil
}

type Link struct{ URL, Referer string }

func (c *Client) resolve(ctx context.Context, m, hash string) (Link, error) {
	r, err := c.get(ctx, m, "ads.php", url.Values{"md5": {hash}}, true)
	if err != nil {
		return Link{}, err
	}
	d, err := goquery.NewDocumentFromReader(bytes.NewReader(r.Body))
	if err != nil {
		return Link{}, err
	}
	var found Link
	d.Find(`a[href]`).EachWithBreak(func(_ int, a *goquery.Selection) bool {
		u, e := r.URL.Parse(a.AttrOr("href", ""))
		if e != nil || u.Host != r.URL.Host || (u.Scheme != "http" && u.Scheme != "https") || path.Base(u.Path) != "get.php" || !strings.EqualFold(u.Query().Get("md5"), hash) || u.Query().Get("key") == "" {
			return true
		}
		found = Link{u.String(), r.URL.String()}
		return false
	})
	if found.URL == "" {
		return found, errors.New("no valid LibGen download link on " + m)
	}
	return found, nil
}
func (c *Client) Resolve(ctx context.Context, hash string) (Link, error) {
	if !ValidHash(hash) {
		return Link{}, errors.New("invalid LibGen MD5")
	}
	var errs []error
	for _, m := range c.DownloadMirrors {
		if err := ctx.Err(); err != nil {
			return Link{}, err
		}
		l, e := c.resolve(ctx, m, hash)
		if e == nil {
			return l, nil
		}
		errs = append(errs, e)
	}
	return Link{}, errors.Join(errs...)
}

// Download resolves a new key on each mirror and verifies the MD5 before a
// temporary file can enter the library. Partial files are always removed.
func (c *Client) Download(ctx context.Context, b Book, dir string) (string, error) {
	if c.F.Offline {
		return "", fetch.ErrOffline
	}
	if !ValidHash(b.MD5) {
		return "", errors.New("invalid LibGen MD5")
	}
	if !regexp.MustCompile(`^[a-z0-9]{1,8}$`).MatchString(b.Extension) {
		return "", errors.New("missing or invalid LibGen file format")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	var errs []error
	for _, m := range c.DownloadMirrors {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		l, err := c.resolve(ctx, m, b.MD5)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		p, err := c.downloadFile(ctx, l, b, dir)
		if err == nil {
			return p, nil
		}
		errs = append(errs, err)
	}
	return "", fmt.Errorf("LibGen download failed: %w", errors.Join(errs...))
}
func (c *Client) downloadFile(ctx context.Context, l Link, b Book, dir string) (result string, err error) {
	req, err := http.NewRequestWithContext(ctx, "GET", l.URL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Referer", l.Referer)
	client := *c.F.Client
	client.Timeout = 5 * time.Minute
	r, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return "", fmt.Errorf("download HTTP %d", r.StatusCode)
	}
	if strings.Contains(r.Header.Get("Content-Type"), "html") {
		return "", errors.New("LibGen returned a web page instead of a book")
	}
	tmp, err := os.CreateTemp(dir, ".download-*."+b.Extension)
	if err != nil {
		return "", err
	}
	defer func() {
		tmp.Close()
		if result == "" {
			os.Remove(tmp.Name())
		}
	}()
	h := md5.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(r.Body, (100<<20)+1))
	if err != nil {
		return "", err
	}
	if n > 100<<20 {
		return "", errors.New("book is larger than 100 MB")
	}
	if r.ContentLength >= 0 && n != r.ContentLength {
		return "", errors.New("truncated LibGen download")
	}
	if hex.EncodeToString(h.Sum(nil)) != strings.ToLower(b.MD5) {
		return "", errors.New("LibGen download MD5 mismatch")
	}
	if err = tmp.Close(); err != nil {
		return "", err
	}
	return tmp.Name(), nil
}
func (c *Client) Status(ctx context.Context) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range append(append([]string{}, c.SearchMirrors...), c.DownloadMirrors...) {
		if seen[m] {
			continue
		}
		seen[m] = true
		r, e := c.get(ctx, m, "", nil, true)
		state := "OK"
		if e != nil {
			state = e.Error()
		} else if !contains(string(r.Body), "libgen") && !contains(string(r.Body), "library genesis") {
			state = "unexpected mirror page"
		}
		out = append(out, m+" — "+state)
		if ctx.Err() != nil {
			break
		}
	}
	return out
}
