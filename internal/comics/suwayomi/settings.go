package suwayomi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Setting is one of the server's settings, as its GraphQL schema describes
// it (so a newer server's settings show without W5F knowing them).
type Setting struct {
	Name        string
	Description string
	Kind        string // SCALAR, ENUM or OBJECT
	Type        string // Boolean, Int, Float, String, …, or the enum/object type
	List        bool
	Values      []string  // an enum's values
	Fields      []Setting // an object's fields (conversions)
	Settable    bool      // setSettings takes it
	Group       string
	Secret      bool
}

// Settings are the server's settings and their values.
type Settings struct {
	List   []Setting
	Values map[string]any
}

// Get finds a setting.
func (s Settings) Get(name string) (Setting, bool) {
	for _, x := range s.List {
		if x.Name == name {
			return x, true
		}
	}
	return Setting{}, false
}

type gqlType struct {
	Kind   string   `json:"kind"`
	Name   string   `json:"name"`
	OfType *gqlType `json:"ofType"`
}

// unwrap turns NON_NULL and LIST wrappers into a base type and a list flag.
func (t *gqlType) unwrap() (kind, name string, list bool) {
	for t != nil {
		switch t.Kind {
		case "NON_NULL":
		case "LIST":
			list = true
		default:
			return t.Kind, t.Name, list
		}
		t = t.OfType
	}
	return "", "", list
}

const typeRef = `kind name ofType { kind name ofType { kind name ofType { kind name } } }`

type gqlField struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Type        gqlType `json:"type"`
}

func (c *Client) typeFields(ctx context.Context, name string, input bool) ([]gqlField, []string, error) {
	var r struct {
		T *struct {
			Fields      []gqlField `json:"fields"`
			InputFields []gqlField `json:"inputFields"`
			EnumValues  []struct {
				Name string `json:"name"`
			} `json:"enumValues"`
		} `json:"__type"`
	}
	q := `query($n: String!) { __type(name: $n) { fields { name description type { ` + typeRef + ` } } inputFields { name description type { ` + typeRef + ` } } enumValues { name } } }`
	if err := c.do(ctx, q, map[string]any{"n": name}, &r); err != nil {
		return nil, nil, err
	}
	if r.T == nil {
		return nil, nil, fmt.Errorf("this Suwayomi has no %s type", name)
	}
	var enum []string
	for _, e := range r.T.EnumValues {
		enum = append(enum, e.Name)
	}
	if input {
		return r.T.InputFields, enum, nil
	}
	return r.T.Fields, enum, nil
}

// describe turns a schema field into a Setting (objects with their fields).
func (c *Client) describe(ctx context.Context, f gqlField, depth int, enums map[string][]string) (Setting, error) {
	kind, name, list := f.Type.unwrap()
	s := Setting{Name: f.Name, Description: f.Description, Kind: kind, Type: name, List: list}
	switch kind {
	case "ENUM":
		if _, ok := enums[name]; !ok {
			_, vals, err := c.typeFields(ctx, name, false)
			if err != nil {
				return s, err
			}
			enums[name] = vals
		}
		s.Values = enums[name]
	case "OBJECT":
		if depth > 3 {
			return s, nil
		}
		fields, _, err := c.typeFields(ctx, name, false)
		if err != nil {
			return s, err
		}
		for _, sub := range fields {
			d, err := c.describe(ctx, sub, depth+1, enums)
			if err != nil {
				return s, err
			}
			s.Fields = append(s.Fields, d)
		}
	}
	return s, nil
}

// selection is the GraphQL selection that reads settings.
func selection(list []Setting) string {
	var b strings.Builder
	for _, s := range list {
		b.WriteString(s.Name)
		if s.Kind == "OBJECT" && len(s.Fields) > 0 {
			b.WriteString(" { " + selection(s.Fields) + " }")
		}
		b.WriteByte(' ')
	}
	return b.String()
}

// ServerSettings reads the server's settings: which there are (from the
// schema, deprecated ones left out) and their values.
func (c *Client) ServerSettings(ctx context.Context) (Settings, error) {
	var out Settings
	fields, _, err := c.typeFields(ctx, "SettingsType", false)
	if err != nil {
		return out, err
	}
	// What setSettings takes.
	settable := map[string]bool{}
	inFields, _, err := c.typeFields(ctx, "SetSettingsInput", true)
	if err != nil {
		return out, err
	}
	for _, f := range inFields {
		if f.Name == "settings" {
			_, name, _ := f.Type.unwrap()
			partial, _, err := c.typeFields(ctx, name, true)
			if err != nil {
				return out, err
			}
			for _, p := range partial {
				settable[p.Name] = true
			}
		}
	}
	enums := map[string][]string{}
	for _, f := range fields {
		if f.Name == "id" || strings.HasPrefix(f.Name, "__") {
			continue
		}
		s, err := c.describe(ctx, f, 0, enums)
		if err != nil {
			return out, err
		}
		s.Settable = settable[s.Name]
		s.Group = GroupOf(s.Name)
		s.Secret = secretSetting(s.Name)
		out.List = append(out.List, s)
	}
	var r struct {
		Settings map[string]any `json:"settings"`
	}
	if err := c.do(ctx, `{ settings { `+selection(out.List)+` } }`, nil, &r); err != nil {
		return out, err
	}
	out.Values = r.Settings
	return out, nil
}

