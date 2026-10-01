package comics

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"w5f/internal/comics/suwayomi"
	"w5f/internal/doc"
)

// Form is a small input screen a page opens with a link (the reader shows
// it; hidden fields show dots). cmd/w5f hands these to the reader.
type Form struct {
	Title  string
	Intro  []string
	Fields []FormField
	Note   string
	// Save gets the answers in order; it returns the page to open next.
	Save func(values []string) (next, status string, err error)
}

// FormField is one answer.
type FormField struct {
	Label  string
	Hint   string
	Hidden bool
}

// Form links: one setting, the server's Authentication, and the account
// W5F signs in with.
const (
	SettingFormPrefix = "w5f:comics/setting/"
	AuthFormPrefix    = "w5f:comics/auth"
	LoginFormPrefix   = "w5f:comics/login"
)

// IsForm reports a link that opens a form.
func IsForm(href string) bool {
	return strings.HasPrefix(href, SettingFormPrefix) || strings.HasPrefix(href, SourcePrefFormPrefix) || href == AuthFormPrefix || href == LoginFormPrefix
}

// The settings read last (forms are built from them without asking again).
var (
	settingsMu    sync.Mutex
	settingsCache suwayomi.Settings
	settingsWhen  time.Time
)

func (e Env) serverSettings(ctx context.Context, fresh bool) (suwayomi.Settings, error) {
	settingsMu.Lock()
	cached, when := settingsCache, settingsWhen
	settingsMu.Unlock()
	if !fresh && cached.List != nil && time.Since(when) < 10*time.Minute {
		return cached, nil
	}
	if err := e.ensureServer(ctx); err != nil {
		return suwayomi.Settings{}, err
	}
	s, err := e.client().ServerSettings(ctx)
	if err != nil {
		return s, err
	}
	settingsMu.Lock()
	settingsCache, settingsWhen = s, time.Now()
	settingsMu.Unlock()
	return s, nil
}

// label turns socksProxyHost into "Socks proxy host" and webUIFlavor into
// "Web UI flavor".
func label(name string) string {
	rs := []rune(name)
	var words []string
	start := 0
	for i := 1; i < len(rs); i++ {
		upper, prevLower := unicode.IsUpper(rs[i]), unicode.IsLower(rs[i-1])
		acronymEnd := unicode.IsUpper(rs[i-1]) && upper && i+1 < len(rs) && unicode.IsLower(rs[i+1])
		if (upper && prevLower) || acronymEnd {
			words = append(words, string(rs[start:i]))
			start = i
		}
	}
	words = append(words, string(rs[start:]))
	known := map[string]string{"url": "URL", "opds": "OPDS", "jwt": "JWT", "ttl": "TTL", "cbz": "CBZ", "ip": "IP",
		"gql": "GraphQL", "kcef": "KCEF", "koreader": "KOReader", "id": "ID", "api": "API"}
	for i, w := range words {
		lw := strings.ToLower(w)
		switch {
		case known[lw] != "":
			words[i] = known[lw]
		case w == strings.ToUpper(w) && len(w) > 1:
			// an acronym stays as it is
		case i == 0:
			words[i] = strings.ToUpper(lw[:1]) + lw[1:]
		default:
			words[i] = lw
		}
	}
	return strings.Join(words, " ")
}

func groupTitle(id string) string {
	for _, g := range suwayomi.Groups {
		if g.ID == id {
			return g.Title
		}
	}
	return id
}

// authSetting are the settings the Authentication form changes together
// (W5F must keep its own account in step, or it locks itself out).
func authSetting(name string) bool {
	return name == "authMode" || name == "authUsername" || name == "authPassword"
}

func settingsHome(ctx context.Context, env Env, notice string) (*doc.Document, error) {
	st, err := env.serverSettings(ctx, true)
	if err != nil {
		return nil, err
	}
	p := newPage("Suwayomi settings", "w5f:comics/settings")
	if notice != "" {
		p.note("info", notice)
	}
	p.para(doc.Inline{dim("Suwayomi " + firstOf(env.Server.Version(), "?") + " · "), p.a("w5f:comics/server/check", "check for an update"),
		dim(" · "), p.a("w5f:comics/server/restart", "restart it")})
	p.para(doc.Inline{dim("Changes go to the server at once and stay in its server.conf. What W5F gives the server when it starts (address, folders, …) is W5F's and shown as such.")})
	count := map[string]int{}
	for _, s := range st.List {
		count[s.Group]++
	}
	var items []doc.Inline
	for _, g := range suwayomi.Groups {
		if count[g.ID] == 0 {
			continue
		}
		items = append(items, doc.Inline{p.a("w5f:comics/settings/"+g.ID, g.Title), dim(fmt.Sprintf("  %d", count[g.ID]))})
	}
	p.list(items)
	p.para(doc.Inline{p.a("w5f:comics/settings/extension", "Extension stores"), dim(" are on the Extension tab.")})
	// What W5F gives at start and the server does not list among its
	// settings (the launcher's Root directory, for one).
	forced := env.Server.Forced()
	var given []string
	for k, v := range forced {
		if _, listed := st.Get(k); !listed {
			given = append(given, k+" = "+v)
		}
	}
	if len(given) > 0 {
		sort.Strings(given)
		p.para(doc.Inline{{Text: "Given by W5F at start: ", Style: doc.Bold}, dim(strings.Join(given, " · "))})
	}
	p.para(doc.Inline{p.a("w5f:comics", "← Comics")})
	return p.d, nil
}

