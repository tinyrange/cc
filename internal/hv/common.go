package hv

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"

	"github.com/tinyrange/cc/internal/timeslice"
)

var (
	ErrInterrupted           = errors.New("operation interrupted")
	ErrVMHalted              = errors.New("virtual machine halted")
	ErrHypervisorUnsupported = errors.New("hypervisor unsupported on this platform")
	ErrGuestRequestedReboot  = errors.New("guest requested reboot")
	ErrYield                 = errors.New("yield to host")
	ErrUserYield             = errors.New("user yield to host")
)

type CpuArchitecture string

const (
	ArchitectureInvalid CpuArchitecture = "invalid"
	ArchitectureX86_64  CpuArchitecture = "x86_64"
	ArchitectureARM64   CpuArchitecture = "arm64"
	ArchitectureRISCV64 CpuArchitecture = "riscv64"
)

var ArchitectureNative CpuArchitecture

func init() {
	switch runtime.GOARCH {
	case "amd64":
		ArchitectureNative = ArchitectureX86_64
	case "arm64":
		ArchitectureNative = ArchitectureARM64
	}
}

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

	// AMD64 Special Registers
	RegisterAMD64Cr3

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
	RegisterARM64Xzr // Zero register (reads as 0, writes are discarded)
	RegisterARM64Sp
	RegisterARM64Pc
	RegisterARM64Pstate
	RegisterARM64Vbar
	RegisterARM64GicrBase

	// RISC-V General-Purpose Registers
	RegisterRISCVX0
	RegisterRISCVX1
	RegisterRISCVX2
	RegisterRISCVX3
	RegisterRISCVX4
	RegisterRISCVX5
	RegisterRISCVX6
	RegisterRISCVX7
	RegisterRISCVX8
	RegisterRISCVX9
	RegisterRISCVX10
	RegisterRISCVX11
	RegisterRISCVX12
	RegisterRISCVX13
	RegisterRISCVX14
	RegisterRISCVX15
	RegisterRISCVX16
	RegisterRISCVX17
	RegisterRISCVX18
	RegisterRISCVX19
	RegisterRISCVX20
	RegisterRISCVX21
	RegisterRISCVX22
	RegisterRISCVX23
	RegisterRISCVX24
	RegisterRISCVX25
	RegisterRISCVX26
	RegisterRISCVX27
	RegisterRISCVX28
	RegisterRISCVX29
	RegisterRISCVX30
	RegisterRISCVX31
	RegisterRISCVPc
)

