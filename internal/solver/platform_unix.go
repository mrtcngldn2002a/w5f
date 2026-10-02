//go:build !windows

package solver

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func freeBytes(path string) (uint64, error) {
	var s syscall.Statfs_t
	e := syscall.Statfs(path, &s)
	return uint64(s.Bavail) * uint64(s.Bsize), e
}
func detach(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }
func prepareInstall(cmd *exec.Cmd) {
	detach(cmd)
	cmd.Cancel = func() error { return forceTerminate(cmd.Process.Pid) }
	cmd.WaitDelay = 3 * time.Second
}
func stamp(pid int) (string, error) {
	b, e := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if e != nil {
		return "", e
	}
	_, rest, ok := strings.Cut(string(b), ") ")
	fields := strings.Fields(rest)
	if !ok || len(fields) < 20 || fields[0] == "Z" {
		return "", os.ErrNotExist
	}
	return fields[19], nil
}
func terminate(pid int) error      { return signalTree(pid, syscall.SIGTERM) }
func forceTerminate(pid int) error { return signalTree(pid, syscall.SIGKILL) }

// Xvfb creates a separate process session. Its pid and creation identity are
// captured while it is still a child, so stopping the helper also stops its
// display without touching another browser or another helper's display.
func signalTree(pid int, signal syscall.Signal) error {
	parents := map[int]int{}
	ids := map[int]string{}
	entries, _ := os.ReadDir("/proc")
	for _, entry := range entries {
		child, e := strconv.Atoi(entry.Name())
		if e != nil {
			continue
		}
		b, _ := os.ReadFile(filepath.Join("/proc", entry.Name(), "stat"))
		_, rest, _ := strings.Cut(string(b), ") ")
		fields := strings.Fields(rest)
		if len(fields) < 20 {
			continue
		}
		parent, _ := strconv.Atoi(fields[1])
		parents[child] = parent
		ids[child] = fields[19]
	}
	children := map[int]string{}
	for child, identity := range ids {
		for p, depth := child, 0; p > 1 && depth < 30; depth++ {
			if parents[p] == pid {
				children[child] = identity
				break
			}
			next := parents[p]
			if next == p {
				break
			}
			p = next
		}
	}
	e := syscall.Kill(-pid, signal)
	for child, identity := range children {
		if current, err := stamp(child); err == nil && current == identity {
			_ = syscall.Kill(child, signal)
		}
	}
	return e
}
func processOwner(port int) string {
	// Resolve the listening socket's process and its actual supervisor. An
	// unrelated s6 service must not label helpers on other ports as s6-owned.
	inodes := map[string]bool{}
	for _, table := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		b, _ := os.ReadFile(table)
		for _, line := range strings.Split(string(b), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 10 || fields[3] != "0A" {
				continue
			}
			_, p, _ := strings.Cut(fields[1], ":")
			n, e := strconv.ParseInt(p, 16, 32)
			if e == nil && int(n) == port {
				inodes["socket:["+fields[9]+"]"] = true
			}
		}
	}
	entries, _ := os.ReadDir("/proc")
	for _, entry := range entries {
		pid, e := strconv.Atoi(entry.Name())
		if e != nil {
			continue
		}
		fds, _ := os.ReadDir(filepath.Join("/proc", entry.Name(), "fd"))
		found := false
		for _, fd := range fds {
			target, _ := os.Readlink(filepath.Join("/proc", entry.Name(), "fd", fd.Name()))
			if inodes[target] {
				found = true
				break
			}
		}
		if !found {
			continue
		}
		for parent, depth := pid, 0; parent > 1 && depth < 20; depth++ {
			b, _ := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", parent))
			if strings.Contains(string(b), "s6-supervise") {
				return "s6 service"
			}
			stat, _ := os.ReadFile(fmt.Sprintf("/proc/%d/stat", parent))
			_, rest, _ := strings.Cut(string(stat), ") ")
			fields := strings.Fields(rest)
			if len(fields) < 2 {
				break
			}
			parent, _ = strconv.Atoi(fields[1])
		}
		return fmt.Sprintf("external process (pid %d)", pid)
	}
	return "external (not started by W5F)"
}
