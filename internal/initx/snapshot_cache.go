package initx

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/tinyrange/cc/internal/hv"
)

// SnapshotIO provides platform-specific snapshot serialization.
// This interface allows the cache to work with different hypervisor backends.
type SnapshotIO interface {
	SaveSnapshot(path string, snap hv.Snapshot) error
	LoadSnapshot(path string) (hv.Snapshot, error)
}

// SnapshotCache manages boot snapshots in a cache directory.
type SnapshotCache struct {
	cacheDir   string
	snapshotIO SnapshotIO
}

// NewSnapshotCache creates a cache manager for the given directory.
func NewSnapshotCache(cacheDir string, io SnapshotIO) *SnapshotCache {
	return &SnapshotCache{cacheDir: cacheDir, snapshotIO: io}
}

// GetSnapshotPath returns the path to the snapshot file for a given config hash.
func (c *SnapshotCache) GetSnapshotPath(configHash hv.VMConfigHash) string {
	return filepath.Join(c.cacheDir, hex.EncodeToString(configHash[:])+".snap")
}

// HasValidSnapshot checks if a valid snapshot exists.
// Returns true if snapshot exists and is newer than referenceTime (typically kernel mod time).
func (c *SnapshotCache) HasValidSnapshot(configHash hv.VMConfigHash, referenceTime time.Time) bool {
	snapPath := c.GetSnapshotPath(configHash)
	info, err := os.Stat(snapPath)
	if err != nil {
		return false
	}
	// Snapshot must be newer than the reference time (usually kernel mod time)
	return info.ModTime().After(referenceTime)
}

// EnsureDir creates the cache directory if it doesn't exist.
func (c *SnapshotCache) EnsureDir() error {
	return os.MkdirAll(c.cacheDir, 0755)
}

// InvalidateCache removes the cached snapshot for a config hash.
func (c *SnapshotCache) InvalidateCache(configHash hv.VMConfigHash) error {
	snapPath := c.GetSnapshotPath(configHash)
	err := os.Remove(snapPath)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// InvalidateAll removes all cached snapshots.
func (c *SnapshotCache) InvalidateAll() error {
	entries, err := os.ReadDir(c.cacheDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".snap" {
			_ = os.Remove(filepath.Join(c.cacheDir, entry.Name()))
		}
	}
	return nil
}

// SaveSnapshot saves a snapshot to the cache.
func (c *SnapshotCache) SaveSnapshot(configHash hv.VMConfigHash, snap hv.Snapshot) error {
	if c.snapshotIO == nil {
		return fmt.Errorf("snapshot IO not configured")
	}
	if err := c.EnsureDir(); err != nil {
		return fmt.Errorf("ensure cache dir: %w", err)
	}
	snapPath := c.GetSnapshotPath(configHash)
	return c.snapshotIO.SaveSnapshot(snapPath, snap)
}

// LoadSnapshot loads a snapshot from the cache.
func (c *SnapshotCache) LoadSnapshot(configHash hv.VMConfigHash) (hv.Snapshot, error) {
	if c.snapshotIO == nil {
		return nil, fmt.Errorf("snapshot IO not configured")
	}
	snapPath := c.GetSnapshotPath(configHash)
	return c.snapshotIO.LoadSnapshot(snapPath)
}
