package suwayomi

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
)

// ReadServerConf reads another Suwayomi's server.conf (HOCON as Suwayomi
// writes it: "server.name = value # note", lists over several lines) into
// plain text values: strings unquoted, lists joined with ", ".
func ReadServerConf(path string) (map[string]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	lines := strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
	for i := 0; i < len(lines); i++ {
		rest, ok := strings.CutPrefix(strings.TrimSpace(lines[i]), "server.")
		if !ok {
			continue
		}
		name, value, ok := strings.Cut(rest, "=")
		if !ok {
			continue
		}
		name, value = strings.TrimSpace(name), stripComment(value)
		if strings.HasPrefix(value, "[") && !strings.Contains(value, "]") {
			for i+1 < len(lines) { // a list over several lines
				i++
				value += " " + stripComment(lines[i])
				if strings.Contains(lines[i], "]") {
					break
				}
			}
		}
		out[name] = confValue(value)
	}
	return out, nil
}

// stripComment drops a "# note" that is not inside quotes.
func stripComment(s string) string {
	in := false
	for i, r := range s {
		switch {
		case r == '"' && (i == 0 || s[i-1] != '\\'):
			in = !in
		case r == '#' && !in:
			return strings.TrimSpace(s[:i])
		}
	}
	return strings.TrimSpace(s)
}

func confValue(v string) string {
	v = strings.TrimSpace(v)
	switch {
	case strings.HasPrefix(v, `"`):
		var s string
		if json.Unmarshal([]byte(v), &s) == nil {
			return s
		}
		return strings.Trim(v, `"`)
	case strings.HasPrefix(v, "[") && strings.HasSuffix(v, "]"):
		var parts []string
		for _, p := range strings.Split(strings.Trim(v, "[]"), ",") {
			if p = strings.TrimSpace(p); p != "" {
				parts = append(parts, confValue(p))
			}
		}
		return strings.Join(parts, ", ")
	}
	return v
}

// SameValue compares a value from a server.conf with one the server reports
// (numbers by value: "23.0" is 23).
func SameValue(s Setting, conf, current string) bool {
	if conf == current {
		return true
	}
	if s.Type == "Boolean" && !s.List {
		on := func(x string) string {
			switch strings.ToLower(x) {
			case "true", "on":
				return "on"
			case "false", "off":
				return "off"
			}
			return x
		}
		return on(conf) == on(current)
	}
	if s.Kind == "OBJECT" || s.List {
		empty := func(x string) bool { return x == "" || x == "{}" || x == "[]" }
		return empty(conf) && empty(current)
	}
	a, err1 := strconv.ParseFloat(conf, 64)
	b, err2 := strconv.ParseFloat(current, 64)
	if err1 == nil && err2 == nil {
		return a == b
	}
	// Durations: server.conf writes "5m" or "60d", the server answers
	// "PT5M" or "PT1440H".
	da, ok1 := durationSeconds(conf)
	db, ok2 := durationSeconds(current)
	return ok1 && ok2 && da == db
}

var durationUnits = map[byte]float64{'s': 1, 'm': 60, 'h': 3600, 'd': 86400}

// durationSeconds reads "90s", "5m", "60d" or ISO 8601 "PT5M", "P60D".
func durationSeconds(s string) (float64, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	iso := strings.HasPrefix(s, "p")
	s = strings.TrimPrefix(strings.TrimPrefix(s, "p"), "t")
	s = strings.ReplaceAll(s, "t", "")
	if s == "" {
		return 0, false
	}
	total, num := 0.0, ""
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9' || c == '.':
			num += string(c)
		case durationUnits[c] > 0 && num != "":
			n, err := strconv.ParseFloat(num, 64)
			if err != nil {
				return 0, false
			}
			total += n * durationUnits[c]
			num = ""
		default:
			return 0, false
		}
	}
	if num != "" || (!iso && total == 0 && !strings.ContainsAny(s, "smhd")) {
		return 0, false
	}
	return total, true
}
