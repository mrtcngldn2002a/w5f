// Package suwayomitest is a small Suwayomi-Server for tests: its settings
// schema (as v2.4 describes it), settings, setSettings, and the three ways
// its Authentication signs in, as the server's own code checks them.
package suwayomitest

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// Server is the fake. Values are its settings; Sets counts setSettings.
type Server struct {
	URL    string
	Port   int
	mu     sync.Mutex
	Values map[string]any
	Sets   int
	// Prefs are source 42's own settings, as Suwayomi lists them; Stores
	// are the extension stores.
	Prefs  []map[string]any
	Stores []string
	// Releases is what the GitHub release API answers (see ReleaseAPI).
	sessions map[string]bool
	tokens   map[string]bool
}

type field struct {
	name, kind, typ string
	list, settable  bool
	desc            string
}

// The schema: a slice of Suwayomi's settings, each kind of value once.
var fields = []field{
	{name: "ip", kind: "SCALAR", typ: "String", settable: true},
	{name: "port", kind: "SCALAR", typ: "Int", settable: true},
	{name: "socksProxyEnabled", kind: "SCALAR", typ: "Boolean", settable: true},
	{name: "socksProxyHost", kind: "SCALAR", typ: "String", settable: true},
	{name: "socksProxyPassword", kind: "SCALAR", typ: "String", settable: true},
	{name: "downloadsPath", kind: "SCALAR", typ: "String", settable: true},
	{name: "autoDownloadNewChapters", kind: "SCALAR", typ: "Boolean", settable: true},
	{name: "globalUpdateInterval", kind: "SCALAR", typ: "Float", settable: true, desc: "Time in hours"},
	{name: "opdsItemsPerPage", kind: "SCALAR", typ: "Int", settable: true},
	{name: "webUIFlavor", kind: "ENUM", typ: "WebUIFlavor", settable: true},
	{name: "authMode", kind: "ENUM", typ: "AuthMode", settable: true},
	{name: "authUsername", kind: "SCALAR", typ: "String", settable: true},
	{name: "authPassword", kind: "SCALAR", typ: "String", settable: true},
	{name: "extensionRepos", kind: "SCALAR", typ: "String", list: true, settable: true},
	{name: "downloadConversions", kind: "OBJECT", typ: "SettingsDownloadConversionType", list: true, settable: true},
	{name: "flareSolverrEnabled", kind: "SCALAR", typ: "Boolean", settable: true},
	{name: "kcefEnabled", kind: "SCALAR", typ: "Boolean", settable: true},
	{name: "aboutOnly", kind: "SCALAR", typ: "String"}, // shown, not settable
}

var enums = map[string][]string{
	"WebUIFlavor": {"WEBUI", "VUI", "CUSTOM"},
	"AuthMode":    {"NONE", "BASIC_AUTH", "SIMPLE_LOGIN", "UI_LOGIN"},
}

// New starts the fake on a free port.
func New(t *testing.T) *Server {
	s := &Server{sessions: map[string]bool{}, tokens: map[string]bool{}, Values: map[string]any{
		"ip": "127.0.0.1", "port": 4567, "socksProxyEnabled": false, "socksProxyHost": "", "socksProxyPassword": "",
		"downloadsPath": "/c", "autoDownloadNewChapters": false, "globalUpdateInterval": 12.0, "opdsItemsPerPage": 50,
		"webUIFlavor": "WEBUI", "authMode": "NONE", "authUsername": "", "authPassword": "",
		"extensionRepos": []any{}, "flareSolverrEnabled": false, "kcefEnabled": true, "aboutOnly": "x",
		"downloadConversions": []any{map[string]any{"mimeType": "image/webp", "target": "image/jpeg"}},
	}, Stores: []string{"https://example.org/repo/index.min.json"}, Prefs: []map[string]any{
		{"__typename": "SwitchPreference", "key": "show_notes", "title": "Show author's notes", "visible": true, "enabled": true, "currentValue": nil, "default": true},
		{"__typename": "ListPreference", "key": "quality", "title": "Image quality", "summary": "Pages as the site sends them", "visible": true, "enabled": true,
			"currentValue": "high", "default": "high", "entries": []any{"High", "Low"}, "entryValues": []any{"high", "low"}},
		{"__typename": "MultiSelectListPreference", "key": "langs", "title": "Languages", "visible": true, "enabled": true,
			"currentValue": []any{"en"}, "default": []any{"en"}, "entries": []any{"English", "Turkish"}, "entryValues": []any{"en", "tr"}},
		{"__typename": "EditTextPreference", "key": "ua", "title": "User agent", "visible": true, "enabled": true, "currentValue": nil, "default": "", "dialogMessage": "Leave empty for the default"},
		{"__typename": "CheckBoxPreference", "key": "hidden", "title": "Hidden one", "visible": false, "enabled": true, "currentValue": false, "default": false},
	}}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(s.serve))
	srv.Listener.Close()
	srv.Listener = ln
	srv.Start()
	t.Cleanup(srv.Close)
	s.URL = srv.URL
	s.Port = ln.Addr().(*net.TCPAddr).Port
	return s
}

