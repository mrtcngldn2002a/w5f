package suwayomi

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Server is a Suwayomi-Server that W5F starts on demand: headless, local
// only, CBZ downloads, with JVM settings measured on the W5F laptop
// (2026-09-30: 352 MB idle, 13 s start, vs 390 MB and 23 s by default).
type Server struct {
	Dir       string // holds Suwayomi-Server-*.jar, data/, server.log, pid
	Java      string // "" = java from PATH
	Heap      string // "" = 256m
	Port      int    // 0 = 4567
	Downloads string // CBZ downloads
	Local     string // the Local source folder (the user's own files)
}

func (s Server) port() int {
	if s.Port > 0 {
		return s.Port
	}
	return 4567
}

// Addr is the server's address (local only).
func (s Server) Addr() string { return fmt.Sprintf("http://127.0.0.1:%d", s.port()) }

// Jar is the newest Suwayomi-Server jar in Dir ("" when none).
func (s Server) Jar() string {
	jars, _ := filepath.Glob(filepath.Join(s.Dir, "Suwayomi-Server-*.jar"))
	sort.Strings(jars)
	if len(jars) == 0 {
		return ""
	}
	return jars[len(jars)-1]
}

// Args are the java arguments that start the server.
func (s Server) Args() []string {
	heap := s.Heap
	if heap == "" {
		heap = "256m"
	}
	const c = "-Dsuwayomi.tachidesk.config.server."
	return []string{
		"-Xmx" + heap, "-XX:+UseSerialGC", "-XX:TieredStopAtLevel=1", "-XX:ReservedCodeCacheSize=48m",
		"-XX:MaxMetaspaceSize=160m", "-Xss512k", "-XX:CICompilerCount=1", "-XX:+UseStringDeduplication",
		c + "rootDir=" + filepath.Join(s.Dir, "data"),
		c + "ip=127.0.0.1", c + "port=" + strconv.Itoa(s.port()),
		c + "webUIEnabled=false", c + "initialOpenInBrowserEnabled=false", c + "systemTrayEnabled=false",
		c + "kcefEnabled=false", c + "downloadAsCbz=true",
		c + "downloadsPath=" + s.Downloads, c + "localSourcePath=" + s.Local,
		c + "updateMangas=false", c + "maxSourcesInParallel=2",
		"-jar", s.Jar(),
	}
}

// startWait is how long a start may take (about 13 s on the W5F laptop).
func (s Server) startWait() time.Duration { return 90 * time.Second }

func (s Server) pidFile() string { return filepath.Join(s.Dir, "w5f.pid") }

// Running reports whether a server answers (one that asks W5F to sign in
// answers too: it must not be started a second time).
func (s Server) Running(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	_, err := s.Client().Version(ctx)
	return err == nil || errors.Is(err, ErrUnauthorized)
}

// Start launches the server unless one already answers, and waits until it
// does (up to wait). It reports whether this call started it.
func (s Server) Start(ctx context.Context, wait time.Duration) (bool, error) {
	if s.Running(ctx) {
		return false, nil
	}
	// A server launched earlier may still be starting (or stopping), or be
	// left over without a pid file: wait for it rather than start a second
	// one on the same database.
	pid, ok := s.pid()
	if !ok || !alive(pid) {
		if stray := s.strays(); len(stray) > 0 {
			pid, ok = stray[0], true
			_ = os.WriteFile(s.pidFile(), []byte(strconv.Itoa(pid)), 0o644)
		}
	}
	if ok && alive(pid) {
		deadline := time.Now().Add(wait)
		for time.Now().Before(deadline) && alive(pid) {
			if s.Running(ctx) {
				return false, nil
			}
			select {
			case <-ctx.Done():
				return false, ctx.Err()
			case <-time.After(time.Second):
			}
		}
		if alive(pid) {
			return false, fmt.Errorf("Suwayomi (pid %d) did not answer within %s", pid, wait)
		}
	}
	jar := s.Jar()
	if jar == "" {
		return false, fmt.Errorf("no Suwayomi-Server jar in %s (w5f comics server install)", s.Dir)
	}
	java := s.Java
	if java == "" {
		var err error
		if java, err = exec.LookPath("java"); err != nil {
			return false, errors.New("Java is not installed (Suwayomi needs Java 21: apt install openjdk-21-jre-headless)")
		}
	}
	for _, d := range []string{s.Downloads, s.Local, filepath.Join(s.Dir, "data")} {
		if d != "" {
			_ = os.MkdirAll(d, 0o755)
		}
	}
	log, err := os.OpenFile(filepath.Join(s.Dir, "server.log"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return false, err
	}
	cmd := exec.Command(java, s.Args()...)
	cmd.Stdout, cmd.Stderr = log, log
	cmd.Dir = s.Dir
	detach(cmd)
	if err := cmd.Start(); err != nil {
		log.Close()
		return false, err
	}
	log.Close()
	_ = os.WriteFile(s.pidFile(), []byte(strconv.Itoa(cmd.Process.Pid)), 0o644)
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		select {
		case err := <-exited:
			_ = os.Remove(s.pidFile())
			return false, fmt.Errorf("Suwayomi stopped while starting (%v); see %s", err, filepath.Join(s.Dir, "server.log"))
		case <-ctx.Done():
			return true, ctx.Err()
		case <-time.After(time.Second):
		}
		if s.Running(ctx) {
			return true, nil
		}
	}
	return true, fmt.Errorf("Suwayomi did not answer within %s; see %s", wait, filepath.Join(s.Dir, "server.log"))
}

// ours reports a server on W5F's data folder (started by W5F, or left over
// on its folder), which W5F may stop and start.
func (s Server) ours() bool {
	if pid, ok := s.pid(); ok && alive(pid) {
		return true
	}
	return len(s.strays()) > 0
}

func (s Server) pid() (int, bool) {
	b, err := os.ReadFile(s.pidFile())
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	return pid, err == nil && pid > 0
}

// Stop ends a server that W5F started (the one in the pid file, or one left
// over on this data folder) and waits until it has exited, so a new start
// does not meet its database.
func (s Server) Stop() error {
	pid, ok := s.pid()
	if !ok || !alive(pid) {
		stray := s.strays()
		if len(stray) == 0 {
			_ = os.Remove(s.pidFile())
			return errors.New("no server started by W5F")
		}
		pid = stray[0]
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		_ = os.Remove(s.pidFile())
		return err
	}
	if err := terminate(p); err != nil && !errors.Is(err, os.ErrProcessDone) {
		_ = os.Remove(s.pidFile())
		return err
	}
	for i := 0; i < 150 && alive(pid); i++ {
		time.Sleep(100 * time.Millisecond)
	}
	if alive(pid) {
		_ = p.Kill()
	}
	_ = os.Remove(s.pidFile())
	if len(s.strays()) > 0 {
		return s.Stop()
	}
	return nil
}
