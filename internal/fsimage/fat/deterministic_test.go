package fat

import (
	"crypto/sha256"
	"testing"
	"time"

	"j5.nz/cc/internal/fsimage/common"
	"j5.nz/cc/internal/fsimage/vm"
)

// TestDeterministicGeneration verifies that filesystems generate identical bytes with same input
func TestDeterministicGeneration(t *testing.T) {
	const size = 1024 * 1024 * 1024 // 1GB - forces FAT32

	// Test with deterministic config
	t.Run("WithDeterministicConfig", func(t *testing.T) {
		config := DefaultDeterministicConfig()

		// Generate first filesystem
		filesystem1 := createTestFilesystem(t, size, config)

		// Generate second filesystem with identical config
		filesystem2 := createTestFilesystem(t, size, config)

		// Compare byte-for-byte
		if !compareBytesEqual(filesystem1, filesystem2) {
			t.Error("Deterministic filesystems should be byte-identical but differ")
		}

		// Verify using SHA256 hashes
		hash1 := sha256.Sum256(filesystem1)
		hash2 := sha256.Sum256(filesystem2)

		if hash1 != hash2 {
			t.Errorf("Filesystem hashes differ:\nFirst:  %x\nSecond: %x", hash1, hash2)
		}
	})

	// Test without deterministic config (should be non-deterministic due to timestamps)
	t.Run("WithoutDeterministicConfig", func(t *testing.T) {
		// Generate first filesystem
		filesystem1 := createTestFilesystem(t, size, nil)

		// Longer delay to ensure different timestamps (FAT only has 2-second precision)
		time.Sleep(3 * time.Second)

		// Generate second filesystem
		filesystem2 := createTestFilesystem(t, size, nil)

		// Should be different due to timestamps
		if compareBytesEqual(filesystem1, filesystem2) {
			// Compare hashes for debugging
			hash1 := sha256.Sum256(filesystem1)
			hash2 := sha256.Sum256(filesystem2)
			t.Errorf("Non-deterministic filesystems should differ but are identical:\nHash1: %x\nHash2: %x", hash1, hash2)
		}
	})
}

// TestCustomDeterministicConfig tests different deterministic configurations
func TestCustomDeterministicConfig(t *testing.T) {
	const size = 1024 * 1024 * 1024 // 1GB

	// Test with custom timestamp
	t.Run("CustomTimestamp", func(t *testing.T) {
		customTime := time.Date(2023, 12, 25, 10, 30, 0, 0, time.UTC)
		volumeSerial := uint32(0xDEADBEEF)

		config := &DeterministicConfig{
			FixedTimestamp:       &customTime,
			SortDirectoryEntries: true,
			VolumeSerial:         &volumeSerial,
		}

		// Generate two filesystems with same custom config
		filesystem1 := createTestFilesystem(t, size, config)
		filesystem2 := createTestFilesystem(t, size, config)

		// Should be identical
		hash1 := sha256.Sum256(filesystem1)
		hash2 := sha256.Sum256(filesystem2)

		if hash1 != hash2 {
			t.Errorf("Custom config filesystems should be identical but differ")
		}
	})

	// Test with different volume serials
	t.Run("DifferentVolumeSerials", func(t *testing.T) {
		fatEpoch := time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)

		serial1 := uint32(0x11111111)
		config1 := &DeterministicConfig{
			FixedTimestamp:       &fatEpoch,
			SortDirectoryEntries: true,
			VolumeSerial:         &serial1,
		}

		serial2 := uint32(0x22222222)
		config2 := &DeterministicConfig{
			FixedTimestamp:       &fatEpoch,
			SortDirectoryEntries: true,
			VolumeSerial:         &serial2,
		}

		filesystem1 := createTestFilesystem(t, size, config1)
		filesystem2 := createTestFilesystem(t, size, config2)

		// Should be different due to different volume serials
		hash1 := sha256.Sum256(filesystem1)
		hash2 := sha256.Sum256(filesystem2)

		if hash1 == hash2 {
			t.Error("Filesystems with different volume serials should differ")
		}
	})
}

