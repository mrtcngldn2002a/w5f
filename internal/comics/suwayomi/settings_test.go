package suwayomi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"w5f/internal/comics/suwayomi/suwayomitest"
)

func TestServerSettingsFromTheSchema(t *testing.T) {
	f := suwayomitest.New(t)
	c := New(f.URL)
	st, err := c.ServerSettings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	get := func(n string) Setting {
		s, ok := st.Get(n)
		if !ok {
			t.Fatalf("no %s in %+v", n, st.List)
		}
		return s
	}
	if s := get("webUIFlavor"); s.Kind != "ENUM" || strings.Join(s.Values, ",") != "WEBUI,VUI,CUSTOM" || s.Group != "webui" {
		t.Errorf("enum: %+v", s)
	}
	if s := get("downloadConversions"); s.Kind != "OBJECT" || !s.List || len(s.Fields) != 2 || s.Group != "conversions" {
		t.Errorf("object: %+v", s)
	}
	if s := get("extensionRepos"); !s.List || s.Type != "String" {
		t.Errorf("list: %+v", s)
	}
	if !get("socksProxyPassword").Secret || get("socksProxyHost").Secret || get("socksProxyHost").Group != "proxy" {
		t.Error("secret or group")
	}
	if get("aboutOnly").Settable || !get("opdsItemsPerPage").Settable {
		t.Error("settable")
	}
	if get("globalUpdateInterval").Description != "Time in hours" || get("globalUpdateInterval").Group != "updates" {
		t.Error("description or group")
	}
	if _, ok := st.Get("id"); ok {
		t.Error("the id is not a setting")
	}
	if st.Values["opdsItemsPerPage"] != 50.0 {
		t.Errorf("values: %v", st.Values)
	}
}

func TestParseAndSetValues(t *testing.T) {
	f := suwayomitest.New(t)
	c := New(f.URL)
	ctx := context.Background()
	st, _ := c.ServerSettings(ctx)
	set := func(name, text string) error {
		s, _ := st.Get(name)
		v, err := ParseValue(s, text)
		if err != nil {
			return err
		}
		return c.SetServerSettings(ctx, map[string]any{name: v})
	}
	for _, tc := range []struct{ name, text string }{
		{"socksProxyEnabled", "on"}, {"globalUpdateInterval", "6,5"}, {"opdsItemsPerPage", "20"}, {"webUIFlavor", "vui"},
		{"extensionRepos", "https://a.example/index.min.json, https://b.example/index.min.json"},
		{"downloadConversions", `[{"mimeType":"image/png","target":"image/webp"}]`},
		{"socksProxyHost", "proxy.lan"},
	} {
		if err := set(tc.name, tc.text); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
	}
	v := f.Values
	if v["socksProxyEnabled"] != true || v["globalUpdateInterval"] != 6.5 || v["opdsItemsPerPage"] != 20.0 || v["webUIFlavor"] != "VUI" || v["socksProxyHost"] != "proxy.lan" {
		t.Errorf("%v", v)
	}
	if r := v["extensionRepos"].([]any); len(r) != 2 || r[1] != "https://b.example/index.min.json" {
		t.Errorf("list: %v", r)
	}
	if conv := v["downloadConversions"].([]any); conv[0].(map[string]any)["target"] != "image/webp" {
		t.Errorf("object: %v", conv)
	}
	for _, bad := range []struct{ name, text string }{{"socksProxyEnabled", "maybe"}, {"opdsItemsPerPage", "many"}, {"webUIFlavor", "nope"}, {"downloadConversions", "{"}} {
		if err := set(bad.name, bad.text); err == nil {
			t.Errorf("%s accepted %q", bad.name, bad.text)
		}
	}
	if err := set("opdsItemsPerPage", "0"); err == nil || !strings.Contains(err.Error(), "Validation errors") {
		t.Errorf("the server's own check: %v", err)
	}
	st, _ = c.ServerSettings(ctx)
	if s, _ := st.Get("downloadConversions"); !strings.Contains(FormatValue(s, st.Values["downloadConversions"]), `"image/webp"`) {
		t.Error("format object")
	}
	if s, _ := st.Get("extensionRepos"); FormatValue(s, st.Values["extensionRepos"]) != "https://a.example/index.min.json, https://b.example/index.min.json" {
		t.Error("format list")
	}
}

func TestEveryAuthenticationModeSignsIn(t *testing.T) {
	for _, mode := range []string{"BASIC_AUTH", "SIMPLE_LOGIN", "UI_LOGIN"} {
		t.Run(mode, func(t *testing.T) {
			f := suwayomitest.New(t)
			ctx := context.Background()
			open := New(f.URL)
			if err := open.SetServerSettings(ctx, map[string]any{"authMode": mode, "authUsername": "ayse", "authPassword": "gizli"}); err != nil {
				t.Fatal(err)
			}
			if _, err := New(f.URL).Version(ctx); !errors.Is(err, ErrUnauthorized) {
				t.Fatalf("without an account: %v", err)
			}
			wrong := New(f.URL)
			wrong.Auth = Auth{Mode: mode, Username: "ayse", Password: "yanlis"}
			if _, err := wrong.Version(ctx); !errors.Is(err, ErrUnauthorized) {
				t.Fatalf("a wrong password: %v", err)
			}
			c := New(f.URL)
			c.Auth = Auth{Mode: mode, Username: "ayse", Password: "gizli"}
			if v, err := c.Version(ctx); err != nil || v != "v2.4.2366" {
				t.Fatalf("signed in: %v %q", err, v)
			}
			if _, err := c.ServerSettings(ctx); err != nil {
				t.Fatal(err)
			}
			// A session the server forgot is opened again.
			if mode != "BASIC_AUTH" {
				c.sess.token = "stale"
				if mode == "SIMPLE_LOGIN" {
					c.sess.jar, _ = cookiejar.New(nil) // the server forgot its session
					c.sess.signed = true
				}
				if _, err := c.Version(ctx); err != nil {
					t.Fatalf("after the session expired: %v", err)
				}
			}
		})
	}
}

