//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows"
)

// enableVTProcessing enables Windows Virtual Terminal (VT) processing for
// proper ANSI escape code handling in interactive mode.
func enableVTProcessing() (restore func(), err error) {
	stdin := windows.Handle(os.Stdin.Fd())
	stdout := windows.Handle(os.Stdout.Fd())

	var originalInMode, originalOutMode uint32

	// Get current console modes
	if err := windows.GetConsoleMode(stdin, &originalInMode); err != nil {
		return nil, err
	}
	if err := windows.GetConsoleMode(stdout, &originalOutMode); err != nil {
		return nil, err
	}

	// Enable VT processing on stdout (for escape codes)
	// ENABLE_VIRTUAL_TERMINAL_PROCESSING = 0x0004
	newOutMode := originalOutMode | 0x0004
	if err := windows.SetConsoleMode(stdout, newOutMode); err != nil {
		return nil, err
	}

	// Enable VT input on stdin
	// ENABLE_VIRTUAL_TERMINAL_INPUT = 0x0200
	newInMode := originalInMode | 0x0200
	if err := windows.SetConsoleMode(stdin, newInMode); err != nil {
		// Restore stdout mode on error
		windows.SetConsoleMode(stdout, originalOutMode)
		return nil, err
	}

	// Return a function to restore original modes
	restore = func() {
		windows.SetConsoleMode(stdin, originalInMode)
		windows.SetConsoleMode(stdout, originalOutMode)
	}

	return restore, nil
}
