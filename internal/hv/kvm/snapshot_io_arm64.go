//go:build linux && arm64

package kvm

import (
	"encoding/binary"
	"fmt"
	"io"
	"sort"

	"github.com/tinyrange/cc/internal/hv"
)

// writeSnapshot dispatches to the appropriate architecture-specific writer.
func writeSnapshot(w io.Writer, snap hv.Snapshot) error {
	switch s := snap.(type) {
	case *arm64Snapshot:
		return writeArm64Snapshot(w, s)
	default:
		return fmt.Errorf("unsupported snapshot type for arm64: %T", snap)
	}
}

// readSnapshotBody reads the snapshot body after the header has been read.
func readSnapshotBody(r io.Reader, arch hv.CpuArchitecture) (hv.Snapshot, error) {
	if arch != hv.ArchitectureARM64 {
		return nil, fmt.Errorf("cannot read %s snapshot on arm64", arch)
	}
	return readArm64Snapshot(r)
}

// writeArm64Snapshot writes an ARM64 KVM snapshot.
func writeArm64Snapshot(w io.Writer, snap *arm64Snapshot) error {
	// Write header
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

	// Write vCPU states in sorted order
	cpuIDs := make([]int, 0, len(snap.cpuStates))
	for id := range snap.cpuStates {
		cpuIDs = append(cpuIDs, id)
	}
	sort.Ints(cpuIDs)

	for _, cpuID := range cpuIDs {
		if err := writeArm64VcpuSnapshot(w, cpuID, snap.cpuStates[cpuID]); err != nil {
			return fmt.Errorf("write vcpu %d: %w", cpuID, err)
		}
	}

	// Write clock data
	if snap.clockData != nil {
		if err := binary.Write(w, binary.LittleEndian, uint8(1)); err != nil {
			return fmt.Errorf("write clock present: %w", err)
		}
		if err := writeClockData(w, snap.clockData); err != nil {
			return fmt.Errorf("write clock: %w", err)
		}
	} else {
		if err := binary.Write(w, binary.LittleEndian, uint8(0)); err != nil {
			return fmt.Errorf("write clock absent: %w", err)
		}
	}

	// Write memory (gzip compressed)
	if err := writeCompressedMemory(w, snap.memory); err != nil {
		return fmt.Errorf("write memory: %w", err)
	}

	// Write device snapshots
	if err := writeDeviceSnapshots(w, snap.deviceSnapshots); err != nil {
		return fmt.Errorf("write devices: %w", err)
	}

	return nil
}

// readArm64Snapshot reads an ARM64 KVM snapshot (header already read).
func readArm64Snapshot(r io.Reader) (*arm64Snapshot, error) {
	snap := &arm64Snapshot{
		cpuStates:       make(map[int]arm64VcpuSnapshot),
		deviceSnapshots: make(map[string]interface{}),
	}

	// Read vCPU count
	var vcpuCount uint32
	if err := binary.Read(r, binary.LittleEndian, &vcpuCount); err != nil {
		return nil, fmt.Errorf("read vcpu count: %w", err)
	}

	// Read vCPU states
	for i := uint32(0); i < vcpuCount; i++ {
		cpuID, state, err := readArm64VcpuSnapshot(r)
		if err != nil {
			return nil, fmt.Errorf("read vcpu %d: %w", i, err)
		}
		snap.cpuStates[cpuID] = state
	}

	// Read clock data
	var clockPresent uint8
	if err := binary.Read(r, binary.LittleEndian, &clockPresent); err != nil {
		return nil, fmt.Errorf("read clock present: %w", err)
	}
	if clockPresent != 0 {
		clock, err := readClockData(r)
		if err != nil {
			return nil, fmt.Errorf("read clock: %w", err)
		}
		snap.clockData = &clock
	}

	// Read memory
	memory, err := readCompressedMemory(r)
	if err != nil {
		return nil, fmt.Errorf("read memory: %w", err)
	}
	snap.memory = memory

	// Read device snapshots
	devices, err := readDeviceSnapshots(r)
	if err != nil {
		return nil, fmt.Errorf("read devices: %w", err)
	}
	snap.deviceSnapshots = devices

	return snap, nil
}

func writeArm64VcpuSnapshot(w io.Writer, cpuID int, snap arm64VcpuSnapshot) error {
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

func readArm64VcpuSnapshot(r io.Reader) (int, arm64VcpuSnapshot, error) {
	var snap arm64VcpuSnapshot

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
