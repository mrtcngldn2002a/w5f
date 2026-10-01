package comics

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSourceSettingsPage(t *testing.T) {
	env, f := settingsEnv(t)
	ctx := context.Background()
	d, err := Route(ctx, "w5f:comics/source/42/settings", env)
	if err != nil {
		t.Fatal(err)
	}
	tx := text(d)
	if d.Title != "Test Source: settings" {
		t.Errorf("title %q", d.Title)
	}
	for _, want := range []string{"Show author's notes: on", "Image quality: High", "Languages: English", "User agent: (empty)"} {
		if !strings.Contains(tx, want) {
			t.Errorf("lacks %q:\n%s", want, tx)
		}
	}
	if strings.Contains(tx, "Hidden one") {
		t.Error("an invisible setting is shown")
	}
	if !hasLink(d, "w5f:comics/source/42/pref?p=0&v=off") || !hasLink(d, "w5f:comics/source/42/pref?p=1&v=low") ||
		!hasLink(d, "w5f:comics/source/42/pref?p=2&t=1&v=tr") || !hasLink(d, SourcePrefFormPrefix+"42/3") {
		t.Errorf("links: %+v", d.Links)
	}

	// A switch, a list choice and a toggled multiple choice.
	for _, href := range []string{"w5f:comics/source/42/pref?p=0&v=off", "w5f:comics/source/42/pref?p=1&v=low", "w5f:comics/source/42/pref?p=2&t=1&v=tr"} {
		if _, err := Route(ctx, href, env); err != nil {
			t.Fatalf("%s: %v", href, err)
		}
	}
	if f.Prefs[0]["currentValue"] != false || f.Prefs[1]["currentValue"] != "low" {
		t.Errorf("switch/list: %v %v", f.Prefs[0]["currentValue"], f.Prefs[1]["currentValue"])
	}
	if langs, _ := f.Prefs[2]["currentValue"].([]any); len(langs) != 2 {
		t.Errorf("toggle adds tr: %v", f.Prefs[2]["currentValue"])
	}
	Route(ctx, "w5f:comics/source/42/pref?p=2&t=1&v=en", env)
	if langs, _ := f.Prefs[2]["currentValue"].([]any); len(langs) != 1 || langs[0] != "tr" {
		t.Errorf("toggle removes en: %v", f.Prefs[2]["currentValue"])
	}
	if _, err := Route(ctx, "w5f:comics/source/42/pref?p=1&v=evil", env); err == nil {
		t.Error("a value that is not a choice is refused")
	}

	// A text setting through its form.
	form, err := env.Form(SourcePrefFormPrefix + "42/3")
	if err != nil || form.Title != "User agent" {
		t.Fatalf("form: %v %+v", err, form)
	}
	if next, _, err := form.Save([]string{"  W5F test  "}); err != nil || next != "w5f:comics/source/42/settings" || f.Prefs[3]["currentValue"] != "W5F test" {
		t.Errorf("form save: %v %q %v", err, next, f.Prefs[3]["currentValue"])
	}
	if !IsForm(SourcePrefFormPrefix + "42/3") {
		t.Error("the form link is a form")
	}
	if _, err := Route(ctx, "w5f:comics/source/7/settings", env); err == nil {
		t.Error("an unknown source is an error")
	}
}

// Another Suwayomi's server.conf (as its launcher writes it) against this
// server: what differs is copied, what belongs to one computer is not.
const otherConf = `# Server ip and port bindings.
server.ip = "0.0.0.0" # default: "0.0.0.0"
server.port = 4567
server.socksProxyEnabled = true
server.socksProxyHost = ""
server.socksProxyPassword = "secret"
server.downloadsPath = "C:\\Comics"
server.autoDownloadNewChapters = true # default: false ; download new chapters
server.globalUpdateInterval = 24.0
server.opdsItemsPerPage = 50.0
server.webUIFlavor = "VUI"
server.flareSolverrEnabled = true
server.authMode = BASIC_AUTH
server.extensionStores = [
    "https://example.org/repo/index.min.json",
    "https://second.example/index.min.json" # a second store
] # default: []
`

func TestSyncFromAnotherServerConf(t *testing.T) {
	env, f := settingsEnv(t)
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "server.conf")
	os.WriteFile(path, []byte(otherConf), 0o644)
	items, missing, err := env.SyncPlan(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]SyncItem{}
	for _, it := range items {
		got[it.Name] = it
	}
	copyNames := []string{"autoDownloadNewChapters", "globalUpdateInterval"}
	for _, n := range copyNames {
		if it := got[n]; it.Same() || it.Skip != "" {
			t.Errorf("%s should be copied: %+v", n, it)
		}
	}
	if !got["opdsItemsPerPage"].Same() {
		t.Errorf("50.0 is 50: %+v", got["opdsItemsPerPage"])
	}
	for n, why := range map[string]string{"ip": "W5F gives", "socksProxyEnabled": "network", "downloadsPath": "own", "webUIFlavor": "own",
		"flareSolverrEnabled": "bot-check", "authMode": "account"} {
		if it := got[n]; !strings.Contains(it.Skip, why) {
			t.Errorf("%s kept here (%s): %+v", n, why, it)
		}
	}
	if got["socksProxyPassword"].There == "secret" {
		t.Error("a secret is shown")
	}
	if len(missing) != 1 || missing[0] != "https://second.example/index.min.json" {
		t.Errorf("missing stores: %v", missing)
	}

	n, err := env.ApplySync(ctx, path)
	if err != nil || n != 3 {
		t.Fatalf("applied %d: %v", n, err)
	}
	if f.Values["autoDownloadNewChapters"] != true || f.Values["globalUpdateInterval"] != 24.0 || f.Values["ip"] != "127.0.0.1" ||
		f.Values["flareSolverrEnabled"] != false || f.Values["socksProxyEnabled"] != false || f.Values["authMode"] != "NONE" {
		t.Errorf("after the sync: %v", f.Values)
	}
	if len(f.Stores) != 2 {
		t.Errorf("stores: %v", f.Stores)
	}
	// A second run finds nothing more to copy.
	if items, missing, _ := env.SyncPlan(ctx, path); len(missing) != 0 {
		t.Errorf("again: %v", missing)
	} else {
		for _, it := range items {
			if !it.Same() && it.Skip == "" {
				t.Errorf("still differs: %+v", it)
			}
		}
	}
}
