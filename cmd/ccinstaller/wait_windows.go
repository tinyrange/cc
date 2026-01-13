//go:build windows

package main

import (
	"golang.org/x/sys/windows"
)

// waitForProcessExit waits for a process to exit (Windows implementation).
func waitForProcessExit(pid int) {
	// Open the process with SYNCHRONIZE permission
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		// Process likely already exited or doesn't exist
		return
	}
	defer windows.CloseHandle(handle)

	// Wait for the process to exit (30 second timeout)
	windows.WaitForSingleObject(handle, 30000)
}
