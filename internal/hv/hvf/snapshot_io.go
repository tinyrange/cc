//go:build darwin && arm64

package hvf

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
	"github.com/tinyrange/cc/internal/hv/hvf/bindings"
)

// SaveSnapshot writes a VM snapshot to the specified file path.
func SaveSnapshot(path string, snap hv.Snapshot) error {
	hvfSnap, ok := snap.(*arm64HvfSnapshot)
	if !ok {
		return fmt.Errorf("snapshot is not an HVF ARM64 snapshot")
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer f.Close()

	if err := WriteSnapshot(f, hvfSnap); err != nil {
		return fmt.Errorf("write snapshot: %w", err)
	}

	return nil
}

// LoadSnapshot reads a VM snapshot from the specified file path.
func LoadSnapshot(path string) (hv.Snapshot, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	snap, err := ReadSnapshot(f)
	if err != nil {
		return nil, fmt.Errorf("read snapshot: %w", err)
	}

	return snap, nil
}

// WriteSnapshot writes an HVF ARM64 snapshot to a writer.
func WriteSnapshot(w io.Writer, snap *arm64HvfSnapshot) error {
	// Write header fields individually (CpuArchitecture is string-based, not fixed-size)
	if err := binary.Write(w, binary.LittleEndian, hv.SnapshotMagic); err != nil {
		return fmt.Errorf("write magic: %w", err)
	}
	if err := binary.Write(w, binary.LittleEndian, hv.SnapshotVersion); err != nil {
		return fmt.Errorf("write version: %w", err)
	}
	if err := binary.Write(w, binary.LittleEndian, hv.ArchToSnapshotArch(hv.ArchitectureARM64)); err != nil {
		return fmt.Errorf("write arch: %w", err)
	}
	if err := binary.Write(w, binary.LittleEndian, uint32(0)); err != nil { // flags
		return fmt.Errorf("write flags: %w", err)
	}

	// Write vCPU count
	if err := binary.Write(w, binary.LittleEndian, uint32(len(snap.cpuStates))); err != nil {
		return fmt.Errorf("write vcpu count: %w", err)
	}

	// Write vCPU states in sorted order for determinism
	cpuIDs := make([]int, 0, len(snap.cpuStates))
	for id := range snap.cpuStates {
		cpuIDs = append(cpuIDs, id)
	}
	sort.Ints(cpuIDs)

	for _, cpuID := range cpuIDs {
		if err := writeVcpuSnapshot(w, cpuID, snap.cpuStates[cpuID]); err != nil {
			return fmt.Errorf("write vcpu %d: %w", cpuID, err)
		}
	}

	// Write GIC state
	if err := binary.Write(w, binary.LittleEndian, uint32(len(snap.gicState))); err != nil {
		return fmt.Errorf("write gic length: %w", err)
	}
	if _, err := w.Write(snap.gicState); err != nil {
		return fmt.Errorf("write gic state: %w", err)
	}

	// Write memory (gzip compressed)
	// First compress to buffer to get compressed size
	var compressedBuf bytes.Buffer
	gzw := gzip.NewWriter(&compressedBuf)
	if _, err := gzw.Write(snap.memory); err != nil {
		gzw.Close()
		return fmt.Errorf("compress memory: %w", err)
	}
	if err := gzw.Close(); err != nil {
		return fmt.Errorf("close gzip compressor: %w", err)
	}

	// Write uncompressed size, compressed size, then compressed data
	if err := binary.Write(w, binary.LittleEndian, uint64(len(snap.memory))); err != nil {
		return fmt.Errorf("write memory uncompressed length: %w", err)
	}
	if err := binary.Write(w, binary.LittleEndian, uint64(compressedBuf.Len())); err != nil {
		return fmt.Errorf("write memory compressed length: %w", err)
	}
	if _, err := w.Write(compressedBuf.Bytes()); err != nil {
		return fmt.Errorf("write compressed memory: %w", err)
	}

	// Write device snapshots
	if err := binary.Write(w, binary.LittleEndian, uint32(len(snap.deviceSnapshots))); err != nil {
		return fmt.Errorf("write device count: %w", err)
	}

	// Write device snapshots in sorted order
	deviceIDs := make([]string, 0, len(snap.deviceSnapshots))
	for id := range snap.deviceSnapshots {
		deviceIDs = append(deviceIDs, id)
	}
	sort.Strings(deviceIDs)

	for _, deviceID := range deviceIDs {
		if err := writeDeviceSnapshot(w, deviceID, snap.deviceSnapshots[deviceID]); err != nil {
			return fmt.Errorf("write device %s: %w", deviceID, err)
		}
	}

	return nil
}

// ReadSnapshot reads an HVF ARM64 snapshot from a reader.
func ReadSnapshot(r io.Reader) (*arm64HvfSnapshot, error) {
	// Read header fields individually
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
	if hv.SnapshotArchToArch(arch) != hv.ArchitectureARM64 {
		return nil, fmt.Errorf("architecture mismatch: expected ARM64, got %s", hv.SnapshotArchToArch(arch))
	}
	_ = flags // reserved for future use

	snap := &arm64HvfSnapshot{
		cpuStates:       make(map[int]arm64HvfVcpuSnapshot),
		deviceSnapshots: make(map[string]interface{}),
	}

	// Read vCPU count
	var vcpuCount uint32
	if err := binary.Read(r, binary.LittleEndian, &vcpuCount); err != nil {
		return nil, fmt.Errorf("read vcpu count: %w", err)
	}

	// Read vCPU states
	for i := uint32(0); i < vcpuCount; i++ {
		cpuID, vcpuSnap, err := readVcpuSnapshot(r)
		if err != nil {
			return nil, fmt.Errorf("read vcpu %d: %w", i, err)
		}
		snap.cpuStates[cpuID] = vcpuSnap
	}

	// Read GIC state
	var gicLen uint32
	if err := binary.Read(r, binary.LittleEndian, &gicLen); err != nil {
		return nil, fmt.Errorf("read gic length: %w", err)
	}
	snap.gicState = make([]byte, gicLen)
	if _, err := io.ReadFull(r, snap.gicState); err != nil {
		return nil, fmt.Errorf("read gic state: %w", err)
	}

	// Read memory (gzip compressed)
	var memLen, compressedLen uint64
	if err := binary.Read(r, binary.LittleEndian, &memLen); err != nil {
		return nil, fmt.Errorf("read memory uncompressed length: %w", err)
	}
	if err := binary.Read(r, binary.LittleEndian, &compressedLen); err != nil {
		return nil, fmt.Errorf("read memory compressed length: %w", err)
	}

	// Read compressed data into buffer, then decompress
	compressedData := make([]byte, compressedLen)
	if _, err := io.ReadFull(r, compressedData); err != nil {
		return nil, fmt.Errorf("read compressed memory: %w", err)
	}

	gzr, err := gzip.NewReader(bytes.NewReader(compressedData))
	if err != nil {
		return nil, fmt.Errorf("create gzip reader: %w", err)
	}
	snap.memory = make([]byte, memLen)
	if _, err := io.ReadFull(gzr, snap.memory); err != nil {
		gzr.Close()
		return nil, fmt.Errorf("decompress memory: %w", err)
	}
	if err := gzr.Close(); err != nil {
		return nil, fmt.Errorf("close gzip reader: %w", err)
	}

	// Read device snapshots
	var deviceCount uint32
	if err := binary.Read(r, binary.LittleEndian, &deviceCount); err != nil {
		return nil, fmt.Errorf("read device count: %w", err)
	}

	for i := uint32(0); i < deviceCount; i++ {
		deviceID, deviceSnap, err := readDeviceSnapshot(r)
		if err != nil {
			return nil, fmt.Errorf("read device %d: %w", i, err)
		}
		snap.deviceSnapshots[deviceID] = deviceSnap
	}

	return snap, nil
}

func writeVcpuSnapshot(w io.Writer, cpuID int, snap arm64HvfVcpuSnapshot) error {
	// Write CPU ID
	if err := binary.Write(w, binary.LittleEndian, uint32(cpuID)); err != nil {
		return fmt.Errorf("write cpu id: %w", err)
	}

	// Write GP registers
	if err := binary.Write(w, binary.LittleEndian, uint32(len(snap.GPRegisters))); err != nil {
		return fmt.Errorf("write gp count: %w", err)
	}
	// Sort keys for determinism
	gpKeys := make([]bindings.Reg, 0, len(snap.GPRegisters))
	for k := range snap.GPRegisters {
		gpKeys = append(gpKeys, k)
	}
	sort.Slice(gpKeys, func(i, j int) bool { return gpKeys[i] < gpKeys[j] })
	for _, k := range gpKeys {
		if err := binary.Write(w, binary.LittleEndian, uint32(k)); err != nil {
			return fmt.Errorf("write gp key: %w", err)
		}
		if err := binary.Write(w, binary.LittleEndian, snap.GPRegisters[k]); err != nil {
			return fmt.Errorf("write gp value: %w", err)
		}
	}

	// Write sys registers
	if err := binary.Write(w, binary.LittleEndian, uint32(len(snap.SysRegisters))); err != nil {
		return fmt.Errorf("write sys count: %w", err)
	}
	sysKeys := make([]bindings.SysReg, 0, len(snap.SysRegisters))
	for k := range snap.SysRegisters {
		sysKeys = append(sysKeys, k)
	}
	sort.Slice(sysKeys, func(i, j int) bool { return sysKeys[i] < sysKeys[j] })
	for _, k := range sysKeys {
		if err := binary.Write(w, binary.LittleEndian, uint16(k)); err != nil {
			return fmt.Errorf("write sys key: %w", err)
		}
		if err := binary.Write(w, binary.LittleEndian, snap.SysRegisters[k]); err != nil {
			return fmt.Errorf("write sys value: %w", err)
		}
	}

	// Write SIMD/FP registers
	if err := binary.Write(w, binary.LittleEndian, uint32(len(snap.SimdFPRegisters))); err != nil {
		return fmt.Errorf("write simd count: %w", err)
	}
	simdKeys := make([]bindings.SIMDReg, 0, len(snap.SimdFPRegisters))
	for k := range snap.SimdFPRegisters {
		simdKeys = append(simdKeys, k)
	}
	sort.Slice(simdKeys, func(i, j int) bool { return simdKeys[i] < simdKeys[j] })
	for _, k := range simdKeys {
		if err := binary.Write(w, binary.LittleEndian, uint32(k)); err != nil {
			return fmt.Errorf("write simd key: %w", err)
		}
		v := snap.SimdFPRegisters[k]
		if err := binary.Write(w, binary.LittleEndian, v.Low()); err != nil {
			return fmt.Errorf("write simd low: %w", err)
		}
		if err := binary.Write(w, binary.LittleEndian, v.High()); err != nil {
			return fmt.Errorf("write simd high: %w", err)
		}
	}

	// Write VTimerOffset
	if err := binary.Write(w, binary.LittleEndian, snap.VTimerOffset); err != nil {
		return fmt.Errorf("write vtimer offset: %w", err)
	}

	return nil
}

func readVcpuSnapshot(r io.Reader) (int, arm64HvfVcpuSnapshot, error) {
	var snap arm64HvfVcpuSnapshot

	// Read CPU ID
	var cpuID uint32
	if err := binary.Read(r, binary.LittleEndian, &cpuID); err != nil {
		return 0, snap, fmt.Errorf("read cpu id: %w", err)
	}

	// Read GP registers
	var gpCount uint32
	if err := binary.Read(r, binary.LittleEndian, &gpCount); err != nil {
		return 0, snap, fmt.Errorf("read gp count: %w", err)
	}
	snap.GPRegisters = make(map[bindings.Reg]uint64, gpCount)
	for i := uint32(0); i < gpCount; i++ {
		var k uint32
		var v uint64
		if err := binary.Read(r, binary.LittleEndian, &k); err != nil {
			return 0, snap, fmt.Errorf("read gp key: %w", err)
		}
		if err := binary.Read(r, binary.LittleEndian, &v); err != nil {
			return 0, snap, fmt.Errorf("read gp value: %w", err)
		}
		snap.GPRegisters[bindings.Reg(k)] = v
	}

	// Read sys registers
	var sysCount uint32
	if err := binary.Read(r, binary.LittleEndian, &sysCount); err != nil {
		return 0, snap, fmt.Errorf("read sys count: %w", err)
	}
	snap.SysRegisters = make(map[bindings.SysReg]uint64, sysCount)
	for i := uint32(0); i < sysCount; i++ {
		var k uint16
		var v uint64
		if err := binary.Read(r, binary.LittleEndian, &k); err != nil {
			return 0, snap, fmt.Errorf("read sys key: %w", err)
		}
		if err := binary.Read(r, binary.LittleEndian, &v); err != nil {
			return 0, snap, fmt.Errorf("read sys value: %w", err)
		}
		snap.SysRegisters[bindings.SysReg(k)] = v
	}

	// Read SIMD/FP registers
	var simdCount uint32
	if err := binary.Read(r, binary.LittleEndian, &simdCount); err != nil {
		return 0, snap, fmt.Errorf("read simd count: %w", err)
	}
	snap.SimdFPRegisters = make(map[bindings.SIMDReg]bindings.SimdFP, simdCount)
	for i := uint32(0); i < simdCount; i++ {
		var k uint32
		var low, high uint64
		if err := binary.Read(r, binary.LittleEndian, &k); err != nil {
			return 0, snap, fmt.Errorf("read simd key: %w", err)
		}
		if err := binary.Read(r, binary.LittleEndian, &low); err != nil {
			return 0, snap, fmt.Errorf("read simd low: %w", err)
		}
		if err := binary.Read(r, binary.LittleEndian, &high); err != nil {
			return 0, snap, fmt.Errorf("read simd high: %w", err)
		}
		snap.SimdFPRegisters[bindings.SIMDReg(k)] = bindings.NewSimdFP(low, high)
	}

	// Read VTimerOffset
	if err := binary.Read(r, binary.LittleEndian, &snap.VTimerOffset); err != nil {
		return 0, snap, fmt.Errorf("read vtimer offset: %w", err)
	}

	return int(cpuID), snap, nil
}

func writeDeviceSnapshot(w io.Writer, deviceID string, snap interface{}) error {
	// Write device ID
	idBytes := []byte(deviceID)
	if err := binary.Write(w, binary.LittleEndian, uint32(len(idBytes))); err != nil {
		return fmt.Errorf("write id length: %w", err)
	}
	if _, err := w.Write(idBytes); err != nil {
		return fmt.Errorf("write id: %w", err)
	}

	// Encode device snapshot with gob
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	if err := enc.Encode(&snap); err != nil {
		return fmt.Errorf("gob encode: %w", err)
	}

	// Write encoded data
	if err := binary.Write(w, binary.LittleEndian, uint32(buf.Len())); err != nil {
		return fmt.Errorf("write data length: %w", err)
	}
	if _, err := w.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("write data: %w", err)
	}

	return nil
}

func readDeviceSnapshot(r io.Reader) (string, interface{}, error) {
	// Read device ID
	var idLen uint32
	if err := binary.Read(r, binary.LittleEndian, &idLen); err != nil {
		return "", nil, fmt.Errorf("read id length: %w", err)
	}
	idBytes := make([]byte, idLen)
	if _, err := io.ReadFull(r, idBytes); err != nil {
		return "", nil, fmt.Errorf("read id: %w", err)
	}
	deviceID := string(idBytes)

	// Read encoded data
	var dataLen uint32
	if err := binary.Read(r, binary.LittleEndian, &dataLen); err != nil {
		return "", nil, fmt.Errorf("read data length: %w", err)
	}
	data := make([]byte, dataLen)
	if _, err := io.ReadFull(r, data); err != nil {
		return "", nil, fmt.Errorf("read data: %w", err)
	}

	// Decode with gob
	var snap interface{}
	dec := gob.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&snap); err != nil {
		return "", nil, fmt.Errorf("gob decode: %w", err)
	}

	return deviceID, snap, nil
}
