//go:build linux || darwin

package main

import (
	"syscall"
	"testing"
)

func TestUnixSignalName(t *testing.T) {
	tests := []struct {
		sig  syscall.Signal
		want string
	}{
		{sig: syscall.SIGHUP, want: "HUP"},
		{sig: syscall.SIGQUIT, want: "QUIT"},
		{sig: syscall.SIGTERM, want: "TERM"},
	}
	for _, tt := range tests {
		got, ok := signalName(tt.sig)
		if !ok || got != tt.want {
			t.Fatalf("signalName(%v) = %q, %v; want %q, true", tt.sig, got, ok, tt.want)
		}
	}
}
