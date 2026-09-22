package hypervisor

import (
	"context"

	"j5.nz/cc/hypervisor/x86state"
)

// X86Exit exposes architectural exits. IO.Data is borrowed until execution
// resumes (including after a RunUntil handler returns true); fill it for input
// instructions. CompleteMMIORead completes MMIO reads.
type X86Exit struct {
	Reason  uint32
	Port    uint16
	Size    uint8
	Count   uint32
	Write   bool
	Data    []byte
	Address uint64
}

// RAMRegion maps a page-aligned range of a shared backing allocation.
// Unmapped ranges remain device apertures and generate MMIO exits.
type RAMRegion struct{ Address, Offset, Size uint64 }

const (
	X86ExitIO       uint32 = 2
	X86ExitDebug    uint32 = 4
	X86ExitHLT      uint32 = 5
	X86ExitMMIO     uint32 = 6
	X86ExitShutdown uint32 = 8
)

// X86 owns its RAM and one CPU. All methods except Cancel and SetIRQ must be serialized.
// The PC interrupt controllers and interval timer are provided by the backend.
// Firmware, disk and display device semantics belong to the caller.
type X86 interface {
	// SupportedCPUID returns an owned table of accelerator-supported leaves.
	SupportedCPUID() ([]x86state.CPUIDEntry, error)
	// SetCPUID installs the machine's feature policy before the first Run.
	SetCPUID([]x86state.CPUIDEntry) error
	MapRAM(base, size uint64) ([]byte, error)
	MapRAMRegions(size uint64, regions []RAMRegion) ([]byte, error)
	Registers() (x86state.Registers, error)
	SystemRegisters() (x86state.SystemRegisters, error)
	SetRegisters(x86state.Registers) error
	SetSystemRegisters(x86state.SystemRegisters) error
	Run(context.Context) (X86Exit, error)
	// RunUntil services handled exits within one native execution call. The
	// caller supplies a context deadline when an execution bound is required.
	// The handler runs synchronously on the owning CPU thread. Returning false
	// leaves the exit to the caller; an error or context cancellation stops the
	// call. Every guest instruction still executes through the accelerator.
	RunUntil(context.Context, func(X86Exit) (bool, error)) (X86Exit, error)
	// CompleteIO commits a pending IO/MMIO operation without executing the
	// next instruction. Call before inspecting or changing CPU state after IO.
	CompleteIO() error
	CompleteMMIORead(value uint64, size uint32)
	// SetIRQ may run concurrently with Run/RunUntil. The caller must join
	// pending IRQ delivery before Close.
	SetIRQ(line uint32, level bool) error
	InterruptState() (map[string]uint64, error)
	Cancel() error
	Close() error
}
