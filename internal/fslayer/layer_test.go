package fslayer

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteLayerEmpty(t *testing.T) {
	dir := t.TempDir()

	data := &LayerData{}
	layer, err := WriteLayer(data, dir)
	if err != nil {
		t.Fatalf("WriteLayer failed: %v", err)
	}

	if layer.Hash == "" {
		t.Error("Expected non-empty hash")
	}

	if _, err := os.Stat(layer.IndexPath); err != nil {
		t.Errorf("Index file not found: %v", err)
	}
	if _, err := os.Stat(layer.ContentsPath); err != nil {
		t.Errorf("Contents file not found: %v", err)
	}
}

func TestWriteLayerWithEntries(t *testing.T) {
	dir := t.TempDir()

	data := &LayerData{
		Entries: []LayerEntry{
			{
				Path:    "/test/file.txt",
				Kind:    LayerEntryRegular,
				Mode:    0o644,
				UID:     1000,
				GID:     1000,
				ModTime: time.Now(),
				Size:    12,
				Data:    []byte("hello world\n"),
			},
			{
				Path:    "/test/dir",
				Kind:    LayerEntryDirectory,
				Mode:    fs.ModeDir | 0o755,
				UID:     0,
				GID:     0,
				ModTime: time.Now(),
			},
			{
				Path:    "/test/link",
				Kind:    LayerEntrySymlink,
				Mode:    fs.ModeSymlink | 0o777,
				ModTime: time.Now(),
				Data:    []byte("file.txt"),
			},
			{
				Path:    "/test/deleted",
				Kind:    LayerEntryDeleted,
				ModTime: time.Now(),
			},
		},
	}

	layer, err := WriteLayer(data, dir)
	if err != nil {
		t.Fatalf("WriteLayer failed: %v", err)
	}

	// Verify files exist
	if _, err := os.Stat(layer.IndexPath); err != nil {
		t.Errorf("Index file not found: %v", err)
	}
	if _, err := os.Stat(layer.ContentsPath); err != nil {
		t.Errorf("Contents file not found: %v", err)
	}

	// Hash should be consistent
	hash1 := layer.Hash

	// Write same data again
	layer2, err := WriteLayer(data, dir)
	if err != nil {
		t.Fatalf("WriteLayer (2nd) failed: %v", err)
	}

	// Content-addressable: should return same hash
	if layer2.Hash != hash1 {
		t.Errorf("Expected consistent hash, got %s vs %s", hash1, layer2.Hash)
	}
}

func TestReadLayer(t *testing.T) {
	dir := t.TempDir()

	data := &LayerData{
		Entries: []LayerEntry{
			{
				Path:    "/test.txt",
				Kind:    LayerEntryRegular,
				Mode:    0o644,
				ModTime: time.Now(),
				Size:    5,
				Data:    []byte("test\n"),
			},
		},
	}

	written, err := WriteLayer(data, dir)
	if err != nil {
		t.Fatalf("WriteLayer failed: %v", err)
	}

	// Read it back
	read, err := ReadLayer(dir, written.Hash)
	if err != nil {
		t.Fatalf("ReadLayer failed: %v", err)
	}

	if read.Hash != written.Hash {
		t.Errorf("Hash mismatch: %s vs %s", read.Hash, written.Hash)
	}
	if read.IndexPath != written.IndexPath {
		t.Errorf("IndexPath mismatch: %s vs %s", read.IndexPath, written.IndexPath)
	}
}

func TestReadLayerNotFound(t *testing.T) {
	dir := t.TempDir()

	_, err := ReadLayer(dir, "nonexistent-hash")
	if err == nil {
		t.Error("Expected error for nonexistent layer")
	}
}

func TestWriteLayerSubdir(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "nested", "layers")

	data := &LayerData{
		Entries: []LayerEntry{
			{
				Path:    "/file.txt",
				Kind:    LayerEntryRegular,
				Mode:    0o644,
				ModTime: time.Now(),
				Size:    4,
				Data:    []byte("test"),
			},
		},
	}

	layer, err := WriteLayer(data, subdir)
	if err != nil {
		t.Fatalf("WriteLayer to nested dir failed: %v", err)
	}

	if _, err := os.Stat(layer.IndexPath); err != nil {
		t.Errorf("Index file not found: %v", err)
	}
}
