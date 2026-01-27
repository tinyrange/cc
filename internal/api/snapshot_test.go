package api

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

// testTimeoutOption is a local timeout option for tests.
type testTimeoutOption struct{ d time.Duration }

func (testTimeoutOption) IsOption()                 {}
func (o testTimeoutOption) Duration() time.Duration { return o.d }

func withTimeout(d time.Duration) Option {
	return testTimeoutOption{d: d}
}

func TestFilesystemSnapshotRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Create OCI client and pull alpine
	client, err := NewOCIClient()
	if err != nil {
		t.Fatalf("NewOCIClient: %v", err)
	}

	source, err := client.Pull(ctx, "alpine:3.19")
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}

	// Create first instance
	inst1, err := New(source,
		withMemoryMB(256),
		withTimeout(time.Minute),
	)
	if err != nil {
		if errors.Is(err, ErrHypervisorUnavailable) {
			t.Skipf("Hypervisor unavailable: %v", err)
		}
		t.Fatalf("New instance 1: %v", err)
	}

	// Create a test file in the first instance
	testContent := []byte("snapshot test content 12345")
	if err := inst1.WriteFile("/test-snapshot.txt", testContent, 0644); err != nil {
		inst1.Close()
		t.Fatalf("WriteFile: %v", err)
	}

	// Create a directory with a file
	if err := inst1.MkdirAll("/testdir/subdir", 0755); err != nil {
		inst1.Close()
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := inst1.WriteFile("/testdir/subdir/nested.txt", []byte("nested content"), 0644); err != nil {
		inst1.Close()
		t.Fatalf("WriteFile nested: %v", err)
	}

	// Take a snapshot
	t.Log("Taking snapshot...")
	cacheDir := t.TempDir()
	snap, err := inst1.SnapshotFilesystem(
		WithCacheDir(cacheDir),
	)
	if err != nil {
		inst1.Close()
		t.Fatalf("SnapshotFilesystem: %v", err)
	}
	t.Logf("Snapshot cache key: %s", snap.CacheKey())

	// Close first instance
	inst1.Close()

	// Create second instance from snapshot
	t.Log("Creating instance from snapshot...")
	inst2, err := New(snap,
		withMemoryMB(256),
		withTimeout(time.Minute),
	)
	if err != nil {
		snap.Close()
		t.Fatalf("New instance 2 from snapshot: %v", err)
	}
	defer inst2.Close()
	defer snap.Close()

	// Verify the test file exists and has correct content
	content, err := inst2.ReadFile("/test-snapshot.txt")
	if err != nil {
		t.Fatalf("ReadFile from snapshot: %v", err)
	}
	if string(content) != string(testContent) {
		t.Errorf("Content mismatch: got %q, want %q", string(content), string(testContent))
	}

	// Verify nested file
	nestedContent, err := inst2.ReadFile("/testdir/subdir/nested.txt")
	if err != nil {
		t.Fatalf("ReadFile nested from snapshot: %v", err)
	}
	if string(nestedContent) != "nested content" {
		t.Errorf("Nested content mismatch: got %q", string(nestedContent))
	}

	t.Log("Snapshot round-trip test passed!")
}

func TestFilesystemSnapshotFactoryBasic(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// Create OCI client
	client, err := NewOCIClient()
	if err != nil {
		t.Fatalf("NewOCIClient: %v", err)
	}

	cacheDir := t.TempDir()

	// Build a snapshot with a simple file creation
	t.Log("Building snapshot with factory...")
	snap, err := NewFilesystemSnapshotFactory(client, cacheDir).
		From("alpine:3.19").
		Run("mkdir", "-p", "/factory-test").
		Run("sh", "-c", "echo 'factory created' > /factory-test/file.txt").
		Build(ctx)
	if err != nil {
		if errors.Is(err, ErrHypervisorUnavailable) {
			t.Skipf("Hypervisor unavailable: %v", err)
		}
		t.Fatalf("Factory Build: %v", err)
	}
	defer snap.Close()

	t.Logf("Snapshot built with cache key: %s", snap.CacheKey())

	// Create instance from snapshot
	t.Log("Creating instance from factory snapshot...")
	inst, err := New(snap,
		withMemoryMB(256),
		withTimeout(time.Minute),
	)
	if err != nil {
		t.Fatalf("New from factory snapshot: %v", err)
	}
	defer inst.Close()

	// Verify the file exists
	content, err := inst.ReadFile("/factory-test/file.txt")
	if err != nil {
		t.Fatalf("ReadFile from factory snapshot: %v", err)
	}
	// The echo command adds a newline
	expected := "factory created\n"
	if string(content) != expected {
		t.Errorf("Content mismatch: got %q, want %q", string(content), expected)
	}

	t.Log("Factory snapshot test passed!")
}

