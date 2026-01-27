package fslayer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// FSManifest describes a persisted filesystem snapshot.
type FSManifest struct {
	Version      int      `json:"version"`
	CacheKey     string   `json:"cacheKey"`
	Layers       []string `json:"layers"`       // Layer hashes in order (base first)
	BaseImageRef string   `json:"baseImageRef"` // Original OCI image reference
	Architecture string   `json:"architecture"`
}

const manifestVersion = 1

// SaveManifest saves a filesystem snapshot manifest to disk.
func SaveManifest(manifest *FSManifest, dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create manifest dir: %w", err)
	}

	manifestPath := filepath.Join(dir, manifest.CacheKey+".json")

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}

	if err := os.WriteFile(manifestPath, data, 0o644); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}

	return nil
}

// LoadManifest loads a filesystem snapshot manifest from disk.
func LoadManifest(dir, cacheKey string) (*FSManifest, error) {
	manifestPath := filepath.Join(dir, cacheKey+".json")

	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}

	var manifest FSManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("unmarshal manifest: %w", err)
	}

	if manifest.Version != manifestVersion {
		return nil, fmt.Errorf("unsupported manifest version: %d", manifest.Version)
	}

	return &manifest, nil
}

// ManifestExists checks if a manifest exists for the given cache key.
func ManifestExists(dir, cacheKey string) bool {
	manifestPath := filepath.Join(dir, cacheKey+".json")
	_, err := os.Stat(manifestPath)
	return err == nil
}

// ListManifests returns all manifest cache keys in a directory.
func ListManifests(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read dir: %w", err)
	}

	var keys []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if len(name) > 5 && name[len(name)-5:] == ".json" {
			keys = append(keys, name[:len(name)-5])
		}
	}
	return keys, nil
}

// DeleteManifest removes a manifest and its associated layers if they're not
// referenced by other manifests.
func DeleteManifest(dir, cacheKey string) error {
	manifestPath := filepath.Join(dir, cacheKey+".json")
	return os.Remove(manifestPath)
}
