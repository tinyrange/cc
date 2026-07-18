//go:build freebsd || netbsd || openbsd

package guestagent

import (
	"bytes"
	"os/exec"
	"strconv"
	"syscall"
)

// reapPlatformOrphans asks the native process table for zombies parented by
// PID 1, then waits only for their exact PIDs. The tracked set contains direct
// command children whose status remains owned by os/exec.
func reapPlatformOrphans(tracked map[int]struct{}) {
	output, err := exec.Command("/bin/ps", "-ax", "-o", "pid=", "-o", "ppid=", "-o", "stat=").Output()
	if err != nil {
		return
	}
	for _, line := range bytes.Split(output, []byte{'\n'}) {
		fields := bytes.Fields(line)
		if len(fields) < 3 || len(fields[2]) == 0 || fields[2][0] != 'Z' {
			continue
		}
		pid64, pidErr := strconv.ParseInt(string(fields[0]), 10, 32)
		ppid64, ppidErr := strconv.ParseInt(string(fields[1]), 10, 32)
		if pidErr != nil || ppidErr != nil || ppid64 != 1 {
			continue
		}
		pid := int(pid64)
		if _, ok := tracked[pid]; ok {
			continue
		}
		var status syscall.WaitStatus
		_, _ = syscall.Wait4(pid, &status, syscall.WNOHANG, nil)
	}
}
