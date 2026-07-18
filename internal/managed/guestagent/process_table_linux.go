//go:build linux

package guestagent

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const processFamilyPollInterval = 2 * time.Millisecond

func processSnapshot(token string) (map[int]int, map[int]struct{}) {
	table := make(map[int]int)
	tagged := make(map[int]struct{})
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return table, tagged
	}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "stat"))
		if err != nil {
			continue
		}
		// comm may contain spaces and parentheses; fields following its final ')'
		// begin with state and ppid.
		closeParen := strings.LastIndexByte(string(data), ')')
		if closeParen < 0 {
			continue
		}
		fields := strings.Fields(string(data[closeParen+1:]))
		if len(fields) < 2 {
			continue
		}
		ppid, err := strconv.Atoi(fields[1])
		if err == nil {
			table[pid] = ppid
		}
		if token != "" {
			environ, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "environ"))
			marker := []byte(processFamilyEnvironmentName + "=" + token)
			if err == nil && bytes.Contains(append(environ, 0), append(marker, 0)) {
				tagged[pid] = struct{}{}
			}
		}
	}
	return table, tagged
}
