package vfs

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewOSDirBackend(t *testing.T) {
	t.Run("valid directory", func(t *testing.T) {
		dir := t.TempDir()
		backend, err := NewOSDirBackend(dir, false)
		if err != nil {
			t.Fatalf("NewOSDirBackend failed: %v", err)
		}
		if backend == nil {
			t.Fatal("expected non-nil backend")
		}
		if backend.hostPath == "" {
			t.Fatal("expected hostPath to be set")
		}
		if backend.readOnly {
			t.Fatal("expected readOnly to be false")
		}
	})

	t.Run("valid directory read-only", func(t *testing.T) {
		dir := t.TempDir()
		backend, err := NewOSDirBackend(dir, true)
		if err != nil {
			t.Fatalf("NewOSDirBackend failed: %v", err)
		}
		if !backend.readOnly {
			t.Fatal("expected readOnly to be true")
		}
	})

	t.Run("non-existent directory", func(t *testing.T) {
		_, err := NewOSDirBackend("/nonexistent/path/that/should/not/exist", false)
		if err == nil {
			t.Fatal("expected error for non-existent directory")
		}
	})

	t.Run("file not directory", func(t *testing.T) {
		dir := t.TempDir()
		file := filepath.Join(dir, "testfile")
		if err := os.WriteFile(file, []byte("test"), 0644); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}
		_, err := NewOSDirBackend(file, false)
		if err == nil {
			t.Fatal("expected error for file (not directory)")
		}
	})
}

func TestOSDirBackend_Stat(t *testing.T) {
	dir := t.TempDir()
	backend, err := NewOSDirBackend(dir, false)
	if err != nil {
		t.Fatalf("NewOSDirBackend failed: %v", err)
	}

	mode := backend.Stat()
	if mode&fs.ModeDir == 0 {
		// Stat returns just mode bits, not ModeDir
		// Check that it's a reasonable directory permission
		if mode != 0o755 {
			t.Errorf("expected mode 0755, got %v", mode)
		}
	}
}

func TestOSDirBackend_ModTime(t *testing.T) {
	dir := t.TempDir()
	backend, err := NewOSDirBackend(dir, false)
	if err != nil {
		t.Fatalf("NewOSDirBackend failed: %v", err)
	}

	modTime := backend.ModTime()
	// ModTime should be recent (within last minute for temp dir)
	if time.Since(modTime) > time.Minute {
		t.Errorf("ModTime too old: %v", modTime)
	}
}

func TestOSDirBackend_ReadDir(t *testing.T) {
	t.Run("empty directory", func(t *testing.T) {
		dir := t.TempDir()
		backend, err := NewOSDirBackend(dir, false)
		if err != nil {
			t.Fatalf("NewOSDirBackend failed: %v", err)
		}

		entries, err := backend.ReadDir()
		if err != nil {
			t.Fatalf("ReadDir failed: %v", err)
		}
		if len(entries) != 0 {
			t.Errorf("expected 0 entries, got %d", len(entries))
		}
	})

	t.Run("files and directories", func(t *testing.T) {
		dir := t.TempDir()

		// Create a file
		if err := os.WriteFile(filepath.Join(dir, "file1.txt"), []byte("content"), 0644); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}

		// Create a subdirectory
		if err := os.Mkdir(filepath.Join(dir, "subdir"), 0755); err != nil {
			t.Fatalf("Mkdir failed: %v", err)
		}

		backend, err := NewOSDirBackend(dir, false)
		if err != nil {
			t.Fatalf("NewOSDirBackend failed: %v", err)
		}

		entries, err := backend.ReadDir()
		if err != nil {
			t.Fatalf("ReadDir failed: %v", err)
		}
		if len(entries) != 2 {
			t.Errorf("expected 2 entries, got %d", len(entries))
		}

		// Check entries
		foundFile := false
		foundDir := false
		for _, entry := range entries {
			if entry.Name == "file1.txt" && !entry.IsDir {
				foundFile = true
			}
			if entry.Name == "subdir" && entry.IsDir {
				foundDir = true
			}
		}
		if !foundFile {
			t.Error("expected to find file1.txt")
		}
		if !foundDir {
			t.Error("expected to find subdir")
		}
	})

	t.Run("symlinks", func(t *testing.T) {
		dir := t.TempDir()

		// Create a file
		if err := os.WriteFile(filepath.Join(dir, "target.txt"), []byte("content"), 0644); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}

		// Create a symlink
		if err := os.Symlink("target.txt", filepath.Join(dir, "link.txt")); err != nil {
			t.Fatalf("Symlink failed: %v", err)
		}

		backend, err := NewOSDirBackend(dir, false)
		if err != nil {
			t.Fatalf("NewOSDirBackend failed: %v", err)
		}

		entries, err := backend.ReadDir()
		if err != nil {
			t.Fatalf("ReadDir failed: %v", err)
		}
		if len(entries) != 2 {
			t.Errorf("expected 2 entries, got %d", len(entries))
		}
	})
}

