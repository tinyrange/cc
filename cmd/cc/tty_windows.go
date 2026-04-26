//go:build windows

package main

func isTerminalFD(int) bool {
	return false
}
