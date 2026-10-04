// Package solver installs and manages the owner's optional local Byparr.
// Its downloads and processes are separate from W5F updates and page caches.
package solver

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const Version = "3.0.4"
const UVVersion = "0.12.22"
const MinFreeBytes uint64 = 1500 << 20
const manifestName = "w5f-install.json"

// Set to yes only after the isolated Windows installation and live test pass.
var WindowsEnabled = "yes"

// macOS (Apple silicon and Intel) runs the same Byparr headless: no Xvfb,
// its browser hides its own window. Turned on at the owner's request
// (2026-10-04) before a live test on a Mac; "no" turns it off again.
var DarwinEnabled = "yes"
var Progress atomic.Value
var installationMu sync.Mutex
var installationCond = sync.NewCond(&installationMu)
var activeInstallations = map[uint64]context.CancelFunc{}
var nextInstallation uint64
var closingInstallations bool

func beginInstallation(ctx context.Context) (context.Context, func(), error) {
	installationMu.Lock()
	defer installationMu.Unlock()
	if e := ctx.Err(); e != nil {
		return nil, nil, e
	}
	if closingInstallations {
		return nil, nil, context.Canceled
	}
	child, cancel := context.WithCancel(ctx)
	nextInstallation++
	id := nextInstallation
	activeInstallations[id] = cancel
	return child, func() {
		cancel()
		installationMu.Lock()
		delete(activeInstallations, id)
		installationCond.Broadcast()
		installationMu.Unlock()
	}, nil
}
func finishInstallations() {
	installationMu.Lock()
	defer installationMu.Unlock()
	closingInstallations = true
	for _, cancel := range activeInstallations {
		cancel()
	}
	for len(activeInstallations) > 0 {
		installationCond.Wait()
	}
	closingInstallations = false
}

type Artifact struct{ URL, SHA256 string }
type Manifest struct {
	Format      int       `json:"format"`
	Version     string    `json:"version"`
	Installed   time.Time `json:"installed"`
	Files       []string  `json:"files"`
	Directories []string  `json:"directories"`
}
type Installer struct {
	DataDir      string
	GOOS, GOARCH string
	Byparr, UV   Artifact
	Free         func(string) (uint64, error)
	Run          func(context.Context, string, []string, []string, string, io.Writer) error
	Client       *http.Client
	Output       io.Writer
}

func Supported(goos, goarch string) error {
	if goarch == "amd64" && (goos == "linux" || goos == "windows" && WindowsEnabled == "yes") {
		return nil
	}
	if goos == "darwin" && (goarch == "arm64" || goarch == "amd64") && DarwinEnabled == "yes" {
		return nil
	}
	if goos == "windows" && goarch == "amd64" {
		return errors.New("Byparr is not offered on Windows yet")
	}
	return errors.New("Byparr is not available on this system")
}

