//go:build freebsd || netbsd || openbsd

package guestagent

import (
	"bytes"
	"os/exec"
	"strconv"
	"time"
)

const processFamilyPollInterval = 20 * time.Millisecond

func processSnapshot(token string) (map[int]int, map[int]struct{}) {
	table := make(map[int]int)
	tagged := make(map[int]struct{})
	output, err := exec.Command("/bin/ps", "axeww", "-o", "pid=", "-o", "ppid=", "-o", "command=").Output()
	if err != nil {
		return table, tagged
	}
	for _, line := range bytes.Split(output, []byte{'\n'}) {
		fields := bytes.Fields(line)
		if len(fields) < 2 {
			continue
		}
		pid, pidErr := strconv.Atoi(string(fields[0]))
		ppid, ppidErr := strconv.Atoi(string(fields[1]))
		if pidErr == nil && ppidErr == nil {
			table[pid] = ppid
			if token != "" && bytes.Contains(line, []byte(processFamilyEnvironmentName+"="+token)) {
				tagged[pid] = struct{}{}
			}
		}
	}
	return table, tagged
}
