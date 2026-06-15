package fat

import (
	"crypto/sha256"
	"testing"
	"time"

	"j5.nz/cc/internal/fsimage/common"
	"j5.nz/cc/internal/fsimage/vm"
)

// TestSimpleDeterministic tests basic deterministic filesystem generation
func TestSimpleDeterministic(t *testing.T) {
	const size = 1024 * 1024 * 1024 // 1GB

	// Create deterministic config
	config := DefaultDeterministicConfig()

	// Generate filesystem twice with identical config
	fs1 := createSimpleFilesystem(t, size, config)
	fs2 := createSimpleFilesystem(t, size, config)

	// Calculate hashes
	hash1 := sha256.Sum256(fs1)
	hash2 := sha256.Sum256(fs2)

	if hash1 != hash2 {
		t.Errorf("Deterministic filesystems should be identical:\nFirst:  %x\nSecond: %x", hash1, hash2)
	} else {
		t.Logf("SUCCESS: Deterministic filesystems are byte-identical (hash: %x)", hash1)
	}
}

// TestVolumeSerialDeterminism tests that volume serial numbers work deterministically
func TestVolumeSerialDeterminism(t *testing.T) {
	const size = 1024 * 1024 * 1024 // 1GB

	// Test with specific volume serial
	customSerial := uint32(0xABCDEF00)
	fatEpoch := time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)

	config := &DeterministicConfig{
		FixedTimestamp:       &fatEpoch,
		SortDirectoryEntries: true,
		VolumeSerial:         &customSerial,
	}

	// Create filesystem with custom serial
	vmFS := vm.NewVirtualMemory(size, 4096)
	writer, err := CreateFATFileSystemWithConfig(vmFS, size, config)
	if err != nil {
		t.Fatalf("Failed to create filesystem: %v", err)
	}

	// Check that volume serial was set correctly
	fs := writer.layout.Fs()
	actualSerial := fs.VolumeId()

	if actualSerial != customSerial {
		t.Errorf("Volume serial mismatch: expected 0x%08X, got 0x%08X", customSerial, actualSerial)
	} else {
		t.Logf("SUCCESS: Volume serial correctly set to 0x%08X", actualSerial)
	}
}

// TestTimestampDeterminism tests that timestamps are set deterministically
func TestTimestampDeterminism(t *testing.T) {
	const size = 1024 * 1024 * 1024 // 1GB

	// Use a specific timestamp
	customTime := time.Date(2024, 1, 15, 14, 30, 45, 0, time.UTC)
	volumeSerial := uint32(0x12345678)

	config := &DeterministicConfig{
		FixedTimestamp:       &customTime,
		SortDirectoryEntries: true,
		VolumeSerial:         &volumeSerial,
	}

	// Create two filesystems
	fs1 := createSimpleFilesystem(t, size, config)
	fs2 := createSimpleFilesystem(t, size, config)

	// They should be identical
	if !compareBytesEqual(fs1, fs2) {
		t.Error("Filesystems with fixed timestamps should be identical")
	} else {
		t.Log("SUCCESS: Fixed timestamps produce identical filesystems")
	}
}

// createSimpleFilesystem creates a simple filesystem with minimal content
func createSimpleFilesystem(t *testing.T, size int64, config *DeterministicConfig) []byte {
	vmFS := vm.NewVirtualMemory(size, 4096)
	writer, err := CreateFATFileSystemWithConfig(vmFS, size, config)
	if err != nil {
		t.Fatalf("Failed to create filesystem: %v", err)
	}

	// Get root directory
	root, err := writer.WritableRootDirectory()
	if err != nil {
		t.Fatalf("Failed to get root: %v", err)
	}

	// Create a single file
	fileNode, err := writer.AllocateNode()
	if err != nil {
		t.Fatalf("Failed to allocate node: %v", err)
	}

	if err := fileNode.SetName("hello.txt"); err != nil {
		t.Fatalf("Failed to set name: %v", err)
	}

	content := vm.RawRegion([]byte("Hello, deterministic world!"))
	if err := writer.WriteContents(fileNode, &content); err != nil {
		t.Fatalf("Failed to write contents: %v", err)
	}

	// Write root directory
	if err := writer.WriteDirectory(root, []common.WritableNode{fileNode}); err != nil {
		t.Fatalf("Failed to write directory: %v", err)
	}

	// Finalize
	if err := writer.Finalize(); err != nil {
		t.Fatalf("Failed to finalize: %v", err)
	}

	// Extract bytes
	data := make([]byte, size)
	if _, err := vmFS.ReadAt(data, 0); err != nil {
		t.Fatalf("Failed to read data: %v", err)
	}

	return data
}
