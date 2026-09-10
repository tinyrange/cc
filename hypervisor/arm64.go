// Package hypervisor exposes in-process execution of caller-constructed machines.
// Inputs are architectural state and memory, with no image or host-path policy.
package hypervisor

import (
	"context"
	"io"
)

type BlockDevice interface {
	io.ReaderAt
	io.WriterAt
	Size() int64
}
type MMIODevice interface {
	Contains(address uint64, size int) bool
	Read(address uint64, size int) (uint64, error)
	Write(address uint64, size int, value uint64) error
}

// NVMePCIConfiguration describes an ECAM bus and one NVMe function. Interrupt is
// a GIC interrupt ID, including the 32 interrupt offset for SPIs.
type NVMePCIConfiguration struct {
	ConfigBase, ConfigSize, WindowBase, WindowSize, BAR uint64
	Device                                              uint8
	Interrupt                                           uint32
}

type ARM64State struct {
	X                      [31]uint64
	Q                      [32][16]byte
	PC, PState, FPCR, FPSR uint64
	// System uses architectural op0:op1:CRn:CRm:op2 register encodings.
	System map[uint16]uint64
}

type Exit struct {
	Reason                                        uint32
	PC, Syndrome, VirtualAddress, PhysicalAddress uint64
}

const (
	ExitCanceled uint32 = iota
	ExitException
	ExitVirtualTimer
	ExitUnknown
)

const (
	RegisterPC     uint32 = 31
	RegisterFPCR   uint32 = 32
	RegisterFPSR   uint32 = 33
	RegisterPState uint32 = 34
)

// ARM64 owns its mapped RAM and VCPU. Methods other than Cancel must be serialized.
type ARM64 interface {
	MapRAM(base, size uint64) ([]byte, error)
	Restore(ARM64State) error
	Run(context.Context) (Exit, error)
	Register(index uint32) (uint64, error)
	SetRegister(index uint32, value uint64) error
	SystemRegister(encoding uint16) (uint64, error)
	// Counter reads the native virtual counter and its frequency in Hz.
	Counter() (ticks, frequency uint64, err error)
	HandleSystemInstruction(syndrome uint64) (bool, error)
	HandlePSCI() (bool, error)
	SetDebugExceptionTrapping(bool) error
	DeliverDebugException(Exit) error
	NewNVMePCI(NVMePCIConfiguration, BlockDevice) (MMIODevice, error)
	EmulateMMIO(Exit, MMIODevice) (bool, error)
	InterruptState() (map[string]uint64, error)
	Advance() error
	Cancel() error
	Close() error
}
