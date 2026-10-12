package kernelvm

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestBSDValidationPrecedesExecution(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	valid := BSDOptions{OS: "openbsd", Marker: "ready", MemoryMB: 256}
	tests := []struct {
		name   string
		ctx    context.Context
		kernel []byte
		opts   BSDOptions
		want   string
	}{
		{"os", ctx, []byte{1}, BSDOptions{OS: "bad", Marker: "ready"}, "BSD OS"},
		{"kernel", ctx, nil, valid, "kernel"},
		{"deadline", context.Background(), []byte{1}, valid, "deadline"},
		{"marker", ctx, []byte{1}, BSDOptions{OS: "openbsd"}, "marker"},
		{"memory", ctx, []byte{1}, BSDOptions{OS: "openbsd", Marker: "ready", MemoryMB: 8193}, "memory"},
		{"console", ctx, []byte{1}, BSDOptions{OS: "openbsd", Marker: "ready", Console: []ConsoleStep{{Send: "x"}}}, "console"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := RunBSD(tc.ctx, tc.kernel, tc.opts)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v; want %s", err, tc.want)
			}
		})
	}
}
