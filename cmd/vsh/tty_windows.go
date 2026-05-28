//go:build windows

package main

import (
	"fmt"
	"os"
)

func isTerminalFD(int) bool {
	return false
}

func terminalSize(*os.File) (int, int, error) {
	return 0, 0, fmt.Errorf("terminal size is unsupported on windows")
}

func makeRawTerminal(*os.File) (func(), error) {
	return func() {}, nil
}

func hostSignals(_ bool) []os.Signal {
	return []os.Signal{os.Interrupt}
}

func isResizeSignal(os.Signal) bool {
	return false
}

func signalName(sig os.Signal) (string, bool) {
	switch sig {
	case os.Interrupt:
		return "INT", true
	default:
		return "", false
	}
}
