//go:build windows

package main

import "os"

func redirectStdoutStderrToFile(f *os.File) bool {
	if f == nil {
		return false
	}
	os.Stdout = f
	os.Stderr = f
	return true
}
