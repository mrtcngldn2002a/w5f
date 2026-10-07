package fiction

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"

	"w5f/internal/doc"
	"w5f/internal/fetch"
	"w5f/internal/reddit"
	"w5f/internal/store"
)

// listReddit reads an author's posts with the owner's Reddit session
// (set by the source package; a variable so tests can stub it).
var listReddit func(ctx context.Context, author string, maxPages int, stop func(reddit.PostInfo) bool) ([]reddit.PostInfo, error)

// SetRedditLister connects the Reddit reader.
func SetRedditLister(f func(ctx context.Context, author string, maxPages int, stop func(reddit.PostInfo) bool) ([]reddit.PostInfo, error)) {
	listReddit = f
}

var (
	rePartMarker = regexp.MustCompile(`(?i)[\[(]?\s*(?:\b(?:part|pt\.?|chapter|ch\.?)\s*([0-9]+|[ivxlc]+)\b|#\s*([0-9]+))\s*[\])]?`)
	reEndMarker  = regexp.MustCompile(`(?i)[\[(]?\s*\b(?:final|finale|conclusion|update|epilogue)\b\s*[\])]?`)
	reNotWord    = regexp.MustCompile(`[^\p{L}\p{N}]+`)
)

// SeriesKey is a Reddit title without its part markers, for matching the
// parts of one series, and the part number when the title has one.
func SeriesKey(title string) (key string, part int) {
	if m := rePartMarker.FindStringSubmatch(title); m != nil {
		part = partNumber(m[1] + m[2])
	}
	t := rePartMarker.ReplaceAllString(title, " ")
	t = reEndMarker.ReplaceAllString(t, " ")
	t = strings.NewReplacer("'", "", "’", "").Replace(strings.ToLower(t))
	return strings.Join(strings.Fields(reNotWord.ReplaceAllString(t, " ")), " "), part
}

// seriesTitle is the title shown for a series (original case).
func seriesTitle(title string) string {
	t := reEndMarker.ReplaceAllString(rePartMarker.ReplaceAllString(title, " "), " ")
	return strings.Trim(strings.Join(strings.Fields(t), " "), " -–—:,|.")
}

func partNumber(s string) int {
	var n int
	if _, err := fmt.Sscan(s, &n); err == nil {
		return n
	}
	vals := map[rune]int{'i': 1, 'v': 5, 'x': 10, 'l': 50, 'c': 100}
	total, prev := 0, 0
	rs := []rune(strings.ToLower(s))
	for i := len(rs) - 1; i >= 0; i-- {
		v := vals[rs[i]]
		if v < prev {
			total -= v
		} else {
			total += v
			prev = v
		}
	}
	return total
}

// orderParts sorts a series: by part number when every part has a distinct
// one, else by posting time.
func orderParts(ps []reddit.PostInfo) []reddit.PostInfo {
	out := append([]reddit.PostInfo(nil), ps...)
	nums := map[int]bool{}
	byNumber := true
	for _, p := range out {
		_, n := SeriesKey(p.Title)
		if n == 0 || nums[n] {
			byNumber = false
			break
		}
		nums[n] = true
	}
	sort.SliceStable(out, func(i, j int) bool {
		if byNumber {
			_, a := SeriesKey(out[i].Title)
			_, b := SeriesKey(out[j].Title)
			return a < b
		}
		return out[i].Posted.Before(out[j].Posted)
	})
	return out
}

