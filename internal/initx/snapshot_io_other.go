//go:build !(darwin && arm64) && !(linux && arm64) && !(linux && amd64) && !(windows && arm64) && !(windows && amd64)

package initx

// GetSnapshotIO returns nil on unsupported platforms.
func GetSnapshotIO() SnapshotIO {
	return nil
}
