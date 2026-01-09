//go:build linux

package kvm

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/gob"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/tinyrange/cc/internal/hv"
)

// SaveSnapshot writes a KVM snapshot to the specified file path.
func SaveSnapshot(path string, snap hv.Snapshot) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer f.Close()

	if err := writeSnapshot(f, snap); err != nil {
		return fmt.Errorf("write snapshot: %w", err)
	}

	return nil
}

// LoadSnapshot reads a KVM snapshot from the specified file path.
func LoadSnapshot(path string) (hv.Snapshot, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	snap, err := readSnapshot(f)
	if err != nil {
		return nil, fmt.Errorf("read snapshot: %w", err)
	}

	return snap, nil
}

// readSnapshot reads a KVM snapshot from a reader (architecture-agnostic entry point).
func readSnapshot(r io.Reader) (hv.Snapshot, error) {
	// Read header
	var magic, version, arch, flags uint32
	if err := binary.Read(r, binary.LittleEndian, &magic); err != nil {
		return nil, fmt.Errorf("read magic: %w", err)
	}
	if err := binary.Read(r, binary.LittleEndian, &version); err != nil {
		return nil, fmt.Errorf("read version: %w", err)
	}
	if err := binary.Read(r, binary.LittleEndian, &arch); err != nil {
		return nil, fmt.Errorf("read arch: %w", err)
	}
	if err := binary.Read(r, binary.LittleEndian, &flags); err != nil {
		return nil, fmt.Errorf("read flags: %w", err)
	}

	if magic != hv.SnapshotMagic {
		return nil, fmt.Errorf("invalid magic: expected %#x, got %#x", hv.SnapshotMagic, magic)
	}
	if version != hv.SnapshotVersion {
		return nil, fmt.Errorf("unsupported version: %d", version)
	}
	_ = flags // reserved

	cpuArch := hv.SnapshotArchToArch(arch)
	return readSnapshotBody(r, cpuArch)
}

// Common helpers used by both architectures

func writeClockData(w io.Writer, clock *kvmClockData) error {
	if err := binary.Write(w, binary.LittleEndian, clock.Clock); err != nil {
		return fmt.Errorf("write clock: %w", err)
	}
	if err := binary.Write(w, binary.LittleEndian, clock.Flags); err != nil {
		return fmt.Errorf("write flags: %w", err)
	}
	if err := binary.Write(w, binary.LittleEndian, clock.Realtime); err != nil {
		return fmt.Errorf("write realtime: %w", err)
	}
	if err := binary.Write(w, binary.LittleEndian, clock.HostTSC); err != nil {
		return fmt.Errorf("write host_tsc: %w", err)
	}
	return nil
}

func readClockData(r io.Reader) (kvmClockData, error) {
	var clock kvmClockData
	if err := binary.Read(r, binary.LittleEndian, &clock.Clock); err != nil {
		return clock, fmt.Errorf("read clock: %w", err)
	}
	if err := binary.Read(r, binary.LittleEndian, &clock.Flags); err != nil {
		return clock, fmt.Errorf("read flags: %w", err)
	}
	if err := binary.Read(r, binary.LittleEndian, &clock.Realtime); err != nil {
		return clock, fmt.Errorf("read realtime: %w", err)
	}
	if err := binary.Read(r, binary.LittleEndian, &clock.HostTSC); err != nil {
		return clock, fmt.Errorf("read host_tsc: %w", err)
	}
	return clock, nil
}

func writeCompressedMemory(w io.Writer, memory []byte) error {
	var compressedBuf bytes.Buffer
	gzw := gzip.NewWriter(&compressedBuf)
	if _, err := gzw.Write(memory); err != nil {
		gzw.Close()
		return fmt.Errorf("compress memory: %w", err)
	}
	if err := gzw.Close(); err != nil {
		return fmt.Errorf("close gzip compressor: %w", err)
	}

	if err := binary.Write(w, binary.LittleEndian, uint64(len(memory))); err != nil {
		return fmt.Errorf("write uncompressed size: %w", err)
	}
	if err := binary.Write(w, binary.LittleEndian, uint64(compressedBuf.Len())); err != nil {
		return fmt.Errorf("write compressed size: %w", err)
	}
	if _, err := w.Write(compressedBuf.Bytes()); err != nil {
		return fmt.Errorf("write compressed data: %w", err)
	}

	return nil
}