func settingsGroup(ctx context.Context, env Env, group, notice string) (*doc.Document, error) {
	st, err := env.serverSettings(ctx, notice != "")
	if err != nil {
		return nil, err
	}
	forced, notes := env.Server.Forced(), env.Server.Notes()
	p := newPage("Suwayomi: "+groupTitle(group), "w5f:comics/settings/"+group)
	if notice != "" {
		p.note("info", notice)
	}
	if group == "auth" {
		mode := suwayomi.FormatValue(suwayomi.Setting{}, st.Values["authMode"])
		user := suwayomi.FormatValue(suwayomi.Setting{}, st.Values["authUsername"])
		p.para(doc.Inline{{Text: "Authentication: ", Style: doc.Bold}, {Text: firstOf(mode, "NONE")}, dim("  user: " + firstOf(user, "(none)") + " · password hidden")})
		p.para(doc.Inline{p.a(AuthFormPrefix, "change Authentication"), dim("  (W5F keeps signing in with the new account)")})
	}
	if group == "extension" {
		// Suwayomi 2.4 keeps the repositories apart from its settings (the
		// launcher shows them on this tab as "Extension stores").
		p.heading("Extension stores")
		if _, stores, err := env.client().Extensions(ctx, false); err != nil {
			p.note("warn", err.Error())
		} else {
			var repos []doc.Inline
			for _, s := range stores {
				repos = append(repos, doc.Inline{{Text: s.IndexURL}, dim("  "),
					p.a("w5f:comics/repo/remove?"+url.Values{"url": {s.IndexURL}, "back": {"settings"}}.Encode(), "remove")})
			}
			if len(repos) == 0 {
				p.para(doc.Inline{dim("None yet. W5F comes with no repositories; the ones you add are your choice.")})
			}
			p.list(repos)
		}
		p.para(doc.Inline{p.a(RepoPage(), "+ add a repository"), dim("   "), p.a("w5f:comics/extensions", "the extensions they offer")})
		p.heading("Settings")
	}
	if group == "cloudflare" {
		p.note("warn", "FlareSolverr is a separate service that answers bot checks for Suwayomi's sources. W5F itself never bypasses such checks; using it here is your choice.")
	}
	var items []doc.Inline
	for _, s := range st.List {
		if s.Group != group || (group == "auth" && authSetting(s.Name)) {
			continue
		}
		in := doc.Inline{{Text: label(s.Name), Style: doc.Bold}}
		val := suwayomi.FormatValue(s, st.Values[s.Name])
		switch {
		case s.Secret && val != "":
			val = "(set, hidden)"
		case val == "":
			val = "(empty)"
		}
		in = append(in, doc.Span{Text: ": " + clip(val, 120)})
		if fv, ok := forced[s.Name]; ok {
			in = append(in, dim("  · W5F keeps it at "+fv+" (given when the server starts)"))
			items = append(items, in)
			continue
		}
		if !s.Settable {
			in = append(in, dim("  · this server does not let it be changed through its API"))
			items = append(items, in)
			continue
		}
		set := func(v string) string {
			return "w5f:comics/settings/set?" + url.Values{"n": {s.Name}, "v": {v}, "g": {group}}.Encode()
		}
		switch {
		case s.Type == "Boolean" && !s.List:
			if st.Values[s.Name] == true {
				in = append(in, dim("  · "), p.a(set("off"), "turn off"))
			} else {
				in = append(in, dim("  · "), p.a(set("on"), "turn on"))
			}
		case s.Kind == "ENUM" && !s.List:
			in = append(in, dim("  ·"))
			for _, v := range s.Values {
				if v == val {
					continue
				}
				in = append(in, dim(" "), p.a(set(v), v))
			}
		default:
			in = append(in, dim("  · "), p.a(SettingFormPrefix+s.Name, "change"))
		}
		if n := notes[s.Name]; n != "" {
			in = append(in, dim("  — "+n))
		} else if s.Description != "" {
			in = append(in, dim("  — "+s.Description))
		}
		items = append(items, in)
	}
	p.list(items)
	p.para(doc.Inline{p.a("w5f:comics/settings", "← all settings")})
	return p.d, nil
}

