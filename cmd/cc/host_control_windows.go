//go:build windows

package main

import (
	"fmt"
	"os"
)

type unsupportedSignal string

func (s unsupportedSignal) Signal() {}
func (s unsupportedSignal) String() string {
	return string(s)
}

func hostSignals(_ bool) []os.Signal {
	return []os.Signal{os.Interrupt}
}

func isResizeSignal(os.Signal) bool {
	return false
}

func terminalSize(*os.File) (int, int, error) {
	return 0, 0, fmt.Errorf("terminal size is unsupported on windows")
}

func signalName(sig os.Signal) (string, bool) {
	switch sig {
	case os.Interrupt:
		return "INT", true
	default:
		return "", false
	}
}

func unsupportedSignalForTest() os.Signal {
	return unsupportedSignal("unsupported")
}
