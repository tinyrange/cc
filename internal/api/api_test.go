package api

import (
	"bytes"
	"context"
	"io/fs"
	"testing"
	"time"

	"github.com/tinyrange/cc/internal/vfs"
)

func TestNewOCIClient(t *testing.T) {
	client, err := NewOCIClient()
	if err != nil {
		t.Fatalf("NewOCIClient() error = %v", err)
	}
	if client == nil {
		t.Fatal("NewOCIClient() returned nil")
	}
}

func TestGenerateID(t *testing.T) {
	id1 := generateID()
	id2 := generateID()

	if id1 == "" {
		t.Error("generateID() returned empty string")
	}
	if id1 == id2 {
		t.Error("generateID() returned same ID twice")
	}
}

// TestCoWReadAfterWrite tests that writing to a file through the API
// and then reading it back returns the written content (not the original).
func TestCoWReadAfterWrite(t *testing.T) {
	// Create a backend with abstract files
	backend := vfs.NewVirtioFsBackendWithAbstract()

	// Create a simple in-memory abstract dir to serve as root
	rootDir := &testAbstractDir{
		entries: []vfs.AbstractDirEntry{
			{Name: "testfile.txt", IsDir: false, Mode: 0644, Size: 13},
		},
		files: map[string]vfs.AbstractFile{
			"testfile.txt": &vfs.BytesFile{},
		},
	}
	// Initialize the BytesFile with original content
	originalContent := []byte("original data")
	rootDir.files["testfile.txt"] = vfs.NewBytesFile(originalContent, 0644)

	if err := backend.SetAbstractRoot(rootDir); err != nil {
		t.Fatalf("SetAbstractRoot() error = %v", err)
	}

	// Create a mock instance to hold the fsBackend
	inst := &instance{
		fsBackend: backend,
	}

	// Create the instanceFS
	ifs := &instanceFS{
		inst: inst,
		ctx:  context.Background(),
	}

	// Verify we can read the original content
	data, err := ifs.ReadFile("/testfile.txt")
	if err != nil {
		t.Fatalf("ReadFile() initial read error = %v", err)
	}
	if !bytes.Equal(data, originalContent) {
		t.Errorf("ReadFile() initial = %q, want %q", data, originalContent)
	}

	// Write new content
	newContent := []byte("modified content from CoW layer")
	if err := ifs.WriteFile("/testfile.txt", newContent, 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// Read it back - should get the new content
	data, err = ifs.ReadFile("/testfile.txt")
	if err != nil {
		t.Fatalf("ReadFile() after write error = %v", err)
	}
	if !bytes.Equal(data, newContent) {
		t.Errorf("ReadFile() after write = %q, want %q", data, newContent)
	}

	// Verify the original source still has original data
	// (The abstract file should be unchanged since CoW copies on write)
	origData, _ := rootDir.files["testfile.txt"].ReadAt(0, 1024)
	if !bytes.Equal(origData, originalContent) {
		t.Errorf("Original file was modified = %q, want %q", origData, originalContent)
	}
}

// TestCoWInstanceIsolation tests that two instances from the same source
// have independent CoW layers.
func TestCoWInstanceIsolation(t *testing.T) {
	// Create shared abstract root
	rootDir := &testAbstractDir{
		entries: []vfs.AbstractDirEntry{
			{Name: "shared.txt", IsDir: false, Mode: 0644, Size: 12},
		},
		files: map[string]vfs.AbstractFile{
			"shared.txt": vfs.NewBytesFile([]byte("shared data"), 0644),
		},
	}

	// Create two separate backends from the same abstract root
	backend1 := vfs.NewVirtioFsBackendWithAbstract()
	if err := backend1.SetAbstractRoot(rootDir); err != nil {
		t.Fatalf("SetAbstractRoot() for backend1 error = %v", err)
	}

	backend2 := vfs.NewVirtioFsBackendWithAbstract()
	if err := backend2.SetAbstractRoot(rootDir); err != nil {
		t.Fatalf("SetAbstractRoot() for backend2 error = %v", err)
	}

	// Create two mock instances
	inst1 := &instance{fsBackend: backend1}
	inst2 := &instance{fsBackend: backend2}

	ifs1 := &instanceFS{inst: inst1, ctx: context.Background()}
	ifs2 := &instanceFS{inst: inst2, ctx: context.Background()}

	// Write different content to each instance
	content1 := []byte("instance 1 content")
	content2 := []byte("instance 2 different content")

	if err := ifs1.WriteFile("/shared.txt", content1, 0644); err != nil {
		t.Fatalf("WriteFile() to inst1 error = %v", err)
	}
	if err := ifs2.WriteFile("/shared.txt", content2, 0644); err != nil {
		t.Fatalf("WriteFile() to inst2 error = %v", err)
	}

	// Verify each instance sees its own content
	data1, err := ifs1.ReadFile("/shared.txt")
	if err != nil {
		t.Fatalf("ReadFile() from inst1 error = %v", err)
	}
	if !bytes.Equal(data1, content1) {
		t.Errorf("inst1.ReadFile() = %q, want %q", data1, content1)
	}

	data2, err := ifs2.ReadFile("/shared.txt")
	if err != nil {
		t.Fatalf("ReadFile() from inst2 error = %v", err)
	}
	if !bytes.Equal(data2, content2) {
		t.Errorf("inst2.ReadFile() = %q, want %q", data2, content2)
	}
}

// testAbstractDir implements vfs.AbstractDir for testing.
type testAbstractDir struct {
	entries []vfs.AbstractDirEntry
	files   map[string]vfs.AbstractFile
	dirs    map[string]vfs.AbstractDir
}

func (d *testAbstractDir) ReadDir() ([]vfs.AbstractDirEntry, error) {
	return d.entries, nil
}

func (d *testAbstractDir) Lookup(name string) (vfs.AbstractEntry, error) {
	if f, ok := d.files[name]; ok {
		return vfs.AbstractEntry{File: f}, nil
	}
	if dir, ok := d.dirs[name]; ok {
		return vfs.AbstractEntry{Dir: dir}, nil
	}
	return vfs.AbstractEntry{}, fs.ErrNotExist
}

func (d *testAbstractDir) Stat() fs.FileMode {
	return 0755
}

func (d *testAbstractDir) ModTime() time.Time {
	return time.Now()
}
