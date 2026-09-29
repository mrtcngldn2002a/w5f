package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"w5f/internal/update"
)

func TestKeygenSignVerify(t *testing.T) {
	dir := t.TempDir()
	priv, pub := filepath.Join(dir, "k", "release.key"), filepath.Join(dir, "release.pub")
	if err := keygen(priv, pub); err != nil {
		t.Fatal(err)
	}
	if err := keygen(priv, pub); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("keygen must never replace a key: %v", err)
	}
	key, err := loadKey(priv, pub)
	if err != nil {
		t.Fatal(err)
	}
	m := update.Manifest{Version: "0.7.0", Assets: map[string]update.Asset{"linux-amd64": {Name: "w5f-linux-amd64", SHA256: "ab", Size: 1}}}
	out := t.TempDir()
	if err := writeManifest(out, m, key, "0.7.0"); err != nil {
		t.Fatal(err)
	}
	mb, _ := os.ReadFile(filepath.Join(out, update.ManifestName))
	sb, _ := os.ReadFile(filepath.Join(out, update.SignatureName))
	pb, _ := os.ReadFile(pub)
	pk, err := update.ParseKey(string(pb), 32)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := update.Verify(mb, sb, pk); err != nil || got.Version != "0.7.0" {
		t.Errorf("the updater must accept the tool's release: %v", err)
	}
	// A public key from another pair is caught before building.
	other := filepath.Join(dir, "other")
	if err := keygen(filepath.Join(other, "release.key"), filepath.Join(other, "release.pub")); err != nil {
		t.Fatal(err)
	}
	if _, err := loadKey(priv, filepath.Join(other, "release.pub")); err == nil {
		t.Error("a mismatched public key must be refused")
	}
}
