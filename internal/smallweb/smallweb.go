// Package smallweb reads the small internet: Gemini capsules and Gopher
// holes, converted into W5F documents like any page.
package smallweb

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"w5f/internal/doc"
)

// Env says where known hosts and the cache live.
type Env struct {
	DataDir  string // gemini_hosts (trust on first use)
	CacheDir string // cached responses ("" = no cache)
	Offline  bool
	Timeout  time.Duration // per request; 0 = 20 s
}

// maxBody caps a response.
const maxBody = 2 << 20

// freshFor is how long a cached response is used without asking again.
const freshFor = 15 * time.Minute

// IsTarget reports gemini:// and gopher:// addresses and W5F's own pages.
func IsTarget(t string) bool {
	l := strings.ToLower(t)
	return strings.HasPrefix(l, "gemini://") || strings.HasPrefix(l, "gopher://") || t == "w5f:smallweb" || strings.HasPrefix(t, "w5f:smallweb/")
}

func (e Env) timeout() time.Duration {
	if e.Timeout > 0 {
		return e.Timeout
	}
	return 20 * time.Second
}

// response is a raw answer (also what the cache stores).
type response struct {
	Status  int       `json:"status"` // gemini status; gopher uses 20
	Meta    string    `json:"meta"`   // gemini meta; gopher: the item type
	Body    []byte    `json:"body"`
	URL     string    `json:"url"`
	Fetched time.Time `json:"fetched"`
}

func (e Env) cachePath(u string) string {
	if e.CacheDir == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(u))
	return filepath.Join(e.CacheDir, "smallweb", hex.EncodeToString(sum[:])+".json")
}

func (e Env) cached(u string) (*response, bool) {
	p := e.cachePath(u)
	if p == "" {
		return nil, false
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, false
	}
	var r response
	if json.Unmarshal(b, &r) != nil {
		return nil, false
	}
	return &r, e.Offline || time.Since(r.Fetched) < freshFor
}

func (e Env) store(r *response) {
	p := e.cachePath(r.URL)
	if p == "" || r.Status/10 != 2 {
		return
	}
	if os.MkdirAll(filepath.Dir(p), 0o755) != nil {
		return
	}
	b, _ := json.Marshal(r)
	_ = os.WriteFile(p, b, 0o644)
}

// fetch gets one address, from the cache when fresh.
func (e Env) fetch(ctx context.Context, u *url.URL) (*response, error) {
	if r, fresh := e.cached(u.String()); fresh {
		return r, nil
	}
	if e.Offline {
		return nil, errors.New("offline and not in the cache")
	}
	var r *response
	var err error
	switch strings.ToLower(u.Scheme) {
	case "gemini":
		r, err = e.gemini(ctx, u)
	case "gopher":
		r, err = e.gopher(ctx, u)
	default:
		err = fmt.Errorf("unsupported scheme %s", u.Scheme)
	}
	if err != nil {
		if old, _ := e.cached(u.String()); old != nil {
			return old, nil // stale copy beats an error
		}
		return nil, err
	}
	r.URL, r.Fetched = u.String(), time.Now()
	e.store(r)
	return r, nil
}

