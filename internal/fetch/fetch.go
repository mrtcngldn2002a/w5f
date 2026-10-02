// Package fetch is W5F's single HTTP client: identified User-Agent, per-host
// politeness delay, conditional requests and an on-disk cache that doubles as
// the offline store.
package fetch

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/publicsuffix"
)

// ErrOffline is returned when offline mode is on and nothing is cached.
var ErrOffline = errors.New("offline and not in the cache")

// HTTPError is a non-success HTTP status.
type HTTPError struct {
	URL    string
	Status int
}

func (e *HTTPError) Error() string { return fmt.Sprintf("%s: HTTP %d", e.URL, e.Status) }

// Gone reports whether the error means the page no longer exists, which makes
// it a candidate for an archived copy.
func Gone(err error) bool {
	var he *HTTPError
	if errors.As(err, &he) {
		return he.Status == 404 || he.Status == 410
	}
	return false
}

// Response is a fetched (or cached) resource.
type Response struct {
	Body        []byte
	URL         *url.URL // final URL after redirects
	ContentType string
	Fetched     time.Time
	FromCache   bool // served from disk without a successful network round trip
	Stale       bool // served from disk because the network failed
}

// Options tune a single request.
type Options struct {
	ddgRetried  bool
	anubisTried bool
	// Revalidate skips the freshness window and asks the server (reload).
	Revalidate bool
	// NoStore disables caching for this request.
	NoStore bool
	// Header adds request headers (e.g. a session cookie).
	Header http.Header
}

// Fetcher performs requests. The zero value is not usable; use New.
type Fetcher struct {
	catalogOnce sync.Once
	catalog     *Fetcher
	Client      *http.Client
	UserAgent   string
	CacheDir    string        // empty disables the disk cache
	Offline     bool          // never touch the network
	Fresh       time.Duration // serve cached copies younger than this without asking
	HostGap     time.Duration // minimum delay between requests to one host
	// HostGaps overrides HostGap for hosts ending in the given suffix; sites
	// like Reddit rate-limit anonymous readers aggressively.
	HostGaps map[string]time.Duration
	// SolverURL is a local bot-check helper (FlareSolverr-style /v1) asked
	// when a page is a verification wall. Empty keeps ordinary HTTP behaviour.
	SolverURL string

	mu   sync.Mutex
	last map[string]time.Time
}

// New returns a Fetcher with W5F defaults.
func New(cacheDir, version string) *Fetcher {
	jar, _ := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	return &Fetcher{
		Client: &http.Client{Timeout: 25 * time.Second, Jar: jar},
		// The "Mozilla/5.0 (compatible; …)" form is the long-standing convention
		// for identified crawlers and readers; it still names W5F honestly.
		UserAgent: "Mozilla/5.0 (compatible; w5f/" + version + "; W5F Archive Node terminal reader)",
		CacheDir:  cacheDir,
		Fresh:     15 * time.Minute,
		HostGap:   400 * time.Millisecond,
		HostGaps: map[string]time.Duration{"reddit.com": 3 * time.Second, "duckduckgo.com": 3 * time.Second,
			"royalroad.com": time.Second, "archiveofourown.org": time.Second, "spacebattles.com": time.Second,
			"sufficientvelocity.com": time.Second, "questionablequesting.com": time.Second},
		last: map[string]time.Time{},
	}
}

type meta struct {
	URL          string    `json:"url"`
	FinalURL     string    `json:"final_url"`
	ContentType  string    `json:"content_type"`
	ETag         string    `json:"etag,omitempty"`
	LastModified string    `json:"last_modified,omitempty"`
	Fetched      time.Time `json:"fetched"`
}

func (f *Fetcher) paths(u string) (metaPath, bodyPath string) {
	sum := sha256.Sum256([]byte(u))
	h := hex.EncodeToString(sum[:])
	dir := filepath.Join(f.CacheDir, "http", h[:2])
	return filepath.Join(dir, h+".json"), filepath.Join(dir, h+".body")
}

