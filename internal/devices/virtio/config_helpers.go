package virtio

import "encoding/binary"

// ReadConfigWindow reads a 4-byte window from config bytes at the given offset.
// Returns (value, handled, error). If offset is outside the config bytes range, returns (0, true, nil).
// Use this for devices with read-only config space.
// Accepts both absolute offsets (>= VIRTIO_MMIO_CONFIG) and relative offsets (< VIRTIO_MMIO_CONFIG).
func ReadConfigWindow(offset uint64, configBytes []byte) (uint32, bool, error) {
	// Handle both absolute and relative offsets.
	// The adapter path provides relative offsets (already subtracted VIRTIO_MMIO_CONFIG),
	// while the direct handler path provides absolute offsets.
	var rel uint64
	if offset >= VIRTIO_MMIO_CONFIG {
		rel = offset - VIRTIO_MMIO_CONFIG
	} else {
		rel = offset
	}
	if int(rel) >= len(configBytes) {
		return 0, true, nil
	}
	var buf [4]byte
	copy(buf[:], configBytes[rel:])
	return binary.LittleEndian.Uint32(buf[:]), true, nil
}

// WriteConfigNoop handles write to read-only config space.
// Returns (handled, error). Always returns (true, nil) since config writes are always handled (as no-ops).
// Use this for devices with read-only config space.
// Accepts both absolute offsets (>= VIRTIO_MMIO_CONFIG) and relative offsets (< VIRTIO_MMIO_CONFIG).
func WriteConfigNoop(offset uint64) (bool, error) {
	return true, nil
}
