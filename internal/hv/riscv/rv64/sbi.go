package rv64

import "fmt"

// SBI Extension IDs
const (
	SBIExtBase        = 0x10
	SBIExtTimer       = 0x54494D45 // "TIME"
	SBIExtIPI         = 0x735049   // "sPI"
	SBIExtRFence      = 0x52464E43 // "RFNC"
	SBIExtHSM         = 0x48534D   // "HSM"
	SBIExtSRST        = 0x53525354 // "SRST"
	SBIExtLegacyPutchar = 0x01
	SBIExtLegacyGetchar = 0x02
)

// SBI Base extension function IDs
const (
	SBIBaseGetSpecVersion  = 0
	SBIBaseGetImplID       = 1
	SBIBaseGetImplVersion  = 2
	SBIBaseProbeExtension  = 3
	SBIBaseGetMvendorID    = 4
	SBIBaseGetMarchID      = 5
	SBIBaseGetMimplID      = 6
)

// SBI Timer extension function IDs
const (
	SBITimerSetTimer = 0
)

// SBI HSM (Hart State Management) function IDs
const (
	SBIHSMHartStart  = 0
	SBIHSMHartStop   = 1
	SBIHSMHartStatus = 2
)

// SBI error codes
const (
	SBISuccess            = 0
	SBIErrFailed          = -1
	SBIErrNotSupported    = -2
	SBIErrInvalidParam    = -3
	SBIErrDenied          = -4
	SBIErrInvalidAddress  = -5
	SBIErrAlreadyAvail    = -6
)

// HandleSBI handles SBI calls from S-mode
// a7 = extension ID, a6 = function ID
// a0-a5 = arguments
// Returns: a0 = error code, a1 = value
func (m *Machine) HandleSBI() error {
	ext := m.CPU.X[17]  // a7
	fid := m.CPU.X[16]  // a6

	// Debug SBI calls
	if m.DebugOutput != nil {
		fmt.Fprintf(m.DebugOutput, "SBI call: ext=0x%x fid=%d a0=0x%x a1=0x%x PC=0x%x\n",
			ext, fid, m.CPU.X[10], m.CPU.X[11], m.CPU.PC)
	}

	var err int64 = SBISuccess
	var val uint64 = 0

	switch ext {
	case SBIExtLegacyPutchar:
		// Legacy console putchar - a0 = character
		ch := byte(m.CPU.X[10])
		if m.UART.Output != nil {
			m.UART.Output.Write([]byte{ch})
		}
		err = SBISuccess

	case SBIExtLegacyGetchar:
		// Legacy console getchar - returns char in a0 or -1
		// Read from UART if data available
		v, _ := m.UART.Read(UARTRegLSR, 1)
		if v&UARTLSRDataReady != 0 {
			v, _ = m.UART.Read(UARTRegRBR, 1)
			val = v
		} else {
			val = 0xffffffffffffffff // -1
		}
		err = SBISuccess

	case SBIExtBase:
		err, val = m.handleSBIBase(fid)

	case SBIExtTimer:
		err, val = m.handleSBITimer(fid)

	case SBIExtIPI:
		// IPI - just succeed (single hart)
		err = SBISuccess

	case SBIExtRFence:
		// Remote fence - just succeed (single hart, no action needed)
		err = SBISuccess

	case SBIExtHSM:
		err, val = m.handleSBIHSM(fid)

	case SBIExtSRST:
		// System reset - halt
		return ErrHalt

	default:
		// Unknown extension
		err = SBIErrNotSupported
	}

	// Return values
	m.CPU.X[10] = uint64(err) // a0 = error
	m.CPU.X[11] = val         // a1 = value

	return nil
}

func (m *Machine) handleSBIBase(fid uint64) (int64, uint64) {
	switch fid {
	case SBIBaseGetSpecVersion:
		// SBI spec version 1.0
		return SBISuccess, 0x01000000

	case SBIBaseGetImplID:
		// Our implementation ID (made up)
		return SBISuccess, 0x43435f5256363447 // "CC_RV64G"

	case SBIBaseGetImplVersion:
		return SBISuccess, 0x00010000

	case SBIBaseProbeExtension:
		// Check if extension is supported
		extID := m.CPU.X[10]
		switch extID {
		case SBIExtBase, SBIExtTimer, SBIExtIPI, SBIExtRFence, SBIExtHSM,
			SBIExtLegacyPutchar, SBIExtLegacyGetchar:
			return SBISuccess, 1
		default:
			return SBISuccess, 0
		}

	case SBIBaseGetMvendorID:
		return SBISuccess, 0

	case SBIBaseGetMarchID:
		return SBISuccess, 0

	case SBIBaseGetMimplID:
		return SBISuccess, 0

	default:
		return SBIErrNotSupported, 0
	}
}

