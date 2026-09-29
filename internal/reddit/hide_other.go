//go:build !windows

package reddit

import "os/exec"

func hideWindow(*exec.Cmd) {}
