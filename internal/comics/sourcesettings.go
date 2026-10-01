package comics

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"w5f/internal/comics/suwayomi"
	"w5f/internal/doc"
)

// SourcePrefFormPrefix opens the form for a source's text setting:
// w5f:comics/sourcepref/<source>/<position>.
const SourcePrefFormPrefix = "w5f:comics/sourcepref/"

func sourceSettingsHref(id string) string { return "w5f:comics/source/" + id + "/settings" }

func prefHref(id string, pos int, v string, toggle bool) string {
	q := url.Values{"p": {strconv.Itoa(pos)}, "v": {v}}
	if toggle {
		q.Set("t", "1")
	}
	return "w5f:comics/source/" + id + "/pref?" + q.Encode()
}

// sourceSettingsDoc lists a source's own settings with ways to change them.
func sourceSettingsDoc(ctx context.Context, c *suwayomi.Client, id, notice string) (*doc.Document, error) {
	name, prefs, err := c.SourcePreferences(ctx, id)
	if err != nil {
		return nil, err
	}
	p := newPage(name+": settings", sourceSettingsHref(id))
	if notice != "" {
		p.note("info", notice)
	}
	p.para(doc.Inline{dim("The source's own settings, as its extension offers them. They are kept by this Suwayomi only: another computer's Suwayomi keeps its own.")})
	var items []doc.Inline
	for _, pr := range prefs {
		if !pr.Visible {
			continue
		}
		in := doc.Inline{{Text: firstOf(pr.Title, pr.Key), Style: doc.Bold}}
		switch pr.Type {
		case "Switch", "CheckBox":
			state, flip, label := "off", "on", "turn on"
			if pr.On {
				state, flip, label = "on", "off", "turn off"
			}
			in = append(in, doc.Span{Text: ": " + state})
			if pr.Enabled {
				in = append(in, dim("  · "), p.a(prefHref(id, pr.Position, flip, false), label))
			}
		case "List":
			in = append(in, doc.Span{Text: ": " + firstOf(pr.Label(pr.Text), "(none)")})
			if pr.Enabled {
				in = append(in, dim("  ·"))
				for i, v := range pr.EntryValues {
					if v == pr.Text || i >= len(pr.Entries) {
						continue
					}
					in = append(in, dim(" "), p.a(prefHref(id, pr.Position, v, false), pr.Entries[i]))
				}
			}
		case "MultiSelectList":
			var chosen []string
			for _, v := range pr.Values {
				chosen = append(chosen, pr.Label(v))
			}
			in = append(in, doc.Span{Text: ": " + firstOf(strings.Join(chosen, ", "), "(none)")})
			if pr.Enabled {
				in = append(in, dim("  · toggle:"))
				for i, v := range pr.EntryValues {
					if i < len(pr.Entries) {
						in = append(in, dim(" "), p.a(prefHref(id, pr.Position, v, true), pr.Entries[i]))
					}
				}
			}
		case "EditText":
			in = append(in, doc.Span{Text: ": " + firstOf(clip(pr.Text, 100), "(empty)")})
			if pr.Enabled {
				in = append(in, dim("  · "), p.a(fmt.Sprintf("%s%s/%d", SourcePrefFormPrefix, id, pr.Position), "change"))
			}
		default:
			in = append(in, dim("  (a "+pr.Type+" setting W5F cannot change)"))
		}
		// Android writes %s in a summary for the value chosen, shown already.
		if sum := strings.TrimSpace(strings.ReplaceAll(pr.Summary, "%s", pr.Label(pr.Text))); sum != "" && sum != pr.Label(pr.Text) {
			in = append(in, dim("  — "+clip(sum, 160)))
		}
		items = append(items, in)
	}
	if len(items) == 0 {
		p.para(doc.Inline{dim("This source has no settings of its own.")})
	} else {
		p.list(items)
	}
	p.para(doc.Inline{p.a("w5f:comics/sources", "← Sources")})
	return p.d, nil
}

// findPref reads the source's settings again and finds one by position,
// so a change always applies to what the server has now.
func findPref(ctx context.Context, c *suwayomi.Client, id string, pos int) (suwayomi.SourcePref, error) {
	_, prefs, err := c.SourcePreferences(ctx, id)
	if err != nil {
		return suwayomi.SourcePref{}, err
	}
	for _, p := range prefs {
		if p.Position == pos {
			return p, nil
		}
	}
	return suwayomi.SourcePref{}, errors.New("that setting is not there any more")
}

// setSourcePref applies a link: on/off, a list value, or a value toggled in
// a multiple choice.
func setSourcePref(ctx context.Context, c *suwayomi.Client, id string, q url.Values) (*doc.Document, error) {
	pos, err := strconv.Atoi(q.Get("p"))
	if err != nil {
		return nil, errors.New("bad setting address")
	}
	pr, err := findPref(ctx, c, id, pos)
	if err != nil {
		return nil, err
	}
	v := q.Get("v")
	var value any
	switch pr.Type {
	case "Switch", "CheckBox":
		value = v == "on"
	case "List":
		if !contains(pr.EntryValues, v) {
			return nil, fmt.Errorf("%q is not one of the choices", v)
		}
		value = v
	case "MultiSelectList":
		if !contains(pr.EntryValues, v) {
			return nil, fmt.Errorf("%q is not one of the choices", v)
		}
		vals := []string{}
		for _, x := range pr.Values {
			if x != v {
				vals = append(vals, x)
			}
		}
		if !contains(pr.Values, v) {
			vals = append(vals, v)
		}
		value = vals
	default:
		return nil, fmt.Errorf("a %s setting is changed with its form", pr.Type)
	}
	if err := c.SetSourcePreference(ctx, id, pr, value); err != nil {
		return nil, err
	}
	return sourceSettingsDoc(ctx, c, id, firstOf(pr.Title, pr.Key)+" changed.")
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// sourcePrefForm is the form for a text setting.
func (e Env) sourcePrefForm(href string) (*Form, error) {
	rest := strings.TrimPrefix(href, SourcePrefFormPrefix)
	id, posText, ok := strings.Cut(rest, "/")
	pos, err := strconv.Atoi(posText)
	if !ok || err != nil {
		return nil, errors.New("bad setting address")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c := e.client()
	pr, err := findPref(ctx, c, id, pos)
	if err != nil {
		return nil, err
	}
	if pr.Type != "EditText" {
		return nil, fmt.Errorf("a %s setting is changed with its links", pr.Type)
	}
	intro := []string{}
	for _, s := range []string{pr.Summary, pr.Dialog} {
		if s != "" {
			intro = append(intro, s)
		}
	}
	return &Form{
		Title:  firstOf(pr.Title, pr.Key),
		Intro:  intro,
		Fields: []FormField{{Label: firstOf(pr.Title, pr.Key), Hint: "now: " + firstOf(clip(pr.Text, 70), "(empty)")}},
		Note:   "Kept by this Suwayomi for the source.",
		Save: func(v []string) (string, string, error) {
			if len(v) == 0 {
				return "", "Nothing changed.", nil
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := c.SetSourcePreference(ctx, id, pr, strings.TrimSpace(v[0])); err != nil {
				return "", "", err
			}
			return sourceSettingsHref(id), firstOf(pr.Title, pr.Key) + " saved.", nil
		},
	}, nil
}
