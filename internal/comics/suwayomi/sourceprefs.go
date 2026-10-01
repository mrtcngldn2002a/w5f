package suwayomi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// SourcePref is one of a source's own settings (what an extension lets you
// choose: a quality, a language, filters, a site option). Position is its
// place in the list, which is how Suwayomi changes it.
type SourcePref struct {
	Position    int
	Type        string // Switch, CheckBox, EditText, List, MultiSelectList
	Key         string
	Title       string
	Summary     string
	Visible     bool
	Enabled     bool
	On          bool     // Switch, CheckBox
	Text        string   // EditText, List (the chosen value)
	Values      []string // MultiSelectList (the chosen values)
	Entries     []string // List, MultiSelectList: what is shown
	EntryValues []string // … and what is kept
	Dialog      string   // EditText, MultiSelectList: the dialog's message
}

// Label is what a list value is shown as.
func (p SourcePref) Label(value string) string {
	for i, v := range p.EntryValues {
		if v == value && i < len(p.Entries) {
			return p.Entries[i]
		}
	}
	return value
}

// prefFields reads every kind; the value fields get an alias per kind, as
// GraphQL does not let one name carry a Boolean here and a String there.
const prefFields = `__typename
	... on SwitchPreference { key title summary visible enabled boolValue: currentValue boolDefault: default }
	... on CheckBoxPreference { key title summary visible enabled boolValue: currentValue boolDefault: default }
	... on EditTextPreference { key title summary visible enabled textValue: currentValue textDefault: default dialogMessage }
	... on ListPreference { key title summary visible enabled textValue: currentValue textDefault: default entries entryValues }
	... on MultiSelectListPreference { key title summary visible enabled listValue: currentValue listDefault: default entries entryValues dialogMessage }`

// SourcePreferences reads a source's name and settings.
func (c *Client) SourcePreferences(ctx context.Context, id string) (string, []SourcePref, error) {
	var r struct {
		Source *struct {
			DisplayName string            `json:"displayName"`
			Preferences []json.RawMessage `json:"preferences"`
		} `json:"source"`
	}
	if err := c.do(ctx, `query($id: LongString!) { source(id: $id) { displayName preferences { `+prefFields+` } } }`,
		map[string]any{"id": id}, &r); err != nil {
		return "", nil, err
	}
	if r.Source == nil {
		return "", nil, fmt.Errorf("no source %s on this server", id)
	}
	var out []SourcePref
	for i, raw := range r.Source.Preferences {
		var x struct {
			Typename    string   `json:"__typename"`
			Key         string   `json:"key"`
			Title       string   `json:"title"`
			Summary     string   `json:"summary"`
			Visible     bool     `json:"visible"`
			Enabled     bool     `json:"enabled"`
			BoolValue   *bool    `json:"boolValue"`
			BoolDefault *bool    `json:"boolDefault"`
			TextValue   *string  `json:"textValue"`
			TextDefault *string  `json:"textDefault"`
			ListValue   []string `json:"listValue"`
			ListDefault []string `json:"listDefault"`
			Entries     []string `json:"entries"`
			EntryValues []string `json:"entryValues"`
			Dialog      string   `json:"dialogMessage"`
		}
		if err := json.Unmarshal(raw, &x); err != nil {
			return "", nil, err
		}
		p := SourcePref{Position: i, Type: strings.TrimSuffix(x.Typename, "Preference"), Key: x.Key, Title: x.Title, Summary: x.Summary,
			Visible: x.Visible, Enabled: x.Enabled, Entries: x.Entries, EntryValues: x.EntryValues, Dialog: x.Dialog}
		// A setting never changed has no value yet: the extension's default.
		switch {
		case x.BoolValue != nil:
			p.On = *x.BoolValue
		case x.BoolDefault != nil:
			p.On = *x.BoolDefault
		}
		switch {
		case x.TextValue != nil:
			p.Text = *x.TextValue
		case x.TextDefault != nil:
			p.Text = *x.TextDefault
		}
		p.Values = x.ListValue
		if p.Values == nil {
			p.Values = x.ListDefault
		}
		out = append(out, p)
	}
	return r.Source.DisplayName, out, nil
}

// SetSourcePreference changes one of a source's settings: a bool for a
// switch or check box, a string for text or a list, a []string for a
// multiple choice.
func (c *Client) SetSourcePreference(ctx context.Context, id string, p SourcePref, value any) error {
	change := map[string]any{"position": p.Position}
	switch p.Type {
	case "Switch":
		change["switchState"] = value
	case "CheckBox":
		change["checkBoxState"] = value
	case "EditText":
		change["editTextState"] = value
	case "List":
		change["listState"] = value
	case "MultiSelectList":
		change["multiSelectState"] = value
	default:
		return fmt.Errorf("W5F does not know how to change a %s setting", p.Type)
	}
	return c.do(ctx, `mutation($in: UpdateSourcePreferenceInput!) { updateSourcePreference(input: $in) { clientMutationId } }`,
		map[string]any{"in": map[string]any{"source": id, "change": change}}, nil)
}
