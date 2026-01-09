//go:build darwin && arm64

package bench

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/hv/factory"
	"github.com/tinyrange/cc/internal/hv/hvf"
)

func TestSnapshotSaveLoad(t *testing.T) {
	hyper, err := factory.Open()
	if err != nil {
		t.Skipf("Open hypervisor: %v", err)
	}
	defer hyper.Close()

	if hyper.Architecture() != hv.ArchitectureARM64 {
		t.Skipf("Test requires ARM64 architecture")
	}

	// Create a VM with some memory
	vm, err := hyper.NewVirtualMachine(hv.SimpleVMConfig{
		NumCPUs:          1,
		MemSize:          4 * 1024 * 1024, // 4 MiB for faster test
		MemBase:          0x80000000,
		InterruptSupport: true,
	})
	if err != nil {
		t.Fatalf("Create VM: %v", err)
	}
	defer vm.Close()

	// Capture snapshot
	snap, err := vm.CaptureSnapshot()
	if err != nil {
		t.Fatalf("CaptureSnapshot: %v", err)
	}

	// Save snapshot to temp file
	tmpDir := t.TempDir()
	snapPath := filepath.Join(tmpDir, "test.snap")

	if err := hvf.SaveSnapshot(snapPath, snap); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}

	// Check file was created
	info, err := os.Stat(snapPath)
	if err != nil {
		t.Fatalf("Stat snapshot file: %v", err)
	}
	t.Logf("Snapshot file size: %d bytes (4 MiB memory)", info.Size())

	// Load snapshot from file
	loadedSnap, err := hvf.LoadSnapshot(snapPath)
	if err != nil {
		t.Fatalf("LoadSnapshot: %v", err)
	}

	// Restore the loaded snapshot - verifies all fields were serialized correctly
	if err := vm.RestoreSnapshot(loadedSnap); err != nil {
		t.Fatalf("RestoreSnapshot: %v", err)
	}

	t.Log("Snapshot save/load/restore roundtrip successful")
}

func BenchmarkSnapshotSave(b *testing.B) {
	hyper, err := factory.Open()
	if err != nil {
		b.Skipf("Open hypervisor: %v", err)
	}
	defer hyper.Close()

	if hyper.Architecture() != hv.ArchitectureARM64 {
		b.Skipf("Benchmark requires ARM64 architecture")
	}

	vm, err := hyper.NewVirtualMachine(hv.SimpleVMConfig{
		NumCPUs:          1,
		MemSize:          64 * 1024 * 1024, // 64 MiB
		MemBase:          0x80000000,
		InterruptSupport: true,
	})
	if err != nil {
		b.Skipf("Create VM: %v", err)
	}
	defer vm.Close()

	snap, err := vm.CaptureSnapshot()
	if err != nil {
		b.Fatalf("CaptureSnapshot: %v", err)
	}

	tmpDir := b.TempDir()

	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		snapPath := filepath.Join(tmpDir, "bench.snap")
		if err := hvf.SaveSnapshot(snapPath, snap); err != nil {
			b.Fatalf("SaveSnapshot: %v", err)
		}
	}
}

func BenchmarkSnapshotLoad(b *testing.B) {
	hyper, err := factory.Open()
	if err != nil {
		b.Skipf("Open hypervisor: %v", err)
	}
	defer hyper.Close()

	if hyper.Architecture() != hv.ArchitectureARM64 {
		b.Skipf("Benchmark requires ARM64 architecture")
	}

	vm, err := hyper.NewVirtualMachine(hv.SimpleVMConfig{
		NumCPUs:          1,
		MemSize:          64 * 1024 * 1024, // 64 MiB
		MemBase:          0x80000000,
		InterruptSupport: true,
	})
	if err != nil {
		b.Skipf("Create VM: %v", err)
	}
	defer vm.Close()

	snap, err := vm.CaptureSnapshot()
	if err != nil {
		b.Fatalf("CaptureSnapshot: %v", err)
	}

	tmpDir := b.TempDir()
	snapPath := filepath.Join(tmpDir, "bench.snap")

	if err := hvf.SaveSnapshot(snapPath, snap); err != nil {
		b.Fatalf("SaveSnapshot: %v", err)
	}

	b.ResetTimer()
	for b.Loop() {
		_, err := hvf.LoadSnapshot(snapPath)
		if err != nil {
			b.Fatalf("LoadSnapshot: %v", err)
		}
	}
}
