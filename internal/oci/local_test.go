package oci

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFromDir(t *testing.T) {
	// Create a temp directory with minimal image structure
	dir := t.TempDir()

	// Create a minimal config.json (no layers)
	cfg := RuntimeConfig{
		Layers:     []string{},
		Env:        []string{"PATH=/usr/bin"},
		Entrypoint: []string{"/bin/sh"},
		Cmd:        []string{"-c", "echo hello"},
		WorkingDir: "/",
	}

	cfgData, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("failed to marshal config: %v", err)
	}

	cfgPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(cfgPath, cfgData, 0o644); err != nil {
		t.Fatalf("failed to write config.json: %v", err)
	}

	// Load the image
	img, err := LoadFromDir(dir)
	if err != nil {
		t.Fatalf("LoadFromDir failed: %v", err)
	}

	if img.Dir != dir {
		t.Errorf("img.Dir = %q, want %q", img.Dir, dir)
	}

	if len(img.Layers) != 0 {
		t.Errorf("len(img.Layers) = %d, want 0", len(img.Layers))
	}

	if len(img.Config.Env) != 1 || img.Config.Env[0] != "PATH=/usr/bin" {
		t.Errorf("img.Config.Env = %v, want [PATH=/usr/bin]", img.Config.Env)
	}

	if len(img.Config.Entrypoint) != 1 || img.Config.Entrypoint[0] != "/bin/sh" {
		t.Errorf("img.Config.Entrypoint = %v, want [/bin/sh]", img.Config.Entrypoint)
	}

	// Test Command() method
	cmd := img.Command(nil)
	if len(cmd) != 3 {
		t.Errorf("img.Command(nil) length = %d, want 3", len(cmd))
	}
	if cmd[0] != "/bin/sh" {
		t.Errorf("cmd[0] = %q, want %q", cmd[0], "/bin/sh")
	}
}

func TestLoadFromDirNotExists(t *testing.T) {
	_, err := LoadFromDir("/nonexistent/path")
	if err == nil {
		t.Error("LoadFromDir on nonexistent path should fail")
	}
}

func TestLoadFromDirNoConfig(t *testing.T) {
	dir := t.TempDir()
	_, err := LoadFromDir(dir)
	if err == nil {
		t.Error("LoadFromDir on empty dir should fail")
	}
}
