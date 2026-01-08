//go:build !darwin || !arm64

package initx

// GetSnapshotIO returns nil on unsupported platforms.
func GetSnapshotIO() SnapshotIO {
	return nil
}
