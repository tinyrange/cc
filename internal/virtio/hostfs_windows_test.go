//go:build windows

package virtio

import (
	"syscall"
	"testing"

	"golang.org/x/sys/windows"
	"j5.nz/cc/internal/linuxabi"
)

func TestMapHostErrorMapsBrokenPipeToEPIPE(t *testing.T) {
	errno, ok := mapHostError(syscall.Errno(windows.ERROR_BROKEN_PIPE))
	if !ok {
		t.Fatal("mapHostError(ERROR_BROKEN_PIPE) ok = false")
	}
	if errno != linuxabi.EPIPE {
		t.Fatalf("mapHostError(ERROR_BROKEN_PIPE) = %d, want EPIPE %d", errno, linuxabi.EPIPE)
	}
}
