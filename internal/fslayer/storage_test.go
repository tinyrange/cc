package fslayer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveAndLoadManifest(t *testing.T) {
	dir := t.TempDir()

	manifest := &FSManifest{
		Version:      1,
		CacheKey:     "test-cache-key-12345678",
		Layers:       []string{"layer1hash", "layer2hash"},
		BaseImageRef: "alpine:latest",
		Architecture: "amd64",
	}

	// Save
	err := SaveManifest(manifest, dir)
	if err != nil {
		t.Fatalf("SaveManifest failed: %v", err)
	}

	// Verify file exists
	manifestPath := filepath.Join(dir, manifest.CacheKey+".json")
	if _, err := os.Stat(manifestPath); err != nil {
		t.Errorf("Manifest file not found: %v", err)
	}

	// Load
	loaded, err := LoadManifest(dir, manifest.CacheKey)
	if err != nil {
		t.Fatalf("LoadManifest failed: %v", err)
	}

	// Verify fields
	if loaded.Version != manifest.Version {
		t.Errorf("Version mismatch: %d != %d", loaded.Version, manifest.Version)
	}
	if loaded.CacheKey != manifest.CacheKey {
		t.Errorf("CacheKey mismatch: %s != %s", loaded.CacheKey, manifest.CacheKey)
	}
	if loaded.BaseImageRef != manifest.BaseImageRef {
		t.Errorf("BaseImageRef mismatch: %s != %s", loaded.BaseImageRef, manifest.BaseImageRef)
	}
	if loaded.Architecture != manifest.Architecture {
		t.Errorf("Architecture mismatch: %s != %s", loaded.Architecture, manifest.Architecture)
	}
	if len(loaded.Layers) != len(manifest.Layers) {
		t.Errorf("Layers length mismatch: %d != %d", len(loaded.Layers), len(manifest.Layers))
	}
	for i, l := range loaded.Layers {
		if l != manifest.Layers[i] {
			t.Errorf("Layer %d mismatch: %s != %s", i, l, manifest.Layers[i])
		}
	}
}

func TestLoadManifestNotFound(t *testing.T) {
	dir := t.TempDir()

	_, err := LoadManifest(dir, "nonexistent-key")
	if err == nil {
		t.Error("Expected error for nonexistent manifest")
	}
}

func TestManifestExists(t *testing.T) {
	dir := t.TempDir()
	cacheKey := "test-key-abc123"

	// Should not exist initially
	if ManifestExists(dir, cacheKey) {
		t.Error("Manifest should not exist initially")
	}

	// Save manifest
	manifest := &FSManifest{
		Version:  1,
		CacheKey: cacheKey,
		Layers:   []string{},
	}
	if err := SaveManifest(manifest, dir); err != nil {
		t.Fatalf("SaveManifest failed: %v", err)
	}

	// Should exist now
	if !ManifestExists(dir, cacheKey) {
		t.Error("Manifest should exist after save")
	}
}

func TestListManifests(t *testing.T) {
	dir := t.TempDir()

	// Initially empty
	keys, err := ListManifests(dir)
	if err != nil {
		t.Fatalf("ListManifests failed: %v", err)
	}
	if len(keys) != 0 {
		t.Errorf("Expected 0 manifests, got %d", len(keys))
	}

	// Add some manifests
	for _, key := range []string{"key1", "key2", "key3"} {
		manifest := &FSManifest{
			Version:  1,
			CacheKey: key,
			Layers:   []string{},
		}
		if err := SaveManifest(manifest, dir); err != nil {
			t.Fatalf("SaveManifest failed: %v", err)
		}
	}

	// List should return 3
	keys, err = ListManifests(dir)
	if err != nil {
		t.Fatalf("ListManifests failed: %v", err)
	}
	if len(keys) != 3 {
		t.Errorf("Expected 3 manifests, got %d", len(keys))
	}

	// Check all keys are present
	keyMap := make(map[string]bool)
	for _, k := range keys {
		keyMap[k] = true
	}
	for _, expected := range []string{"key1", "key2", "key3"} {
		if !keyMap[expected] {
			t.Errorf("Missing expected key: %s", expected)
		}
	}
}

func TestListManifestsNonexistentDir(t *testing.T) {
	keys, err := ListManifests("/nonexistent/path/that/does/not/exist")
	if err != nil {
		t.Fatalf("ListManifests should not fail for nonexistent dir: %v", err)
	}
	if keys != nil && len(keys) != 0 {
		t.Errorf("Expected nil or empty slice, got %v", keys)
	}
}

func TestDeleteManifest(t *testing.T) {
	dir := t.TempDir()
	cacheKey := "delete-test-key"

	// Save manifest
	manifest := &FSManifest{
		Version:  1,
		CacheKey: cacheKey,
		Layers:   []string{},
	}
	if err := SaveManifest(manifest, dir); err != nil {
		t.Fatalf("SaveManifest failed: %v", err)
	}

	// Verify it exists
	if !ManifestExists(dir, cacheKey) {
		t.Fatal("Manifest should exist after save")
	}

	// Delete
	if err := DeleteManifest(dir, cacheKey); err != nil {
		t.Fatalf("DeleteManifest failed: %v", err)
	}

	// Verify it's gone
	if ManifestExists(dir, cacheKey) {
		t.Error("Manifest should not exist after delete")
	}
}

func TestSaveManifestCreatesDir(t *testing.T) {
	baseDir := t.TempDir()
	nestedDir := filepath.Join(baseDir, "nested", "manifests")

	manifest := &FSManifest{
		Version:  1,
		CacheKey: "nested-test",
		Layers:   []string{},
	}

	// Should create nested directories
	if err := SaveManifest(manifest, nestedDir); err != nil {
		t.Fatalf("SaveManifest should create nested dirs: %v", err)
	}

	if !ManifestExists(nestedDir, manifest.CacheKey) {
		t.Error("Manifest should exist in nested dir")
	}
}
