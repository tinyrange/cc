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
	// Verify options implement the Option interface
	var _ cc.Option = cc.WithMemoryMB(256)
	var _ cc.Option = cc.WithEnv("FOO=bar")
	var _ cc.Option = cc.WithTimeout(0)
	var _ cc.Option = cc.WithWorkdir("/app")
	var _ cc.Option = cc.WithUser("1000")

	// Verify pull options implement the OCIPullOption interface
	var _ cc.OCIPullOption = cc.WithPlatform("linux", "amd64")
	var _ cc.OCIPullOption = cc.WithAuth("user", "pass")
	var _ cc.OCIPullOption = cc.WithPullPolicy(cc.PullAlways)
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