func readCompressedMemory(r io.Reader) ([]byte, error) {
	var uncompressedSize, compressedSize uint64
	if err := binary.Read(r, binary.LittleEndian, &uncompressedSize); err != nil {
		return nil, fmt.Errorf("read uncompressed size: %w", err)
	}
	if err := binary.Read(r, binary.LittleEndian, &compressedSize); err != nil {
		return nil, fmt.Errorf("read compressed size: %w", err)
	}

	compressedData := make([]byte, compressedSize)
	if _, err := io.ReadFull(r, compressedData); err != nil {
		return nil, fmt.Errorf("read compressed data: %w", err)
	}

	gzr, err := gzip.NewReader(bytes.NewReader(compressedData))
	if err != nil {
		return nil, fmt.Errorf("create gzip reader: %w", err)
	}
	defer gzr.Close()

	memory := make([]byte, uncompressedSize)
	if _, err := io.ReadFull(gzr, memory); err != nil {
		return nil, fmt.Errorf("decompress memory: %w", err)
	}

	return memory, nil
}

func writeDeviceSnapshots(w io.Writer, devices map[string]interface{}) error {
	if err := binary.Write(w, binary.LittleEndian, uint32(len(devices))); err != nil {
		return fmt.Errorf("write device count: %w", err)
	}

	// Write in sorted order for determinism
	deviceIDs := make([]string, 0, len(devices))
	for id := range devices {
		deviceIDs = append(deviceIDs, id)
	}
	sort.Strings(deviceIDs)

	for _, id := range deviceIDs {
		// Write device ID
		idBytes := []byte(id)
		if err := binary.Write(w, binary.LittleEndian, uint32(len(idBytes))); err != nil {
			return fmt.Errorf("write device id length: %w", err)
		}
		if _, err := w.Write(idBytes); err != nil {
			return fmt.Errorf("write device id: %w", err)
		}

		// Encode device snapshot with gob
		var buf bytes.Buffer
		enc := gob.NewEncoder(&buf)
		snap := devices[id]
		if err := enc.Encode(&snap); err != nil {
			return fmt.Errorf("gob encode device %s: %w", id, err)
		}

		// Write encoded data
		if err := binary.Write(w, binary.LittleEndian, uint32(buf.Len())); err != nil {
			return fmt.Errorf("write device data length: %w", err)
		}
		if _, err := w.Write(buf.Bytes()); err != nil {
			return fmt.Errorf("write device data: %w", err)
		}
	}

	return nil
}

func readDeviceSnapshots(r io.Reader) (map[string]interface{}, error) {
	var count uint32
	if err := binary.Read(r, binary.LittleEndian, &count); err != nil {
		return nil, fmt.Errorf("read device count: %w", err)
	}

	devices := make(map[string]interface{}, count)

	for i := uint32(0); i < count; i++ {
		// Read device ID
		var idLen uint32
		if err := binary.Read(r, binary.LittleEndian, &idLen); err != nil {
			return nil, fmt.Errorf("read device id length: %w", err)
		}
		idBytes := make([]byte, idLen)
		if _, err := io.ReadFull(r, idBytes); err != nil {
			return nil, fmt.Errorf("read device id: %w", err)
		}
		id := string(idBytes)

		// Read encoded data
		var dataLen uint32
		if err := binary.Read(r, binary.LittleEndian, &dataLen); err != nil {
			return nil, fmt.Errorf("read device data length: %w", err)
		}
		data := make([]byte, dataLen)
		if _, err := io.ReadFull(r, data); err != nil {
			return nil, fmt.Errorf("read device data: %w", err)
		}

		// Decode with gob
		var snap interface{}
		dec := gob.NewDecoder(bytes.NewReader(data))
		if err := dec.Decode(&snap); err != nil {
			return nil, fmt.Errorf("gob decode device %s: %w", id, err)
		}

		devices[id] = snap
	}

	return devices, nil
}
