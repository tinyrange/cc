//go:build windows

package helper

import (
	"os"
	"os/signal"
)

func signalNotify(ch chan<- os.Signal) {
	// On Windows, SIGTERM doesn't exist. os.Interrupt is emulated via
	// GenerateConsoleCtrlEvent (Ctrl+C / Ctrl+Break).
	signal.Notify(ch, os.Interrupt)
}
