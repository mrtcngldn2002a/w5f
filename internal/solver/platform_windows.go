//go:build windows

package solver

import (
	"fmt"
	"os/exec"
	"strconv"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

func freeBytes(path string) (uint64, error) {
	p, e := windows.UTF16PtrFromString(path)
	if e != nil {
		return 0, e
	}
	var free, total, available uint64
	e = windows.GetDiskFreeSpaceEx(p, &available, &total, &free)
	return available, e
}
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000 | 0x00000200, HideWindow: true}
}
func prepareInstall(cmd *exec.Cmd) {
	detach(cmd)
	cmd.Cancel = func() error { return forceTerminate(cmd.Process.Pid) }
	cmd.WaitDelay = 3 * time.Second
}
func stamp(pid int) (string, error) {
	h, e := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, uint32(pid))
	if e != nil {
		return "", e
	}
	defer windows.CloseHandle(h)
	event, e := windows.WaitForSingleObject(h, 0)
	if e != nil || event != uint32(windows.WAIT_TIMEOUT) {
		return "", fmt.Errorf("process is not running")
	}
	var created, exit, kernel, user windows.Filetime
	e = windows.GetProcessTimes(h, &created, &exit, &kernel, &user)
	return fmt.Sprintf("%d:%d", created.HighDateTime, created.LowDateTime), e
}
func terminate(pid int) error { return forceTerminate(pid) }
func forceTerminate(pid int) error {
	cmd := exec.Command("taskkill.exe", "/PID", strconv.Itoa(pid), "/T", "/F")
	detach(cmd)
	return cmd.Run()
}
func processOwner(port int) string { return "external (not started by W5F)" }
