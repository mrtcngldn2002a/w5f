//go:build windows

package suwayomi

import (
	"os"
	"os/exec"
	"syscall"
)

const createNewProcessGroup = 0x00000200

func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup}
}

func terminate(p *os.Process) error { return p.Kill() }

// alive reports a running process (Windows: one that can be opened).
func alive(pid int) bool {
	h, err := syscall.OpenProcess(syscall.PROCESS_QUERY_INFORMATION|syscall.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(h)
	ev, err := syscall.WaitForSingleObject(h, 0)
	return err == nil && ev == syscall.WAIT_TIMEOUT
}

// strays is not looked for on Windows (the laptop runs Linux).
func (s Server) strays() []int { return nil }
