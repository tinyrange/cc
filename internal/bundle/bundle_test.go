package bundle

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsBundleDir(t *testing.T) {
	// Create a temp directory
	dir := t.TempDir()

	// Initially it should not be a bundle dir
	if IsBundleDir(dir) {
		t.Error("empty dir should not be a bundle dir")
	}

	// Create ccbundle.yaml
	yamlPath := filepath.Join(dir, "ccbundle.yaml")
	if err := os.WriteFile(yamlPath, []byte("version: 1\nname: test\n"), 0o644); err != nil {
		t.Fatalf("failed to write yaml: %v", err)
	}

	// Now it should be a bundle dir
	if !IsBundleDir(dir) {
		t.Error("dir with ccbundle.yaml should be a bundle dir")
	}
}

func TestLoadMetadata(t *testing.T) {
	dir := t.TempDir()

	yamlContent := `version: 1
name: "Test Bundle"
description: "A test bundle"
icon: icon.png
boot:
  imageDir: image
  command:
    - /bin/sh
    - -c
    - echo hello
  cpus: 2
  memoryMB: 2048
  network: true
  exec: false
  dmesg: true
`

	yamlPath := filepath.Join(dir, "ccbundle.yaml")
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0o644); err != nil {
		t.Fatalf("failed to write yaml: %v", err)
	}

	meta, err := LoadMetadata(dir)
	if err != nil {
		t.Fatalf("LoadMetadata failed: %v", err)
	}

	if meta.Version != 1 {
		t.Errorf("Version = %d, want 1", meta.Version)
	}
	if meta.Name != "Test Bundle" {
		t.Errorf("Name = %q, want %q", meta.Name, "Test Bundle")
	}
	if meta.Description != "A test bundle" {
		t.Errorf("Description = %q, want %q", meta.Description, "A test bundle")
	}
	if meta.Icon != "icon.png" {
		t.Errorf("Icon = %q, want %q", meta.Icon, "icon.png")
	}
	if meta.Boot.ImageDir != "image" {
		t.Errorf("Boot.ImageDir = %q, want %q", meta.Boot.ImageDir, "image")
	}
	if len(meta.Boot.Command) != 3 {
		t.Errorf("Boot.Command length = %d, want 3", len(meta.Boot.Command))
	}
	if meta.Boot.CPUs != 2 {
		t.Errorf("Boot.CPUs = %d, want 2", meta.Boot.CPUs)
	}
	if meta.Boot.MemoryMB != 2048 {
		t.Errorf("Boot.MemoryMB = %d, want 2048", meta.Boot.MemoryMB)
	}
	if meta.Boot.Exec {
		t.Error("Boot.Exec should be false")
	}
	if !meta.Boot.Dmesg {
		t.Error("Boot.Dmesg should be true")
	}
}

func TestWriteTemplate(t *testing.T) {
	dir := t.TempDir()

	meta := Metadata{
		Version:     1,
		Name:        "{{name}}",
		Description: "{{description}}",
		Boot: BootConfig{
			ImageDir: "image",
			CPUs:     1,
			MemoryMB: 1024,
		},
	}

	if err := WriteTemplate(dir, meta); err != nil {
		t.Fatalf("WriteTemplate failed: %v", err)
	}

	// Verify the file was created
	if !IsBundleDir(dir) {
		t.Error("WriteTemplate should create ccbundle.yaml")
	}

	// Load it back
	loaded, err := LoadMetadata(dir)
	if err != nil {
		t.Fatalf("LoadMetadata after WriteTemplate failed: %v", err)
	}

	if loaded.Version != 1 {
		t.Errorf("loaded.Version = %d, want 1", loaded.Version)
	}
	if loaded.Name != "{{name}}" {
		t.Errorf("loaded.Name = %q, want %q", loaded.Name, "{{name}}")
	}
	if loaded.Boot.CPUs != 1 {
		t.Errorf("loaded.Boot.CPUs = %d, want 1", loaded.Boot.CPUs)
	}
}
