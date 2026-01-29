package api

import (
	"os"
	"path/filepath"
)

// CacheDir represents a cache directory configuration.
// It provides a unified way to configure cache directories for both
// OCIClient and Instance, ensuring they share the same cache location.
type CacheDir struct {
	path string
}

// NewCacheDir creates a cache directory config.
// If path is empty, uses the platform-specific default cache directory.
func NewCacheDir(path string) (*CacheDir, error) {
	if path == "" {
		// Use platform-specific default
		userCacheDir, err := os.UserCacheDir()
		if err != nil {
			return nil, &Error{Op: "cache", Err: err}
		}
		path = filepath.Join(userCacheDir, "cc")
	}

	// Ensure the directory exists
	if err := os.MkdirAll(path, 0755); err != nil {
		return nil, &Error{Op: "cache", Path: path, Err: err}
	}

	return &CacheDir{path: path}, nil
}

// Path returns the resolved cache directory path.
func (c *CacheDir) Path() string {
	return c.path
}

// OCIPath returns the path for OCI image cache.
func (c *CacheDir) OCIPath() string {
	return filepath.Join(c.path, "oci")
}

// QEMUPath returns the path for QEMU emulation binaries cache.
func (c *CacheDir) QEMUPath() string {
	return filepath.Join(c.path, "qemu")
}

// SnapshotPath returns the path for filesystem snapshot cache.
func (c *CacheDir) SnapshotPath() string {
	return filepath.Join(c.path, "snapshots")
}