func (i Installer) defaults() Installer {
	if i.GOOS == "" {
		i.GOOS = runtime.GOOS
	}
	if i.GOARCH == "" {
		i.GOARCH = runtime.GOARCH
	}
	if i.Byparr.URL == "" {
		i.Byparr = Artifact{"https://codeload.github.com/ThePhaseless/Byparr/tar.gz/refs/tags/v" + Version, "e71246efb5e6908f1297efc98b97793f406b404eb8248682f6be1aa458d44bb9"}
	}
	if i.UV.URL == "" {
		asset, hash := "uv-x86_64-unknown-linux-gnu.tar.gz", "b9980552309f09c15172b8be828555e375097f16deb459795ce7bfd200380f0b"
		switch {
		case i.GOOS == "windows":
			asset, hash = "uv-x86_64-pc-windows-msvc.zip", "ea1397797a0ca15f63516dd0f49c2dde9776db9be5861cab152ebe8ad199894d"
		case i.GOOS == "darwin" && i.GOARCH == "arm64":
			asset, hash = "uv-aarch64-apple-darwin.tar.gz", "5d714de09501a59393ceca78f4bc232a50478729640d251907160299b2a93ddd"
		case i.GOOS == "darwin":
			asset, hash = "uv-x86_64-apple-darwin.tar.gz", "1b8a5b316883df2daf20fb9a446e5b230e01d947d57aba2694977c5ac5a7e98c"
		}
		i.UV = Artifact{"https://github.com/astral-sh/uv/releases/download/" + UVVersion + "/" + asset, hash}
	}
	if i.Free == nil {
		i.Free = freeBytes
	}
	if i.Run == nil {
		i.Run = run
	}
	if i.Client == nil {
		i.Client = &http.Client{Timeout: 15 * time.Minute}
	}
	if i.Output == nil {
		i.Output = io.Discard
	}
	return i
}
func Dir(data, version string) string { return filepath.Join(data, "byparr-v"+version) }
func Python(dir, goos string) string {
	if goos == "windows" {
		return filepath.Join(dir, ".venv", "Scripts", "python.exe")
	}
	return filepath.Join(dir, ".venv", "bin", "python")
}
func present(dir string) bool {
	if _, e := os.Stat(filepath.Join(dir, ".w5f-installing")); e == nil {
		return false
	}
	for _, p := range []string{filepath.Join(dir, "main.py"), filepath.Join(dir, "uv.lock"), Python(dir, runtime.GOOS)} {
		if st, e := os.Stat(p); e != nil || st.IsDir() {
			return false
		}
	}
	return true
}
func cleanData(data string) (string, error) {
	if data == "" {
		return "", errors.New("no solver data folder")
	}
	p, e := filepath.Abs(data)
	if e != nil {
		return "", e
	}
	// Resolve existing parents before checking the protected cache boundary.
	parent := p
	var tail []string
	for {
		if _, e := os.Lstat(parent); e == nil {
			break
		}
		next := filepath.Dir(parent)
		if next == parent {
			return "", errors.New("cannot resolve solver data folder")
		}
		tail = append(tail, filepath.Base(parent))
		parent = next
	}
	resolved, e := filepath.EvalSymlinks(parent)
	if e != nil {
		return "", e
	}
	for k := len(tail) - 1; k >= 0; k-- {
		resolved = filepath.Join(resolved, tail[k])
	}
	p = resolved
	// All solver writes are outside the page cache, including custom test roots.
	h, _ := os.UserHomeDir()
	cache, _ := os.UserCacheDir()
	for _, blocked := range []string{filepath.Join(h, ".cache", "w5f"), filepath.Join(cache, "w5f")} {
		rel, e := filepath.Rel(blocked, p)
		if e == nil && (rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)) {
			return "", errors.New("solver data must not be inside W5F's protected cache")
		}
	}
	return p, nil
}
func (i Installer) Install(ctx context.Context) (installed string, err error) {
	ctx, finished, e := beginInstallation(ctx)
	if e != nil {
		return "", e
	}
	defer finished()
	i = i.defaults()
	if e := Supported(i.GOOS, i.GOARCH); e != nil {
		return "", e
	}
	data, e := cleanData(i.DataDir)
	if e != nil {
		return "", e
	}
	i.DataDir = data
	if e = os.MkdirAll(data, 0700); e != nil {
		return "", e
	}
	dir := Dir(data, Version)
	if present(dir) {
		fmt.Fprintln(i.Output, "Byparr", Version, "is already installed; existing files are kept.")
		return dir, nil
	}
	if _, e = os.Lstat(dir); !errors.Is(e, os.ErrNotExist) {
		return "", fmt.Errorf("%s already exists; it is not changed (inspect the incomplete/manual installation)", dir)
	}
	available, e := i.Free(data)
	if e != nil {
		return "", e
	}
	if available < MinFreeBytes {
		return "", fmt.Errorf("not enough free disk space: need at least 1.5 GB, have %.1f GB", float64(available)/(1<<30))
	}
	lock := filepath.Join(data, ".w5f-solver-install.lock")
	lf, e := os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return "", fmt.Errorf("another solver installation may be running: %w", e)
	}
	lf.Close()
	defer os.Remove(lock)
	tmp, e := os.MkdirTemp(data, ".solver-download-")
	if e != nil {
		return "", e
	}
	defer os.RemoveAll(tmp)
	if e = os.Mkdir(dir, 0700); e != nil {
		return "", e
	}
	if e = os.WriteFile(filepath.Join(dir, ".w5f-installing"), []byte("W5F solver installation in progress\n"), 0600); e != nil {
		os.Remove(dir)
		return "", e
	}
	complete := false
	defer func() {
		if !complete {
			if cleanupErr := os.RemoveAll(dir); cleanupErr != nil {
				err = errors.Join(err, fmt.Errorf("could not clean incomplete installation %s: %w", dir, cleanupErr))
			}
		}
	}()
	defer Progress.Store("")
	step := func(n int, msg string) {
		line := fmt.Sprintf("%d/5 %s", n, msg)
		Progress.Store(line)
		fmt.Fprintln(i.Output, line)
	}
	step(1, "Downloading and verifying uv "+UVVersion)
	uvArchive := filepath.Join(tmp, "uv.archive")
	if e = i.download(ctx, i.UV, uvArchive, 64<<20); e != nil {
		return "", e
	}
	uvDir := filepath.Join(dir, "tools")
	if e = extract(uvArchive, uvDir, i.GOOS == "windows", i.GOOS != "windows"); e != nil {
		return "", e
	}
	uv := filepath.Join(uvDir, "uv")
	if i.GOOS == "windows" {
		uv += ".exe"
	}
	if _, e = os.Stat(uv); e != nil {
		return "", fmt.Errorf("uv archive has no executable: %w", e)
	}
	env := installEnv(dir, tmp)
	step(2, "Installing verified Python 3.14")
	if e = i.Run(ctx, uv, []string{"python", "install", "3.14", "--no-bin"}, env, dir, i.Output); e != nil {
		return "", e
	}
	step(3, "Downloading and verifying Byparr "+Version)
	archive := filepath.Join(tmp, "byparr.tar.gz")
	if e = i.download(ctx, i.Byparr, archive, 16<<20); e != nil {
		return "", e
	}
	if e = extract(archive, dir, false, true); e != nil {
		return "", e
	}
	step(4, "Installing locked packages (uv sync --frozen --no-dev)")
	if e = i.Run(ctx, uv, []string{"sync", "--frozen", "--no-dev", "--python", "3.14"}, env, dir, i.Output); e != nil {
		return "", e
	}
	step(5, "Downloading sealed Firefox and GeoIP (GeoIP is not hash-pinned)")
	python := Python(dir, i.GOOS)
	if e = i.Run(ctx, python, []string{"-m", "invisible_playwright", "fetch"}, env, dir, i.Output); e != nil {
		return "", e
	}
	if e = i.Run(ctx, python, []string{"-c", "from invisible_core._geoip_db import ensure_geoip_mmdb; ensure_geoip_mmdb()"}, env, dir, i.Output); e != nil {
		return "", e
	}
	// Materialise the pinned package's font list before recording ownership.
	// Otherwise the first browser session creates an unlisted payload file.
	if e = i.Run(ctx, python, []string{"-c", "from invisible_core.launch import cached_font_manifest_path; from invisible_core._fpforge.profile import FONT_MANIFEST; cached_font_manifest_path(FONT_MANIFEST)"}, env, dir, i.Output); e != nil {
		return "", e
	}
	if e = ctx.Err(); e != nil {
		return "", e
	}
	if e = os.Remove(filepath.Join(dir, ".w5f-installing")); e != nil {
		return "", e
	}
	if e = writeManifest(dir, Version); e != nil {
		return "", e
	}
	complete = true
	fmt.Fprintln(i.Output, "Byparr installed in", dir)
	return dir, nil
}