func (f *Fetcher) load(u string) (*meta, []byte) {
	if f.CacheDir == "" {
		return nil, nil
	}
	mp, bp := f.paths(u)
	mb, err := os.ReadFile(mp)
	if err != nil {
		return nil, nil
	}
	var m meta
	if json.Unmarshal(mb, &m) != nil {
		return nil, nil
	}
	body, err := os.ReadFile(bp)
	if err != nil {
		return nil, nil
	}
	// Read now: the cache's limit removes the pages read longest ago first.
	now := time.Now()
	_ = os.Chtimes(bp, now, now)
	_ = os.Chtimes(mp, now, now)
	return &m, body
}

func (f *Fetcher) store(m *meta, body []byte) {
	if f.CacheDir == "" {
		return
	}
	mp, bp := f.paths(m.URL)
	if err := os.MkdirAll(filepath.Dir(mp), 0o755); err != nil {
		return
	}
	mb, _ := json.Marshal(m)
	// Body first: a meta file without its body is treated as a miss.
	if writeAtomic(bp, body) == nil {
		_ = writeAtomic(mp, mb)
	}
}

func writeAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (m *meta) response(body []byte, stale bool) *Response {
	fu, _ := url.Parse(m.FinalURL)
	if fu == nil {
		fu, _ = url.Parse(m.URL)
	}
	return &Response{Body: body, URL: fu, ContentType: m.ContentType, Fetched: m.Fetched, FromCache: true, Stale: stale}
}

