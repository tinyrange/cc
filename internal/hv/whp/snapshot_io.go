//go:build windows && (amd64 || arm64)

package whp

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

// SaveSnapshot writes a WHP snapshot to the specified file path.
func SaveSnapshot(path string, snap hv.Snapshot) error {
	whpSnap, ok := snap.(*whpSnapshot)
	if !ok {
		return fmt.Errorf("snapshot is not a WHP snapshot: %T", snap)
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer f.Close()

	if err := writeSnapshot(f, whpSnap); err != nil {
		return fmt.Errorf("write snapshot: %w", err)
	}

	return nil
}

// LoadSnapshot reads a WHP snapshot from the specified file path.
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

// writeSnapshot writes a WHP snapshot to a writer.
func writeSnapshot(w io.Writer, snap *whpSnapshot) error {
	// Write header
	if err := binary.Write(w, binary.LittleEndian, hv.SnapshotMagic); err != nil {
		return fmt.Errorf("write magic: %w", err)
	}
	if err := binary.Write(w, binary.LittleEndian, hv.SnapshotVersion); err != nil {
		return fmt.Errorf("write version: %w", err)
	}
	if err := binary.Write(w, binary.LittleEndian, hv.ArchToSnapshotArch(snap.Arch)); err != nil {
		return fmt.Errorf("write arch: %w", err)
	}
	if err := binary.Write(w, binary.LittleEndian, uint32(0)); err != nil { // flags
		return fmt.Errorf("write flags: %w", err)
	}

	// Write vCPU count
	if err := binary.Write(w, binary.LittleEndian, uint32(len(snap.CpuStates))); err != nil {
		return fmt.Errorf("write vcpu count: %w", err)
	}

	// Write vCPU states in sorted order
	cpuIDs := make([]int, 0, len(snap.CpuStates))
	for id := range snap.CpuStates {
		cpuIDs = append(cpuIDs, id)
	}
	sort.Ints(cpuIDs)

	for _, cpuID := range cpuIDs {
		if err := writeVcpuSnapshot(w, cpuID, snap.CpuStates[cpuID]); err != nil {
			return fmt.Errorf("write vcpu %d: %w", cpuID, err)
		}
	}

	// Write memory (gzip compressed)
	if err := writeCompressedMemory(w, snap.Memory); err != nil {
		return fmt.Errorf("write memory: %w", err)
	}

	// Write device snapshots
	if err := writeDeviceSnapshots(w, snap.DeviceSnapshots); err != nil {
		return fmt.Errorf("write devices: %w", err)
	}

	return nil
}

// readSnapshot reads a WHP snapshot from a reader.
func readSnapshot(r io.Reader) (*whpSnapshot, error) {
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

	snap := &whpSnapshot{
		Arch:            cpuArch,
		CpuStates:       make(map[int]whpVcpuSnapshot),
		DeviceSnapshots: make(map[string]interface{}),
	}

	// Read vCPU count
	var vcpuCount uint32
	if err := binary.Read(r, binary.LittleEndian, &vcpuCount); err != nil {
		return nil, fmt.Errorf("read vcpu count: %w", err)
	}

	// Read vCPU states
	for i := uint32(0); i < vcpuCount; i++ {
		cpuID, state, err := readVcpuSnapshot(r)
		if err != nil {
			return nil, fmt.Errorf("read vcpu %d: %w", i, err)
		}
		snap.CpuStates[cpuID] = state
	}

	// Read memory
	memory, err := readCompressedMemory(r)
	if err != nil {
		return nil, fmt.Errorf("read memory: %w", err)
	}
	snap.Memory = memory

	// Read device snapshots
	devices, err := readDeviceSnapshots(r)
	if err != nil {
		return nil, fmt.Errorf("read devices: %w", err)
	}
	snap.DeviceSnapshots = devices

	return snap, nil
}

func writeVcpuSnapshot(w io.Writer, cpuID int, snap whpVcpuSnapshot) error {
	// Write CPU ID
	if err := binary.Write(w, binary.LittleEndian, uint32(cpuID)); err != nil {
		return fmt.Errorf("write cpu id: %w", err)
	}

	// Write register count
	if err := binary.Write(w, binary.LittleEndian, uint32(len(snap.Registers))); err != nil {
		return fmt.Errorf("write register count: %w", err)
	}

	// Write registers in sorted order
	regKeys := make([]hv.Register, 0, len(snap.Registers))
	for k := range snap.Registers {
		regKeys = append(regKeys, k)
	}
	sort.Slice(regKeys, func(i, j int) bool { return regKeys[i] < regKeys[j] })

	for _, k := range regKeys {
		if err := binary.Write(w, binary.LittleEndian, uint32(k)); err != nil {
			return fmt.Errorf("write register key: %w", err)
		}
		if err := binary.Write(w, binary.LittleEndian, snap.Registers[k]); err != nil {
			return fmt.Errorf("write register value: %w", err)
		}
	}

	return nil
}

func readVcpuSnapshot(r io.Reader) (int, whpVcpuSnapshot, error) {
	var snap whpVcpuSnapshot

	// Read CPU ID
	var cpuID uint32
	if err := binary.Read(r, binary.LittleEndian, &cpuID); err != nil {
		return 0, snap, fmt.Errorf("read cpu id: %w", err)
	}

	// Read register count
	var regCount uint32
	if err := binary.Read(r, binary.LittleEndian, &regCount); err != nil {
		return 0, snap, fmt.Errorf("read register count: %w", err)
	}

	snap.Registers = make(map[hv.Register]uint64, regCount)
	for i := uint32(0); i < regCount; i++ {
		var k uint32
		var v uint64
		if err := binary.Read(r, binary.LittleEndian, &k); err != nil {
			return 0, snap, fmt.Errorf("read register key: %w", err)
		}
		if err := binary.Read(r, binary.LittleEndian, &v); err != nil {
			return 0, snap, fmt.Errorf("read register value: %w", err)
		}
		snap.Registers[hv.Register(k)] = v
	}

	return int(cpuID), snap, nil
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