func postKey(u string) string {
	p, err := url.Parse(u)
	if err != nil {
		return u
	}
	parts := strings.Split(strings.Trim(p.Path, "/"), "/")
	for i, s := range parts {
		if s == "comments" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return p.Path
}

// seriesParts filters an author's posts to one series in one subreddit.
func seriesParts(sub, key string, posts []reddit.PostInfo) []reddit.PostInfo {
	var out []reddit.PostInfo
	seen := map[string]bool{}
	for _, p := range posts {
		if k, _ := SeriesKey(p.Title); k != key || !strings.EqualFold(p.Sub, sub) || seen[postKey(p.URL)] {
			continue
		}
		seen[postKey(p.URL)] = true
		out = append(out, p)
	}
	return orderParts(out)
}

type redditSeries struct {
	url, title, author string
	parts              []reddit.PostInfo
}

var (
	seriesMu    sync.Mutex
	seriesCache = map[string]redditSeries{}
)

func seriesURL(sub, author, key string) string {
	return "reddit-series:" + sub + "/" + author + "/" + url.PathEscape(key)
}

// DecorateRedditPost adds series navigation to a Reddit post page: a
// "Series · part i of n" line and ]/[ through the parts.
func DecorateRedditPost(ctx context.Context, env Env, d *doc.Document, info *reddit.PostInfo) {
	if info == nil || info.Author == "" || info.Author == "[deleted]" || listReddit == nil {
		return
	}
	key, _ := SeriesKey(info.Title)
	if key == "" {
		return
	}
	since := info.Posted.AddDate(0, -6, 0)
	posts, err := listReddit(ctx, info.Author, 3, func(oldest reddit.PostInfo) bool {
		return !info.Posted.IsZero() && oldest.Posted.Before(since)
	})
	if err != nil {
		return
	}
	parts := seriesParts(info.Sub, key, append(posts, *info))
	if len(parts) < 2 {
		bodyLinks(d, info)
		return
	}
	idx := -1
	for i, p := range parts {
		if postKey(p.URL) == postKey(info.URL) {
			idx = i
		}
	}
	if idx < 0 {
		return
	}
	su := seriesURL(info.Sub, info.Author, key)
	seriesMu.Lock()
	seriesCache[su] = redditSeries{url: su, title: seriesTitle(parts[0].Title), author: info.Author, parts: parts}
	seriesMu.Unlock()
	d.Ref = "rseries:" + su
	line := doc.Inline{plain(fmt.Sprintf("Series · part %d of %d", idx+1, len(parts)), doc.Italic)}
	if idx > 0 {
		d.Prev = parts[idx-1].URL
		line = append(line, plain(" · ", 0), doc.Span{Text: "‹ previous", Link: link(d, d.Prev, "previous part")})
	}
	if idx < len(parts)-1 {
		d.Next = parts[idx+1].URL
		line = append(line, plain(" · ", 0), doc.Span{Text: "next ›", Link: link(d, d.Next, "next part")})
	}
	line = append(line, plain(" · u follows the series", doc.Italic))
	d.Blocks = append([]doc.Block{para(line...)}, d.Blocks...)
}

// bodyLinks: without a series on the author page, "next/previous part"
// links in the post still bind ] and [.
func bodyLinks(d *doc.Document, info *reddit.PostInfo) {
	for _, l := range info.Links {
		if !strings.Contains(l.Href, "/comments/") {
			continue
		}
		t := strings.ToLower(l.Text)
		switch {
		case d.Next == "" && (strings.Contains(t, "next") || strings.HasPrefix(t, "part")):
			d.Next = l.Href
		case d.Prev == "" && (strings.Contains(t, "prev") || strings.Contains(t, "last")):
			d.Prev = l.Href
		}
	}
}

// storeRedditSeries stores the series of a decorated post page as a serial.
func storeRedditSeries(db *store.DB, d *doc.Document) (int64, bool) {
	su := strings.TrimPrefix(d.Ref, "rseries:")
	seriesMu.Lock()
	s, ok := seriesCache[su]
	seriesMu.Unlock()
	if !ok {
		return 0, false
	}
	id, err := db.UpsertSerial(store.Serial{Kind: "reddit", URL: su, Title: s.title, Author: "u/" + s.author})
	if err != nil {
		return 0, false
	}
	chs := make([]store.SerialChapter, 0, len(s.parts))
	for _, p := range s.parts {
		chs = append(chs, store.SerialChapter{Title: p.Title, URL: p.URL, Published: p.Posted})
	}
	if _, err := db.MergeChapters(id, chs); err != nil {
		return 0, false
	}
	_ = db.MarkSeen(id)
	return id, true
}

// redditAdapter checks a stored Reddit series for new parts.
type redditAdapter struct{}

func init() { adapters = append(adapters, redditAdapter{}) }

func (redditAdapter) Kind() string                  { return "reddit" }
func (redditAdapter) Match(*url.URL) (string, bool) { return "", false }

func (redditAdapter) Serial(ctx context.Context, f *fetch.Fetcher, serial string) (*Serial, error) {
	rest := strings.TrimPrefix(serial, "reddit-series:")
	parts := strings.SplitN(rest, "/", 4) // r, sub, author, key
	if len(parts) != 4 || listReddit == nil {
		return nil, errors.New("Reddit: not a series address or Reddit not connected")
	}
	sub, author := parts[0]+"/"+parts[1], parts[2]
	key, _ := url.PathUnescape(parts[3])
	posts, err := listReddit(ctx, author, 5, nil)
	if err != nil {
		return nil, fmt.Errorf("Reddit: %w", err)
	}
	s := &Serial{Kind: "reddit", URL: serial, Author: "u/" + author}
	for _, p := range seriesParts(sub, key, posts) {
		if s.Title == "" {
			s.Title = seriesTitle(p.Title)
		}
		s.Chapters = append(s.Chapters, Chapter{Title: p.Title, URL: p.URL, Published: p.Posted})
	}
	return s, nil
}

func (redditAdapter) Chapter(ctx context.Context, f *fetch.Fetcher, ch Chapter) (*doc.Document, error) {
	return nil, errors.New("Reddit parts open through the Reddit reader (connect Reddit: g → reddit-login)")
}
