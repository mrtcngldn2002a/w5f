// Command w5f-release makes signed W5F releases (developer tool; it is not
// shipped and never uploads anything).
//
//	go run ./cmd/w5f-release keygen
//	go run ./cmd/w5f-release build -version 0.7.0 [-repo owner/w5f]
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"w5f/internal/update"
)

// target is one binary of a release.
type target struct{ goos, goarch, amd64, platform, name string }

var targets = []target{
	{"linux", "amd64", "v1", "linux-amd64", "w5f-linux-amd64"}, // the W5F laptop (Core 2: no SSE4.2)
	{"linux", "386", "", "linux-386", "w5f-linux-386"},
	{"windows", "amd64", "v1", "windows-amd64", "w5f-windows-amd64.exe"},
}

// keyPath is where the private key lives: outside the repo.
func keyPath() string {
	if p := os.Getenv("W5F_RELEASE_KEY"); p != "" {
		return p
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = "."
	}
	return filepath.Join(dir, "w5f-release", "release.key")
}

const pubPath = "internal/update/release.pub"

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: w5f-release keygen | build -version X.Y.Z [-repo owner/w5f]")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "keygen":
		err = keygen(keyPath(), pubPath)
		if err == nil {
			fmt.Println("Private key:", keyPath(), "(keep it safe; never commit it)")
			fmt.Println("Public key: ", pubPath, "(commit this; releases are checked against it)")
		}
	case "build":
		fs := flag.NewFlagSet("build", flag.ExitOnError)
		ver := fs.String("version", "", "release version, e.g. 0.7.0")
		repo := fs.String("repo", "", "GitHub repo built in as the update source (owner/w5f)")
		fs.Parse(os.Args[2:])
		err = build(strings.TrimPrefix(*ver, "v"), *repo)
	default:
		err = errors.New("unknown command " + os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "w5f-release:", err)
		os.Exit(1)
	}
}

// keygen writes a new key pair; it never replaces an existing private key.
func keygen(priv, pub string) error {
	if _, err := os.Stat(priv); err == nil {
		return fmt.Errorf("%s already exists: a new key would make every installed W5F refuse updates", priv)
	}
	pk, sk, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(priv), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(priv, []byte(base64.StdEncoding.EncodeToString(sk)+"\n"), 0o600); err != nil {
		return err
	}
	return os.WriteFile(pub, []byte("# W5F release key (ed25519, base64). Written by: go run ./cmd/w5f-release keygen\n"+
		base64.StdEncoding.EncodeToString(pk)+"\n"), 0o644)
}

// loadKey reads the private key and checks it matches the public key the
// binaries will carry.
func loadKey(priv, pub string) (ed25519.PrivateKey, error) {
	b, err := os.ReadFile(priv)
	if err != nil {
		return nil, fmt.Errorf("no release key (%v): run keygen first", err)
	}
	sk, err := update.ParseKey(string(b), ed25519.PrivateKeySize)
	if err != nil {
		return nil, err
	}
	pb, err := os.ReadFile(pub)
	if err != nil {
		return nil, err
	}
	pk, err := update.ParseKey(string(pb), ed25519.PublicKeySize)
	if err != nil || !ed25519.PublicKey(pk).Equal(ed25519.PrivateKey(sk).Public()) {
		return nil, fmt.Errorf("%s does not match the private key", pub)
	}
	return ed25519.PrivateKey(sk), nil
}

func build(version, repo string) error {
	if update.Newer("0.0.1", version) || strings.Count(version, ".") != 2 {
		return errors.New("give a version like -version 0.7.0")
	}
	key, err := loadKey(keyPath(), pubPath)
	if err != nil {
		return err
	}
	dir := filepath.Join("dist", "v"+version)
	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("%s already exists", dir)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	ld := "-s -w -X main.version=" + version
	if repo != "" {
		ld += " -X w5f/internal/update.DefaultRepo=" + repo
	}
	m := update.Manifest{Version: version, Date: time.Now().Format("2006-01-02"), Assets: map[string]update.Asset{}}
	for _, t := range targets {
		out := filepath.Join(dir, t.name)
		fmt.Println("building", t.name)
		cmd := exec.Command("go", "build", "-trimpath", "-ldflags", ld, "-o", out, "./cmd/w5f")
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+t.goos, "GOARCH="+t.goarch, "GOAMD64="+t.amd64)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%s: %w", t.name, err)
		}
		a, err := assetOf(out)
		if err != nil {
			return err
		}
		m.Assets[t.platform] = a
	}
	return writeManifest(dir, m, key, version)
}

func assetOf(path string) (update.Asset, error) {
	f, err := os.Open(path)
	if err != nil {
		return update.Asset{}, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	return update.Asset{Name: filepath.Base(path), SHA256: hex.EncodeToString(h.Sum(nil)), Size: n}, err
}

func writeManifest(dir string, m update.Manifest, key ed25519.PrivateKey, version string) error {
	mb, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, update.ManifestName), mb, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, update.SignatureName), update.Sign(mb, key), 0o644); err != nil {
		return err
	}
	fmt.Printf("\nRelease v%s is ready in %s (signed).\nPublish it (this tool does not upload):\n", version, dir)
	fmt.Printf("  gh release create v%s %s/*  --title \"W5F %s\"\n", version, filepath.ToSlash(dir), version)
	fmt.Println("  or on github.com: Releases → Draft a new release → tag v" + version + " → attach every file in the folder.")
	return nil
}
