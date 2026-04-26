//go:build linux || darwin

package main

import (
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func hostSignals(tty bool) []os.Signal {
	signals := []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGHUP}
	if tty {
		signals = append(signals, syscall.SIGWINCH)
	}
	return signals
}

func isResizeSignal(sig os.Signal) bool {
	return sig == syscall.SIGWINCH
}

func terminalSize(file *os.File) (int, int, error) {
	ws, err := unix.IoctlGetWinsize(int(file.Fd()), unix.TIOCGWINSZ)
	if err != nil {
		return 0, 0, err
	}
	return int(ws.Col), int(ws.Row), nil
}

func signalName(sig os.Signal) (string, bool) {
	switch sig {
	case os.Interrupt:
		return "INT", true
	case syscall.SIGHUP:
		return "HUP", true
	case syscall.SIGQUIT:
		return "QUIT", true
	case syscall.SIGTERM:
		return "TERM", true
	default:
		return "", false
	}
}

func unsupportedSignalForTest() os.Signal {
	return syscall.SIGUSR1
}