// Load opens a gemini:// or gopher:// address as a document.
func Load(ctx context.Context, e Env, raw string) (*doc.Document, error) {
	if raw == "w5f:smallweb" {
		return menuDoc(), nil
	}
	if IsInputPage(raw) {
		// A web search page is built from its address; a capsule's input
		// page is asked for again.
		q, err := url.ParseQuery(strings.TrimPrefix(raw, inputPrefix))
		if err != nil || q.Get("u") == "" {
			return nil, errors.New("bad input address")
		}
		if q.Get("k") == "" {
			return Load(ctx, e, q.Get("u"))
		}
		su, err := url.Parse(q.Get("u"))
		// Web searches, and W5F's own Comics questions (source search, add
		// a repository), which answer to w5f:comics/ addresses.
		if err != nil || (su.Scheme != "http" && su.Scheme != "https" && !strings.HasPrefix(q.Get("u"), "w5f:comics/")) {
			return nil, errors.New("bad search address")
		}
		d := inputPage(raw, su, q.Get("p"), false)
		d.Origin = "local"
		return d, nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	for hop := 0; hop < 6; hop++ {
		if strings.EqualFold(u.Scheme, "gopher") {
			return e.gopherDoc(ctx, u)
		}
		r, err := e.fetch(ctx, u)
		if err != nil {
			return nil, err
		}
		switch r.Status / 10 {
		case 1:
			return inputDoc(u, r.Meta, r.Status == 11), nil
		case 2:
			return geminiDoc(u, r), nil
		case 3:
			next, err := u.Parse(strings.TrimSpace(r.Meta))
			if err != nil {
				return nil, fmt.Errorf("bad redirect %q", r.Meta)
			}
			if !strings.EqualFold(next.Scheme, "gemini") {
				d := &doc.Document{Title: "Redirect", URL: u.String(), Origin: "live"}
				d.Links = []doc.Link{{Href: next.String(), Text: next.String()}}
				d.Blocks = []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: "This capsule sends you to another kind of address: "}, {Text: next.String(), Link: 1}}}}
				return d, nil
			}
			u = next
		case 4, 5:
			return nil, fmt.Errorf("Gemini %d: %s", r.Status, r.Meta)
		case 6:
			return nil, fmt.Errorf("Gemini %d: this capsule wants a client certificate, which W5F does not support (%s)", r.Status, r.Meta)
		default:
			return nil, fmt.Errorf("Gemini: unknown status %d", r.Status)
		}
	}
	return nil, errors.New("Gemini: too many redirects")
}

// inputPrefix starts the address of an input page.
const inputPrefix = "w5f:smallweb/input?"

// IsInputPage reports a page that asks for input (enter answers it).
func IsInputPage(docURL string) bool { return strings.HasPrefix(docURL, inputPrefix) }

// WebSearchPage is the address of an input page that searches a web site:
// the answer goes to searchURL?<key>=<text>.
func WebSearchPage(searchURL, key, prompt string) string {
	return inputPrefix + url.Values{"u": {searchURL}, "k": {key}, "p": {prompt}}.Encode()
}

// inputDoc asks for the input a capsule (or a Gopher search) wants; the
// reader answers with enter (or g → ? <text>).
func inputDoc(u *url.URL, prompt string, sensitive bool) *doc.Document {
	return inputPage(inputPrefix+url.Values{"u": {u.String()}}.Encode(), u, prompt, sensitive)
}

func inputPage(address string, u *url.URL, prompt string, sensitive bool) *doc.Document {
	d := &doc.Document{Title: "Input requested", URL: address, Origin: "live"}
	if prompt == "" {
		prompt = "This address asks for input."
	}
	d.Blocks = []doc.Block{
		doc.Notice{Kind: "info", Text: prompt},
		doc.Paragraph{Text: doc.Inline{{Text: "Press "}, {Text: "enter", Style: doc.Bold}, {Text: " and type your answer (or "}, {Text: "g → ? <your text>", Style: doc.Code}, {Text: ")  (" + u.Host + ")", Style: doc.Italic}}},
	}
	if sensitive {
		d.Blocks = append(d.Blocks, doc.Notice{Kind: "warn", Text: "The capsule marks this input as sensitive; the g prompt shows what you type."})
	}
	return d
}

// InputCommand turns "? text" typed at the g prompt on an input page into
// the address that answers it ("" when it does not apply).
func InputCommand(docURL, input string) string {
	in := strings.TrimSpace(input)
	if !strings.HasPrefix(in, "?") || !IsInputPage(docURL) {
		return ""
	}
	q, err := url.ParseQuery(strings.TrimPrefix(docURL, inputPrefix))
	if err != nil || q.Get("u") == "" {
		return ""
	}
	text := strings.TrimSpace(strings.TrimPrefix(in, "?"))
	if text == "" {
		return ""
	}
	target := q.Get("u")
	if k := q.Get("k"); k != "" {
		return target + "?" + url.Values{k: {text}}.Encode()
	}
	if strings.HasPrefix(strings.ToLower(target), "gopher://") {
		return target + "%09" + url.PathEscape(text)
	}
	if i := strings.Index(target, "?"); i >= 0 {
		target = target[:i]
	}
	return target + "?" + strings.ReplaceAll(url.QueryEscape(text), "+", "%20")
}