func TestOSDirBackend_Lookup(t *testing.T) {
	t.Run("regular file", func(t *testing.T) {
		dir := t.TempDir()
		content := []byte("test content")
		if err := os.WriteFile(filepath.Join(dir, "test.txt"), content, 0644); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}

		backend, err := NewOSDirBackend(dir, false)
		if err != nil {
			t.Fatalf("NewOSDirBackend failed: %v", err)
		}

		entry, err := backend.Lookup("test.txt")
		if err != nil {
			t.Fatalf("Lookup failed: %v", err)
		}
		if entry.File == nil {
			t.Fatal("expected File to be non-nil")
		}
		if entry.Dir != nil || entry.Symlink != nil {
			t.Fatal("expected only File to be set")
		}

		// Test file methods
		size, mode := entry.File.Stat()
		if size != uint64(len(content)) {
			t.Errorf("expected size %d, got %d", len(content), size)
		}
		if mode&0644 != 0644 {
			t.Errorf("unexpected mode: %v", mode)
		}
	})

	t.Run("directory", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, "subdir"), 0755); err != nil {
			t.Fatalf("Mkdir failed: %v", err)
		}

		backend, err := NewOSDirBackend(dir, false)
		if err != nil {
			t.Fatalf("NewOSDirBackend failed: %v", err)
		}

		entry, err := backend.Lookup("subdir")
		if err != nil {
			t.Fatalf("Lookup failed: %v", err)
		}
		if entry.Dir == nil {
			t.Fatal("expected Dir to be non-nil")
		}
		if entry.File != nil || entry.Symlink != nil {
			t.Fatal("expected only Dir to be set")
		}
	})

	t.Run("symlink", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "target.txt"), []byte("content"), 0644); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}
		if err := os.Symlink("target.txt", filepath.Join(dir, "link.txt")); err != nil {
			t.Fatalf("Symlink failed: %v", err)
		}

		backend, err := NewOSDirBackend(dir, false)
		if err != nil {
			t.Fatalf("NewOSDirBackend failed: %v", err)
		}

		entry, err := backend.Lookup("link.txt")
		if err != nil {
			t.Fatalf("Lookup failed: %v", err)
		}
		if entry.Symlink == nil {
			t.Fatal("expected Symlink to be non-nil")
		}
		if entry.Symlink.Target() != "target.txt" {
			t.Errorf("expected target 'target.txt', got '%s'", entry.Symlink.Target())
		}
	})

	t.Run("not found", func(t *testing.T) {
		dir := t.TempDir()
		backend, err := NewOSDirBackend(dir, false)
		if err != nil {
			t.Fatalf("NewOSDirBackend failed: %v", err)
		}

		_, err = backend.Lookup("nonexistent")
		if err == nil {
			t.Fatal("expected error for nonexistent file")
		}
	})
}