// setSetting changes one setting from a link (on/off, an enum value).
func setSetting(ctx context.Context, env Env, name, text, group string) (*doc.Document, error) {
	st, err := env.serverSettings(ctx, false)
	if err != nil {
		return nil, err
	}
	s, ok := st.Get(name)
	if !ok {
		return nil, fmt.Errorf("this Suwayomi has no setting %q", name)
	}
	if err := env.applySetting(ctx, s, text); err != nil {
		return nil, err
	}
	if group == "" {
		group = s.Group
	}
	return settingsGroup(ctx, env, group, label(name)+" changed.")
}

func (e Env) applySetting(ctx context.Context, s suwayomi.Setting, text string) error {
	if _, forced := e.Server.Forced()[s.Name]; forced {
		return fmt.Errorf("%s is given by W5F when the server starts", label(s.Name))
	}
	if authSetting(s.Name) {
		return errors.New("change Authentication with its own form, so W5F keeps signing in")
	}
	v, err := suwayomi.ParseValue(s, text)
	if err != nil {
		return err
	}
	defer forgetSettings()
	return e.client().SetServerSettings(ctx, map[string]any{s.Name: v})
}

// forgetSettings makes the next page ask the server again.
func forgetSettings() {
	settingsMu.Lock()
	settingsCache = suwayomi.Settings{}
	settingsMu.Unlock()
}