// Get fetches u, using the cache according to opts.
func (f *Fetcher) Get(ctx context.Context, u *url.URL, opts Options) (*Response, error) {
	key := u.String()
	cm, cbody := f.load(key)
	if cm != nil && ChallengeReason(cbody) != "" {
		cm, cbody = nil, nil
	}
	if opts.NoStore {
		cm, cbody = nil, nil
	}
	if f.Offline {
		if cm != nil {
			return cm.response(cbody, false), nil
		}
		return nil, fmt.Errorf("%s: %w", key, ErrOffline)
	}
	if cm != nil && !opts.Revalidate && time.Since(cm.Fetched) < f.Fresh {
		return cm.response(cbody, false), nil
	}

	client, userAgent := f.Client, f.UserAgent
	if browserPageHost(u.Hostname()) {
		compatible, err := f.ForCatalog()
		if err != nil {
			return nil, err
		}
		client, userAgent = compatible.Client, compatible.UserAgent
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, key, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml;q=0.9,*/*;q=0.5")
	for k, vs := range opts.Header {
		req.Header.Del(k)
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	if cm != nil && !opts.NoStore {
		if cm.ETag != "" {
			req.Header.Set("If-None-Match", cm.ETag)
		}
		if cm.LastModified != "" {
			req.Header.Set("If-Modified-Since", cm.LastModified)
		}
	}
	f.wait(ctx, u.Host)
	resp, err := client.Do(req)
	if err != nil {
		if cm != nil {
			return cm.response(cbody, true), nil // network down: serve what we have
		}
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified && cm != nil {
		cm.Fetched = time.Now()
		f.store(cm, cbody)
		r := cm.response(cbody, false)
		r.FromCache = false
		return r, nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	if reason := ChallengeReason(body); reason != "" {
		// Anubis asks every visitor for a small proof of work; doing it is
		// what the page is for, and the pass cookie stays in the jar.
		if !opts.anubisTried && isAnubis(body) {
			resp.Body.Close()
			if err := f.solveAnubis(ctx, client, userAgent, resp.Request.URL, body); err == nil {
				opts.anubisTried = true
				opts.Revalidate = true
				return f.Get(ctx, u, opts)
			}
		}
		if f.catalog == f && !opts.ddgRetried && strings.Contains(reason, "DDoS-Guard") && f.bootstrapDDG(ctx, resp.Request.URL) {
			resp.Body.Close()
			opts.ddgRetried = true
			opts.Revalidate = true
			return f.Get(ctx, u, opts)
		}
		ce := &ChallengeError{URL: resp.Request.URL.String(), Reason: reason}
		if f.solverFor(u) {
			resp.Body.Close()
			solved, err := f.solved(ctx, key, u, opts)
			if err == nil {
				return solved, nil
			}
			ce.Helper = err.Error()
		}
		return nil, ce
	}
	if resp.StatusCode >= 400 {
		if blockedStatus(resp) && f.solverFor(u) {
			resp.Body.Close()
			if solved, err := f.solved(ctx, key, u, opts); err == nil {
				return solved, nil
			}
		}
		if cm != nil && (resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests) {
			return cm.response(cbody, true), nil
		}
		cause := &HTTPError{URL: key, Status: resp.StatusCode}
		return nil, cause
	}
	m := &meta{
		URL:          key,
		FinalURL:     resp.Request.URL.String(),
		ContentType:  resp.Header.Get("Content-Type"),
		ETag:         resp.Header.Get("ETag"),
		LastModified: resp.Header.Get("Last-Modified"),
		Fetched:      time.Now(),
	}
	if !opts.NoStore {
		f.store(m, body)
	}
	return &Response{Body: body, URL: resp.Request.URL, ContentType: m.ContentType, Fetched: m.Fetched}, nil
}

// solved is the helper's page for u, cached like any other (offline reading
// works afterwards).
func (f *Fetcher) solved(ctx context.Context, key string, u *url.URL, opts Options) (*Response, error) {
	r, err := f.solve(ctx, u)
	if err != nil {
		return nil, err
	}
	if !opts.NoStore {
		f.store(&meta{URL: key, FinalURL: r.URL.String(), ContentType: r.ContentType, Fetched: r.Fetched}, r.Body)
	}
	return r, nil
}

// PostJSON sends a JSON body and decodes a JSON reply into out. Not cached.
func (f *Fetcher) PostJSON(ctx context.Context, u string, in, out any) error {
	if f.Offline {
		return ErrOffline
	}
	b, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", f.UserAgent)
	req.Header.Set("Content-Type", "application/json")
	if pu, err := url.Parse(u); err == nil {
		f.wait(ctx, pu.Host)
	}
	resp, err := f.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return &HTTPError{URL: u, Status: resp.StatusCode}
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out)
}

// wait enforces the per-host politeness gap.
func (f *Fetcher) wait(ctx context.Context, host string) {
	gap := f.HostGap
	for suffix, g := range f.HostGaps {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			gap = g
		}
	}
	if gap <= 0 {
		return
	}
	f.mu.Lock()
	next := f.last[host].Add(gap)
	now := time.Now()
	if next.Before(now) {
		next = now
	}
	f.last[host] = next
	f.mu.Unlock()
	if d := time.Until(next); d > 0 {
		select {
		case <-time.After(d):
		case <-ctx.Done():
		}
	}
}

// PostForm submits a form (a site's search) and returns the page. It is
// not cached: the same address answers differently for different bodies.
func (f *Fetcher) PostForm(ctx context.Context, u string, form url.Values) (*Response, error) {
	if f.Offline {
		return nil, fmt.Errorf("%s: %w", u, ErrOffline)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", f.UserAgent)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "text/html,application/xhtml+xml;q=0.9,*/*;q=0.5")
	// Search forms commonly require their own origin and session cookie.
	if origin, err := url.Parse(u); err == nil {
		req.Header.Set("Origin", origin.Scheme+"://"+origin.Host)
		req.Header.Set("Referer", u)
	}
	if pu, err := url.Parse(u); err == nil {
		f.wait(ctx, pu.Host)
	}
	resp, err := f.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	if reason := ChallengeReason(body); reason != "" {
		return nil, &ChallengeError{URL: resp.Request.URL.String(), Reason: reason}
	}
	if resp.StatusCode >= 400 {
		return nil, &HTTPError{URL: u, Status: resp.StatusCode}
	}
	return &Response{Body: body, URL: resp.Request.URL, ContentType: resp.Header.Get("Content-Type"), Fetched: time.Now()}, nil
}