// TestDirectoryEntrySorting verifies that directory entries are sorted deterministically
func TestDirectoryEntrySorting(t *testing.T) {
	const size = 1024 * 1024 * 1024 // 1GB

	config := DefaultDeterministicConfig()

	// Create filesystem with files added in different orders
	filesystem1 := createTestFilesystemWithFiles(t, size, config, []string{"zebra.txt", "alpha.txt", "beta.txt"})
	filesystem2 := createTestFilesystemWithFiles(t, size, config, []string{"alpha.txt", "zebra.txt", "beta.txt"})
	filesystem3 := createTestFilesystemWithFiles(t, size, config, []string{"beta.txt", "alpha.txt", "zebra.txt"})

	// All should be identical due to sorting
	hash1 := sha256.Sum256(filesystem1)
	hash2 := sha256.Sum256(filesystem2)
	hash3 := sha256.Sum256(filesystem3)

	if hash1 != hash2 || hash2 != hash3 {
		t.Error("Filesystems with same files but different creation order should be identical when sorted")
	}
}

// createTestFilesystem creates a test filesystem with some content
func createTestFilesystem(t *testing.T, size int64, config *DeterministicConfig) []byte {
	vmFS := vm.NewVirtualMemory(size, 4096)
	writer, err := CreateFATFileSystemWithConfig(vmFS, size, config)
	if err != nil {
		t.Fatalf("Failed to create FAT filesystem: %v", err)
	}

	// Create some test content
	root, err := writer.WritableRootDirectory()
	if err != nil {
		t.Fatalf("Failed to get root directory: %v", err)
	}

	// Create a file
	fileNode, err := writer.AllocateNode()
	if err != nil {
		t.Fatalf("Failed to allocate file node: %v", err)
	}

	if err := fileNode.SetName("test.txt"); err != nil {
		t.Fatalf("Failed to set file name: %v", err)
	}

	content := vm.RawRegion([]byte("Hello, deterministic world!"))
	if err := writer.WriteContents(fileNode, &content); err != nil {
		t.Fatalf("Failed to write file contents: %v", err)
	}

	// Create a directory
	dirNode, err := writer.AllocateNode()
	if err != nil {
		t.Fatalf("Failed to allocate directory node: %v", err)
	}

	if err := dirNode.SetName("testdir"); err != nil {
		t.Fatalf("Failed to set directory name: %v", err)
	}

	fatNode := dirNode.(*FATWritableNode)
	fatNode.SetDir()

	// Write directory structure
	if err := writer.WriteDirectory(dirNode, []common.WritableNode{}); err != nil {
		t.Fatalf("Failed to write directory: %v", err)
	}

	if err := writer.WriteDirectory(root, []common.WritableNode{fileNode, dirNode}); err != nil {
		t.Fatalf("Failed to write root directory: %v", err)
	}

	// Finalize
	if err := writer.Finalize(); err != nil {
		t.Fatalf("Failed to finalize filesystem: %v", err)
	}

	// Extract bytes
	data := make([]byte, size)
	if _, err := vmFS.ReadAt(data, 0); err != nil {
		t.Fatalf("Failed to read filesystem data: %v", err)
	}

	return data
}

// createTestFilesystemWithFiles creates a filesystem with specific files
func createTestFilesystemWithFiles(t *testing.T, size int64, config *DeterministicConfig, filenames []string) []byte {
	vmFS := vm.NewVirtualMemory(size, 4096)
	writer, err := CreateFATFileSystemWithConfig(vmFS, size, config)
	if err != nil {
		t.Fatalf("Failed to create FAT filesystem: %v", err)
	}

	root, err := writer.WritableRootDirectory()
	if err != nil {
		t.Fatalf("Failed to get root directory: %v", err)
	}

	var children []common.WritableNode

	// Create files in the specified order
	for i, filename := range filenames {
		fileNode, err := writer.AllocateNode()
		if err != nil {
			t.Fatalf("Failed to allocate file node %d: %v", i, err)
		}

		if err := fileNode.SetName(filename); err != nil {
			t.Fatalf("Failed to set file name %s: %v", filename, err)
		}

		content := vm.RawRegion([]byte("content of " + filename))
		if err := writer.WriteContents(fileNode, &content); err != nil {
			t.Fatalf("Failed to write contents for %s: %v", filename, err)
		}

		children = append(children, fileNode)
	}

	if err := writer.WriteDirectory(root, children); err != nil {
		t.Fatalf("Failed to write root directory: %v", err)
	}

	if err := writer.Finalize(); err != nil {
		t.Fatalf("Failed to finalize filesystem: %v", err)
	}

	// Extract bytes
	data := make([]byte, size)
	if _, err := vmFS.ReadAt(data, 0); err != nil {
		t.Fatalf("Failed to read filesystem data: %v", err)
	}

	return data
}

// compareBytesEqual compares two byte slices for exact equality
func compareBytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}

	for i := 0; i < len(a); i++ {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}
