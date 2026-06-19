//go:build windows && amd64

package vm

import (
	"context"
	"strings"
	"testing"

	"j5.nz/cc/client"
)

func TestWindowsRuntimeBuiltinBSDDoesNotFallThroughToImageStore(t *testing.T) {
	backend := NewRuntimeBackend(nil, nil, t.TempDir())
	for _, tc := range []struct {
		name string
		run  func(context.Context) error
	}{
		{
			name: "start",
			run: func(ctx context.Context) error {
				_, err := backend.StartStream(ctx, client.CreateInstanceRequest{Image: "@freebsd"}, nil)
				return err
			},
		},
		{
			name: "start_blank",
			run: func(ctx context.Context) error {
				_, err := backend.StartBlankStream(ctx, client.StartInstanceRequest{Image: "@openbsd"}, nil)
				return err
			},
		},
		{
			name: "run",
			run: func(ctx context.Context) error {
				_, err := backend.Run(ctx, client.RunRequest{Image: "@netbsd"})
				return err
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.run(context.Background())
			if err == nil {
				t.Fatal("built-in BSD request unexpectedly succeeded")
			}
			if strings.Contains(err.Error(), "image store") || strings.Contains(err.Error(), "image.json") {
				t.Fatalf("built-in BSD request fell through to image store: %v", err)
			}
			if !strings.Contains(err.Error(), "WHP managed BSD guests") {
				t.Fatalf("built-in BSD request error = %v, want WHP managed BSD blocker", err)
			}
		})
	}
}
