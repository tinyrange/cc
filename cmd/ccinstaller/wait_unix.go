//go:build !windows

package main

import (
	"os"
	"syscall"
	"time"
)

// waitForProcessExit waits for a process to exit (Unix implementation).
func waitForProcessExit(pid int) {
	for range 60 { // Max 30 seconds
		p, err := os.FindProcess(pid)
		if err != nil {
			return
		}

		// On Unix, FindProcess always succeeds, need to check if process exists
		// by sending signal 0
		if err := p.Signal(syscall.Signal(0)); err != nil {
			return // Process has exited
		}

		time.Sleep(500 * time.Millisecond)
	}
}
