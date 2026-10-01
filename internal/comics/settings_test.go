package comics

import (
	"context"
	"strings"
	"testing"

	"w5f/internal/comics/suwayomi/suwayomitest"
)

func settingsEnv(t *testing.T) (Env, *suwayomitest.Server) {
	env, _ := testEnv(t)
	f := suwayomitest.New(t)
	env.Server.Port = f.Port
	settingsMu.Lock()
	settingsCache.List = nil
	settingsMu.Unlock()
	return env, f
}

func TestSettingsPages(t *testing.T) {
	env, f := settingsEnv(t)
	ctx := context.Background()
	home, err := Route(ctx, "w5f:comics/settings", env)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"SOCKS proxy", "Downloader", "Authentication", "Cloudflare", "Conversions", "check for an update"} {
		if !strings.Contains(text(home), want) {
			t.Errorf("home lacks %q:\n%s", want, text(home))
		}
	}
	proxy, _ := Route(ctx, "w5f:comics/settings/proxy", env)
	if !hasLink(proxy, "w5f:comics/settings/set?g=proxy&n=socksProxyEnabled&v=on") || !hasLink(proxy, SettingFormPrefix+"socksProxyHost") {
		t.Fatalf("proxy page links: %+v", proxy.Links)
	}
	// on/off by a link: applied on the server.
	d, err := Route(ctx, "w5f:comics/settings/set?g=proxy&n=socksProxyEnabled&v=on", env)
	if err != nil || f.Values["socksProxyEnabled"] != true || !strings.Contains(text(d), "Socks proxy enabled changed") {
		t.Fatalf("toggle: %v %v\n%s", err, f.Values["socksProxyEnabled"], text(d))
	}
	// What W5F gives on the command line is W5F's.
	net, _ := Route(ctx, "w5f:comics/settings/network", env)
	if !strings.Contains(text(net), "W5F keeps it at 127.0.0.1") || hasLink(net, SettingFormPrefix+"ip") {
		t.Errorf("network:\n%s", text(net))
	}
	if _, err := Route(ctx, "w5f:comics/settings/set?n=ip&v=0.0.0.0", env); err == nil || f.Values["ip"] != "127.0.0.1" {
		t.Error("a W5F setting was changed")
	}
	// An enum's values are links.
	web, _ := Route(ctx, "w5f:comics/settings/webui", env)
	if !hasLink(web, "w5f:comics/settings/set?g=webui&n=webUIFlavor&v=VUI") {
		t.Errorf("enum links: %+v", web.Links)
	}
	// A read-only one says so.
	other, _ := Route(ctx, "w5f:comics/settings/other", env)
	if !strings.Contains(text(other), "does not let it be changed") {
		t.Errorf("read-only:\n%s", text(other))
	}
	cf, _ := Route(ctx, "w5f:comics/settings/cloudflare", env)
	if !strings.Contains(text(cf), "your choice") || !hasLink(cf, "w5f:comics/settings/set?g=cloudflare&n=flareSolverrEnabled&v=on") {
		t.Errorf("cloudflare:\n%s", text(cf))
	}
	// The Extension tab shows the stores, as the launcher does (Suwayomi 2.4
	// keeps them apart from its settings).
	ext, err := Route(ctx, "w5f:comics/settings/extension", env)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text(ext), "https://example.org/repo/index.min.json") || !hasLink(ext, RepoPage()) ||
		!hasLink(ext, "w5f:comics/repo/remove?back=settings&url=https%3A%2F%2Fexample.org%2Frepo%2Findex.min.json") {
		t.Errorf("extension tab:\n%s", text(ext))
	}
	if !strings.Contains(text(home), "Given by W5F at start: ") || !strings.Contains(text(home), "rootDir = ") {
		t.Errorf("home shows what W5F gives at start:\n%s", text(home))
	}
}

