package solver

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// macOS has no /proc: the kernel's process table (sysctl kern.proc) gives
// the same parent and start time.

const szomb = 5 // a zombie (SZOMB in <sys/proc.h>)

func procStamp(p *unix.KinfoProc) string {
	return fmt.Sprintf("%d.%06d", p.Proc.P_starttime.Sec, p.Proc.P_starttime.Usec)
}

// stamp is a process's start time: with its pid, it tells the process W5F
// started from a later one given the same pid.
func stamp(pid int) (string, error) {
	p, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil || int(p.Proc.P_pid) != pid || p.Proc.P_stat == szomb {
		return "", os.ErrNotExist
	}
	return procStamp(p), nil
}

// processTable is every process's parent and stamp.
func processTable() (parents map[int]int, ids map[int]string) {
	parents, ids = map[int]int{}, map[int]string{}
	all, _ := unix.SysctlKinfoProcSlice("kern.proc.all")
	for i := range all {
		pid := int(all[i].Proc.P_pid)
		parents[pid] = int(all[i].Eproc.Ppid)
		ids[pid] = procStamp(&all[i])
	}
	return parents, ids
}

// processOwner names what holds the helper's port; a Mac does not say
// without lsof, so it is only known not to be W5F's.
func processOwner(port int) string { return "external (not started by W5F)" }