var registerNames = map[Register]string{
	RegisterAMD64Rax:    "RAX",
	RegisterAMD64Rbx:    "RBX",
	RegisterAMD64Rcx:    "RCX",
	RegisterAMD64Rdx:    "RDX",
	RegisterAMD64Rsi:    "RSI",
	RegisterAMD64Rdi:    "RDI",
	RegisterAMD64Rsp:    "RSP",
	RegisterAMD64Rbp:    "RBP",
	RegisterAMD64R8:     "R8",
	RegisterAMD64R9:     "R9",
	RegisterAMD64R10:    "R10",
	RegisterAMD64R11:    "R11",
	RegisterAMD64R12:    "R12",
	RegisterAMD64R13:    "R13",
	RegisterAMD64R14:    "R14",
	RegisterAMD64R15:    "R15",
	RegisterAMD64Rip:    "RIP",
	RegisterAMD64Rflags: "RFLAGS",

	RegisterAMD64Cr3: "CR3",

	RegisterARM64X0:       "X0",
	RegisterARM64X1:       "X1",
	RegisterARM64X2:       "X2",
	RegisterARM64X3:       "X3",
	RegisterARM64X4:       "X4",
	RegisterARM64X5:       "X5",
	RegisterARM64X6:       "X6",
	RegisterARM64X7:       "X7",
	RegisterARM64X8:       "X8",
	RegisterARM64X9:       "X9",
	RegisterARM64X10:      "X10",
	RegisterARM64X11:      "X11",
	RegisterARM64X12:      "X12",
	RegisterARM64X13:      "X13",
	RegisterARM64X14:      "X14",
	RegisterARM64X15:      "X15",
	RegisterARM64X16:      "X16",
	RegisterARM64X17:      "X17",
	RegisterARM64X18:      "X18",
	RegisterARM64X19:      "X19",
	RegisterARM64X20:      "X20",
	RegisterARM64X21:      "X21",
	RegisterARM64X22:      "X22",
	RegisterARM64X23:      "X23",
	RegisterARM64X24:      "X24",
	RegisterARM64X25:      "X25",
	RegisterARM64X26:      "X26",
	RegisterARM64X27:      "X27",
	RegisterARM64X28:      "X28",
	RegisterARM64X29:      "X29",
	RegisterARM64X30:      "X30",
	RegisterARM64Sp:       "SP",
	RegisterARM64Pc:       "PC",
	RegisterARM64Pstate:   "PSTATE",
	RegisterARM64Vbar:     "VBAR",
	RegisterARM64GicrBase: "GICR_BASE",

	RegisterRISCVX0:  "X0",
	RegisterRISCVX1:  "X1",
	RegisterRISCVX2:  "X2",
	RegisterRISCVX3:  "X3",
	RegisterRISCVX4:  "X4",
	RegisterRISCVX5:  "X5",
	RegisterRISCVX6:  "X6",
	RegisterRISCVX7:  "X7",
	RegisterRISCVX8:  "X8",
	RegisterRISCVX9:  "X9",
	RegisterRISCVX10: "X10",
	RegisterRISCVX11: "X11",
	RegisterRISCVX12: "X12",
	RegisterRISCVX13: "X13",
	RegisterRISCVX14: "X14",
	RegisterRISCVX15: "X15",
	RegisterRISCVX16: "X16",
	RegisterRISCVX17: "X17",
	RegisterRISCVX18: "X18",
	RegisterRISCVX19: "X19",
	RegisterRISCVX20: "X20",
	RegisterRISCVX21: "X21",
	RegisterRISCVX22: "X22",
	RegisterRISCVX23: "X23",
	RegisterRISCVX24: "X24",
	RegisterRISCVX25: "X25",
	RegisterRISCVX26: "X26",
	RegisterRISCVX27: "X27",
	RegisterRISCVX28: "X28",
	RegisterRISCVX29: "X29",
	RegisterRISCVX30: "X30",
	RegisterRISCVX31: "X31",
	RegisterRISCVPc:  "PC",
}

func (r Register) String() string {
	if name, ok := registerNames[r]; ok {
		return name
	}
	return fmt.Sprintf("Register(0x%X)", uint64(r))
}

type VirtualCPU interface {
	VirtualMachine() VirtualMachine
	ID() int

	SetRegisters(regs map[Register]RegisterValue) error
	GetRegisters(regs map[Register]RegisterValue) error

	Run(ctx context.Context) error
}

