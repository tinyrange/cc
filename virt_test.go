package cc_test

import (
	"context"
	"errors"
	"log"
	"os"
	"testing"
	"time"

	cc "github.com/tinyrange/cc"
)

func TestMain(m *testing.M) {
	// Ensure the test binary is signed with hypervisor entitlement (macOS)
	if err := cc.EnsureExecutableIsSigned(); err != nil {
		log.Fatalf("Failed to sign executable: %v", err)
	}
	os.Exit(m.Run())
}

func TestEndToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Create OCI client
	client, err := cc.NewOCIClient()
	if err != nil {
		t.Fatalf("NewOCIClient() error = %v", err)
	}

	// Pull alpine image
	source, err := client.Pull(ctx, "alpine:latest")
	if err != nil {
		t.Fatalf("Pull() error = %v", err)
	}

	// Create instance
	inst, err := cc.New(source, cc.WithMemoryMB(128))
	if err != nil {
		if errors.Is(err, cc.ErrHypervisorUnavailable) {
			t.Skip("Skipping: hypervisor unavailable (CI environment)")
		}
		t.Fatalf("New() error = %v", err)
	}
	defer inst.Close()

	// Read a file from the container
	data, err := inst.ReadFile("/etc/os-release")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	if len(data) == 0 {
		t.Error("ReadFile() returned empty data")
	}

	t.Logf("Read %d bytes from /etc/os-release", len(data))
}

func TestNewOCIClient(t *testing.T) {
	client, err := cc.NewOCIClient()
	if err != nil {
		t.Fatalf("NewOCIClient() error = %v", err)
	}
	if client == nil {
		t.Fatal("NewOCIClient() returned nil")
	}
}

func TestOptions(t *testing.T) {
	// Create a cache directory for testing
	cache, err := cc.NewCacheDir(t.TempDir())
	if err != nil {
		t.Fatalf("NewCacheDir() error = %v", err)
	}

	// Verify options implement the Option interface
	var _ cc.Option = cc.WithMemoryMB(256)
	var _ cc.Option = cc.WithTimeout(0)
	var _ cc.Option = cc.WithUser("1000")
	var _ cc.Option = cc.WithCPUs(2)
	var _ cc.Option = cc.WithInteractiveIO(nil, nil)
	var _ cc.Option = cc.WithDmesg()
	var _ cc.Option = cc.WithPacketCapture(nil)
	var _ cc.Option = cc.WithGPU()
	var _ cc.Option = cc.WithMount(cc.MountConfig{Tag: "test"})
	var _ cc.Option = cc.WithCache(cache)

	// Verify pull options implement the OCIPullOption interface
	var _ cc.OCIPullOption = cc.WithPlatform("linux", "amd64")
	var _ cc.OCIPullOption = cc.WithAuth("user", "pass")
	var _ cc.OCIPullOption = cc.WithPullPolicy(cc.PullAlways)

	// Verify filesystem snapshot options implement the FilesystemSnapshotOption interface
	var _ cc.FilesystemSnapshotOption = cc.WithSnapshotExcludes("/tmp/*")
	var _ cc.FilesystemSnapshotOption = cc.WithSnapshotCacheDir(cache.SnapshotPath())

	// Verify dockerfile options implement the DockerfileOption interface
	var _ cc.DockerfileOption = cc.WithBuildContextDir(".")
	var _ cc.DockerfileOption = cc.WithBuildArg("key", "value")
	var _ cc.DockerfileOption = cc.WithDockerfileCacheDir(cache.SnapshotPath())
}

func TestPullPolicy(t *testing.T) {
	if cc.PullIfNotPresent != 0 {
		t.Error("PullIfNotPresent should be 0")
	}
	if cc.PullAlways != 1 {
		t.Error("PullAlways should be 1")
	}
	if cc.PullNever != 2 {
		t.Error("PullNever should be 2")
	}
}

// BenchmarkCommandInExistingGuest benchmarks running commands in an already-running guest.
// This measures the overhead of command execution via vsock.
func BenchmarkCommandInExistingGuest(b *testing.B) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Create OCI client
	client, err := cc.NewOCIClient()
	if err != nil {
		b.Fatalf("NewOCIClient() error = %v", err)
	}

	// Pull alpine image
	source, err := client.Pull(ctx, "alpine:latest")
	if err != nil {
		b.Fatalf("Pull() error = %v", err)
	}

	// Create instance (entrypoint is skipped by default for command execution)
	inst, err := cc.New(source, cc.WithMemoryMB(128))
	if err != nil {
		if errors.Is(err, cc.ErrHypervisorUnavailable) {
			b.Skip("Skipping: hypervisor unavailable (CI environment)")
		}
		b.Fatalf("New() error = %v", err)
	}
	defer inst.Close()

	// Reset timer after setup
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		cmd := inst.Command("/bin/true")
		err := cmd.Run()
		if err != nil {
			b.Fatalf("Command() error = %v", err)
		}
	}
}

// BenchmarkCommandWithNewGuest benchmarks the full cycle of creating a guest and running a command.
// This measures the total latency including VM boot time.
func BenchmarkCommandWithNewGuest(b *testing.B) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Create OCI client
	client, err := cc.NewOCIClient()
	if err != nil {
		b.Fatalf("NewOCIClient() error = %v", err)
	}

	// Pull alpine image once (not part of benchmark)
	source, err := client.Pull(ctx, "alpine:latest")
	if err != nil {
		b.Fatalf("Pull() error = %v", err)
	}

	// Verify hypervisor is available
	testInst, err := cc.New(source, cc.WithMemoryMB(128))
	if err != nil {
		if errors.Is(err, cc.ErrHypervisorUnavailable) {
			b.Skip("Skipping: hypervisor unavailable (CI environment)")
		}
		b.Fatalf("New() error = %v", err)
	}
	testInst.Close()

	// Reset timer after setup
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		inst, err := cc.New(source, cc.WithMemoryMB(128))
		if err != nil {
			b.Fatalf("New() error = %v", err)
		}

		cmd := inst.Command("/bin/true")
		err = cmd.Run()
		if err != nil {
			inst.Close()
			b.Fatalf("Command() error = %v", err)
		}

		inst.Close()
	}
}
