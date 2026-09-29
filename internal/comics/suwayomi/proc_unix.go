//go:build !windows

package suwayomi

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
)

// detach keeps the server running on its own (it is stopped by Stop).
func detach(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }

// terminate asks the JVM to shut down cleanly.
func terminate(p *os.Process) error { return p.Signal(syscall.SIGTERM) }

// alive reports a running process.
func alive(pid int) bool {
	p, err := os.FindProcess(pid)
	return err == nil && p.Signal(syscall.Signal(0)) == nil
}

// strays finds Suwayomi processes running on this server's data folder
// (Linux /proc), e.g. one left without a pid file.
func (s Server) strays() []int {
	mark := []byte("suwayomi.tachidesk.config.server.rootDir=" + filepath.Join(s.Dir, "data"))
	ents, _ := os.ReadDir("/proc")
	var out []int
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == os.Getpid() {
			continue
		}
		cmd, err := os.ReadFile(filepath.Join("/proc", e.Name(), "cmdline"))
		if err == nil && bytes.Contains(cmd, mark) && bytes.Contains(cmd, []byte("-jar")) {
			out = append(out, pid)
		}
	}
	return out
}
