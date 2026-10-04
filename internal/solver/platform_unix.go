//go:build !windows

package solver

import (
	"os/exec"
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
func terminate(pid int) error      { return signalTree(pid, syscall.SIGTERM) }
func forceTerminate(pid int) error { return signalTree(pid, syscall.SIGKILL) }

// Xvfb creates a separate process session. Its pid and creation identity are
// captured while it is still a child, so stopping the helper also stops its
// display without touching another browser or another helper's display.
func signalTree(pid int, signal syscall.Signal) error {
	parents, ids := processTable()
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