func TestAuthIsKeptPrivately(t *testing.T) {
	s := Server{Dir: t.TempDir()}
	if s.Auth().On() {
		t.Fatal("an account from nowhere")
	}
	if err := s.SaveAuth(Auth{Mode: "BASIC_AUTH", Username: "u", Password: "p"}); err != nil {
		t.Fatal(err)
	}
	// Windows has no Unix modes (the file sits in the user's own profile).
	if st, _ := os.Stat(s.authFile()); runtime.GOOS != "windows" && st.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", st.Mode())
	}
	if c := s.Client(); c.Auth.Password != "p" {
		t.Error("the client does not sign in with it")
	}
	s.SaveAuth(Auth{Mode: "NONE"})
	if _, err := os.Stat(s.authFile()); err == nil {
		t.Error("NONE keeps no account")
	}
}

func TestRunningCountsAServerThatAsksToSignIn(t *testing.T) {
	f := suwayomitest.New(t)
	New(f.URL).SetServerSettings(context.Background(), map[string]any{"authMode": "BASIC_AUTH", "authUsername": "a", "authPassword": "b"})
	s := Server{Dir: t.TempDir(), Port: f.Port}
	if !s.Running(context.Background()) {
		t.Error("a server that asks W5F to sign in must not be started a second time")
	}
}

func TestForcedAreTheCommandLineSettings(t *testing.T) {
	s := Server{Dir: t.TempDir(), Downloads: "/d", Local: "/l"}
	f := s.Forced()
	for _, k := range []string{"ip", "port", "rootDir", "webUIEnabled", "downloadsPath", "localSourcePath", "downloadAsCbz", "maxSourcesInParallel"} {
		if _, ok := f[k]; !ok {
			t.Errorf("%s is not marked as W5F's", k)
		}
	}
	if f["downloadsPath"] != "/d" || f["ip"] != "127.0.0.1" {
		t.Errorf("%v", f)
	}
	if _, ok := f["socksProxyHost"]; ok {
		t.Error("a free setting marked as W5F's")
	}
}

func TestUpdate(t *testing.T) {
	jar := []byte("the new jar")
	sum := sha256.Sum256(jar)
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			fmt.Fprintf(w, `{"tag_name":"v2.5.1","assets":[{"name":"Suwayomi-Server-v2.5.1.jar","browser_download_url":"%[1]s/jar","size":%[2]d},{"name":"Checksums.sha256","browser_download_url":"%[1]s/sums"}]}`, srv.URL, len(jar))
		case "/jar":
			w.Write(jar)
		case "/sums":
			fmt.Fprintf(w, "%s  Suwayomi-Server-v2.5.1.jar\n", hex.EncodeToString(sum[:]))
		}
	}))
	defer srv.Close()
	old := ReleaseAPI
	ReleaseAPI = srv.URL + "/latest"
	defer func() { ReleaseAPI = old }()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "Suwayomi-Server-v2.4.2366.jar"), []byte("old"), 0o644)
	s := Server{Dir: dir, Port: freePort(t)}
	if s.Version() != "v2.4.2366" {
		t.Fatalf("version: %q", s.Version())
	}
	v, changed, err := s.Update(context.Background(), nil)
	if err != nil || !changed || v != "v2.5.1" {
		t.Fatalf("%v %v %q", err, changed, v)
	}
	if jars, _ := filepath.Glob(filepath.Join(dir, "*.jar")); len(jars) != 1 {
		t.Errorf("old jars stay: %v", jars)
	}
	if _, changed, err := s.Update(context.Background(), nil); err != nil || changed {
		t.Errorf("again: %v %v", err, changed)
	}
}

func TestServerErrorsAreShortAndNotesAreRead(t *testing.T) {
	got := cleanMessage("Exception while fetching data (/setSettings) : Validation errors: opdsItemsPerPage: Value (0) must be at least 10\n\njava.lang.Exception: Validation errors…\n\tat x")
	if got != "Validation errors: opdsItemsPerPage: Value (0) must be at least 10" {
		t.Errorf("%q", got)
	}
	s := Server{Dir: t.TempDir()}
	os.MkdirAll(filepath.Join(s.Dir, "data"), 0o755)
	os.WriteFile(filepath.Join(s.Dir, "data", "server.conf"), []byte(`server.opdsItemsPerPage = 42 # default: 100 ; range: [10, 5000]
server.downloadConversions = {}
# server.downloadConversions."image/webp" = {
server.authMode = NONE # default: NONE ; options: NONE, BASIC_AUTH
`), 0o644)
	n := s.Notes()
	if n["opdsItemsPerPage"] != "default: 100 ; range: [10, 5000]" || n["authMode"] == "" || len(n) != 2 {
		t.Errorf("%v", n)
	}
}
