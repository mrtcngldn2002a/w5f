package suwayomi

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

// TestLiveSettings changes settings on a real server when W5F_SUWAYOMI_LIVE
// is set, and puts every one back: values of each kind, and each
// Authentication mode signed in to.
func TestLiveSettings(t *testing.T) {
	base := os.Getenv("W5F_SUWAYOMI_LIVE")
	if base == "" {
		t.Skip("set W5F_SUWAYOMI_LIVE to run against a server")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	c := New(base)
	st, err := c.ServerSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d settings", len(st.List))
	for _, s := range st.List {
		if s.Group == "other" {
			t.Errorf("%s has no group", s.Name)
		}
	}
	try := func(name, text string) {
		t.Helper()
		s, ok := st.Get(name)
		if !ok {
			t.Fatalf("no %s", name)
		}
		old := st.Values[name]
		v, err := ParseValue(s, text)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if err := c.SetServerSettings(ctx, map[string]any{name: v}); err != nil {
			t.Fatalf("%s = %q: %v", name, text, err)
		}
		now, _ := c.ServerSettings(ctx)
		t.Logf("%s: %v → %v", name, FormatValue(s, old), FormatValue(s, now.Values[name]))
		if err := c.SetServerSettings(ctx, map[string]any{name: old}); err != nil {
			t.Errorf("putting %s back: %v", name, err)
		}
	}
	try("jwtTokenExpiry", "PT10M")
	try("backupTime", "03:30")
	try("downloadConversions", `[{"mimeType":"image/webp","target":"image/jpeg","compressionLevel":0.8}]`)
	try("koreaderSyncPercentageTolerance", "0.01")
	try("flareSolverrEnabled", "on")

	for _, mode := range []string{"BASIC_AUTH", "SIMPLE_LOGIN", "UI_LOGIN"} {
		if err := c.SetServerSettings(ctx, map[string]any{"authMode": mode, "authUsername": "w5f", "authPassword": "test-pass"}); err != nil {
			t.Fatalf("%s: %v", mode, err)
		}
		if _, err := New(base).ServerSettings(ctx); !errors.Is(err, ErrUnauthorized) && mode != "SIMPLE_LOGIN" {
			t.Errorf("%s: a client without the account: %v", mode, err)
		}
		signed := New(base)
		signed.Auth = Auth{Mode: mode, Username: "w5f", Password: "test-pass"}
		if _, err := signed.ServerSettings(ctx); err != nil {
			t.Errorf("%s: signed in: %v", mode, err)
		}
		if err := signed.SetServerSettings(ctx, map[string]any{"authMode": "NONE"}); err != nil {
			t.Fatalf("%s: back to NONE: %v", mode, err)
		}
		c = New(base)
	}
}
