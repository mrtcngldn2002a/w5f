package comics

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestOpenInWebView(t *testing.T) {
	env, f := settingsEnv(t)
	ctx := context.Background()
	var opened []string
	old := openBrowser
	openBrowser = func(u string) error { opened = append(opened, u); return nil }
	t.Cleanup(func() { openBrowser = old })

	target := "https://example.org/title/1 a?x=y&z=ü"
	d, err := Route(ctx, webviewHref(target, "w5f:comics/manga/7"), env)
	if err != nil {
		t.Fatal(err)
	}
	want := env.Server.Addr() + "/api/v1/webview#https%3A%2F%2Fexample.org%2Ftitle%2F1%20a%3Fx%3Dy%26z%3D%C3%BC"
	if len(opened) != 1 || opened[0] != want {
		t.Errorf("opened %v, want %s", opened, want)
	}
	if !strings.Contains(text(d), "Suwayomi keeps the cookies") || !hasLink(d, "w5f:comics/manga/7") {
		t.Errorf("page:\n%s", text(d))
	}

	// Only web pages; a back link stays within Comics.
	for _, bad := range []string{"file:///etc/passwd", "javascript:alert(1)", "w5f:usenet"} {
		if _, err := Route(ctx, webviewHref(bad, ""), env); err == nil {
			t.Errorf("%s opened", bad)
		}
	}
	d, _ = Route(ctx, webviewHref("https://example.org/", "https://evil.example"), env)
	if hasLink(d, "https://evil.example") {
		t.Error("back went outside Comics")
	}

	// With the WebView off, it says where to turn it on and opens nothing.
	f.Values["kcefEnabled"] = false
	forgetSettings()
	opened = nil
	d, err = Route(ctx, webviewHref(target, ""), env)
	if err != nil || len(opened) != 0 || !strings.Contains(text(d), "WebView is off") || !hasLink(d, "w5f:comics/settings/webview") {
		t.Errorf("off: %v %v\n%s", err, opened, text(d))
	}
}

func TestFailedPageOffersTheWebView(t *testing.T) {
	d := failedDoc("w5f:comics/manga/7", "A series", errors.New("HTTP error 404"), "https://example.org/series/7", "w5f:comics/following")
	if !strings.Contains(text(d), "HTTP error 404") || !hasLink(d, webviewHref("https://example.org/series/7", "w5f:comics/manga/7")) {
		t.Errorf("failed page:\n%s %+v", text(d), d.Links)
	}
	d = failedDoc("w5f:comics/manga/7", "A series", errors.New("no"), "", "w5f:comics/following")
	for _, l := range d.Links {
		if strings.Contains(l.Href, "webview") {
			t.Error("no WebView link without a web address")
		}
	}
}
