package hv

import (
	"context"
	"errors"
	"fmt"
	"io"
)

var (
	ErrVMHalted              = errors.New("virtual machine halted")
	ErrHypervisorUnsupported = errors.New("hypervisor unsupported on this platform")
)

type CpuArchitecture string

const (
	ArchitectureInvalid CpuArchitecture = "invalid"
	ArchitectureX86_64  CpuArchitecture = "x86_64"
	ArchitectureARM64   CpuArchitecture = "arm64"
)

type RegisterValue interface {
	isRegisterValue()
}

type Register64 uint64

func (r Register64) isRegisterValue() {}

type Register uint64

const (
	RegisterInvalid Register = iota

	// AMD64 Regular Registers
	RegisterAMD64Rax
	RegisterAMD64Rbx
	RegisterAMD64Rcx
	RegisterAMD64Rdx
	RegisterAMD64Rsi
	RegisterAMD64Rdi
	RegisterAMD64Rsp
	RegisterAMD64Rbp
	RegisterAMD64R8
	RegisterAMD64R9
	RegisterAMD64R10
	RegisterAMD64R11
	RegisterAMD64R12
	RegisterAMD64R13
	RegisterAMD64R14
	RegisterAMD64R15
	RegisterAMD64Rip
	RegisterAMD64Rflags

	// ARM64 General-Purpose Registers
	RegisterARM64X0
	RegisterARM64X1
	RegisterARM64X2
	RegisterARM64X3
	RegisterARM64X4
	RegisterARM64X5
	RegisterARM64X6
	RegisterARM64X7
	RegisterARM64X8
	RegisterARM64X9
	RegisterARM64X10
	RegisterARM64X11
	RegisterARM64X12
	RegisterARM64X13
	RegisterARM64X14
	RegisterARM64X15
	RegisterARM64X16
	RegisterARM64X17
	RegisterARM64X18
	RegisterARM64X19
	RegisterARM64X20
	RegisterARM64X21
	RegisterARM64X22
	RegisterARM64X23
	RegisterARM64X24
	RegisterARM64X25
	RegisterARM64X26
	RegisterARM64X27
	RegisterARM64X28
	RegisterARM64X29
	RegisterARM64X30
	RegisterARM64Sp
	RegisterARM64Pc
	RegisterARM64Pstate
	RegisterARM64Vbar
)

type VirtualCPU interface {
	VirtualMachine() VirtualMachine
	ID() int

	SetRegisters(regs map[Register]RegisterValue) error
	GetRegisters(regs map[Register]RegisterValue) error

	Run(ctx context.Context) error
}

type VirtualCPUAmd64 interface {
	VirtualCPU

	SetProtectedMode() error
	SetLongModeWithSelectors(
		pagingBase uint64,
		addrSpaceSize int,
		codeSelector, dataSelector uint16,
	) error
}

type RunConfig interface {
	Run(ctx context.Context, vcpu VirtualCPU) error
}

type Device interface {
	Init(vm VirtualMachine) error
}

type MMIORegion struct {
	Address uint64
	Size    uint64
}

type MemoryMappedIODevice interface {
	Device

	MMIORegions() []MMIORegion

	ReadMMIO(addr uint64, data []byte) error
	WriteMMIO(addr uint64, data []byte) error
}

type SimpleMMIODevice struct {
	Regions []MMIORegion

	ReadFunc  func(addr uint64, data []byte) error
	WriteFunc func(addr uint64, data []byte) error
}

func (d SimpleMMIODevice) MMIORegions() []MMIORegion { return d.Regions }
func (d SimpleMMIODevice) ReadMMIO(addr uint64, data []byte) error {
	if d.ReadFunc != nil {
		return d.ReadFunc(addr, data)
	}
	return fmt.Errorf("unhandled read from MMIO address 0x%X", addr)
}
func (d SimpleMMIODevice) WriteMMIO(addr uint64, data []byte) error {
	if d.WriteFunc != nil {
		return d.WriteFunc(addr, data)
	}
	return fmt.Errorf("unhandled write to MMIO address 0x%X", addr)
}
func (d SimpleMMIODevice) Init(vm VirtualMachine) error {
	return nil
}

