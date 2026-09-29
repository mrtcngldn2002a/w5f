package update

import (
	"crypto/ed25519"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"strings"
)

// Manifest lists a release's binaries; its exact bytes are signed.
type Manifest struct {
	Version string           `json:"version"`
	Date    string           `json:"date"`
	Assets  map[string]Asset `json:"assets"` // by platform, e.g. "linux-amd64"
}

// Asset is one binary of a release.
type Asset struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// Release file names next to the binaries.
const (
	ManifestName  = "manifest.json"
	SignatureName = "manifest.json.sig"
)

// Platform is this binary's key in a manifest.
func Platform() string { return runtime.GOOS + "-" + runtime.GOARCH }

//go:embed release.pub
var embeddedKey string

// PublicKey is the release key built into the binary (nil in builds that
// have none: they cannot update themselves).
func PublicKey() ed25519.PublicKey {
	k, err := ParseKey(embeddedKey, ed25519.PublicKeySize)
	if err != nil {
		return nil
	}
	return ed25519.PublicKey(k)
}

// ParseKey reads a base64 key of the given size ("" and comment lines are
// skipped).
func ParseKey(s string, size int) ([]byte, error) {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		b, err := base64.StdEncoding.DecodeString(line)
		if err != nil || len(b) != size {
			return nil, errors.New("not a release key")
		}
		return b, nil
	}
	return nil, errors.New("no key")
}

// Verify checks a manifest's signature and reads it.
func Verify(manifest, sig []byte, key ed25519.PublicKey) (*Manifest, error) {
	if len(key) != ed25519.PublicKeySize {
		return nil, errors.New("this build has no release key, so it cannot check updates")
	}
	s, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sig)))
	if err != nil || !ed25519.Verify(key, manifest, s) {
		return nil, errors.New("the release signature does not match: refusing it")
	}
	var m Manifest
	if err := json.Unmarshal(manifest, &m); err != nil {
		return nil, fmt.Errorf("unreadable manifest: %w", err)
	}
	if m.Version == "" {
		return nil, errors.New("the manifest has no version")
	}
	return &m, nil
}

// Sign signs manifest bytes (release tool).
func Sign(manifest []byte, key ed25519.PrivateKey) []byte {
	return []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(key, manifest)) + "\n")
}
