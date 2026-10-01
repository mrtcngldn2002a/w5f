//go:build !windows

package browser

import (
	"os/exec"
	"syscall"
)

// detach starts Chromium in its own session, so closing W5F leaves it open.
func detach(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }
