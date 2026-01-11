package hv

import (
	"crypto/sha256"
	"encoding/binary"
)

// VMConfigHash represents a hash of VM configuration for cache validation.
// Two snapshots can only be restored if they have the same config hash.
type VMConfigHash [32]byte

// DeviceConfig captures device configuration for hashing.
type DeviceConfig struct {
	ID      string
	Base    uint64
	Size    uint64
	IRQLine uint32
}

// ComputeConfigHash computes a deterministic hash of VM configuration parameters.
// This is used to validate that a cached snapshot matches the current VM configuration.
func ComputeConfigHash(arch CpuArchitecture, memSize, memBase uint64,
	cpuCount int, deviceConfigs []DeviceConfig) VMConfigHash {
	h := sha256.New()

	// Architecture
	h.Write([]byte(arch))
	h.Write([]byte{0}) // null terminator

	// Memory configuration
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], memSize)
	h.Write(buf[:])
	binary.LittleEndian.PutUint64(buf[:], memBase)
	h.Write(buf[:])

	// CPU count
	binary.LittleEndian.PutUint64(buf[:], uint64(cpuCount))
	h.Write(buf[:])

	// Device configurations (order matters)
	for _, dc := range deviceConfigs {
		h.Write([]byte(dc.ID))
		h.Write([]byte{0}) // null terminator
		binary.LittleEndian.PutUint64(buf[:], dc.Base)
		h.Write(buf[:])
		binary.LittleEndian.PutUint64(buf[:], dc.Size)
		h.Write(buf[:])
		binary.LittleEndian.PutUint32(buf[:4], dc.IRQLine)
		h.Write(buf[:4])
	}

	var result VMConfigHash
	copy(result[:], h.Sum(nil))
	return result
}

// String returns a hex string representation of the hash.
func (h VMConfigHash) String() string {
	const hexChars = "0123456789abcdef"
	result := make([]byte, 64)
	for i, b := range h {
		result[i*2] = hexChars[b>>4]
		result[i*2+1] = hexChars[b&0x0f]
	}
	return string(result)
}
