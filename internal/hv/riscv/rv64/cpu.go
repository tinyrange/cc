// Package rv64 implements a clean RV64GC emulator for booting Linux.
package rv64

import (
	"encoding/binary"
	"fmt"
	"io"
)

// Memory layout constants
const (
	RAMBase    uint64 = 0x8000_0000 // RAM starts at 2GB
	CLINTBase  uint64 = 0x0200_0000 // Core Local Interruptor
	CLINTSize  uint64 = 0x000c_0000
	PLICBase   uint64 = 0x0c00_0000 // Platform Level Interrupt Controller
	PLICSize   uint64 = 0x0400_0000
	UARTBase   uint64 = 0x1000_0000 // UART for early console
	UARTSize   uint64 = 0x0000_1000
	VirtIOBase uint64 = 0x1000_1000 // VirtIO devices start here
	VirtIOSize uint64 = 0x0000_1000
)

// Privilege levels
const (
	PrivUser       uint8 = 0
	PrivSupervisor uint8 = 1
	PrivMachine    uint8 = 3
)

// ISA extension bits for misa
const (
	MisaA uint64 = 1 << 0  // Atomic
	MisaC uint64 = 1 << 2  // Compressed
	MisaD uint64 = 1 << 3  // Double-precision float
	MisaF uint64 = 1 << 5  // Single-precision float
	MisaI uint64 = 1 << 8  // RV64I base
	MisaM uint64 = 1 << 12 // Multiply/Divide
	MisaS uint64 = 1 << 18 // Supervisor mode
	MisaU uint64 = 1 << 20 // User mode
)

// MXL values for misa
const (
	MXL32 uint64 = 1
	MXL64 uint64 = 2
)

// mstatus bits
const (
	MstatusSIE  uint64 = 1 << 1
	MstatusMIE  uint64 = 1 << 3
	MstatusSPIE uint64 = 1 << 5
	MstatusMPIE uint64 = 1 << 7
	MstatusSPP  uint64 = 1 << 8
	MstatusMPP  uint64 = 3 << 11
	MstatusFS   uint64 = 3 << 13
	MstatusMPRV uint64 = 1 << 17
	MstatusSUM  uint64 = 1 << 18
	MstatusMXR  uint64 = 1 << 19
	MstatusTVM  uint64 = 1 << 20
	MstatusTW   uint64 = 1 << 21
	MstatusTSR  uint64 = 1 << 22
	MstatusSD   uint64 = 1 << 63
)

// mstatus bit positions
const (
	MstatusSPPShift = 8
	MstatusMPPShift = 11
	MstatusFSShift  = 13
)

// mip/mie bits
const (
	MipSSIP uint64 = 1 << 1  // Supervisor software interrupt pending
	MipMSIP uint64 = 1 << 3  // Machine software interrupt pending
	MipSTIP uint64 = 1 << 5  // Supervisor timer interrupt pending
	MipMTIP uint64 = 1 << 7  // Machine timer interrupt pending
	MipSEIP uint64 = 1 << 9  // Supervisor external interrupt pending
	MipMEIP uint64 = 1 << 11 // Machine external interrupt pending
)

// Exception causes
const (
	CauseInsnAddrMisaligned  uint64 = 0
	CauseInsnAccessFault     uint64 = 1
	CauseIllegalInsn         uint64 = 2
	CauseBreakpoint          uint64 = 3
	CauseLoadAddrMisaligned  uint64 = 4
	CauseLoadAccessFault     uint64 = 5
	CauseStoreAddrMisaligned uint64 = 6
	CauseStoreAccessFault    uint64 = 7
	CauseEcallFromU          uint64 = 8
	CauseEcallFromS          uint64 = 9
	CauseEcallFromM          uint64 = 11
	CauseInsnPageFault       uint64 = 12
	CauseLoadPageFault       uint64 = 13
	CauseStorePageFault      uint64 = 15
)

// Interrupt causes (with bit 63 set)
const (
	CauseSSoftwareInt uint64 = (1 << 63) | 1
	CauseMSoftwareInt uint64 = (1 << 63) | 3
	CauseSTimerInt    uint64 = (1 << 63) | 5
	CauseMTimerInt    uint64 = (1 << 63) | 7
	CauseSExternalInt uint64 = (1 << 63) | 9
	CauseMExternalInt uint64 = (1 << 63) | 11
)

