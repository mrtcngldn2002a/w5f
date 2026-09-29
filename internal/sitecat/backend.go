package sitecat

import (
	"bytes"
	"context"
	"net/url"
	"path"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"w5f/internal/fetch"
)

// Request is one page request: GET, or POST with a form body.
type Request struct {
	Method string // "GET" (default) or "POST"
	URL    string
	Form   url.Values
}

func (r Request) key() string { return r.Method + " " + r.URL + " " + r.Form.Encode() }

// pageOut is what a backend reads from one result page.
type pageOut struct {
	Results []Result
	Next    *Request // nil on the last page
}

// candidate is one way of searching a site, found while checking it.
type candidate struct {
	Spec SearchSpec
	Desc string // for the check page
}

// site is what detectors see while a site is checked.
type site struct {
	F         *fetch.Fetcher
	Home      *url.URL
	Body      []byte
	Doc       *goquery.Document
	Generator string // meta generator, lower case
	// Pages are the pages whose search forms count: the home page and a
	// linked search page ("Find a book"), if any.
	Pages []sitePage
}

type sitePage struct {
	URL  *url.URL
	Body []byte
}

// backend is how one kind of catalog is searched.
type backend interface {
	// First builds the request for page 1; fieldIgnored as in BuildURL.
	First(p *Profile, q Query) (req Request, fieldIgnored bool)
	// Page reads one result page. pageNo starts at 1.
	Page(ctx context.Context, f *fetch.Fetcher, p *Profile, pageNo int, body []byte, pageURL string) (pageOut, error)
	// Item lists the downloads of one result.
	Item(ctx context.Context, f *fetch.Fetcher, p *Profile, itemURL string) ([]Download, error)
}

// localSearcher is a backend that searches without requests (a file index).
type localSearcher interface {
	SearchLocal(p *Profile, q Query) (rs []Result, fieldIgnored bool, err error)
}

// localProber checks a local-search candidate; note describes what it found.
type localProber interface {
	ProbeLocal(ctx context.Context, f *fetch.Fetcher, p *Profile, word string) (rs []Result, note string, err error)
}

// detector proposes candidates for a site, best first.
type detector func(ctx context.Context, s *site) []candidate

// detectors run in this order when a site is checked.
var detectors = []detector{detectArchive, detectDSpace, detectHTML, detectHints, detectOPDSPath, detectIndex, detectBrowse}

// engineDetector is the last resort, tried only when every other candidate
// failed (set by the search-engine backend).
var engineDetector detector

// maxAttempts caps the candidates tried before the engine fallback.
const maxAttempts = 6

// backendFor returns the backend of a search kind, nil if unknown.
func backendFor(kind string) backend {
	switch kind {
	case "opensearch", "form", "post", "wordpress", "browse":
		return htmlBackend{}
	case "opds":
		return opdsBackend{}
	case "archive":
		return archiveBackend{}
	case "dspace":
		return dspaceBackend{}
	case "index":
		return indexBackend{}
	case "engine":
		return engineBackend{}
	}
	return nil
}

// doRequest performs a GET (cached) or POST (uncached) page request.
func doRequest(ctx context.Context, f *fetch.Fetcher, r Request) (*fetch.Response, error) {
	if strings.EqualFold(r.Method, "POST") {
		return f.PostForm(ctx, r.URL, r.Form)
	}
	u, err := url.Parse(r.URL)
	if err != nil {
		return nil, err
	}
	return f.Get(ctx, u, fetch.Options{})
}

// newSite prepares a checked site: parsed home page, generator and a
// linked search page.
func newSite(ctx context.Context, f *fetch.Fetcher, home *fetch.Response) *site {
	s := &site{F: f, Home: home.URL, Body: home.Body, Pages: []sitePage{{URL: home.URL, Body: home.Body}}}
	if gq, err := goquery.NewDocumentFromReader(bytes.NewReader(home.Body)); err == nil {
		s.Doc = gq
		s.Generator = strings.ToLower(gq.Find(`meta[name="generator"], meta[name="Generator"]`).First().AttrOr("content", ""))
	}
	if su := searchPageLink(home.Body, home.URL); su != nil {
		if sp, err := f.Get(ctx, su, fetch.Options{}); err == nil {
			s.Pages = append(s.Pages, sitePage{URL: sp.URL, Body: sp.Body})
		}
	}
	return s
}

// fileFormatOf names the book format of a file address: by the path's
// extension, or by a query value naming a file ("link.php?file=x.pdf").
func fileFormatOf(u *url.URL) string {
	if f := extFormats[strings.ToLower(path.Ext(u.Path))]; f != "" {
		return f
	}
	for _, vs := range u.Query() {
		for _, v := range vs {
			if f := extFormats[strings.ToLower(path.Ext(v))]; f != "" {
				return f
			}
		}
	}
	return ""
}

// directDownload treats an address that is itself a book file as its own
// download.
func directDownload(itemURL string) (Download, bool) {
	u, err := url.Parse(itemURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return Download{}, false
	}
	f := fileFormatOf(u)
	if f == "" {
		return Download{}, false
	}
	name, _ := url.PathUnescape(path.Base(u.Path))
	return Download{URL: itemURL, Format: f, Label: name}, true
}

// itemDownloads lists the downloads of a result for its catalog's kind.
func itemDownloads(ctx context.Context, f *fetch.Fetcher, p *Profile, itemURL string) ([]Download, error) {
	if p.Search.Kind == "libgen" {
		h, err := libgenHash(itemURL)
		if err != nil {
			return nil, err
		}
		c, err := libgenClient(f, p)
		if err != nil {
			return nil, err
		}
		b, err := c.Details(ctx, h)
		if err != nil {
			return nil, err
		}
		return []Download{{URL: "libgen:" + h, Format: b.Extension, Label: b.Title + " — " + b.Author}}, nil
	}
	if d, ok := directDownload(itemURL); ok {
		return []Download{d}, nil
	}
	if b := backendFor(p.Search.Kind); b != nil {
		return b.Item(ctx, f, p, itemURL)
	}
	return FindDownloads(ctx, f, itemURL, p.Download.Prefer)
}

// notDownloadable explains why a result has no downloads (e.g. an Internet
// Archive lending-library item). It is shown as a note, not an error.
type notDownloadable struct{ Reason string }

func (e *notDownloadable) Error() string { return e.Reason }