func (s *Server) mode() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, _ := s.Values["authMode"].(string)
	return m
}

func (s *Server) cred(u, p string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return u == s.Values["authUsername"] && p == s.Values["authPassword"]
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/login.html" && r.Method == http.MethodPost {
		r.ParseForm()
		if !s.cred(r.Form.Get("user"), r.Form.Get("pass")) {
			w.WriteHeader(200)
			return
		}
		id := fmt.Sprintf("s%d", len(s.sessions)+1)
		s.mu.Lock()
		s.sessions[id] = true
		s.mu.Unlock()
		http.SetCookie(w, &http.Cookie{Name: "JSESSIONID", Value: id, Path: "/"})
		w.Header().Set("Location", "/")
		w.WriteHeader(http.StatusSeeOther)
		return
	}
	b, _ := io.ReadAll(r.Body)
	var req struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}
	json.Unmarshal(b, &req)
	q := req.Query
	switch s.mode() {
	case "BASIC_AUTH":
		u, p, ok := r.BasicAuth()
		if !ok || !s.cred(u, p) {
			w.Header().Set("WWW-Authenticate", "Basic")
			w.WriteHeader(401)
			return
		}
	case "SIMPLE_LOGIN":
		c, err := r.Cookie("JSESSIONID")
		s.mu.Lock()
		ok := err == nil && s.sessions[c.Value]
		s.mu.Unlock()
		if !ok { // the API is not redirected, but its user is a visitor
			writeJSON(w, map[string]any{"errors": []any{map[string]any{"message": "Unauthorized"}}})
			return
		}
	case "UI_LOGIN":
		if strings.Contains(q, "login(") {
			vars := req.Variables
			if !s.cred(fmt.Sprint(vars["u"]), fmt.Sprint(vars["p"])) {
				writeJSON(w, map[string]any{"errors": []any{map[string]any{"message": "Incorrect username or password."}}})
				return
			}
			tok := "tok-" + base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprint(vars["u"])))
			s.mu.Lock()
			s.tokens[tok] = true
			s.mu.Unlock()
			writeJSON(w, map[string]any{"data": map[string]any{"login": map[string]any{"accessToken": tok}}})
			return
		}
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		s.mu.Lock()
		ok := s.tokens[tok]
		s.mu.Unlock()
		if !ok {
			writeJSON(w, map[string]any{"errors": []any{map[string]any{"message": "Unauthorized"}}})
			return
		}
	}
	switch {
	case strings.Contains(q, "__type("):
		writeJSON(w, map[string]any{"data": map[string]any{"__type": typeOf(fmt.Sprint(req.Variables["n"]))}})
	case strings.Contains(q, "aboutServer"):
		writeJSON(w, map[string]any{"data": map[string]any{"aboutServer": map[string]any{"version": "v2.4.2366"}}})
	case strings.Contains(q, "updateSourcePreference"):
		in, _ := req.Variables["in"].(map[string]any)
		ch, _ := in["change"].(map[string]any)
		pos := int(ch["position"].(float64))
		s.mu.Lock()
		for k, v := range ch {
			if k != "position" && pos < len(s.Prefs) {
				s.Prefs[pos]["currentValue"] = v
			}
		}
		s.mu.Unlock()
		writeJSON(w, map[string]any{"data": map[string]any{"updateSourcePreference": map[string]any{"clientMutationId": nil}}})
	case strings.Contains(q, "preferences"):
		if fmt.Sprint(req.Variables["id"]) != "42" {
			writeJSON(w, map[string]any{"data": map[string]any{"source": nil}})
			return
		}
		// As the real server: one field name may not carry a Boolean in one
		// fragment and a String in another, so the client must use aliases.
		if strings.Contains(q, "enabled currentValue") {
			writeJSON(w, map[string]any{"errors": []any{map[string]any{"message": "Validation error (FieldsConflict) : 'source/preferences/currentValue' : returns different types 'Boolean' and 'String'"}}})
			return
		}
		s.mu.Lock()
		var prefs []map[string]any
		for _, p := range s.Prefs {
			kind := map[string]string{"SwitchPreference": "bool", "CheckBoxPreference": "bool", "EditTextPreference": "text",
				"ListPreference": "text", "MultiSelectListPreference": "list"}[fmt.Sprint(p["__typename"])]
			out := map[string]any{}
			for k, v := range p {
				switch k {
				case "currentValue":
					out[kind+"Value"] = v
				case "default":
					out[kind+"Default"] = v
				default:
					out[k] = v
				}
			}
			prefs = append(prefs, out)
		}
		s.mu.Unlock()
		writeJSON(w, map[string]any{"data": map[string]any{"source": map[string]any{"displayName": "Test Source", "preferences": prefs}}})
	case strings.Contains(q, "addExtensionStore"):
		s.mu.Lock()
		s.Stores = append(s.Stores, fmt.Sprint(req.Variables["u"]))
		s.mu.Unlock()
		writeJSON(w, map[string]any{"data": map[string]any{"addExtensionStore": map[string]any{"clientMutationId": nil}}})
	case strings.Contains(q, "extensionStores"):
		s.mu.Lock()
		var nodes []any
		for _, u := range s.Stores {
			nodes = append(nodes, map[string]any{"name": "Store", "indexUrl": u})
		}
		s.mu.Unlock()
		writeJSON(w, map[string]any{"data": map[string]any{"extensions": map[string]any{"nodes": []any{}},
			"extensionStores": map[string]any{"nodes": nodes}}})
	case strings.Contains(q, "setSettings"):
		in, _ := req.Variables["input"].(map[string]any)
		set, _ := in["settings"].(map[string]any)
		if n, ok := set["opdsItemsPerPage"].(float64); ok && n < 1 {
			writeJSON(w, map[string]any{"errors": []any{map[string]any{"message": "Validation errors: opdsItemsPerPage must be at least 1"}}})
			return
		}
		s.mu.Lock()
		for k, v := range set {
			s.Values[k] = v
		}
		s.Sets++
		s.mu.Unlock()
		writeJSON(w, map[string]any{"data": map[string]any{"setSettings": map[string]any{"clientMutationId": nil}}})
	case strings.Contains(q, "settings"):
		s.mu.Lock()
		vals := map[string]any{}
		for k, v := range s.Values {
			vals[k] = v
		}
		s.mu.Unlock()
		writeJSON(w, map[string]any{"data": map[string]any{"settings": vals}})
	default:
		writeJSON(w, map[string]any{"errors": []any{map[string]any{"message": "unexpected: " + q}}})
	}
}