func TestSettingForms(t *testing.T) {
	env, f := settingsEnv(t)
	ctx := context.Background()
	Route(ctx, "w5f:comics/settings", env)
	form, err := env.Form(SettingFormPrefix + "socksProxyHost")
	if err != nil || len(form.Fields) != 1 || form.Fields[0].Hidden {
		t.Fatalf("%v %+v", err, form)
	}
	next, _, err := form.Save([]string{"proxy.lan"})
	if err != nil || f.Values["socksProxyHost"] != "proxy.lan" || !strings.HasPrefix(next, "w5f:comics/settings/proxy") {
		t.Fatalf("%v %q %v", err, next, f.Values["socksProxyHost"])
	}
	d, _ := Route(ctx, next, env)
	if !strings.Contains(text(d), "proxy.lan") || !strings.Contains(text(d), "saved") {
		t.Errorf("after saving:\n%s", text(d))
	}
	pw, _ := env.Form(SettingFormPrefix + "socksProxyPassword")
	if !pw.Fields[0].Hidden {
		t.Error("a password field shows what is typed")
	}
	pw.Save([]string{"s3cret"})
	d, _ = Route(ctx, "w5f:comics/settings/proxy", env)
	if strings.Contains(text(d), "s3cret") || !strings.Contains(text(d), "(set, hidden)") {
		t.Errorf("a secret is shown:\n%s", text(d))
	}
	conv, _ := env.Form(SettingFormPrefix + "downloadConversions")
	if !strings.Contains(conv.Fields[0].Hint, "JSON") {
		t.Errorf("hint: %q", conv.Fields[0].Hint)
	}
	if _, _, err := conv.Save([]string{"not json"}); err == nil {
		t.Error("bad JSON accepted")
	}
	num, _ := env.Form(SettingFormPrefix + "opdsItemsPerPage")
	if _, _, err := num.Save([]string{"0"}); err == nil || !strings.Contains(err.Error(), "Validation") {
		t.Errorf("the server's check: %v", err)
	}
}

func TestAuthenticationKeepsW5FSignedIn(t *testing.T) {
	env, f := settingsEnv(t)
	ctx := context.Background()
	Route(ctx, "w5f:comics/settings", env)
	auth, _ := Route(ctx, "w5f:comics/settings/auth", env)
	if !hasLink(auth, AuthFormPrefix) || hasLink(auth, SettingFormPrefix+"authPassword") {
		t.Fatalf("auth page: %+v", auth.Links)
	}
	form, err := env.Form(AuthFormPrefix)
	if err != nil || !form.Fields[2].Hidden {
		t.Fatalf("%v %+v", err, form)
	}
	if _, _, err := form.Save([]string{"BASIC_AUTH", "ayse", ""}); err == nil {
		t.Error("a mode with no password accepted")
	}
	next, _, err := form.Save([]string{"basic_auth", "ayse", "gizli"})
	if err != nil || f.Values["authMode"] != "BASIC_AUTH" || f.Values["authPassword"] != "gizli" {
		t.Fatalf("%v %v", err, f.Values)
	}
	if a := env.Server.Auth(); a.Mode != "BASIC_AUTH" || a.Password != "gizli" {
		t.Fatalf("W5F's account: %+v", a)
	}
	// W5F still reads the server.
	d, err := Route(ctx, next, env)
	if err != nil || !strings.Contains(text(d), "BASIC_AUTH") {
		t.Fatalf("after: %v\n%s", err, text(d))
	}
	// Another client changes the password: W5F asks for the account.
	f.Values["authPassword"] = "other"
	settingsMu.Lock()
	settingsCache.List = nil
	settingsMu.Unlock()
	d, err = Route(ctx, "w5f:comics/settings", env)
	if err != nil || !hasLink(d, LoginFormPrefix) {
		t.Fatalf("unauthorized: %v\n%s", err, text(d))
	}
	login := env.loginForm()
	login.Save([]string{"BASIC_AUTH", "ayse", "other"})
	if d, err = Route(ctx, "w5f:comics/settings", env); err != nil || hasLink(d, LoginFormPrefix) {
		t.Fatalf("after signing in: %v\n%s", err, text(d))
	}
	// Back to NONE: W5F forgets the account.
	form, _ = env.Form(AuthFormPrefix)
	if _, _, err := form.Save([]string{"NONE", "", ""}); err != nil {
		t.Fatal(err)
	}
	if env.Server.Auth().On() || f.Values["authMode"] != "NONE" {
		t.Error("NONE")
	}
}

func TestLabels(t *testing.T) {
	for in, want := range map[string]string{"socksProxyHost": "Socks proxy host", "webUIFlavor": "Web UI flavor", "maxSourcesInParallel": "Max sources in parallel", "opdsItemsPerPage": "OPDS items per page", "flareSolverrUrl": "Flare solverr URL"} {
		if got := label(in); got != want {
			t.Errorf("%s: %q", in, got)
		}
	}
}
