package fetch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/publicsuffix"
)

// The bot-check helper (Byparr, FlareSolverr or W5F's own, at SolverURL) is
// asked only after ordinary HTTP and W5F's own answers (Anubis, DDoS-Guard)
// met a verification wall. It opens the page in a real browser and returns
// it; its cookies and browser profile stay with it, never in W5F's jar.

// SolverProgress is a status line for the UI while the helper works.
var SolverProgress atomic.Value

// solverMissFor is how long a site the helper could not open is left alone.
const solverMissFor = 24 * time.Hour

var solverState = struct {
	sem    chan struct{} // one page at a time: a browser is the laptop's largest load
	mu     sync.Mutex
	misses map[string]time.Time
}{sem: make(chan struct{}, 1), misses: map[string]time.Time{}}

// errSolverSpent: this draw has had its helper request already.
var errSolverSpent = errors.New("the bot-check helper was asked once already for this")

// solverSkip names sites never sent to the helper. HathiTrust was taken
// out of W5F (2026-10-02): its records and reader stay behind Cloudflare
// even for Byparr (128 s timeouts).
func solverSkip(host string) bool {
	h := strings.ToLower(host)
	return h == "hathitrust.org" || strings.HasSuffix(h, ".hathitrust.org")
}

type solverBudgetKey struct{}

// SolverOnce gives ctx a single helper request: a Deep random draw tries
// several shelves, and each one waiting a minute would make a draw endless.
func SolverOnce(ctx context.Context) context.Context {
	return context.WithValue(ctx, solverBudgetKey{}, new(atomic.Bool))
}

// fileLike reports addresses of files rather than pages; the helper
// returns a rendered page, never the file's bytes.
func fileLike(u *url.URL) bool {
	switch strings.ToLower(path.Ext(u.Path)) {
	case ".pdf", ".epub", ".mobi", ".djvu", ".txt", ".zip", ".cbz", ".cbr", ".cb7", ".jpg", ".jpeg", ".png", ".gif", ".webp", ".mp3", ".ogg":
		return true
	}
	return false
}

func (f *Fetcher) solverFor(u *url.URL) bool {
	return f.SolverURL != "" && (u.Scheme == "http" || u.Scheme == "https") && !solverSkip(u.Hostname()) && !fileLike(u)
}

// blockedStatus is a bot wall's refusal (Cloudflare marks its challenges),
// as opposed to an ordinary 403 or a busy server.
func blockedStatus(resp *http.Response) bool {
	switch resp.StatusCode {
	case http.StatusForbidden, http.StatusTooManyRequests, http.StatusServiceUnavailable:
	default:
		return false
	}
	server := strings.ToLower(resp.Header.Get("Server"))
	return resp.Header.Get("cf-mitigated") != "" || strings.Contains(server, "ddos-guard")
}

func sameSite(a, b string) bool {
	a, b = strings.ToLower(a), strings.ToLower(b)
	if a == b {
		return true
	}
	ra, err1 := publicsuffix.EffectiveTLDPlusOne(a)
	rb, err2 := publicsuffix.EffectiveTLDPlusOne(b)
	return err1 == nil && err2 == nil && ra == rb
}

func solverEndpoint(raw string) (*url.URL, error) {
	base, err := url.Parse(raw)
	if err != nil || base == nil || (base.Scheme != "http" && base.Scheme != "https") || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return nil, errors.New("the bot-check helper's address must name a local HTTP service")
	}
	if host := base.Hostname(); host != "localhost" {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return nil, errors.New("the bot-check helper must be on localhost or a loopback address")
		}
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/v1"
	base.RawPath = ""
	return base, nil
}

// solve asks the helper for target's page.
func (f *Fetcher) solve(ctx context.Context, target *url.URL) (*Response, error) {
	base, err := solverEndpoint(f.SolverURL)
	if err != nil {
		return nil, err
	}
	host := strings.ToLower(target.Hostname())
	solverState.mu.Lock()
	missed, ok := solverState.misses[host]
	solverState.mu.Unlock()
	if ok && time.Since(missed) < solverMissFor {
		return nil, fmt.Errorf("the bot-check helper could not open %s at %s; not asked again today", host, missed.Format("15:04"))
	}
	if spent, _ := ctx.Value(solverBudgetKey{}).(*atomic.Bool); spent != nil && spent.Swap(true) {
		return nil, errSolverSpent
	}
	select {
	case solverState.sem <- struct{}{}:
		defer func() { <-solverState.sem }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	SolverProgress.Store("asking the bot-check helper for " + host + " (up to a minute)")
	defer SolverProgress.Store("")
	r, err := f.askSolver(ctx, base, target)
	if err != nil && ctx.Err() == nil && !errors.Is(err, errNoSolver) {
		solverState.mu.Lock()
		solverState.misses[host] = time.Now()
		solverState.mu.Unlock()
	}
	return r, err
}

// errNoSolver: nothing listens at SolverURL (not installed or not running).
var errNoSolver = errors.New("no bot-check helper is running")

func (f *Fetcher) askSolver(ctx context.Context, base, target *url.URL) (*Response, error) {
	payload, err := json.Marshal(map[string]any{
		"cmd": "request.get", "url": target.String(), "maxTimeout": 60000,
		"session": "w5f", "session_ttl_minutes": 15, "returnOnlyCookies": false,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base.String(), bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	// Do not follow a helper redirect: no page address should leave the local
	// service selected by the user.
	client := &http.Client{Timeout: 75 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		var op *net.OpError
		if errors.As(err, &op) && op.Op == "dial" {
			return nil, fmt.Errorf("%w at %s", errNoSolver, base.Host)
		}
		return nil, fmt.Errorf("bot-check helper: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the bot-check helper returned HTTP %d", resp.StatusCode)
	}
	var reply struct {
		Status   string `json:"status"`
		Solution struct {
			URL      string `json:"url"`
			Status   int    `json:"status"`
			Response string `json:"response"`
		} `json:"solution"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(&reply); err != nil {
		return nil, errors.New("the bot-check helper returned an invalid response")
	}
	if reply.Status != "ok" || reply.Solution.Status < 200 || reply.Solution.Status >= 400 || strings.TrimSpace(reply.Solution.Response) == "" {
		return nil, errors.New("the bot-check helper did not return a readable page")
	}
	final, err := url.Parse(reply.Solution.URL)
	if err != nil || final == nil || (final.Scheme != "http" && final.Scheme != "https") || final.User != nil || !sameSite(final.Hostname(), target.Hostname()) {
		return nil, errors.New("the bot-check helper ended up on another site")
	}
	body := []byte(reply.Solution.Response)
	if len(body) > 16<<20 {
		return nil, errors.New("the bot-check helper's page exceeds 16 MiB")
	}
	if ChallengeReason(body) != "" {
		return nil, errors.New("the bot-check helper still met the verification page")
	}
	return &Response{Body: body, URL: final, ContentType: "text/html; charset=utf-8", Fetched: time.Now()}, nil
}
