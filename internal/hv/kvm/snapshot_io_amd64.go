//go:build linux && amd64

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
	case *snapshot:
		return writeAmd64Snapshot(w, s)
	default:
		return fmt.Errorf("unsupported snapshot type for amd64: %T", snap)
	}
}

// readSnapshotBody reads the snapshot body after the header has been read.
func readSnapshotBody(r io.Reader, arch hv.CpuArchitecture) (hv.Snapshot, error) {
	if arch != hv.ArchitectureX86_64 {
		return nil, fmt.Errorf("cannot read %s snapshot on amd64", arch)
	}
	return readAmd64Snapshot(r)
}

// writeAmd64Snapshot writes an AMD64 KVM snapshot.
func writeAmd64Snapshot(w io.Writer, snap *snapshot) error {
	// Write header
	if err := binary.Write(w, binary.LittleEndian, hv.SnapshotMagic); err != nil {
		return fmt.Errorf("write magic: %w", err)
	}
	if err := binary.Write(w, binary.LittleEndian, hv.SnapshotVersion); err != nil {
		return fmt.Errorf("write version: %w", err)
	}
	if err := binary.Write(w, binary.LittleEndian, hv.ArchToSnapshotArch(hv.ArchitectureX86_64)); err != nil {
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
		if err := writeAmd64VcpuSnapshot(w, cpuID, snap.cpuStates[cpuID]); err != nil {
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

	// Write IRQ chips
	if err := binary.Write(w, binary.LittleEndian, uint32(len(snap.irqChips))); err != nil {
		return fmt.Errorf("write irq chip count: %w", err)
	}
	for i, chip := range snap.irqChips {
		if err := writeIRQChip(w, &chip); err != nil {
			return fmt.Errorf("write irq chip %d: %w", i, err)
		}
	}

	// Write PIT state
	if snap.pitState != nil {
		if err := binary.Write(w, binary.LittleEndian, uint8(1)); err != nil {
			return fmt.Errorf("write pit present: %w", err)
		}
		if err := writePitState(w, snap.pitState); err != nil {
			return fmt.Errorf("write pit: %w", err)
		}
	} else {
		if err := binary.Write(w, binary.LittleEndian, uint8(0)); err != nil {
			return fmt.Errorf("write pit absent: %w", err)
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

// readAmd64Snapshot reads an AMD64 KVM snapshot (header already read).
func readAmd64Snapshot(r io.Reader) (*snapshot, error) {
	snap := &snapshot{
		cpuStates:       make(map[int]vcpuSnapshot),
		deviceSnapshots: make(map[string]interface{}),
	}

	// Read vCPU count
	var vcpuCount uint32
	if err := binary.Read(r, binary.LittleEndian, &vcpuCount); err != nil {
		return nil, fmt.Errorf("read vcpu count: %w", err)
	}

	// Read vCPU states
	for i := uint32(0); i < vcpuCount; i++ {
		cpuID, state, err := readAmd64VcpuSnapshot(r)
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

	// Read IRQ chips
	var irqChipCount uint32
	if err := binary.Read(r, binary.LittleEndian, &irqChipCount); err != nil {
		return nil, fmt.Errorf("read irq chip count: %w", err)
	}
	snap.irqChips = make([]kvmIRQChip, irqChipCount)
	for i := uint32(0); i < irqChipCount; i++ {
		chip, err := readIRQChip(r)
		if err != nil {
			return nil, fmt.Errorf("read irq chip %d: %w", i, err)
		}
		snap.irqChips[i] = chip
	}

	// Read PIT state
	var pitPresent uint8
	if err := binary.Read(r, binary.LittleEndian, &pitPresent); err != nil {
		return nil, fmt.Errorf("read pit present: %w", err)
	}
	if pitPresent != 0 {
		pit, err := readPitState(r)
		if err != nil {
			return nil, fmt.Errorf("read pit: %w", err)
		}
		snap.pitState = &pit
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

func writeAmd64VcpuSnapshot(w io.Writer, cpuID int, snap vcpuSnapshot) error {
	// Write CPU ID
	if err := binary.Write(w, binary.LittleEndian, uint32(cpuID)); err != nil {
		return fmt.Errorf("write cpu id: %w", err)
	}

	// Write Regs
	if err := binary.Write(w, binary.LittleEndian, &snap.Regs); err != nil {
		return fmt.Errorf("write regs: %w", err)
	}

	// Write SRegs
	if err := binary.Write(w, binary.LittleEndian, &snap.SRegs); err != nil {
		return fmt.Errorf("write sregs: %w", err)
	}

	// Write FPU
	if err := binary.Write(w, binary.LittleEndian, &snap.FPU); err != nil {
		return fmt.Errorf("write fpu: %w", err)
	}

	// Write LAPIC
	if snap.LapicPresent {
		if err := binary.Write(w, binary.LittleEndian, uint8(1)); err != nil {
			return fmt.Errorf("write lapic present: %w", err)
		}
		if _, err := w.Write(snap.Lapic.Regs[:]); err != nil {
			return fmt.Errorf("write lapic: %w", err)
		}
	} else {
		if err := binary.Write(w, binary.LittleEndian, uint8(0)); err != nil {
			return fmt.Errorf("write lapic absent: %w", err)
		}
	}

	// Write Xsave
	if err := binary.Write(w, binary.LittleEndian, &snap.Xsave); err != nil {
		return fmt.Errorf("write xsave: %w", err)
	}

	// Write Xcrs
	if err := binary.Write(w, binary.LittleEndian, &snap.Xcrs); err != nil {
		return fmt.Errorf("write xcrs: %w", err)
	}

	// Write MSRs
	if err := binary.Write(w, binary.LittleEndian, uint32(len(snap.Msrs))); err != nil {
		return fmt.Errorf("write msr count: %w", err)
	}
	for i, msr := range snap.Msrs {
		if err := binary.Write(w, binary.LittleEndian, msr.Index); err != nil {
			return fmt.Errorf("write msr %d index: %w", i, err)
		}
		if err := binary.Write(w, binary.LittleEndian, msr.Data); err != nil {
			return fmt.Errorf("write msr %d data: %w", i, err)
		}
	}

	return nil
}

func readAmd64VcpuSnapshot(r io.Reader) (int, vcpuSnapshot, error) {
	var snap vcpuSnapshot

	// Read CPU ID
	var cpuID uint32
	if err := binary.Read(r, binary.LittleEndian, &cpuID); err != nil {
		return 0, snap, fmt.Errorf("read cpu id: %w", err)
	}

	// Read Regs
	if err := binary.Read(r, binary.LittleEndian, &snap.Regs); err != nil {
		return 0, snap, fmt.Errorf("read regs: %w", err)
	}

	// Read SRegs
	if err := binary.Read(r, binary.LittleEndian, &snap.SRegs); err != nil {
		return 0, snap, fmt.Errorf("read sregs: %w", err)
	}

	// Read FPU
	if err := binary.Read(r, binary.LittleEndian, &snap.FPU); err != nil {
		return 0, snap, fmt.Errorf("read fpu: %w", err)
	}

	// Read LAPIC
	var lapicPresent uint8
	if err := binary.Read(r, binary.LittleEndian, &lapicPresent); err != nil {
		return 0, snap, fmt.Errorf("read lapic present: %w", err)
	}
	if lapicPresent != 0 {
		snap.LapicPresent = true
		if _, err := io.ReadFull(r, snap.Lapic.Regs[:]); err != nil {
			return 0, snap, fmt.Errorf("read lapic: %w", err)
		}
	}

	// Read Xsave
	if err := binary.Read(r, binary.LittleEndian, &snap.Xsave); err != nil {
		return 0, snap, fmt.Errorf("read xsave: %w", err)
	}

	// Read Xcrs
	if err := binary.Read(r, binary.LittleEndian, &snap.Xcrs); err != nil {
		return 0, snap, fmt.Errorf("read xcrs: %w", err)
	}

	// Read MSRs
	var msrCount uint32
	if err := binary.Read(r, binary.LittleEndian, &msrCount); err != nil {
		return 0, snap, fmt.Errorf("read msr count: %w", err)
	}
	snap.Msrs = make([]kvmMsrEntry, msrCount)
	for i := uint32(0); i < msrCount; i++ {
		if err := binary.Read(r, binary.LittleEndian, &snap.Msrs[i].Index); err != nil {
			return 0, snap, fmt.Errorf("read msr %d index: %w", i, err)
		}
		if err := binary.Read(r, binary.LittleEndian, &snap.Msrs[i].Data); err != nil {
			return 0, snap, fmt.Errorf("read msr %d data: %w", i, err)
		}
	}

	return int(cpuID), snap, nil
}

func writeIRQChip(w io.Writer, chip *kvmIRQChip) error {
	if err := binary.Write(w, binary.LittleEndian, chip.ChipID); err != nil {
		return fmt.Errorf("write chip id: %w", err)
	}
	if _, err := w.Write(chip.Chip[:]); err != nil {
		return fmt.Errorf("write chip data: %w", err)
	}
	return nil
}

func readIRQChip(r io.Reader) (kvmIRQChip, error) {
	var chip kvmIRQChip
	if err := binary.Read(r, binary.LittleEndian, &chip.ChipID); err != nil {
		return chip, fmt.Errorf("read chip id: %w", err)
	}
	if _, err := io.ReadFull(r, chip.Chip[:]); err != nil {
		return chip, fmt.Errorf("read chip data: %w", err)
	}
	return chip, nil
}

func writePitState(w io.Writer, pit *kvmPitState2) error {
	// Write each channel
	for i := 0; i < 3; i++ {
		ch := &pit.Channels[i]
		if err := binary.Write(w, binary.LittleEndian, ch.Count); err != nil {
			return fmt.Errorf("write channel %d count: %w", i, err)
		}
		if err := binary.Write(w, binary.LittleEndian, ch.LatchedCount); err != nil {
			return fmt.Errorf("write channel %d latched_count: %w", i, err)
		}
		if err := binary.Write(w, binary.LittleEndian, ch.CountLatched); err != nil {
			return fmt.Errorf("write channel %d count_latched: %w", i, err)
		}
		if err := binary.Write(w, binary.LittleEndian, ch.StatusLatched); err != nil {
			return fmt.Errorf("write channel %d status_latched: %w", i, err)
		}
		if err := binary.Write(w, binary.LittleEndian, ch.Status); err != nil {
			return fmt.Errorf("write channel %d status: %w", i, err)
		}
		if err := binary.Write(w, binary.LittleEndian, ch.ReadState); err != nil {
			return fmt.Errorf("write channel %d read_state: %w", i, err)
		}
		if err := binary.Write(w, binary.LittleEndian, ch.WriteState); err != nil {
			return fmt.Errorf("write channel %d write_state: %w", i, err)
		}
		if err := binary.Write(w, binary.LittleEndian, ch.WriteLatch); err != nil {
			return fmt.Errorf("write channel %d write_latch: %w", i, err)
		}
		if err := binary.Write(w, binary.LittleEndian, ch.RWMode); err != nil {
			return fmt.Errorf("write channel %d rw_mode: %w", i, err)
		}
		if err := binary.Write(w, binary.LittleEndian, ch.Mode); err != nil {
			return fmt.Errorf("write channel %d mode: %w", i, err)
		}
		if err := binary.Write(w, binary.LittleEndian, ch.Bcd); err != nil {
			return fmt.Errorf("write channel %d bcd: %w", i, err)
		}
		if err := binary.Write(w, binary.LittleEndian, ch.Gate); err != nil {
			return fmt.Errorf("write channel %d gate: %w", i, err)
		}
		if err := binary.Write(w, binary.LittleEndian, ch.CountLoadTime); err != nil {
			return fmt.Errorf("write channel %d count_load_time: %w", i, err)
		}
	}
	if err := binary.Write(w, binary.LittleEndian, pit.Flags); err != nil {
		return fmt.Errorf("write flags: %w", err)
	}
	return nil
}

func readPitState(r io.Reader) (kvmPitState2, error) {
	var pit kvmPitState2
	for i := 0; i < 3; i++ {
		ch := &pit.Channels[i]
		if err := binary.Read(r, binary.LittleEndian, &ch.Count); err != nil {
			return pit, fmt.Errorf("read channel %d count: %w", i, err)
		}
		if err := binary.Read(r, binary.LittleEndian, &ch.LatchedCount); err != nil {
			return pit, fmt.Errorf("read channel %d latched_count: %w", i, err)
		}
		if err := binary.Read(r, binary.LittleEndian, &ch.CountLatched); err != nil {
			return pit, fmt.Errorf("read channel %d count_latched: %w", i, err)
		}
		if err := binary.Read(r, binary.LittleEndian, &ch.StatusLatched); err != nil {
			return pit, fmt.Errorf("read channel %d status_latched: %w", i, err)
		}
		if err := binary.Read(r, binary.LittleEndian, &ch.Status); err != nil {
			return pit, fmt.Errorf("read channel %d status: %w", i, err)
		}
		if err := binary.Read(r, binary.LittleEndian, &ch.ReadState); err != nil {
			return pit, fmt.Errorf("read channel %d read_state: %w", i, err)
		}
		if err := binary.Read(r, binary.LittleEndian, &ch.WriteState); err != nil {
			return pit, fmt.Errorf("read channel %d write_state: %w", i, err)
		}
		if err := binary.Read(r, binary.LittleEndian, &ch.WriteLatch); err != nil {
			return pit, fmt.Errorf("read channel %d write_latch: %w", i, err)
		}
		if err := binary.Read(r, binary.LittleEndian, &ch.RWMode); err != nil {
			return pit, fmt.Errorf("read channel %d rw_mode: %w", i, err)
		}
		if err := binary.Read(r, binary.LittleEndian, &ch.Mode); err != nil {
			return pit, fmt.Errorf("read channel %d mode: %w", i, err)
		}
		if err := binary.Read(r, binary.LittleEndian, &ch.Bcd); err != nil {
			return pit, fmt.Errorf("read channel %d bcd: %w", i, err)
		}
		if err := binary.Read(r, binary.LittleEndian, &ch.Gate); err != nil {
			return pit, fmt.Errorf("read channel %d gate: %w", i, err)
		}
		if err := binary.Read(r, binary.LittleEndian, &ch.CountLoadTime); err != nil {
			return pit, fmt.Errorf("read channel %d count_load_time: %w", i, err)
		}
	}
	if err := binary.Read(r, binary.LittleEndian, &pit.Flags); err != nil {
		return pit, fmt.Errorf("read flags: %w", err)
	}
	return pit, nil
}