// TestFilesystemSnapshotDebug tests what's being captured in the snapshot.
func TestFilesystemSnapshotDebug(t *testing.T) {
	// skip this test unless a special environment variable is set
	if os.Getenv("CC_TEST_SNAPSHOT_WITH_PACKAGE_INSTALL") != "1" {
		t.Skip("CC_TEST_SNAPSHOT_WITH_PACKAGE_INSTALL is not set, skipping snapshot with package install test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// Create OCI client
	client, err := NewOCIClient()
	if err != nil {
		t.Fatalf("NewOCIClient: %v", err)
	}

	source, err := client.Pull(ctx, "alpine:3.19")
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}

	// Create instance and install a small package
	inst1, err := New(source,
		withMemoryMB(256),
		withTimeout(2*time.Minute),
	)
	if err != nil {
		if errors.Is(err, ErrHypervisorUnavailable) {
			t.Skipf("Hypervisor unavailable: %v", err)
		}
		t.Fatalf("New instance 1: %v", err)
	}

	// Install curl (smaller than gcc)
	cmd := inst1.CommandContext(ctx, "apk", "add", "--no-cache", "curl")
	output, err := cmd.CombinedOutput()
	t.Logf("apk output:\n%s", string(output))
	if err != nil {
		inst1.Close()
		t.Fatalf("apk add failed: %v", err)
	}

	// Check curl exists
	curlInfo, err := inst1.Stat("/usr/bin/curl")
	if err != nil {
		inst1.Close()
		t.Fatalf("curl not found before snapshot: %v", err)
	}
	t.Logf("curl before snapshot: mode=%v size=%d", curlInfo.Mode(), curlInfo.Size())

	// Take snapshot
	cacheDir := t.TempDir()
	snap, err := inst1.SnapshotFilesystem(WithCacheDir(cacheDir))
	if err != nil {
		inst1.Close()
		t.Fatalf("SnapshotFilesystem: %v", err)
	}
	t.Logf("Snapshot cache key: %s", snap.CacheKey())

	// Check the layer files
	fsSnap, ok := snap.(*fsSnapshotSource)
	if !ok {
		inst1.Close()
		snap.Close()
		t.Fatalf("Unexpected snapshot type: %T", snap)
	}
	t.Logf("Snapshot has %d layers", len(fsSnap.layers))
	for i, hash := range fsSnap.layers {
		t.Logf("Layer %d: %s", i, hash)
	}

	inst1.Close()

	// Create new instance from snapshot
	inst2, err := New(snap,
		withMemoryMB(256),
		withTimeout(time.Minute),
	)
	if err != nil {
		snap.Close()
		t.Fatalf("New instance 2: %v", err)
	}
	defer inst2.Close()
	defer snap.Close()

	// Check curl exists after snapshot
	curlInfo, err = inst2.Stat("/usr/bin/curl")
	if err != nil {
		t.Logf("curl NOT found after snapshot: %v", err)
		// Let's try to list /usr/bin
		entries, err := inst2.ReadDir("/usr/bin")
		if err != nil {
			t.Logf("Could not read /usr/bin: %v", err)
		} else {
			t.Logf("/usr/bin has %d entries", len(entries))
			for i, e := range entries {
				if i < 10 {
					t.Logf("  %s", e.Name())
				}
			}
			if len(entries) > 10 {
				t.Logf("  ... and %d more", len(entries)-10)
			}
		}
		t.Fatalf("curl not found after snapshot")
	}
	t.Logf("curl after snapshot: mode=%v size=%d", curlInfo.Mode(), curlInfo.Size())
	t.Log("Test passed!")
}

// TestFilesystemSnapshotExistingDir tests adding a file to an existing directory.
func TestFilesystemSnapshotExistingDir(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Create OCI client
	client, err := NewOCIClient()
	if err != nil {
		t.Fatalf("NewOCIClient: %v", err)
	}

	source, err := client.Pull(ctx, "alpine:3.19")
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}

	// Create first instance
	inst1, err := New(source,
		withMemoryMB(256),
		withTimeout(time.Minute),
	)
	if err != nil {
		if errors.Is(err, ErrHypervisorUnavailable) {
			t.Skipf("Hypervisor unavailable: %v", err)
		}
		t.Fatalf("New instance 1: %v", err)
	}

	// Create a file in /usr/bin (existing directory from alpine)
	testContent := []byte("test file in existing dir")
	if err := inst1.WriteFile("/usr/bin/test-snap-file", testContent, 0755); err != nil {
		inst1.Close()
		t.Fatalf("WriteFile: %v", err)
	}
	t.Log("Created /usr/bin/test-snap-file")

	// Verify the file exists before snapshot
	content, err := inst1.ReadFile("/usr/bin/test-snap-file")
	if err != nil {
		inst1.Close()
		t.Fatalf("ReadFile before snapshot: %v", err)
	}
	if string(content) != string(testContent) {
		inst1.Close()
		t.Fatalf("Content mismatch before snapshot")
	}

	// Take snapshot
	t.Log("Taking snapshot...")
	cacheDir := t.TempDir()
	snap, err := inst1.SnapshotFilesystem(
		WithCacheDir(cacheDir),
	)
	if err != nil {
		inst1.Close()
		t.Fatalf("SnapshotFilesystem: %v", err)
	}
	t.Logf("Snapshot cache key: %s", snap.CacheKey())

	// Close first instance
	inst1.Close()

	// Create second instance from snapshot
	t.Log("Creating instance from snapshot...")
	inst2, err := New(snap,
		withMemoryMB(256),
		withTimeout(time.Minute),
	)
	if err != nil {
		snap.Close()
		t.Fatalf("New instance 2: %v", err)
	}
	defer inst2.Close()
	defer snap.Close()

	// Verify the file exists after snapshot
	content, err = inst2.ReadFile("/usr/bin/test-snap-file")
	if err != nil {
		t.Fatalf("ReadFile after snapshot: %v", err)
	}
	if string(content) != string(testContent) {
		t.Errorf("Content mismatch after snapshot: got %q", string(content))
	}

	t.Log("File in existing directory survives snapshot!")
}

