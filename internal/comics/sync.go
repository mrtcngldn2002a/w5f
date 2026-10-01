package comics

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"w5f/internal/comics/suwayomi"
)

// Settings sync: another Suwayomi's server.conf (the PC's launcher, say)
// compared with this server, and its differences applied — only what does
// not belong to one computer (asked for by the owner, 2026-10-01). Sources'
// own settings and the extensions installed are not copied: which
// extensions a computer has stays the owner's choice there.

// SyncItem is one setting compared.
type SyncItem struct {
	Name, Group string
	Here, There string
	Skip        string // why it is not copied ("" = it is)
}

// Same reports a setting that already matches.
func (i SyncItem) Same() bool { return i.Here == i.There }

// syncSkip says why a setting stays as it is on this computer.
func syncSkip(name string, forced map[string]string) string {
	l := strings.ToLower(name)
	switch {
	case forced[name] != "":
		return "W5F gives it when the server starts"
	case strings.HasPrefix(name, "flareSolverr"):
		return "a bot-check solver: W5F does not copy it; set it here yourself if you want it"
	case strings.HasPrefix(name, "socksProxy"):
		return "this computer's network"
	case strings.HasPrefix(name, "auth"), strings.HasPrefix(name, "jwt"), strings.HasPrefix(name, "syncYomi"),
		strings.Contains(l, "password"), strings.Contains(l, "username"), strings.Contains(l, "apikey"):
		return "an account: set it on each computer"
	}
	switch name {
	case "ip", "port", "rootDir", "downloadsPath", "localSourcePath", "backupPath", "electronPath",
		"initialOpenInBrowserEnabled", "systemTrayEnabled", "databaseType", "databaseUrl", "useHikariConnectionPool":
		return "this computer's own"
	}
	if strings.HasPrefix(name, "webUI") {
		return "this computer's own"
	}
	return ""
}

// SyncPlan compares a server.conf with this server's settings and stores.
func (e Env) SyncPlan(ctx context.Context, confPath string) ([]SyncItem, []string, error) {
	conf, err := suwayomi.ReadServerConf(confPath)
	if err != nil {
		return nil, nil, err
	}
	st, err := e.serverSettings(ctx, true)
	if err != nil {
		return nil, nil, err
	}
	forced := e.Server.Forced()
	var items []SyncItem
	for _, s := range st.List {
		there, ok := conf[s.Name]
		if !ok {
			continue
		}
		here := suwayomi.FormatValue(s, st.Values[s.Name])
		it := SyncItem{Name: s.Name, Group: s.Group, Here: here, There: there}
		if suwayomi.SameValue(s, there, here) {
			it.There = here
		} else if skip := syncSkip(s.Name, forced); skip != "" {
			it.Skip = skip
		} else if !s.Settable {
			it.Skip = "this server does not let it be changed"
		}
		if s.Secret {
			it.Here, it.There = mask(it.Here), mask(it.There)
		}
		items = append(items, it)
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Group < items[j].Group })
	// Extension stores the other computer has and this one has not.
	var missing []string
	if want := conf["extensionStores"]; want != "" {
		_, have, err := e.client().Extensions(ctx, false)
		if err != nil {
			return nil, nil, err
		}
		got := map[string]bool{}
		for _, s := range have {
			got[s.IndexURL] = true
		}
		for _, u := range strings.Split(want, ", ") {
			if u = strings.TrimSpace(u); u != "" && !got[u] {
				missing = append(missing, u)
			}
		}
	}
	return items, missing, nil
}

func mask(v string) string {
	if v == "" {
		return ""
	}
	return "(hidden)"
}

// ApplySync copies what the plan says differs and may be copied, and adds
// the missing extension stores. It returns how many changed.
func (e Env) ApplySync(ctx context.Context, confPath string) (int, error) {
	items, missing, err := e.SyncPlan(ctx, confPath)
	if err != nil {
		return 0, err
	}
	conf, _ := suwayomi.ReadServerConf(confPath)
	st, err := e.serverSettings(ctx, false)
	if err != nil {
		return 0, err
	}
	values := map[string]any{}
	for _, it := range items {
		if it.Same() || it.Skip != "" {
			continue
		}
		s, _ := st.Get(it.Name)
		text := conf[it.Name]
		if (s.Type == "Int" || s.Type == "Long" || s.Type == "Short") && strings.Contains(text, ".") {
			if f, err := strconv.ParseFloat(text, 64); err == nil && f == float64(int64(f)) {
				text = strconv.FormatInt(int64(f), 10)
			}
		}
		v, err := suwayomi.ParseValue(s, text)
		if err != nil {
			return 0, err
		}
		values[it.Name] = v
	}
	n := 0
	if len(values) > 0 {
		defer forgetSettings()
		if err := e.client().SetServerSettings(ctx, values); err != nil {
			return 0, err
		}
		n = len(values)
	}
	for _, u := range missing {
		if err := e.client().AddStore(ctx, u); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