func (m *Machine) handleSBITimer(fid uint64) (int64, uint64) {
	switch fid {
	case SBITimerSetTimer:
		// Set timer - a0 = stime_value
		stime := m.CPU.X[10]
		m.CLINT.SetTimecmp(0, stime)
		// Clear pending timer interrupt
		m.CPU.Mip &^= MipSTIP
		return SBISuccess, 0

	default:
		return SBIErrNotSupported, 0
	}
}

func (m *Machine) handleSBIHSM(fid uint64) (int64, uint64) {
	switch fid {
	case SBIHSMHartStatus:
		// Hart 0 is always running
		hartid := m.CPU.X[10]
		if hartid == 0 {
			return SBISuccess, 0 // STARTED
		}
		return SBIErrInvalidParam, 0

	case SBIHSMHartStart:
		// Only hart 0, already started
		return SBIErrAlreadyAvail, 0

	case SBIHSMHartStop:
		return SBIErrNotSupported, 0

	default:
		return SBIErrNotSupported, 0
	}
}

// SetupPageTables creates initial SV39 page tables for kernel boot
// Returns the SATP value to use
func (m *Machine) SetupPageTables() uint64 {
	// Page table location - put it right at start of RAM
	// We'll use first 8KB for page tables (2 pages)
	rootTable := RAMBase            // Level 2 (root)
	level1Table := RAMBase + 0x1000 // Level 1 for high addresses

	// Clear page table area
	for i := uint64(0); i < 0x3000; i++ {
		m.Bus.Write8(RAMBase+i, 0)
	}

	// PTE bits
	const (
		PTE_V = 1 << 0  // Valid
		PTE_R = 1 << 1  // Readable
		PTE_W = 1 << 2  // Writable
		PTE_X = 1 << 3  // Executable
		PTE_U = 1 << 4  // User accessible
		PTE_G = 1 << 5  // Global
		PTE_A = 1 << 6  // Accessed
		PTE_D = 1 << 7  // Dirty
	)

	// Helper to create a leaf PTE for 1GB page
	makePTE1G := func(pa uint64) uint64 {
		return (pa >> 12) << 10 | PTE_D | PTE_A | PTE_G | PTE_X | PTE_W | PTE_R | PTE_V
	}

	// Helper to create a non-leaf PTE pointing to next level
	makePTENonLeaf := func(nextTable uint64) uint64 {
		return (nextTable >> 12) << 10 | PTE_V
	}

	// Identity map: VA 0x80000000 -> PA 0x80000000 (1GB huge page)
	// VPN[2] = (0x80000000 >> 30) & 0x1FF = 2
	m.Bus.Write64(rootTable+2*8, makePTE1G(0x80000000))

	// Map entry 511 to level1Table for kernel addresses
	// High kernel addresses (0xFFFFFFc0_00000000 - 0xFFFFFFFF_FFFFFFFF)
	// VPN[2] for 0xFFFFFFc0_00000000 = 511
	m.Bus.Write64(rootTable+511*8, makePTENonLeaf(level1Table))

	// In level1Table, map VPN[1] entries for kernel linear mapping
	// Linux RISC-V kernel linear map starts at 0xFFFFFFc0_80000000
	// VPN[1] for 0xFFFFFFc0_80000000 = (0x80000000 >> 21) & 0x1FF = 2
	// But we need 1GB pages... level 1 gives 2MB pages
	// For 1GB at level 1, we need to fill entries 0-511 for a full mapping

	// Actually, for simplicity, let's map entry 2 and 3 in level1Table
	// Entry 2 covers VA 0xFFFFFFc0_40000000 - 0xFFFFFFc0_7FFFFFFF (but we want 80000000)
	// Entry 2 in level 1: VA range = base + entry * 2MB
	// 0xFFFFFFc0_00000000 + 2 * 0x40000000 = 0xFFFFFFc0_80000000 (entry 2 of level 2)

	// Wait, I'm confusing myself. Let me recalculate:
	// For VA 0xFFFFFFc0_80000000:
	// VPN[2] = bits[38:30] = 0x1FF = 511 ✓ (covered by rootTable entry 511)
	// VPN[1] = bits[29:21] = (0x80000000 >> 21) & 0x1FF = 0x40 = 64
	// So in level1Table, entry 64 should map to PA 0x80000000

	// For a 2MB page in level 1:
	for i := uint64(0); i < 64; i++ {
		// Map 2MB pages: VA 0xFFFFFFc0_80000000 + i*2MB -> PA 0x80000000 + i*2MB
		pa := uint64(0x80000000) + i*0x200000
		pte := (pa >> 12) << 10 | PTE_D | PTE_A | PTE_G | PTE_X | PTE_W | PTE_R | PTE_V
		m.Bus.Write64(level1Table+(64+i)*8, pte)
	}

	// Also map the very high addresses (last few entries) to RAM for percpu/stack
	// VA 0xFFFFFFFF_FFFFFFFF and nearby - VPN[1] = 511
	// Map entries 500-511 in level1Table to cover the top of address space
	for i := uint64(500); i < 512; i++ {
		// Map to somewhere in RAM (use 0x80000000 area)
		pa := uint64(0x80000000) + (i-500)*0x200000
		pte := (pa >> 12) << 10 | PTE_D | PTE_A | PTE_G | PTE_X | PTE_W | PTE_R | PTE_V
		m.Bus.Write64(level1Table+i*8, pte)
	}

	// SATP = MODE[63:60] | ASID[59:44] | PPN[43:0]
	// MODE = 8 for SV39
	satpVal := uint64(8)<<60 | (rootTable >> 12)

	return satpVal
}