type X86IOPortDevice interface {
	Device

	IOPorts() []uint16

	ReadIOPort(port uint16, data []byte) error
	WriteIOPort(port uint16, data []byte) error
}

type SimpleX86IOPortDevice struct {
	Ports []uint16

	ReadFunc  func(port uint16, data []byte) error
	WriteFunc func(port uint16, data []byte) error
}

func (d SimpleX86IOPortDevice) IOPorts() []uint16 { return d.Ports }
func (d SimpleX86IOPortDevice) ReadIOPort(port uint16, data []byte) error {
	if d.ReadFunc != nil {
		return d.ReadFunc(port, data)
	}
	return fmt.Errorf("unhandled read from I/O port 0x%X", port)
}
func (d SimpleX86IOPortDevice) WriteIOPort(port uint16, data []byte) error {
	if d.WriteFunc != nil {
		return d.WriteFunc(port, data)
	}
	return fmt.Errorf("unhandled write to I/O port 0x%X", port)
}
func (d SimpleX86IOPortDevice) Init(vm VirtualMachine) error {
	return nil
}

var (
	_ MemoryMappedIODevice = SimpleMMIODevice{}
	_ X86IOPortDevice      = SimpleX86IOPortDevice{}
)

type VirtualMachine interface {
	io.ReaderAt
	io.WriterAt

	io.Closer

	Hypervisor() Hypervisor

	MemorySize() uint64
	MemoryBase() uint64

	Run(ctx context.Context, cfg RunConfig) error

	VirtualCPUCall(id int, f func(vcpu VirtualCPU) error) error

	AddDevice(dev Device) error
}

type VirtualMachineAmd64 interface {
	VirtualMachine

	PulseIRQ(irqLine uint32) error
}

type VMLoader interface {
	Load(vm VirtualMachine) error
}

type VMCallbacks interface {
	OnCreateVM(vm VirtualMachine) error
	OnCreateVCPU(vCpu VirtualCPU) error
}

type VMConfig interface {
	// Assume all methods here will be treated aw dumb getters
	// which can be called multiple times across multiple threads.

	CPUCount() int
	MemorySize() uint64
	MemoryBase() uint64
	NeedsInterruptSupport() bool
	Callbacks() VMCallbacks
	Loader() VMLoader
}

type SimpleVMConfig struct {
	NumCPUs          int
	MemSize          uint64
	MemBase          uint64
	InterruptSupport bool
	VMLoader         VMLoader

	CreateVM   func(vm VirtualMachine) error
	CreateVCPU func(vCpu VirtualCPU) error
}

// OnCreateVM implements VMCallbacks.
func (c SimpleVMConfig) OnCreateVM(vm VirtualMachine) error {
	if c.CreateVM != nil {
		return c.CreateVM(vm)
	}
	return nil
}

// OnCreateVCPU implements VMCallbacks.
func (c SimpleVMConfig) OnCreateVCPU(vCpu VirtualCPU) error {
	if c.CreateVCPU != nil {
		return c.CreateVCPU(vCpu)
	}
	return nil
}

func (c SimpleVMConfig) CPUCount() int               { return c.NumCPUs }
func (c SimpleVMConfig) MemorySize() uint64          { return c.MemSize }
func (c SimpleVMConfig) MemoryBase() uint64          { return c.MemBase }
func (c SimpleVMConfig) NeedsInterruptSupport() bool { return c.InterruptSupport }
func (c SimpleVMConfig) Callbacks() VMCallbacks      { return c }
func (c SimpleVMConfig) Loader() VMLoader            { return c.VMLoader }

var (
	_ VMConfig = SimpleVMConfig{}
)

type Hypervisor interface {
	io.Closer

	Architecture() CpuArchitecture

	NewVirtualMachine(config VMConfig) (VirtualMachine, error)
}