// SetServerSettings changes settings; the server checks them, applies them
// at once and keeps them in its server.conf.
func (c *Client) SetServerSettings(ctx context.Context, values map[string]any) error {
	return c.do(ctx, `mutation($input: SetSettingsInput!) { setSettings(input: $input) { clientMutationId } }`,
		map[string]any{"input": map[string]any{"settings": values}}, nil)
}

func secretSetting(name string) bool {
	l := strings.ToLower(name)
	return strings.Contains(l, "password") || strings.Contains(l, "apikey") || strings.Contains(l, "userkey") || strings.HasSuffix(l, "token")
}

// Groups are the server's setting groups, named as its launcher's tabs.
var Groups = []struct{ ID, Title string }{
	{"network", "Server bindings"}, {"proxy", "SOCKS proxy"}, {"webui", "WebUI"}, {"webview", "Webview"},
	{"downloader", "Downloader"}, {"conversions", "Conversions"}, {"extension", "Extension"},
	{"updates", "Library updates"}, {"auth", "Authentication"}, {"backup", "Backup"},
	{"local", "Local source"}, {"cloudflare", "Cloudflare"}, {"opds", "OPDS"}, {"koreader", "KOReader"},
	{"sync", "Sync"}, {"database", "Database"}, {"misc", "Misc"}, {"other", "Other"},
}

// GroupOf is the group a setting belongs to (Suwayomi's own grouping; a
// setting W5F does not know goes to Other).
func GroupOf(name string) string {
	if g, ok := settingGroups[name]; ok {
		return g
	}
	switch {
	case strings.HasPrefix(name, "socksProxy"):
		return "proxy"
	case strings.HasPrefix(name, "webUI"):
		return "webui"
	case strings.HasPrefix(name, "flareSolverr"):
		return "cloudflare"
	case strings.HasPrefix(name, "opds"):
		return "opds"
	case strings.HasPrefix(name, "koreader"):
		return "koreader"
	case strings.HasPrefix(name, "sync"):
		return "sync"
	case strings.HasPrefix(name, "database"):
		return "database"
	case strings.HasPrefix(name, "autoBackup"), strings.HasPrefix(name, "backup"):
		return "backup"
	case strings.HasPrefix(name, "jwt"), strings.HasPrefix(name, "auth"):
		return "auth"
	case strings.HasPrefix(name, "autoDownload"):
		return "downloader"
	}
	return "other"
}

// settingGroups follows Suwayomi's ServerConfig (v2.4) where the name does
// not tell.
var settingGroups = map[string]string{
	"ip": "network", "port": "network",
	"initialOpenInBrowserEnabled": "webui", "electronPath": "webui",
	"downloadAsCbz": "downloader", "downloadsPath": "downloader", "excludeEntryWithUnreadChapters": "downloader",
	"downloadConversions": "conversions", "serveConversions": "conversions",
	"extensionRepos": "extension", "extensionStores": "extension", "maxSourcesInParallel": "extension",
	"excludeUnreadChapters": "updates", "excludeNotStarted": "updates", "excludeCompleted": "updates",
	"globalUpdateInterval": "updates", "updateMangas": "updates",
	"basicAuthEnabled": "auth", "basicAuthUsername": "auth", "basicAuthPassword": "auth",
	"debugLogsEnabled": "misc", "gqlDebugLogsEnabled": "misc", "systemTrayEnabled": "misc",
	"maxLogFiles": "misc", "maxLogFileSize": "misc", "maxLogFolderSize": "misc",
	"localSourcePath": "local", "kcefEnabled": "webview", "useHikariConnectionPool": "database",
}

// Forced are the settings W5F gives on the java command line (-D): they
// win over server.conf, so they are W5F's to keep and not changed here.
func (s Server) Forced() map[string]string {
	const p = "-Dsuwayomi.tachidesk.config.server."
	out := map[string]string{}
	for _, a := range s.Args() {
		if rest, ok := strings.CutPrefix(a, p); ok {
			k, v, _ := strings.Cut(rest, "=")
			out[k] = v
		}
	}
	return out
}

// Notes are what Suwayomi writes beside each setting in its server.conf:
// the default, the range or options, and what it is for.
func (s Server) Notes() map[string]string {
	b, err := os.ReadFile(filepath.Join(s.Dir, "data", "server.conf"))
	if err != nil {
		return nil
	}
	out := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "server.")
		if !ok {
			continue
		}
		name, rest, ok := strings.Cut(rest, " = ")
		if !ok || strings.ContainsAny(name, ". ") {
			continue
		}
		if _, note, ok := strings.Cut(rest, " # "); ok {
			out[name] = strings.TrimSpace(note)
		}
	}
	return out
}

