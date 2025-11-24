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

	Run(ctx context.Context, cfg RunConfig) error

	VirtualCPUCall(id int, f func(vcpu VirtualCPU) error) error

	AddDevice(dev Device) error
}

type VMLoader interface {
	Load(vm VirtualMachine) error
}

type VMCallbacks interface {
	isVMCallbacks()

	OnCreateVM(vm VirtualMachine) error
	OnCreateVCPU(vCpu VirtualCPU) error
}

type VMConfig interface {
	isVMConfig()

	// Assume all methods here will be treated aw dumb getters
	// which can be called multiple times across multiple threads.

	CPUCount() int
	MemorySize() uint64
	MemoryBase() uint64
	Callbacks() VMCallbacks
	Loader() VMLoader
}

type SimpleVMConfig struct {
	NumCPUs  int
	MemSize  uint64
	MemBase  uint64
	VMLoader VMLoader

	CreateVM   func(vm VirtualMachine) error
	CreateVCPU func(vCpu VirtualCPU) error
}

func (c SimpleVMConfig) isVMCallbacks() {}
func (c SimpleVMConfig) isVMConfig()    {}

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

func (c SimpleVMConfig) CPUCount() int          { return c.NumCPUs }
func (c SimpleVMConfig) MemorySize() uint64     { return c.MemSize }
func (c SimpleVMConfig) MemoryBase() uint64     { return c.MemBase }
func (c SimpleVMConfig) Callbacks() VMCallbacks { return c }
func (c SimpleVMConfig) Loader() VMLoader       { return c.VMLoader }

var (
	_ VMConfig = SimpleVMConfig{}
)

type Hypervisor interface {
	io.Closer

	Architecture() CpuArchitecture

	NewVirtualMachine(config VMConfig) (VirtualMachine, error)
}
