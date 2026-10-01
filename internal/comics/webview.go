package comics

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"w5f/internal/browser"
	"w5f/internal/comics/suwayomi"
	"w5f/internal/doc"
)

// "Open in WebView", as Suwayomi's own clients have it (asked for by the
// owner, 2026-10-01): a source's, a series' or a chapter's page opens in
// Suwayomi's WebView (KCEF), shown through the server's /api/v1/webview
// page in the browser. What you sign in to or answer there stays in
// Suwayomi's cookies, for its sources to use.

// openBrowser is how the page is shown (a variable for tests).
var openBrowser = browser.Open

// browserName names it (a variable for tests).
var browserName = browser.Name

func webviewHref(target, back string) string {
	return "w5f:comics/webview?" + url.Values{"u": {target}, "back": {back}}.Encode()
}

// encodeURIComponent escapes as JavaScript's does (the WebView page reads
// its address back with decodeURIComponent).
func encodeURIComponent(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}

func webviewDoc(ctx context.Context, env Env, target, back string) (*doc.Document, error) {
	u, err := url.Parse(target)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("only web pages open in the WebView (%q is not one)", target)
	}
	if !strings.HasPrefix(back, "w5f:comics") {
		back = "w5f:comics"
	}
	p := newPage("Open in WebView", webviewHref(target, back))
	if st, err := env.serverSettings(ctx, false); err == nil && st.Values["kcefEnabled"] == false {
		p.note("warn", "Suwayomi's WebView is off. Turn it on in Server settings → Webview, then open this again.")
		p.para(doc.Inline{p.a("w5f:comics/settings/webview", "Server settings → Webview"), dim("   "), p.a(back, "← back")})
		return p.d, nil
	}
	view := env.Server.Addr() + "/api/v1/webview#" + encodeURIComponent(target)
	if err := openBrowser(view); err != nil {
		return nil, err
	}
	p.note("info", "Opened in Suwayomi's WebView (in "+browserName()+"): "+target)
	p.para(doc.Inline{dim("This is Suwayomi's own browser: if the site asks you to sign in or to confirm you are a person, do it there yourself, and Suwayomi keeps the cookies for its sources. Close the window when you are done, then reload the page here (ctrl+r).")})
	p.para(doc.Inline{p.a(back, "← back")})
	return p.d, nil
}

// webviewLink is a page's "open in WebView" link (none for a page that is
// not on the web, such as the Local source's).
func webviewLink(p *page, target, back string) []doc.Span {
	if !strings.HasPrefix(target, "http://") && !strings.HasPrefix(target, "https://") {
		return nil
	}
	return []doc.Span{dim("   "), p.a(webviewHref(target, back), "open in WebView")}
}

// failedDoc is a page that could not be loaded from its source, with the
// way to look at the site itself.
func failedDoc(self, title string, err error, target, back string) *doc.Document {
	p := newPage(title, self)
	p.note("warn", err.Error())
	in := doc.Inline{p.a(back, "← back")}
	if w := webviewLink(p, target, self); w != nil {
		in = append(in, w...)
		p.para(doc.Inline{dim("The source could not load this. Open the site in Suwayomi's WebView to see what it says — a page that moved, a sign-in, a check for people — then come back and reload (ctrl+r).")})
	}
	p.para(in)
	return p.d
}

// sourceHome is a source's web address ("" when unknown).
func sourceHome(ctx context.Context, c *suwayomi.Client, id string) string {
	srcs, err := c.Sources(ctx)
	if err != nil {
		return ""
	}
	for _, s := range srcs {
		if s.ID == id {
			return s.HomeURL
		}
	}
	return ""
}
