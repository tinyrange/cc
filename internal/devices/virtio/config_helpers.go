package virtio

import "encoding/binary"

// ReadConfigWindow reads a 4-byte window from config bytes at the given offset.
// Returns (value, handled, error). If offset is not in config range, returns (0, false, nil).
// Use this for devices with read-only config space.
func ReadConfigWindow(offset uint64, configBytes []byte) (uint32, bool, error) {
	if offset < VIRTIO_MMIO_CONFIG {
		return 0, false, nil
	}
	rel := offset - VIRTIO_MMIO_CONFIG
	if int(rel) >= len(configBytes) {
		return 0, true, nil
	}
	var buf [4]byte
	copy(buf[:], configBytes[rel:])
	return binary.LittleEndian.Uint32(buf[:]), true, nil
}

// WriteConfigNoop handles write to read-only config space.
// Returns (handled, error). If offset is not in config range, returns (false, nil).
// Use this for devices with read-only config space.
func WriteConfigNoop(offset uint64) (bool, error) {
	if offset < VIRTIO_MMIO_CONFIG {
		return false, nil
	}
	return true, nil
}