func ref(f field) map[string]any {
	base := map[string]any{"kind": f.kind, "name": f.typ}
	if f.list {
		base = map[string]any{"kind": "LIST", "name": nil, "ofType": map[string]any{"kind": "NON_NULL", "name": nil, "ofType": base}}
	}
	return map[string]any{"kind": "NON_NULL", "name": nil, "ofType": base}
}

func typeOf(name string) any {
	switch name {
	case "SettingsType":
		var fs []any
		for _, f := range fields {
			fs = append(fs, map[string]any{"name": f.name, "description": f.desc, "type": ref(f)})
		}
		fs = append(fs, map[string]any{"name": "id", "type": map[string]any{"kind": "SCALAR", "name": "ID"}})
		return map[string]any{"fields": fs}
	case "SetSettingsInput":
		return map[string]any{"inputFields": []any{
			map[string]any{"name": "clientMutationId", "type": map[string]any{"kind": "SCALAR", "name": "String"}},
			map[string]any{"name": "settings", "type": map[string]any{"kind": "NON_NULL", "ofType": map[string]any{"kind": "INPUT_OBJECT", "name": "PartialSettingsTypeInput"}}},
		}}
	case "PartialSettingsTypeInput":
		var fs []any
		for _, f := range fields {
			if f.settable {
				fs = append(fs, map[string]any{"name": f.name, "type": ref(f)})
			}
		}
		return map[string]any{"inputFields": fs}
	case "SettingsDownloadConversionType":
		return map[string]any{"fields": []any{
			map[string]any{"name": "mimeType", "type": map[string]any{"kind": "SCALAR", "name": "String"}},
			map[string]any{"name": "target", "type": map[string]any{"kind": "SCALAR", "name": "String"}},
		}}
	}
	if vals, ok := enums[name]; ok {
		var ev []any
		for _, v := range vals {
			ev = append(ev, map[string]any{"name": v})
		}
		return map[string]any{"enumValues": ev}
	}
	return nil
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
