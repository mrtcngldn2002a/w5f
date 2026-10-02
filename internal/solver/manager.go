package solver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"w5f/internal/config"
	"w5f/internal/fetch"
	"w5f/internal/store"
)

type processRecord struct {
	PID        int
	Stamp, Dir string
	Started    time.Time
}
type Status struct {
	Version, Dir, Helper, Owner, Problem, Update string
	Running, Managed                             bool
	Bytes                                        int64
}
type Manager struct {
	DataDir, URL string
	mu           sync.Mutex
	started      map[string]processRecord
}

var defaultMu sync.Mutex
var defaultManager *Manager

// AutoStart is enabled by the interactive reader and dump, both of which
// stop their own processes on exit. Administrative one-shot commands do not
// silently leave a helper running; solver start is an explicit exception.
var AutoStart bool

func Default() *Manager {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	data := store.DataDir()
	if defaultManager == nil || defaultManager.DataDir != data {
		defaultManager = &Manager{DataDir: data, URL: config.SolverURL()}
	}
	return defaultManager
}
func Ensure(ctx context.Context, raw string) error {
	if !AutoStart {
		_, e := fetch.ProbeSolver(ctx, raw)
		return e
	}
	m := Default()
	_, e := m.Start(ctx, raw)
	return e
}
func Shutdown() {
	finishInstallations()
	defaultMu.Lock()
	m := defaultManager
	defaultMu.Unlock()
	if m != nil {
		m.Shutdown()
	}
}
func endpoint(raw string) (string, error) {
	u, e := url.Parse(raw)
	if e != nil || u == nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" && u.Path != "/" {
		return "", errors.New("Byparr starts only at a local HTTP address")
	}
	if u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost" {
		return "", errors.New("Byparr listens only on 127.0.0.1")
	}
	port := u.Port()
	n, e := strconv.Atoi(port)
	if e != nil || n < 1 || n > 65535 {
		return "", errors.New("invalid Byparr port")
	}
	return port, nil
}
func (m *Manager) recordPath(raw string) string {
	p, e := endpoint(raw)
	if e != nil {
		return ""
	}
	return filepath.Join(m.DataDir, "solver", "process-"+p+".json")
}
func (m *Manager) record(raw string) (processRecord, bool) {
	var r processRecord
	b, e := os.ReadFile(m.recordPath(raw))
	if e != nil || json.Unmarshal(b, &r) != nil || r.PID <= 0 || !inside(m.DataDir, r.Dir) {
		return r, false
	}
	s, e := stamp(r.PID)
	return r, e == nil && s == r.Stamp
}
func (m *Manager) active() string {
	b, _ := os.ReadFile(filepath.Join(m.DataDir, "solver", "active"))
	name := strings.TrimSpace(string(b))
	if name == "none" {
		return ""
	}
	if strings.HasPrefix(name, "byparr-v") && !strings.ContainsAny(name, "/\\") {
		p := filepath.Join(m.DataDir, name)
		if present(p) {
			return p
		}
	}
	dirs, _ := filepath.Glob(filepath.Join(m.DataDir, "byparr-v*"))
	sort.Slice(dirs, func(i, j int) bool { return versionLess(filepath.Base(dirs[i]), filepath.Base(dirs[j])) })
	for i := len(dirs) - 1; i >= 0; i-- {
		if present(dirs[i]) {
			return dirs[i]
		}
	}
	return ""
}
func versionLess(a, b string) bool {
	aa := strings.Split(strings.TrimPrefix(a, "byparr-v"), ".")
	bb := strings.Split(strings.TrimPrefix(b, "byparr-v"), ".")
	for k := 0; k < 3; k++ {
		av, bv := 0, 0
		if k < len(aa) {
			av, _ = strconv.Atoi(aa[k])
		}
		if k < len(bb) {
			bv, _ = strconv.Atoi(bb[k])
		}
		if av != bv {
			return av < bv
		}
	}
	return a < b
}
func (m *Manager) Status(ctx context.Context) Status {
	s := Status{Dir: m.active()}
	if s.Dir != "" {
		s.Version = strings.TrimPrefix(filepath.Base(s.Dir), "byparr-v")
		_, e := readManifest(s.Dir)
		s.Managed = e == nil
		filepath.WalkDir(s.Dir, func(p string, d fs.DirEntry, e error) error {
			if e == nil && !d.IsDir() {
				if info, e := d.Info(); e == nil {
					s.Bytes += info.Size()
				}
			}
			return nil
		})
		if versionLess("byparr-v"+s.Version, "byparr-v"+Version) {
			s.Update = "Byparr " + s.Version + " → " + Version + ": w5f solver update"
		}
	}
	if m.URL == "" {
		s.Problem = "helper is off ([fetch] solver_url is empty)"
		return s
	}
	name, e := fetch.ProbeSolver(ctx, m.URL)
	if e != nil {
		s.Problem = e.Error()
		return s
	}
	s.Helper, s.Running = name, true
	port := 8191
	if u, e := url.Parse(m.URL); e == nil {
		if n, e := strconv.Atoi(u.Port()); e == nil {
			port = n
		}
	}
	s.Owner = processOwner(port)
	if _, ok := m.record(m.URL); ok {
		s.Owner = "W5F"
	}
	return s
}
func (m *Manager) env(dir, port string) []string {
	env := os.Environ()
	if _, e := readManifest(dir); e == nil {
		env = privateEnv(dir)
	}
	path := os.Getenv("PATH")
	wrapper := filepath.Join(m.DataDir, "cloudflare-helper", "bin")
	if st, e := os.Stat(wrapper); e == nil && st.IsDir() {
		path = wrapper + string(os.PathListSeparator) + path
	}
	safeX := filepath.Join(m.DataDir, "solver", "bin")
	if _, e := os.Stat(filepath.Join(safeX, "Xvfb")); e == nil {
		path = safeX + string(os.PathListSeparator) + path
	}
	return mergeEnv(env, map[string]string{"HOST": "127.0.0.1", "PORT": port, "BLOCK_MEDIA": "true", "LOG_LEVEL": "WARNING", "VERSION": strings.TrimPrefix(filepath.Base(dir), "byparr-v"), "PATH": path, "PYTHONDONTWRITEBYTECODE": "1"})
}
func XvfbHint(data string) string {
	if runtime.GOOS != "linux" {
		return ""
	}
	if _, e := exec.LookPath("Xvfb"); e == nil {
		return ""
	}
	if _, e := os.Stat(filepath.Join(data, "cloudflare-helper", "bin", "Xvfb")); e == nil {
		return ""
	}
	for _, x := range []struct{ cmd, hint string }{{"apt-get", "sudo apt install xvfb"}, {"dnf", "sudo dnf install xorg-x11-server-Xvfb"}, {"pacman", "sudo pacman -S xorg-server-xvfb"}} {
		if _, e := exec.LookPath(x.cmd); e == nil {
			return "Xvfb is needed: " + x.hint
		}
	}
	return "Xvfb is needed: install it with your system's package manager"
}

