//go:build !windows && !darwin

package solver

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// stamp is a process's start time, read from /proc: with its pid, it tells
// the process W5F started from a later one given the same pid.
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

// processTable is every process's parent and stamp, from /proc.
func processTable() (parents map[int]int, ids map[int]string) {
	parents, ids = map[int]int{}, map[int]string{}
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
	return parents, ids
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