func privateEnv(dir string) []string {
	return mergeEnv(os.Environ(), map[string]string{"INVISIBLE_PLAYWRIGHT_CACHE_DIR": filepath.Join(dir, "browser-cache"), "PLAYWRIGHT_BROWSERS_PATH": filepath.Join(dir, "playwright-cache"), "UV_PYTHON_INSTALL_DIR": filepath.Join(dir, "python"), "UV_PYTHON_BIN_DIR": filepath.Join(dir, "python-bin"), "UV_PROJECT_ENVIRONMENT": filepath.Join(dir, ".venv"), "UV_SYSTEM_PYTHON": "false", "UV_PYTHON_PREFERENCE": "only-managed", "UV_NO_CONFIG": "1", "UV_LINK_MODE": "copy", "INVISIBLE_CORE_AUTOFIX": "0", "INVISIBLE_CORE_PIN": "", "INVISIBLE_PLAYWRIGHT_SKEW": "", "INVISIBLE_SEAL_FILE": "", "STEALTHFOX_GEOIP_MMDB": "", "PYTHONPATH": "", "PYTHONHOME": "", "PYTHONDONTWRITEBYTECODE": "1"})
}
func installEnv(dir, tmp string) []string {
	return mergeEnv(privateEnv(dir), map[string]string{"UV_CACHE_DIR": filepath.Join(tmp, "uv-cache"), "TMPDIR": tmp, "TEMP": tmp, "TMP": tmp})
}
func mergeEnv(base []string, values map[string]string) []string {
	out := make([]string, 0, len(base)+len(values))
	for _, entry := range base {
		key, _, _ := strings.Cut(entry, "=")
		found := false
		for k := range values {
			if strings.EqualFold(key, k) {
				found = true
				break
			}
		}
		if !found {
			out = append(out, entry)
		}
	}
	for k, v := range values {
		out = append(out, k+"="+v)
	}
	return out
}
func run(ctx context.Context, exe string, args, env []string, dir string, out io.Writer) error {
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdout, cmd.Stderr = out, out
	prepareInstall(cmd)
	if e := cmd.Run(); e != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%s: %w", filepath.Base(exe), e)
	}
	return nil
}
func (i Installer) download(ctx context.Context, a Artifact, p string, limit int64) error {
	req, e := http.NewRequestWithContext(ctx, "GET", a.URL, nil)
	if e != nil {
		return e
	}
	req.Header.Set("User-Agent", "W5F solver installer")
	resp, e := i.Client.Do(req)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("download returned HTTP %d", resp.StatusCode)
	}
	f, e := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	h := sha256.New()
	n, e := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, limit+1))
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	if n > limit {
		return errors.New("download exceeds its size limit")
	}
	if !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), a.SHA256) {
		return errors.New("download SHA-256 mismatch; installation stopped")
	}
	return nil
}
func archivePath(root, name string, strip bool) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	if strings.HasPrefix(name, "/") || strings.Contains(name, ":") {
		return "", errors.New("unsafe archive path")
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." {
			return "", errors.New("archive path escapes its folder")
		}
	}
	if strip {
		_, name, _ = strings.Cut(name, "/")
	}
	if name == "" || name == "." {
		return "", nil
	}
	p := filepath.Join(root, filepath.FromSlash(name))
	if !inside(root, p) {
		return "", errors.New("unsafe archive path")
	}
	return p, nil
}
func extract(archive, root string, zipped, strip bool) error {
	if e := os.MkdirAll(root, 0700); e != nil {
		return e
	}
	write := func(name string, mode fs.FileMode, size int64, open func() (io.ReadCloser, error)) error {
		p, e := archivePath(root, name, strip)
		if e != nil || p == "" {
			return e
		}
		if mode&os.ModeSymlink != 0 {
			return errors.New("archive contains a symbolic link")
		}
		if mode.IsDir() {
			return os.MkdirAll(p, 0700)
		}
		if !mode.IsRegular() || size > 512<<20 {
			return errors.New("unsupported archive entry")
		}
		if e = os.MkdirAll(filepath.Dir(p), 0700); e != nil {
			return e
		}
		src, e := open()
		if e != nil {
			return e
		}
		defer src.Close()
		dst, e := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode.Perm()|0600)
		if e != nil {
			return e
		}
		_, e = io.Copy(dst, io.LimitReader(src, size))
		ce := dst.Close()
		if e != nil {
			return e
		}
		return ce
	}
	if zipped {
		z, e := zip.OpenReader(archive)
		if e != nil {
			return e
		}
		defer z.Close()
		for _, f := range z.File {
			if e = write(f.Name, f.Mode(), int64(f.UncompressedSize64), f.Open); e != nil {
				return e
			}
		}
		return nil
	}
	f, e := os.Open(archive)
	if e != nil {
		return e
	}
	defer f.Close()
	gz, e := gzip.NewReader(f)
	if e != nil {
		return e
	}
	defer gz.Close()
	r := tar.NewReader(gz)
	type linkEntry struct{ path, target string }
	var links []linkEntry
	for {
		h, e := r.Next()
		if e == io.EOF {
			for _, link := range links {
				target := filepath.Join(filepath.Dir(link.path), filepath.FromSlash(link.target))
				if filepath.IsAbs(link.target) || strings.Contains(link.target, ":") || !inside(root, target) {
					return errors.New("archive link escapes its folder")
				}
				st, e := os.Lstat(target)
				if e != nil || !st.Mode().IsRegular() {
					return errors.New("archive link does not name a regular extracted file")
				}
				if runtime.GOOS != "windows" {
					if e = os.Symlink(link.target, link.path); e != nil {
						return e
					}
				} else {
					src, e := os.Open(target)
					if e != nil {
						return e
					}
					dst, e := os.OpenFile(link.path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, st.Mode().Perm())
					if e != nil {
						src.Close()
						return e
					}
					_, e = io.Copy(dst, src)
					src.Close()
					ce := dst.Close()
					if e != nil {
						return e
					}
					if ce != nil {
						return ce
					}
				}
			}
			return nil
		}
		if e != nil {
			return e
		}
		if h.Typeflag == tar.TypeXGlobalHeader || h.Typeflag == tar.TypeXHeader {
			continue
		}
		if h.Typeflag == tar.TypeSymlink {
			p, e := archivePath(root, h.Name, strip)
			if e != nil {
				return e
			}
			if p != "" {
				links = append(links, linkEntry{p, h.Linkname})
			}
			continue
		}
		if h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeDir {
			return errors.New("unsupported tar archive entry")
		}
		mode := fs.FileMode(h.Mode)
		if h.Typeflag == tar.TypeDir {
			mode |= os.ModeDir
		}
		if e = write(h.Name, mode, h.Size, func() (io.ReadCloser, error) { return io.NopCloser(r), nil }); e != nil {
			return e
		}
	}
}
func inside(root, p string) bool {
	rel, e := filepath.Rel(root, p)
	return e == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
func writeManifest(dir, version string) error {
	m := Manifest{Format: 1, Version: version, Installed: time.Now().UTC()}
	e := filepath.WalkDir(dir, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if p == dir {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		if d.IsDir() {
			m.Directories = append(m.Directories, rel)
		} else {
			m.Files = append(m.Files, rel)
		}
		return nil
	})
	if e != nil {
		return e
	}
	m.Files = append(m.Files, manifestName)
	b, e := json.MarshalIndent(m, "", "  ")
	if e != nil {
		return e
	}
	return os.WriteFile(filepath.Join(dir, manifestName), append(b, '\n'), 0600)
}
func readManifest(dir string) (Manifest, error) {
	var m Manifest
	b, e := os.ReadFile(filepath.Join(dir, manifestName))
	if e != nil {
		return m, errors.New("this installation is manual; W5F will not remove it")
	}
	e = json.Unmarshal(b, &m)
	if e != nil || m.Format != 1 || filepath.Base(dir) != "byparr-v"+m.Version {
		return m, errors.New("invalid solver installation list")
	}
	return m, nil
}
func removeListed(dir string) error {
	m, e := readManifest(dir)
	if e != nil {
		return e
	}
	all := append(append([]string{}, m.Files...), m.Directories...)
	// Validate the entire list before removing anything; symlink parents never followed.
	for _, rel := range all {
		if filepath.IsAbs(rel) || !inside(dir, filepath.Join(dir, rel)) {
			return errors.New("installation list escapes its folder")
		}
		for p := filepath.Dir(filepath.Join(dir, rel)); p != dir; p = filepath.Dir(p) {
			st, e := os.Lstat(p)
			if e == nil && st.Mode()&os.ModeSymlink != 0 {
				return errors.New("installation list crosses a symbolic link")
			}
		}
	}
	st, e := os.Lstat(dir)
	if e != nil {
		return e
	}
	if st.Mode()&os.ModeSymlink != 0 {
		return errors.New("installation folder is a symbolic link")
	}
	// Keep the list until every other listed file was removed successfully.
	for _, rel := range m.Files {
		if rel == manifestName {
			continue
		}
		if e = os.Remove(filepath.Join(dir, rel)); e != nil && !errors.Is(e, os.ErrNotExist) {
			return e
		}
	}
	sort.Slice(m.Directories, func(i, j int) bool { return len(m.Directories[i]) > len(m.Directories[j]) })
	leftover := false
	for _, rel := range m.Directories {
		if e = os.Remove(filepath.Join(dir, rel)); e != nil && !errors.Is(e, os.ErrNotExist) {
			leftover = true
		}
	}
	if leftover {
		return errors.New("unlisted files were kept; installation list retained")
	}
	entries, e := os.ReadDir(dir)
	if e != nil {
		return e
	}
	for _, entry := range entries {
		if entry.Name() != manifestName {
			return errors.New("unlisted files were kept; installation list retained")
		}
	}
	if e = os.Remove(filepath.Join(dir, manifestName)); e != nil {
		return e
	}
	return os.Remove(dir)
}
