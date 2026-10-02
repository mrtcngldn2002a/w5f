package fetch

import (
	"compress/gzip"
	"compress/zlib"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/andybalholm/brotli"
	fhttp "github.com/bogdanfinn/fhttp"
	tlsclient "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
)

// CatalogUserAgent matches the TLS and HTTP/2 Chrome 133 profile below.
const CatalogUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36"

// ForCatalog reuses a browser-compatible connection pool and cookie jar for
// this fetcher. It does not launch a browser, run scripts or switch sources.
func (f *Fetcher) ForCatalog() (*Fetcher, error) {
	f.catalogOnce.Do(func() {
		transport := &catalogTransport{clients: map[string]tlsclient.HttpClient{}}
		client := *f.Client
		client.Transport = transport
		f.catalog = &Fetcher{Client: &client, UserAgent: CatalogUserAgent, CacheDir: f.CacheDir, Offline: f.Offline, Fresh: f.Fresh, HostGap: f.HostGap, HostGaps: f.HostGaps, last: map[string]time.Time{}}
		f.catalog.SolverURL = f.SolverURL
		f.catalog.catalog = f.catalog
		f.catalog.catalogOnce.Do(func() {})
	})
	return f.catalog, nil
}

// net/http owns redirects and cookie scope. The inner client only performs
// one round trip with Chrome's TLS extensions, HTTP/2 settings and header order.
type catalogTransport struct {
	mu      sync.Mutex
	clients map[string]tlsclient.HttpClient
}

func (t *catalogTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	proxy, err := http.ProxyFromEnvironment(req)
	if err != nil {
		return nil, err
	}
	key := ""
	if proxy != nil {
		key = proxy.String()
	}
	t.mu.Lock()
	c := t.clients[key]
	if c == nil {
		opts := []tlsclient.HttpClientOption{tlsclient.WithClientProfile(profiles.Chrome_133), tlsclient.WithRandomTLSExtensionOrder(), tlsclient.WithNotFollowRedirects(), tlsclient.WithTimeoutSeconds(300)}
		if key != "" {
			opts = append(opts, tlsclient.WithProxyUrl(key))
		}
		// Own decompression here for both HTTP versions. fhttp auto-decodes
		// HTTP/2 differently and its deflate decoder can block the header reader.
		opts = append(opts, tlsclient.WithTransportOptions(&tlsclient.TransportOptions{DisableCompression: true}))
		c, err = tlsclient.NewHttpClient(tlsclient.NewNoopLogger(), opts...)
		if err == nil {
			t.clients[key] = c
		}
	}
	t.mu.Unlock()
	if err != nil {
		return nil, err
	}
	r, err := fhttp.NewRequestWithContext(req.Context(), req.Method, req.URL.String(), req.Body)
	if err != nil {
		return nil, err
	}
	r.ContentLength = req.ContentLength
	r.Host = req.Host
	r.Header = fhttp.Header{}
	for k, v := range req.Header {
		r.Header[strings.ToLower(k)] = append([]string{}, v...)
	}
	r.Header["user-agent"] = []string{CatalogUserAgent}
	r.Header["sec-ch-ua"] = []string{`"Not(A:Brand";v="99", "Google Chrome";v="133", "Chromium";v="133"`}
	r.Header["sec-ch-ua-mobile"] = []string{"?0"}
	r.Header["sec-ch-ua-platform"] = []string{`"Windows"`}
	r.Header["accept"] = []string{"text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8"}
	r.Header["accept-language"] = []string{"en-US,en;q=0.9"}
	r.Header["accept-encoding"] = []string{"gzip, deflate, br"}
	r.Header["upgrade-insecure-requests"] = []string{"1"}
	r.Header["sec-fetch-dest"] = []string{"document"}
	r.Header["sec-fetch-mode"] = []string{"navigate"}
	r.Header["sec-fetch-user"] = []string{"?1"}
	site := "none"
	if ref, err := req.URL.Parse(req.Referer()); req.Referer() != "" && err == nil {
		site = "cross-site"
		if ref.Scheme == req.URL.Scheme && ref.Host == req.URL.Host {
			site = "same-origin"
		}
	}
	r.Header["sec-fetch-site"] = []string{site}
	r.Header[fhttp.HeaderOrderKey] = []string{"host", "connection", "content-length", "sec-ch-ua", "sec-ch-ua-mobile", "sec-ch-ua-platform", "upgrade-insecure-requests", "user-agent", "origin", "content-type", "accept", "sec-fetch-site", "sec-fetch-mode", "sec-fetch-user", "sec-fetch-dest", "referer", "accept-encoding", "accept-language", "cookie"}
	resp, err := c.Do(r)
	if err != nil {
		return nil, err
	}
	out := &http.Response{Status: resp.Status, StatusCode: resp.StatusCode, Proto: resp.Proto, ProtoMajor: resp.ProtoMajor, ProtoMinor: resp.ProtoMinor, Header: http.Header(resp.Header), Body: resp.Body, ContentLength: resp.ContentLength, Request: req}
	// fhttp's HTTP/2 transport already decodes the body, but keeps the
	// original encoding headers. Do not decompress that stream a second time.
	if resp.Uncompressed {
		out.Uncompressed = true
		out.ContentLength = -1
		out.Header.Del("Content-Encoding")
		out.Header.Del("Content-Length")
		return out, nil
	}
	var reader io.Reader
	switch strings.ToLower(resp.Header.Get("Content-Encoding")) {
	case "gzip":
		reader, err = gzip.NewReader(resp.Body)
	case "deflate":
		reader, err = zlib.NewReader(resp.Body)
	case "br":
		reader = brotli.NewReader(resp.Body)
	}
	if err != nil {
		resp.Body.Close()
		return nil, err
	}
	if reader != nil {
		out.Body = &decodedBody{Reader: reader, body: resp.Body}
		out.ContentLength = -1
		out.Uncompressed = true
		out.Header.Del("Content-Encoding")
		out.Header.Del("Content-Length")
	}
	return out, nil
}

type decodedBody struct {
	io.Reader
	body io.Closer
}

func (b *decodedBody) Close() error {
	if c, ok := b.Reader.(io.Closer); ok {
		c.Close()
	}
	return b.body.Close()
}
func (t *catalogTransport) CloseIdleConnections() {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, c := range t.clients {
		c.CloseIdleConnections()
	}
}
