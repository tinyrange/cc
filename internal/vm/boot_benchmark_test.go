package vm

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/kernel/alpine"
	"j5.nz/cc/internal/oci"
)

func BenchmarkAlpineSIMGWhoamiBoot(b *testing.B) {
	if err := Supports(); err != nil {
		b.Skipf("VM backend is not supported on this host: %v", err)
	}
	fixture, err := filepath.Abs(filepath.Join("..", "..", "fixtures", "alpine.simg"))
	if err != nil {
		b.Fatalf("resolve alpine.simg fixture: %v", err)
	}

	setupCtx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	root := b.TempDir()
	kernel := alpine.NewManager(filepath.Join(root, "kernel"))
	if err := kernel.Ensure(setupCtx); err != nil {
		b.Fatalf("prepare kernel: %v", err)
	}
	store := oci.NewStore(filepath.Join(root, "images"))
	if _, err := store.Pull(setupCtx, "alpine", fixture); err != nil {
		b.Fatalf("import alpine.simg: %v", err)
	}
	backend := NewRuntimeBackend(kernel, store, filepath.Join(root, "guestinit"))

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		var output strings.Builder
		exitCode := 0
		err := backend.RunStream(ctx, client.RunRequest{
			Image:    "alpine",
			Command:  []string{"sh", "-c", "whoami"},
			MemoryMB: 256,
		}, nil, func(event client.ExecEvent) error {
			switch event.Kind {
			case "stdout", "output":
				if len(event.Data) > 0 {
					output.Write(event.Data)
				} else {
					output.WriteString(event.Output)
				}
			case "exit":
				exitCode = event.ExitCode
			}
			return nil
		})
		cancel()
		if err != nil {
			b.Fatalf("boot alpine.simg and run whoami: %v\noutput:\n%s", err, output.String())
		}
		if exitCode != 0 {
			b.Fatalf("whoami exit code = %d, want 0\noutput:\n%s", exitCode, output.String())
		}
		if strings.TrimSpace(output.String()) != "root" {
			b.Fatalf("whoami output = %q, want root", output.String())
		}
	}
}