// Form builds the form a link opens.
func (e Env) Form(href string) (*Form, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	switch {
	case href == LoginFormPrefix:
		return e.loginForm(), nil
	case href == AuthFormPrefix:
		return e.authForm(ctx)
	case strings.HasPrefix(href, SourcePrefFormPrefix):
		return e.sourcePrefForm(href)
	}
	name := strings.TrimPrefix(href, SettingFormPrefix)
	st, err := e.serverSettings(ctx, false)
	if err != nil {
		return nil, err
	}
	s, ok := st.Get(name)
	if !ok {
		return nil, fmt.Errorf("this Suwayomi has no setting %q", name)
	}
	cur := suwayomi.FormatValue(s, st.Values[name])
	hint := "now: " + clip(cur, 70)
	if s.Secret {
		hint = "hidden; empty keeps it"
	}
	switch {
	case s.Kind == "OBJECT":
		hint = "JSON, like " + clip(cur, 60)
	case s.List:
		hint += " · separate values with commas"
	case s.Kind == "ENUM":
		hint += " · one of " + strings.Join(s.Values, ", ")
	case s.Type == "Int" || s.Type == "Float" || s.Type == "Double":
		hint += " · a number"
	}
	intro := []string{}
	if s.Description != "" {
		intro = append(intro, s.Description)
	}
	if n := e.Server.Notes()[name]; n != "" {
		intro = append(intro, "Suwayomi: "+n)
	}
	return &Form{
		Title:  label(name),
		Intro:  intro,
		Fields: []FormField{{Label: label(name), Hint: hint, Hidden: s.Secret}},
		Note:   "Saved to the Suwayomi server at once (its server.conf).",
		Save: func(v []string) (string, string, error) {
			if len(v) == 0 || (v[0] == "" && s.Secret) {
				return "", "Nothing changed.", nil
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := e.applySetting(ctx, s, v[0]); err != nil {
				return "", "", err
			}
			return "w5f:comics/settings/" + s.Group + "?changed=" + url.QueryEscape(name), label(name) + " saved.", nil
		},
	}, nil
}

// authForm changes the server's Authentication and W5F's own account in
// one go. W5F saves the account first, so it can sign in the moment the
// server starts asking.
func (e Env) authForm(ctx context.Context) (*Form, error) {
	st, err := e.serverSettings(ctx, false)
	if err != nil {
		return nil, err
	}
	modes := []string{"NONE", "BASIC_AUTH", "SIMPLE_LOGIN", "UI_LOGIN"}
	if s, ok := st.Get("authMode"); ok && len(s.Values) > 0 {
		modes = s.Values
	}
	cur := suwayomi.FormatValue(suwayomi.Setting{}, st.Values["authMode"])
	return &Form{
		Title: "Suwayomi Authentication",
		Intro: []string{
			"Who may use the server: NONE, or a username and password (" + strings.Join(modes[1:], ", ") + ").",
			"W5F keeps the account on this computer and signs in with it itself.",
		},
		Fields: []FormField{
			{Label: "Mode", Hint: "now: " + firstOf(cur, "NONE") + " · one of " + strings.Join(modes, ", ")},
			{Label: "Username", Hint: "empty keeps the current one"},
			{Label: "Password", Hint: "hidden; empty keeps the current one", Hidden: true},
		},
		Note: "W5F keeps the account in " + e.Server.Dir + " (readable by you alone).",
		Save: func(v []string) (string, string, error) {
			mode := strings.ToUpper(strings.TrimSpace(v[0]))
			if mode == "" {
				mode = firstOf(cur, "NONE")
			}
			ok := false
			for _, m := range modes {
				ok = ok || m == mode
			}
			if !ok {
				return "", "", fmt.Errorf("the mode is one of %s", strings.Join(modes, ", "))
			}
			old := e.Server.Auth()
			a := suwayomi.Auth{Mode: mode, Username: firstOf(v[1], old.Username, suwayomi.FormatValue(suwayomi.Setting{}, st.Values["authUsername"])),
				Password: firstOf(v[2], old.Password)}
			if a.On() && (a.Username == "" || a.Password == "") {
				return "", "", errors.New("a username and a password are needed with " + mode)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			defer forgetSettings()
			values := map[string]any{"authMode": mode}
			if a.On() {
				values["authUsername"], values["authPassword"] = a.Username, a.Password
			}
			if err := e.client().SetServerSettings(ctx, values); err != nil {
				return "", "", err
			}
			if err := e.Server.SaveAuth(a); err != nil {
				return "", "", fmt.Errorf("the server changed, but W5F could not keep the account: %w (sign in again: %s)", err, LoginFormPrefix)
			}
			return "w5f:comics/settings/auth?changed=auth", "Authentication is " + mode + "; W5F signs in with it.", nil
		},
	}, nil
}

// loginForm keeps the account W5F signs in with, when Authentication was
// changed elsewhere (the launcher, a WebUI) and the server asks.
func (e Env) loginForm() *Form {
	return &Form{
		Title: "Sign W5F in to Suwayomi",
		Intro: []string{"The server's Authentication is on. Give W5F the account it asks for; the server's settings stay as they are."},
		Fields: []FormField{
			{Label: "Mode", Hint: "BASIC_AUTH, SIMPLE_LOGIN or UI_LOGIN, as the server is set"},
			{Label: "Username"},
			{Label: "Password", Hidden: true},
		},
		Note: "Kept in " + e.Server.Dir + " (readable by you alone).",
		Save: func(v []string) (string, string, error) {
			a := suwayomi.Auth{Mode: strings.ToUpper(strings.TrimSpace(v[0])), Username: v[1], Password: v[2]}
			if !a.On() {
				return "", "", errors.New("give the mode the server uses")
			}
			if err := e.Server.SaveAuth(a); err != nil {
				return "", "", err
			}
			return "w5f:comics", "W5F signs in to Suwayomi with this account.", nil
		},
	}
}

// unauthorizedDoc says the server asks W5F to sign in.
func unauthorizedDoc() *doc.Document {
	p := newPage("Suwayomi asks W5F to sign in", "")
	p.note("warn", suwayomi.ErrUnauthorized.Error()+", and W5F has no account for it that works.")
	p.para(doc.Inline{p.a(LoginFormPrefix, "give W5F the account"), dim(" · "), p.a("w5f:comics", "← Comics")})
	return p.d
}

func updateCheck(ctx context.Context, env Env) (*doc.Document, error) {
	p := newPage("Suwayomi update", "")
	l, err := suwayomi.LatestRelease(ctx)
	cur := env.Server.Version()
	switch {
	case err != nil:
		p.note("warn", err.Error())
	case cur != "" && (l.Jar == "Suwayomi-Server-"+cur+".jar" || l.Tag == cur):
		p.note("info", "Suwayomi "+cur+" is the newest release.")
	default:
		p.para(doc.Inline{{Text: "Installed: " + firstOf(cur, "none") + " · newest: " + l.Tag}})
		p.para(doc.Inline{p.a("w5f:comics/server/update", "update to "+l.Tag), dim("  (the official release, checked against its checksums; a server W5F started is stopped and started again)")})
	}
	p.para(doc.Inline{p.a("w5f:comics/settings", "← settings"), dim(" · "), p.a("w5f:comics", "Comics")})
	return p.d, nil
}

func updateServer(ctx context.Context, env Env) (*doc.Document, error) {
	v, changed, err := env.Server.Update(ctx, nil)
	p := newPage("Suwayomi update", "")
	switch {
	case err != nil && changed:
		p.note("warn", "Updated to "+v+", but: "+err.Error())
	case err != nil:
		p.note("warn", err.Error())
	case changed:
		p.note("info", "Suwayomi is now "+v+".")
	default:
		p.note("info", "Suwayomi "+v+" is the newest release.")
	}
	settingsMu.Lock()
	settingsCache = suwayomi.Settings{} // a new release may have new settings
	settingsMu.Unlock()
	p.para(doc.Inline{p.a("w5f:comics/settings", "settings"), dim(" · "), p.a("w5f:comics", "← Comics")})
	return p.d, nil
}

func firstOf(vs ...string) string {
	for _, v := range vs {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