// SetupForLinux configures the CPU state for Linux boot with SBI
func (m *Machine) SetupForLinux(hartid uint64, dtbAddr uint64, kernelEntry uint64) {
	// Set up registers
	m.CPU.X[10] = hartid   // a0 = hart ID
	m.CPU.X[11] = dtbAddr  // a1 = DTB address
	m.CPU.PC = kernelEntry

	// Start in S-mode (Linux expects this with SBI)
	m.CPU.Priv = PrivSupervisor

	// Configure mstatus for S-mode operation
	// SPP = 1 (came from S-mode), SPIE = 1, FS = Initial (1)
	m.CPU.Mstatus = MstatusSPIE | MstatusSPP | (1 << MstatusFSShift)

	// Delegate interrupts and exceptions to S-mode
	// Delegate: user ecall, access faults, page faults, etc.
	m.CPU.Medeleg = (1 << CauseEcallFromU) |
		(1 << CauseInsnAccessFault) |
		(1 << CauseLoadAccessFault) |
		(1 << CauseStoreAccessFault) |
		(1 << CauseInsnPageFault) |
		(1 << CauseLoadPageFault) |
		(1 << CauseStorePageFault) |
		(1 << CauseBreakpoint) |
		(1 << CauseIllegalInsn)

	// Delegate timer, software, and external interrupts to S-mode
	m.CPU.Mideleg = MipSSIP | MipSTIP | MipSEIP

	// Set machine trap vector for SBI calls
	// We'll handle ecall from S-mode in M-mode
	m.CPU.Mtvec = 0 // Will be handled specially

	// Enable counter access from S-mode (bits: CY=0, TM=1, IR=2)
	m.CPU.Mcounteren = 0x7 // CY, TM, IR

	fmt.Printf("SetupForLinux: PC=0x%x, Priv=%d, medeleg=0x%x, mideleg=0x%x\n",
		m.CPU.PC, m.CPU.Priv, m.CPU.Medeleg, m.CPU.Mideleg)
}

// SetupForLinuxWithMMU configures the CPU state with initial page tables
func (m *Machine) SetupForLinuxWithMMU(hartid uint64, dtbAddr uint64, kernelEntry uint64) {
	// Set up initial page tables
	satpVal := m.SetupPageTables()

	// Basic setup
	m.CPU.X[10] = hartid
	m.CPU.X[11] = dtbAddr
	m.CPU.PC = kernelEntry
	m.CPU.Priv = PrivSupervisor
	m.CPU.Mstatus = MstatusSPIE | MstatusSPP

	// Delegate exceptions
	m.CPU.Medeleg = (1 << CauseEcallFromU) |
		(1 << CauseInsnPageFault) |
		(1 << CauseLoadPageFault) |
		(1 << CauseStorePageFault) |
		(1 << CauseBreakpoint) |
		(1 << CauseIllegalInsn)

	m.CPU.Mideleg = MipSSIP | MipSTIP | MipSEIP
	m.CPU.Mtvec = 0

	// Enable MMU - set SATP
	m.CPU.Satp = satpVal

	fmt.Printf("SetupForLinuxWithMMU: PC=0x%x, Priv=%d, SATP=0x%x\n",
		m.CPU.PC, m.CPU.Priv, m.CPU.Satp)
}