// TestFilesystemSnapshotDirectPackageInstall tests snapshotting without the factory
// to isolate whether the issue is with snapshot capture/restore or with layering.
func TestFilesystemSnapshotDirectPackageInstall(t *testing.T) {
	// skip this test unless a special environment variable is set
	if os.Getenv("CC_TEST_SNAPSHOT_WITH_PACKAGE_INSTALL") != "1" {
		t.Skip("CC_TEST_SNAPSHOT_WITH_PACKAGE_INSTALL is not set, skipping snapshot with package install test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Create OCI client
	client, err := NewOCIClient()
	if err != nil {
		t.Fatalf("NewOCIClient: %v", err)
	}

	source, err := client.Pull(ctx, "alpine:3.19")
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}

	// Create first instance and install packages
	t.Log("Creating instance and installing packages...")
	inst1, err := New(source,
		withMemoryMB(256),
		withTimeout(2*time.Minute),
	)
	if err != nil {
		if errors.Is(err, ErrHypervisorUnavailable) {
			t.Skipf("Hypervisor unavailable: %v", err)
		}
		t.Fatalf("New instance 1: %v", err)
	}

	// Install gcc
	cmd := inst1.CommandContext(ctx, "apk", "add", "--no-cache", "gcc", "musl-dev")
	output, err := cmd.CombinedOutput()
	t.Logf("apk output: %s", string(output))
	if err != nil {
		inst1.Close()
		t.Fatalf("apk add failed: %v", err)
	}

	// Verify ld exists BEFORE snapshot
	_, err = inst1.Stat("/usr/bin/ld")
	if err != nil {
		inst1.Close()
		t.Fatalf("ld not found BEFORE snapshot: %v", err)
	}
	t.Log("ld exists before snapshot")

	// Take snapshot
	t.Log("Taking snapshot...")
	cacheDir := t.TempDir()
	snap, err := inst1.SnapshotFilesystem(
		WithCacheDir(cacheDir),
	)
	if err != nil {
		inst1.Close()
		t.Fatalf("SnapshotFilesystem: %v", err)
	}
	t.Logf("Snapshot cache key: %s", snap.CacheKey())

	// Close first instance
	inst1.Close()

	// Create second instance from snapshot
	t.Log("Creating instance from snapshot...")
	inst2, err := New(snap,
		withMemoryMB(256),
		withTimeout(time.Minute),
	)
	if err != nil {
		snap.Close()
		t.Fatalf("New instance 2 from snapshot: %v", err)
	}
	defer inst2.Close()
	defer snap.Close()

	// Verify ld exists AFTER snapshot
	_, err = inst2.Stat("/usr/bin/ld")
	if err != nil {
		t.Fatalf("ld not found AFTER snapshot: %v", err)
	}
	t.Log("ld exists after snapshot - direct snapshot works!")
}

func TestFilesystemSnapshotWithPackageInstall(t *testing.T) {
	// skip this test unless a special environment variable is set
	if os.Getenv("CC_TEST_SNAPSHOT_WITH_PACKAGE_INSTALL") != "1" {
		t.Skip("CC_TEST_SNAPSHOT_WITH_PACKAGE_INSTALL is not set, skipping snapshot with package install test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Create OCI client
	client, err := NewOCIClient()
	if err != nil {
		t.Fatalf("NewOCIClient: %v", err)
	}

	cacheDir := t.TempDir()

	// Build a snapshot with package installation
	t.Log("Building snapshot with factory...")
	snap, err := NewFilesystemSnapshotFactory(client, cacheDir).
		From("alpine:3.19").
		Run("apk", "add", "--no-cache", "gcc", "musl-dev").
		Exclude("/var/cache/*", "/tmp/*").
		Build(ctx)
	if err != nil {
		if errors.Is(err, ErrHypervisorUnavailable) {
			t.Skipf("Hypervisor unavailable: %v", err)
		}
		t.Fatalf("Factory Build: %v", err)
	}
	defer snap.Close()

	t.Logf("Snapshot built with cache key: %s", snap.CacheKey())

	// Create instance from snapshot
	t.Log("Creating instance from snapshot...")
	inst, err := New(snap,
		withMemoryMB(256),
		withTimeout(time.Minute),
	)
	if err != nil {
		t.Fatalf("New from snapshot: %v", err)
	}
	defer inst.Close()

	// Verify gcc exists
	gccInfo, err := inst.Stat("/usr/bin/gcc")
	if err != nil {
		t.Errorf("gcc not found in snapshot: %v", err)
	} else {
		t.Logf("gcc found: mode=%v size=%d", gccInfo.Mode(), gccInfo.Size())
	}

	// Verify ld exists (from binutils, dependency of gcc)
	ldInfo, err := inst.Stat("/usr/bin/ld")
	if err != nil {
		t.Errorf("ld not found in snapshot: %v", err)
	} else {
		t.Logf("ld found: mode=%v size=%d", ldInfo.Mode(), ldInfo.Size())
	}

	// Check cc1 (gcc internal compiler)
	cc1Info, err := inst.Stat("/usr/libexec/gcc/aarch64-alpine-linux-musl/13.2.1/cc1")
	if err != nil {
		t.Logf("cc1 not found: %v", err)
	} else {
		t.Logf("cc1 found: mode=%v size=%d", cc1Info.Mode(), cc1Info.Size())
	}

	// Try to compile a simple program
	t.Log("Testing gcc in snapshot...")
	testCode := `#include <stdio.h>
int main() { printf("Hello from snapshot\n"); return 0; }
`
	// Create a test directory and write the source file
	mkdirCmd := inst.CommandContext(ctx, "mkdir", "-p", "/home/testdir")
	mkdirCmd.Run()

	if err := inst.WriteFile("/home/testdir/test.c", []byte(testCode), 0644); err != nil {
		t.Fatalf("WriteFile test.c: %v", err)
	}

	// Compile with gcc
	gccCmd := inst.CommandContext(ctx, "gcc", "-o", "/home/testdir/test", "/home/testdir/test.c")
	output, err := gccCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("gcc compile failed: %v\nOutput: %s", err, string(output))
	}

	// Run the compiled program
	runCmd := inst.CommandContext(ctx, "/home/testdir/test")
	output, err = runCmd.Output()
	if err != nil {
		t.Fatalf("test run failed: %v", err)
	}
	if string(output) != "Hello from snapshot\n" {
		t.Errorf("Unexpected output: %q", string(output))
	}

	t.Log("Package install snapshot test passed!")
}
