//go:build windows

package reddit

import (
	"os/exec"
	"syscall"
)

// hideWindow keeps the Redlib console window from popping up on Windows.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}
