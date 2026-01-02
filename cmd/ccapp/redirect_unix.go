//go:build !windows

package main

import (
	"os"
	"syscall"
)

func redirectStdoutStderrToFile(f *os.File) bool {
	if f == nil {
		return false
	}
	if err := syscall.Dup2(int(f.Fd()), 1); err != nil {
		return false
	}
	if err := syscall.Dup2(int(f.Fd()), 2); err != nil {
		return false
	}
	return true
}