func TestOSDirBackend_DirectoryTraversal(t *testing.T) {
	// Create a directory structure:
	// tmpdir/
	//   mounted/
	//     file.txt
	//   secret/
	//     sensitive.txt
	tmpdir := t.TempDir()
	mounted := filepath.Join(tmpdir, "mounted")
	secret := filepath.Join(tmpdir, "secret")

	if err := os.Mkdir(mounted, 0755); err != nil {
		t.Fatalf("Mkdir failed: %v", err)
	}
	if err := os.Mkdir(secret, 0755); err != nil {
		t.Fatalf("Mkdir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(mounted, "file.txt"), []byte("safe"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(secret, "sensitive.txt"), []byte("secret data"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	backend, err := NewOSDirBackend(mounted, false)
	if err != nil {
		t.Fatalf("NewOSDirBackend failed: %v", err)
	}

	// Attempt to access parent directory via ..
	_, err = backend.Lookup("../secret/sensitive.txt")
	if err == nil {
		t.Fatal("expected error for directory traversal attempt")
	}

	// Should be able to access file in mounted directory
	entry, err := backend.Lookup("file.txt")
	if err != nil {
		t.Fatalf("Lookup failed for valid file: %v", err)
	}
	if entry.File == nil {
		t.Fatal("expected File to be non-nil")
	}
}

func TestOsFileEntry_ReadAt(t *testing.T) {
	dir := t.TempDir()
	content := []byte("hello world, this is test content")
	if err := os.WriteFile(filepath.Join(dir, "test.txt"), content, 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	backend, err := NewOSDirBackend(dir, false)
	if err != nil {
		t.Fatalf("NewOSDirBackend failed: %v", err)
	}

	entry, err := backend.Lookup("test.txt")
	if err != nil {
		t.Fatalf("Lookup failed: %v", err)
	}

	// Read from beginning
	data, err := entry.File.ReadAt(0, 5)
	if err != nil {
		t.Fatalf("ReadAt failed: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("expected 'hello', got '%s'", string(data))
	}

	// Read from offset
	data, err = entry.File.ReadAt(6, 5)
	if err != nil {
		t.Fatalf("ReadAt failed: %v", err)
	}
	if string(data) != "world" {
		t.Errorf("expected 'world', got '%s'", string(data))
	}

	// Read past end
	data, err = entry.File.ReadAt(uint64(len(content)-3), 10)
	if err != nil {
		t.Fatalf("ReadAt at end failed: %v", err)
	}
	if string(data) != "ent" {
		t.Errorf("expected 'ent', got '%s'", string(data))
	}
}

func TestOsFileEntry_WriteAt(t *testing.T) {
	dir := t.TempDir()
	content := []byte("hello world")
	filePath := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(filePath, content, 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	backend, err := NewOSDirBackend(dir, false)
	if err != nil {
		t.Fatalf("NewOSDirBackend failed: %v", err)
	}

	entry, err := backend.Lookup("test.txt")
	if err != nil {
		t.Fatalf("Lookup failed: %v", err)
	}

	// Write at offset
	if err := entry.File.WriteAt(6, []byte("WORLD")); err != nil {
		t.Fatalf("WriteAt failed: %v", err)
	}

	// Verify
	result, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(result) != "hello WORLD" {
		t.Errorf("expected 'hello WORLD', got '%s'", string(result))
	}
}

func TestOsFileEntry_WriteAt_ReadOnly(t *testing.T) {
	dir := t.TempDir()
	content := []byte("hello world")
	filePath := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(filePath, content, 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	backend, err := NewOSDirBackend(dir, true) // read-only
	if err != nil {
		t.Fatalf("NewOSDirBackend failed: %v", err)
	}

	entry, err := backend.Lookup("test.txt")
	if err != nil {
		t.Fatalf("Lookup failed: %v", err)
	}

	// Write should fail in read-only mode
	err = entry.File.WriteAt(0, []byte("test"))
	if err == nil {
		t.Fatal("expected error for write in read-only mode")
	}
}

func TestOsFileEntry_Truncate(t *testing.T) {
	dir := t.TempDir()
	content := []byte("hello world, this is long content")
	filePath := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(filePath, content, 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	backend, err := NewOSDirBackend(dir, false)
	if err != nil {
		t.Fatalf("NewOSDirBackend failed: %v", err)
	}

	entry, err := backend.Lookup("test.txt")
	if err != nil {
		t.Fatalf("Lookup failed: %v", err)
	}

	// Truncate
	if err := entry.File.Truncate(5); err != nil {
		t.Fatalf("Truncate failed: %v", err)
	}

	// Verify
	result, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(result) != "hello" {
		t.Errorf("expected 'hello', got '%s'", string(result))
	}
}

func TestOsFileEntry_Truncate_ReadOnly(t *testing.T) {
	dir := t.TempDir()
	content := []byte("hello world")
	filePath := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(filePath, content, 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	backend, err := NewOSDirBackend(dir, true) // read-only
	if err != nil {
		t.Fatalf("NewOSDirBackend failed: %v", err)
	}

	entry, err := backend.Lookup("test.txt")
	if err != nil {
		t.Fatalf("Lookup failed: %v", err)
	}

	// Truncate should fail in read-only mode
	err = entry.File.Truncate(5)
	if err == nil {
		t.Fatal("expected error for truncate in read-only mode")
	}
}

func TestOsDirEntry_NestedLookup(t *testing.T) {
	dir := t.TempDir()

	// Create nested structure
	subdir := filepath.Join(dir, "level1", "level2")
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(subdir, "deep.txt"), []byte("deep content"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	backend, err := NewOSDirBackend(dir, false)
	if err != nil {
		t.Fatalf("NewOSDirBackend failed: %v", err)
	}

	// Lookup level1
	entry1, err := backend.Lookup("level1")
	if err != nil {
		t.Fatalf("Lookup level1 failed: %v", err)
	}
	if entry1.Dir == nil {
		t.Fatal("expected Dir for level1")
	}

	// Lookup level2 from level1
	entry2, err := entry1.Dir.Lookup("level2")
	if err != nil {
		t.Fatalf("Lookup level2 failed: %v", err)
	}
	if entry2.Dir == nil {
		t.Fatal("expected Dir for level2")
	}

	// Lookup deep.txt from level2
	entry3, err := entry2.Dir.Lookup("deep.txt")
	if err != nil {
		t.Fatalf("Lookup deep.txt failed: %v", err)
	}
	if entry3.File == nil {
		t.Fatal("expected File for deep.txt")
	}

	// Verify content
	data, err := entry3.File.ReadAt(0, 100)
	if err != nil {
		t.Fatalf("ReadAt failed: %v", err)
	}
	if string(data) != "deep content" {
		t.Errorf("expected 'deep content', got '%s'", string(data))
	}
}

func TestOsDirEntry_ReadDir(t *testing.T) {
	dir := t.TempDir()

	// Create structure
	subdir := filepath.Join(dir, "subdir")
	if err := os.Mkdir(subdir, 0755); err != nil {
		t.Fatalf("Mkdir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(subdir, "a.txt"), []byte("a"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(subdir, "b.txt"), []byte("b"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	backend, err := NewOSDirBackend(dir, false)
	if err != nil {
		t.Fatalf("NewOSDirBackend failed: %v", err)
	}

	entry, err := backend.Lookup("subdir")
	if err != nil {
		t.Fatalf("Lookup failed: %v", err)
	}

	entries, err := entry.Dir.ReadDir()
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(entries))
	}
}

// Interface compliance checks
func TestInterfaceCompliance(t *testing.T) {
	dir := t.TempDir()
	backend, err := NewOSDirBackend(dir, false)
	if err != nil {
		t.Fatalf("NewOSDirBackend failed: %v", err)
	}

	// OSDirBackend implements AbstractDir
	var _ AbstractDir = backend

	// Create file for file entry tests
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("test"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	entry, err := backend.Lookup("file.txt")
	if err != nil {
		t.Fatalf("Lookup failed: %v", err)
	}

	// osFileEntry implements AbstractFile
	var _ AbstractFile = entry.File

	// Create subdir for dir entry tests
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0755); err != nil {
		t.Fatalf("Mkdir failed: %v", err)
	}

	dirEntry, err := backend.Lookup("subdir")
	if err != nil {
		t.Fatalf("Lookup failed: %v", err)
	}

	// osDirEntry implements AbstractDir
	var _ AbstractDir = dirEntry.Dir

	// Create symlink for symlink entry tests
	if err := os.Symlink("file.txt", filepath.Join(dir, "link.txt")); err != nil {
		t.Fatalf("Symlink failed: %v", err)
	}

	symlinkEntry, err := backend.Lookup("link.txt")
	if err != nil {
		t.Fatalf("Lookup failed: %v", err)
	}

	// osSymlinkEntry implements AbstractSymlink
	var _ AbstractSymlink = symlinkEntry.Symlink
}
