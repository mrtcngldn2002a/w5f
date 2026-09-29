package fetch

import (
	"compress/gzip"
	"compress/zlib"
	"context"
	"crypto/x509"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"
	tlsclient "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
)

func TestCatalogTransportSessionRedirectCompression(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.UserAgent() != CatalogUserAgent || r.Header.Get("Sec-Ch-Ua") == "" {
			t.Error("inconsistent browser headers")
		}
		if strings.Contains(strings.Join(r.Header.Values("User-Agent"), " "), "w5f/") {
			t.Error("duplicate User-Agent")
		}
		switch r.URL.Path {
		case "/":
			http.SetCookie(w, &http.Cookie{Name: "catalog_session", Value: "ok", Path: "/"})
			http.Redirect(w, r, "/result", 302)
		case "/result":
			c, e := r.Cookie("catalog_session")
			if e != nil || c.Value != "ok" {
				t.Error("redirect lost cookie")
			}
			w.Header().Set("Content-Encoding", "gzip")
			z := gzip.NewWriter(w)
			fmt.Fprint(z, "catalog results")
			z.Close()
		}
	}))
	defer srv.Close()
	f := New("", "test")
	f.HostGap = 0
	c, err := f.ForCatalog()
	if err != nil {
		t.Fatal(err)
	}
	again, _ := c.ForCatalog()
	if c != again {
		t.Fatal("catalog client wasn't reused")
	}
	u, _ := url.Parse(srv.URL)
	r, err := c.Get(context.Background(), u, Options{})
	if err != nil || string(r.Body) != "catalog results" || r.URL.Path != "/result" {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestCatalogHTTP2DecodesExactlyOnce(t *testing.T) {
	for _, encoding := range []string{"gzip", "br", "deflate"} {
		t.Run(encoding, func(t *testing.T) {
			srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.ProtoMajor != 2 {
					t.Errorf("expected HTTP/2, got %s", r.Proto)
				}
				w.Header().Set("Content-Encoding", encoding)
				switch encoding {
				case "gzip":
					z := gzip.NewWriter(w)
					fmt.Fprint(z, "AO3 works")
					z.Close()
				case "br":
					z := brotli.NewWriter(w)
					fmt.Fprint(z, "AO3 works")
					z.Close()
				case "deflate":
					z := zlib.NewWriter(w)
					fmt.Fprint(z, "AO3 works")
					z.Close()
				}
			}))
			srv.EnableHTTP2 = true
			srv.StartTLS()
			defer srv.Close()
			roots := x509.NewCertPool()
			roots.AddCert(srv.Certificate())
			client, err := tlsclient.NewHttpClient(tlsclient.NewNoopLogger(), tlsclient.WithClientProfile(profiles.Chrome_133), tlsclient.WithTransportOptions(&tlsclient.TransportOptions{RootCAs: roots, DisableCompression: true}))
			if err != nil {
				t.Fatal(err)
			}
			defer client.CloseIdleConnections()
			f := New("", "test")
			f.Client.Transport = &catalogTransport{clients: map[string]tlsclient.HttpClient{"": client}}
			u, _ := url.Parse(srv.URL)
			r, err := f.Get(context.Background(), u, Options{})
			if err != nil {
				t.Fatal(err)
			}
			if string(r.Body) != "AO3 works" {
				t.Fatalf("unexpected decoded content: %q", r.Body)
			}
		})
	}
}

func TestCatalogTransportRejectsUntrustedTLS(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "ok") }))
	defer srv.Close()
	f, _ := New("", "test").ForCatalog()
	u, _ := url.Parse(srv.URL)
	if _, err := f.Get(context.Background(), u, Options{}); err == nil {
		t.Fatal("TLS certificate verification was disabled")
	}
}