// ParseValue reads what the reader typed for a setting.
func ParseValue(s Setting, text string) (any, error) {
	text = strings.TrimSpace(text)
	if s.Kind == "OBJECT" {
		var v any
		if err := json.Unmarshal([]byte(text), &v); err != nil {
			return nil, fmt.Errorf("%s is written as JSON: %v", s.Name, err)
		}
		return v, nil
	}
	if s.List {
		var out []any
		for _, part := range strings.FieldsFunc(text, func(r rune) bool { return r == ',' || r == '\n' }) {
			v, err := parseScalar(s, strings.TrimSpace(part))
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		if out == nil {
			out = []any{}
		}
		return out, nil
	}
	return parseScalar(s, text)
}

func parseScalar(s Setting, text string) (any, error) {
	if s.Kind == "ENUM" {
		for _, v := range s.Values {
			if strings.EqualFold(v, text) {
				return v, nil
			}
		}
		return nil, fmt.Errorf("%s is one of %s", s.Name, strings.Join(s.Values, ", "))
	}
	switch s.Type {
	case "Boolean":
		switch strings.ToLower(text) {
		case "true", "on", "yes", "1", "açık", "evet":
			return true, nil
		case "false", "off", "no", "0", "kapalı", "hayır":
			return false, nil
		}
		return nil, fmt.Errorf("%s is on or off", s.Name)
	case "Int", "Long", "Short":
		n, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%s is a whole number", s.Name)
		}
		return n, nil
	case "Float", "Double":
		f, err := strconv.ParseFloat(strings.ReplaceAll(text, ",", "."), 64)
		if err != nil {
			return nil, fmt.Errorf("%s is a number", s.Name)
		}
		return f, nil
	}
	return text, nil
}

// FormatValue writes a setting's value for the reader.
func FormatValue(s Setting, v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case bool:
		if x {
			return "on"
		}
		return "off"
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case []any:
		if s.Kind != "OBJECT" {
			var parts []string
			for _, e := range x {
				parts = append(parts, FormatValue(Setting{}, e))
			}
			return strings.Join(parts, ", ")
		}
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// Latest is the newest Suwayomi-Server release.
type Latest struct {
	Tag string
	Jar string
}

// LatestRelease asks GitHub for the newest release.
func LatestRelease(ctx context.Context) (Latest, error) {
	rel, err := latestRelease(ctx)
	if err != nil {
		return Latest{}, err
	}
	l := Latest{Tag: rel.Tag}
	for _, a := range rel.Assets {
		if strings.HasPrefix(a.Name, "Suwayomi-Server-") && strings.HasSuffix(a.Name, ".jar") {
			l.Jar = a.Name
		}
	}
	return l, nil
}

// Version is the installed release, from the jar's name ("v2.4.2366").
func (s Server) Version() string {
	j := s.Jar()
	if j == "" {
		return ""
	}
	base := j[strings.LastIndexAny(j, `/\`)+1:]
	return strings.TrimSuffix(strings.TrimPrefix(base, "Suwayomi-Server-"), ".jar")
}

// Update installs the newest release when it is newer than the installed
// one: a server W5F started is stopped for it and started again. A server
// W5F did not start is left alone (stop it first). It reports the version
// now installed and whether it changed.
func (s Server) Update(ctx context.Context, progress func(done, total int64)) (string, bool, error) {
	l, err := LatestRelease(ctx)
	if err != nil {
		return s.Version(), false, err
	}
	cur := s.Version()
	if cur != "" && ("Suwayomi-Server-"+cur+".jar" == l.Jar || cur == l.Tag) {
		return cur, false, nil
	}
	running := s.Running(ctx)
	if running && !s.ours() {
		return cur, false, errors.New("a Suwayomi that W5F did not start is running (the launcher?); close it, then update")
	}
	if running {
		if err := s.Stop(); err != nil {
			return cur, false, err
		}
	}
	if _, err := Install(ctx, s.Dir, progress); err != nil {
		if running {
			_, _ = s.Start(ctx, s.startWait())
		}
		return cur, false, err
	}
	if running {
		if _, err := s.Start(ctx, s.startWait()); err != nil {
			return s.Version(), true, fmt.Errorf("updated, but it did not start again: %w", err)
		}
	}
	return s.Version(), true, nil
}

// Restart stops a server W5F started and starts it again (after a change
// that needs a restart).
func (s Server) Restart(ctx context.Context) error {
	if !s.ours() && s.Running(ctx) {
		return errors.New("this Suwayomi was not started by W5F; restart it where it was started")
	}
	if err := s.Stop(); err != nil {
		return err
	}
	_, err := s.Start(ctx, s.startWait())
	return err
}

// SortedValues lists an enum's values for links.
func SortedValues(s Setting) []string {
	v := append([]string{}, s.Values...)
	sort.Strings(v)
	return v
}
