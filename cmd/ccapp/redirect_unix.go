//go:build !windows

package main

import (
	"os"

	"golang.org/x/sys/unix"
)

func redirectStdoutStderrToFile(f *os.File) bool {
	if f == nil {
		return false
	}
	if err := unix.Dup2(int(f.Fd()), 1); err != nil {
		return false
	}
	if err := unix.Dup2(int(f.Fd()), 2); err != nil {
		return false
	}
	return true
}
