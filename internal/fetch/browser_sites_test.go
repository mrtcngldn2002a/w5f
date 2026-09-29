package fetch

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type pageTestTransport func(*http.Request) (*http.Response, error)

func (rt pageTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return rt(r) }

func TestDiscoveryHostTransportAndOfflineCache(t *testing.T) {
	for _, host := range []string{"www.britannica.com", "hermetic.com", "archive.sacred-texts.com"} {
		t.Run(host, func(t *testing.T) {
			f := New(t.TempDir(), "test")
			f.HostGap = 0
			calls := 0
			f.Client.Transport = pageTestTransport(func(r *http.Request) (*http.Response, error) {
				t.Error("used ordinary transport")
				return nil, errors.New("wrong transport")
			})
			f.catalog = New("", "test")
			f.catalog.UserAgent = CatalogUserAgent
			f.catalog.Client.Transport = pageTestTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.UserAgent() != CatalogUserAgent {
					t.Error("wrong User-Agent")
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader("<title>Reading</title><p>Text</p>")), Request: r}, nil
			})
			f.catalogOnce.Do(func() {})
			u, _ := url.Parse("https://" + host + "/article")
			if _, err := f.Get(context.Background(), u, Options{}); err != nil {
				t.Fatal(err)
			}
			// Switching offline after the compatible client exists must still use
			// the original fetcher's current policy and cache.
			f.Offline = true
			r, err := f.Get(context.Background(), u, Options{Revalidate: true})
			if err != nil || !r.FromCache || calls != 1 {
				t.Fatalf("cache/session mismatch: %+v %v calls=%d", r, err, calls)
			}
			u.Path = "/uncached"
			if _, err := f.Get(context.Background(), u, Options{}); !errors.Is(err, ErrOffline) {
				t.Fatalf("offline request escaped: %v", err)
			}
		})
	}
	for _, host := range []string{"example.org", "hermetic.com.evil.test", "evilbritannica.com", "other.sacred-texts.com"} {
		if browserPageHost(host) {
			t.Errorf("unexpected host match: %s", host)
		}
	}
}