// Start reuses external helpers. Only an unoccupied, configured loopback
// endpoint can get a W5F-owned process and a validated process record.
func (m *Manager) Start(ctx context.Context, raw string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, e := cleanData(m.DataDir)
	if e != nil {
		return false, e
	}
	m.DataDir = data
	if raw == "" {
		return false, nil
	}
	if _, e := fetch.ProbeSolver(ctx, raw); e == nil {
		return false, nil
	} else if !fetch.NoSolver(e) {
		return false, e
	} else if m.active() == "" {
		return false, e
	}
	port, e := endpoint(raw)
	if e != nil {
		return false, e
	}
	dir := m.active()
	if hint := XvfbHint(m.DataDir); hint != "" {
		return false, errors.New(hint)
	}
	listener, e := net.Listen("tcp", "127.0.0.1:"+port)
	if e != nil {
		return false, fmt.Errorf("solver port is occupied; no helper started: %w", e)
	}
	listener.Close()
	state := filepath.Join(m.DataDir, "solver")
	if e = os.MkdirAll(state, 0700); e != nil {
		return false, e
	}
	if e = m.prepareXvfb(dir); e != nil {
		return false, e
	}
	lock := filepath.Join(state, "start-"+port+".lock")
	lf, e := os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return false, errors.New("another W5F is starting Byparr; try again shortly")
	}
	lf.Close()
	defer os.Remove(lock)
	if _, e = fetch.ProbeSolver(ctx, raw); e == nil {
		return false, nil
	} else if !fetch.NoSolver(e) {
		return false, e
	}
	log, e := os.OpenFile(filepath.Join(state, "byparr-"+port+".log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		return false, e
	}
	cmd := exec.Command(Python(dir, runtime.GOOS), filepath.Join(dir, "main.py"))
	cmd.Dir = dir
	cmd.Env = m.env(dir, port)
	cmd.Stdout, cmd.Stderr = log, log
	detach(cmd)
	e = cmd.Start()
	log.Close()
	if e != nil {
		return false, e
	}
	go cmd.Wait()
	identity, e := stamp(cmd.Process.Pid)
	if e != nil {
		forceTerminate(cmd.Process.Pid)
		return false, e
	}
	r := processRecord{PID: cmd.Process.Pid, Stamp: identity, Dir: dir, Started: time.Now()}
	b, _ := json.Marshal(r)
	if e = os.WriteFile(m.recordPath(raw), b, 0600); e != nil {
		forceTerminate(r.PID)
		return false, e
	}
	if m.started == nil {
		m.started = map[string]processRecord{}
	}
	m.started[raw] = r
	waitCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	for {
		if _, e = fetch.ProbeSolver(waitCtx, raw); e == nil {
			return true, nil
		}
		if _, ok := m.record(raw); !ok {
			m.stopLocked(raw)
			return false, fmt.Errorf("Byparr exited; see %s", filepath.Join(state, "byparr-"+port+".log"))
		}
		select {
		case <-waitCtx.Done():
			m.stopLocked(raw)
			return false, waitCtx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}
func (m *Manager) stopLocked(raw string) error {
	r, ok := m.record(raw)
	if !ok {
		if p := m.recordPath(raw); p != "" {
			os.Remove(p)
		}
		delete(m.started, raw)
		return nil
	}
	if e := terminate(r.PID); e != nil {
		return e
	}
	for range 30 {
		if _, ok = m.record(raw); !ok {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if _, ok = m.record(raw); ok {
		if e := forceTerminate(r.PID); e != nil {
			return e
		}
	}
	os.Remove(m.recordPath(raw))
	delete(m.started, raw)
	return nil
}
func (m *Manager) Stop() error { m.mu.Lock(); defer m.mu.Unlock(); return m.stopLocked(m.URL) }
func (m *Manager) Shutdown() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for raw, own := range m.started {
		if current, ok := m.record(raw); ok && current.PID == own.PID && current.Stamp == own.Stamp {
			_ = m.stopLocked(raw)
		} else if s, e := stamp(own.PID); e == nil && s == own.Stamp {
			_ = terminate(own.PID)
		}
		delete(m.started, raw)
	}
}
func (m *Manager) Remove() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, e := cleanData(m.DataDir)
	if e != nil {
		return e
	}
	m.DataDir = data
	dir := m.active()
	if dir == "" {
		return errors.New("Byparr is not installed")
	}
	if _, e := readManifest(dir); e != nil {
		return e
	}
	if _, ok := m.record(m.URL); ok {
		if e := m.stopLocked(m.URL); e != nil {
			return e
		}
	} else if _, e := fetch.ProbeSolver(context.Background(), m.URL); e == nil {
		return errors.New("an external helper is running; its installation is not removed")
	}
	if e := removeListed(dir); e != nil {
		return e
	}
	os.Remove(filepath.Join(m.DataDir, "solver", "active"))
	return nil
}
func (m *Manager) Update(ctx context.Context, out *Installer) error {
	data, e := cleanData(m.DataDir)
	if e != nil {
		return e
	}
	m.DataDir = data
	old := m.active()
	if old != "" && !versionLess(filepath.Base(old), "byparr-v"+Version) {
		return nil
	}
	state := filepath.Join(m.DataDir, "solver")
	if e := os.MkdirAll(state, 0700); e != nil {
		return e
	}
	lock := filepath.Join(state, "update.lock")
	lf, e := os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return errors.New("another solver update is running")
	}
	lf.Close()
	defer os.Remove(lock)
	previous := "none"
	if old != "" {
		previous = filepath.Base(old)
	}
	if e = writeSelection(state, previous); e != nil {
		return e
	}
	i := Installer{DataDir: m.DataDir}
	if out != nil {
		i = *out
		i.DataDir = m.DataDir
	}
	dir, e := i.Install(ctx)
	if e != nil {
		return e
	}
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		return e
	}
	probeURL := "http://" + listener.Addr().String()
	listener.Close()
	// The candidate is selected explicitly, while the regular manager keeps
	// its previous active version until the isolated readiness check succeeds.
	oldActive := filepath.Join(m.DataDir, "solver", "active")
	candidate := &Manager{DataDir: m.DataDir, URL: probeURL}
	// StartAt avoids changing the shared active selection during the probe.
	e = candidate.testCandidate(ctx, dir)
	if e != nil {
		return e
	}
	if e = os.MkdirAll(filepath.Dir(oldActive), 0700); e != nil {
		return e
	}
	if e = writeSelection(state, filepath.Base(dir)); e != nil {
		return e
	}
	// One previous owned version is kept for rollback. Manual installations
	// and any versions that might be used by an external service are retained.
	if old != "" {
		if _, e = fetch.ProbeSolver(ctx, m.URL); fetch.NoSolver(e) {
			dirs, _ := filepath.Glob(filepath.Join(m.DataDir, "byparr-v*"))
			for _, p := range dirs {
				if p != dir && p != old && versionLess(filepath.Base(p), filepath.Base(old)) {
					if _, e := readManifest(p); e == nil {
						_ = removeListed(p)
					}
				}
			}
		}
	}
	return nil
}
func writeSelection(state, name string) error {
	f, e := os.CreateTemp(state, ".active-")
	if e != nil {
		return e
	}
	p := f.Name()
	defer os.Remove(p)
	if _, e = f.WriteString(name); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	return os.Rename(p, filepath.Join(state, "active"))
}
func (m *Manager) testCandidate(ctx context.Context, dir string) error {
	// Probe in a separate process and port, without changing active selection.
	tmp, e := os.MkdirTemp(m.DataDir, ".solver-probe-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(tmp)
	if e = m.prepareXvfb(dir); e != nil {
		return e
	}
	port, _ := endpoint(m.URL)
	l, e := net.Listen("tcp", "127.0.0.1:"+port)
	if e != nil {
		return e
	}
	l.Close()
	log, e := os.Create(filepath.Join(tmp, "probe.log"))
	if e != nil {
		return e
	}
	defer log.Close()
	cmd := exec.Command(Python(dir, runtime.GOOS), filepath.Join(dir, "main.py"))
	cmd.Dir = dir
	cmd.Env = m.env(dir, port)
	cmd.Stdout, cmd.Stderr = log, log
	detach(cmd)
	if e = cmd.Start(); e != nil {
		return e
	}
	go cmd.Wait()
	identity, e := stamp(cmd.Process.Pid)
	if e != nil {
		return e
	}
	defer func() {
		if s, e := stamp(cmd.Process.Pid); e == nil && s == identity {
			_ = forceTerminate(cmd.Process.Pid)
		}
	}()
	wait, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	for {
		if s, e := stamp(cmd.Process.Pid); e != nil || s != identity {
			return errors.New("candidate Byparr exited during its readiness check")
		}
		if _, e = fetch.ProbeSolver(wait, m.URL); e == nil {
			return nil
		}
		select {
		case <-wait.Done():
			return wait.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
func (m *Manager) prepareXvfb(dir string) error {
	if runtime.GOOS != "linux" {
		return nil
	}
	// Preserve the laptop's existing private Xvfb wrapper exactly as it is.
	if _, e := os.Stat(filepath.Join(m.DataDir, "cloudflare-helper", "bin", "Xvfb")); e == nil {
		return nil
	}
	real, e := exec.LookPath("Xvfb")
	if e != nil {
		return errors.New(XvfbHint(m.DataDir))
	}
	dest := filepath.Join(m.DataDir, "solver", "bin", "Xvfb")
	if filepath.Clean(real) == filepath.Clean(dest) {
		return nil
	}
	if b, e := os.ReadFile(dest); e == nil && !strings.HasPrefix(string(b), "#!/bin/sh\n# W5F managed Xvfb launcher\n") {
		return errors.New("existing Xvfb launcher is not W5F-owned; kept unchanged")
	}
	code := `import os,sys
args=sys.argv[1:]; out=[]; i=0
while i<len(args):
 if args[i:i+2]==['-nolisten','unix']: i+=2; continue
 if args[i:i+2]==['-listen','tcp']: out+=['-nolisten','tcp']; i+=2; continue
 out.append(args[i]); i+=1
binary=` + fmt.Sprintf("%q", real) + `
os.execv(binary,[binary]+out)`
	body := "#!/bin/sh\n# W5F managed Xvfb launcher\nexec " + shellQuote(Python(dir, runtime.GOOS)) + " -c " + shellQuote(code) + " \"$@\"\n"
	if e = os.MkdirAll(filepath.Dir(dest), 0700); e != nil {
		return e
	}
	return os.WriteFile(dest, []byte(body), 0700)
}