// CSR addresses
const (
	CSRFflags    uint16 = 0x001
	CSRFrm       uint16 = 0x002
	CSRFcsr      uint16 = 0x003
	CSRCycle     uint16 = 0xC00
	CSRTime      uint16 = 0xC01
	CSRInstret   uint16 = 0xC02
	CSRSstatus   uint16 = 0x100
	CSRSie       uint16 = 0x104
	CSRStvec     uint16 = 0x105
	CSRScounteren uint16 = 0x106
	CSRSscratch  uint16 = 0x140
	CSRSepc      uint16 = 0x141
	CSRScause    uint16 = 0x142
	CSRStval     uint16 = 0x143
	CSRSip       uint16 = 0x144
	CSRSatp      uint16 = 0x180
	CSRMstatus   uint16 = 0x300
	CSRMisa      uint16 = 0x301
	CSRMedeleg   uint16 = 0x302
	CSRMideleg   uint16 = 0x303
	CSRMie       uint16 = 0x304
	CSRMtvec     uint16 = 0x305
	CSRMcounteren uint16 = 0x306
	CSRMscratch  uint16 = 0x340
	CSRMepc      uint16 = 0x341
	CSRMcause    uint16 = 0x342
	CSRMtval     uint16 = 0x343
	CSRMip       uint16 = 0x344
	CSRMhartid   uint16 = 0xF14
)

// CPU represents the RV64GC processor state
type CPU struct {
	// Integer registers x0-x31
	X [32]uint64

	// Floating point registers f0-f31
	F [32]uint64

	// Program counter
	PC uint64

	// Current privilege level
	Priv uint8

	// Cycle counter
	Cycle uint64

	// Instruction retired counter
	Instret uint64

	// CSRs - Machine mode
	Mstatus   uint64
	Misa      uint64
	Medeleg   uint64
	Mideleg   uint64
	Mie       uint64
	Mtvec     uint64
	Mcounteren uint64
	Mscratch  uint64
	Mepc      uint64
	Mcause    uint64
	Mtval     uint64
	Mip       uint64
	Mhartid   uint64

	// CSRs - Supervisor mode
	Sstatus   uint64 // Subset of mstatus
	Sie       uint64 // Subset of mie
	Stvec     uint64
	Scounteren uint64
	Sscratch  uint64
	Sepc      uint64
	Scause    uint64
	Stval     uint64
	Sip       uint64 // Subset of mip
	Satp      uint64

	// Floating point CSRs
	Fflags uint8
	Frm    uint8

	// Memory reservation for LR/SC
	Reservation     uint64
	ReservationValid bool

	// Reference to the bus for memory access
	Bus BusInterface

	// WFI flag - set when waiting for interrupt
	WFI bool

	// Debug log writer
	DebugLog io.Writer
}

// NewCPU creates a new CPU with default state
func NewCPU(bus BusInterface) *CPU {
	cpu := &CPU{
		Bus:  bus,
		Priv: PrivMachine,
		// RV64IMAFDC (GC)
		Misa: (MXL64 << 62) | MisaI | MisaM | MisaA | MisaF | MisaD | MisaC | MisaS | MisaU,
		// Start at RAM base
		PC: RAMBase,
	}
	return cpu
}

// Reset resets the CPU to its initial state
func (cpu *CPU) Reset() {
	for i := range cpu.X {
		cpu.X[i] = 0
	}
	for i := range cpu.F {
		cpu.F[i] = 0
	}
	cpu.PC = RAMBase
	cpu.Priv = PrivMachine
	cpu.Cycle = 0
	cpu.Instret = 0
	cpu.Mstatus = 0
	cpu.Mie = 0
	cpu.Mip = 0
	cpu.Mtvec = 0
	cpu.Mepc = 0
	cpu.Mcause = 0
	cpu.Mtval = 0
	cpu.Mscratch = 0
	cpu.Medeleg = 0
	cpu.Mideleg = 0
	cpu.Stvec = 0
	cpu.Sepc = 0
	cpu.Scause = 0
	cpu.Stval = 0
	cpu.Sscratch = 0
	cpu.Satp = 0
	cpu.WFI = false
	cpu.ReservationValid = false
}

// ReadReg reads an integer register (x0 always returns 0)
func (cpu *CPU) ReadReg(reg uint32) uint64 {
	if reg == 0 {
		return 0
	}
	return cpu.X[reg]
}

// WriteReg writes an integer register (writes to x0 are ignored)
func (cpu *CPU) WriteReg(reg uint32, val uint64) {
	if reg != 0 {
		cpu.X[reg] = val
	}
}

// Endianness
var cpuEndian = binary.LittleEndian

// signExtend sign-extends a value from 'bits' bits to 64 bits
func signExtend(val uint64, bits int) int64 {
	shift := 64 - bits
	return int64(val<<shift) >> shift
}

// signExtend32 sign-extends from 32 bits to 64 bits
func signExtend32(val uint32) int64 {
	return int64(int32(val))
}

// ExceptionError represents a CPU exception
type ExceptionError struct {
	Cause uint64
	Tval  uint64
}

func (e ExceptionError) Error() string {
	return fmt.Sprintf("exception: cause=%d tval=0x%x", e.Cause, e.Tval)
}

// Exception creates an exception with the given cause and tval
func Exception(cause uint64, tval uint64) error {
	return ExceptionError{Cause: cause, Tval: tval}
}