type VirtualCPUDebug interface {
	VirtualCPU

	EnableTrace(maxEntries int) error
	GetTraceBuffer() ([]string, error)
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

type DeviceSnapshot interface {
}

type DeviceSnapshotter interface {
	Device

	DeviceId() string

	CaptureSnapshot() (DeviceSnapshot, error)
	RestoreSnapshot(snap DeviceSnapshot) error
}

type DeviceTemplate interface {
	Create(vm VirtualMachine) (Device, error)
}

type ExitContext interface {
	SetExitTimeslice(id timeslice.TimesliceID)
}

type MMIORegion struct {
	Address uint64
	Size    uint64
}

type MemoryMappedIODevice interface {
	Device

	MMIORegions() []MMIORegion

	ReadMMIO(ctx ExitContext, addr uint64, data []byte) error
	WriteMMIO(ctx ExitContext, addr uint64, data []byte) error
}

type SimpleMMIODevice struct {
	Regions []MMIORegion

	ReadFunc  func(ctx ExitContext, addr uint64, data []byte) error
	WriteFunc func(ctx ExitContext, addr uint64, data []byte) error
}

func (d SimpleMMIODevice) MMIORegions() []MMIORegion { return d.Regions }
func (d SimpleMMIODevice) ReadMMIO(ctx ExitContext, addr uint64, data []byte) error {
	if d.ReadFunc != nil {
		return d.ReadFunc(ctx, addr, data)
	}
	return fmt.Errorf("unhandled read from MMIO address 0x%X", addr)
}
func (d SimpleMMIODevice) WriteMMIO(ctx ExitContext, addr uint64, data []byte) error {
	if d.WriteFunc != nil {
		return d.WriteFunc(ctx, addr, data)
	}
	return fmt.Errorf("unhandled write to MMIO address 0x%X", addr)
}
func (d SimpleMMIODevice) Init(vm VirtualMachine) error {
	return nil
}

type X86IOPortDevice interface {
	Device

	IOPorts() []uint16

	ReadIOPort(ctx ExitContext, port uint16, data []byte) error
	WriteIOPort(ctx ExitContext, port uint16, data []byte) error
}

type SimpleX86IOPortDevice struct {
	Ports []uint16

	ReadFunc  func(ctx ExitContext, port uint16, data []byte) error
	WriteFunc func(ctx ExitContext, port uint16, data []byte) error
}

func (d SimpleX86IOPortDevice) IOPorts() []uint16 { return d.Ports }
func (d SimpleX86IOPortDevice) ReadIOPort(ctx ExitContext, port uint16, data []byte) error {
	if d.ReadFunc != nil {
		return d.ReadFunc(ctx, port, data)
	}
	return fmt.Errorf("unhandled read from I/O port 0x%X", port)
}
func (d SimpleX86IOPortDevice) WriteIOPort(ctx ExitContext, port uint16, data []byte) error {
	if d.WriteFunc != nil {
		return d.WriteFunc(ctx, port, data)
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

type MemoryRegion interface {
	io.ReaderAt
	io.WriterAt

	Size() uint64
}

type Snapshot interface {
}

type VirtualMachine interface {
	io.ReaderAt
	io.WriterAt

	io.Closer

	Hypervisor() Hypervisor

	MemorySize() uint64
	MemoryBase() uint64

	Run(ctx context.Context, cfg RunConfig) error

	SetIRQ(irqLine uint32, level bool) error

	VirtualCPUCall(id int, f func(vcpu VirtualCPU) error) error

	AddDevice(dev Device) error
	AddDeviceFromTemplate(template DeviceTemplate) error

	AllocateMemory(physAddr, size uint64) (MemoryRegion, error)

	CaptureSnapshot() (Snapshot, error)
	RestoreSnapshot(snap Snapshot) error
}

type VirtualMachineAmd64 interface {
	VirtualMachine

	SetIRQ(irqLine uint32, level bool) error
}

type VMLoader interface {
	Load(vm VirtualMachine) error
}

type VMCallbacks interface {
	OnCreateVM(vm VirtualMachine) error
	OnCreateVMWithMemory(vm VirtualMachine) error
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

	CreateVM           func(vm VirtualMachine) error
	CreateVMWithMemory func(vm VirtualMachine) error
	CreateVCPU         func(vCpu VirtualCPU) error
}

// OnCreateVMWithMemory implements VMCallbacks.
func (c SimpleVMConfig) OnCreateVMWithMemory(vm VirtualMachine) error {
	if c.CreateVMWithMemory != nil {
		return c.CreateVMWithMemory(vm)
	}
	return nil
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

type Arm64GICVersion int

const (
	Arm64GICVersionUnknown Arm64GICVersion = iota
	Arm64GICVersion2
	Arm64GICVersion3
)

type Arm64Interrupt struct {
	Type  uint32
	Num   uint32
	Flags uint32
}

type Arm64GICInfo struct {
	Version              Arm64GICVersion
	DistributorBase      uint64
	DistributorSize      uint64
	RedistributorBase    uint64
	RedistributorSize    uint64
	CpuInterfaceBase     uint64
	CpuInterfaceSize     uint64
	ItsBase              uint64
	ItsSize              uint64
	MaintenanceInterrupt Arm64Interrupt
}

type Arm64GICProvider interface {
	Arm64GICInfo() (Arm64GICInfo, bool)
}

type Hypervisor interface {
	io.Closer

	Architecture() CpuArchitecture

	NewVirtualMachine(config VMConfig) (VirtualMachine, error)
}
