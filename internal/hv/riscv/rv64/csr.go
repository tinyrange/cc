package rv64

// csrRead reads a CSR value
func (cpu *CPU) csrRead(csr uint16) (uint64, error) {
	// Check privilege level
	csrPriv := (csr >> 8) & 3
	if uint16(cpu.Priv) < csrPriv {
		return 0, Exception(CauseIllegalInsn, 0)
	}

	switch csr {
	// Floating point CSRs
	case CSRFflags:
		return uint64(cpu.Fflags), nil
	case CSRFrm:
		return uint64(cpu.Frm), nil
	case CSRFcsr:
		return uint64(cpu.Fflags) | (uint64(cpu.Frm) << 5), nil

	// User counters
	case CSRCycle:
		return cpu.Cycle, nil
	case CSRTime:
		return cpu.Cycle, nil // Use cycle as time for now
	case CSRInstret:
		return cpu.Instret, nil

	// Supervisor CSRs
	case CSRSstatus:
		return cpu.readSstatus(), nil
	case CSRSie:
		return cpu.Mie & cpu.Mideleg, nil
	case CSRStvec:
		return cpu.Stvec, nil
	case CSRScounteren:
		return cpu.Scounteren, nil
	case CSRSscratch:
		return cpu.Sscratch, nil
	case CSRSepc:
		return cpu.Sepc, nil
	case CSRScause:
		return cpu.Scause, nil
	case CSRStval:
		return cpu.Stval, nil
	case CSRSip:
		return cpu.Mip & cpu.Mideleg, nil
	case CSRSatp:
		return cpu.Satp, nil

	// Machine CSRs
	case CSRMstatus:
		return cpu.Mstatus, nil
	case CSRMisa:
		return cpu.Misa, nil
	case CSRMedeleg:
		return cpu.Medeleg, nil
	case CSRMideleg:
		return cpu.Mideleg, nil
	case CSRMie:
		return cpu.Mie, nil
	case CSRMtvec:
		return cpu.Mtvec, nil
	case CSRMcounteren:
		return cpu.Mcounteren, nil
	case CSRMscratch:
		return cpu.Mscratch, nil
	case CSRMepc:
		return cpu.Mepc, nil
	case CSRMcause:
		return cpu.Mcause, nil
	case CSRMtval:
		return cpu.Mtval, nil
	case CSRMip:
		return cpu.Mip, nil
	case CSRMhartid:
		return cpu.Mhartid, nil

	default:
		// Unknown CSR - return 0 for now to allow Linux to boot
		return 0, nil
	}
}

// csrWrite writes a CSR value
func (cpu *CPU) csrWrite(csr uint16, val uint64) error {
	// Check privilege level
	csrPriv := (csr >> 8) & 3
	if uint16(cpu.Priv) < csrPriv {
		return Exception(CauseIllegalInsn, 0)
	}

	// Check if read-only (top 2 bits = 11)
	if (csr >> 10) == 3 {
		return Exception(CauseIllegalInsn, 0)
	}

	switch csr {
	// Floating point CSRs
	case CSRFflags:
		cpu.Fflags = uint8(val & 0x1f)
	case CSRFrm:
		cpu.Frm = uint8(val & 0x7)
	case CSRFcsr:
		cpu.Fflags = uint8(val & 0x1f)
		cpu.Frm = uint8((val >> 5) & 0x7)

	// Supervisor CSRs
	case CSRSstatus:
		cpu.writeSstatus(val)
	case CSRSie:
		cpu.Mie = (cpu.Mie &^ cpu.Mideleg) | (val & cpu.Mideleg)
	case CSRStvec:
		cpu.Stvec = val
	case CSRScounteren:
		cpu.Scounteren = val
	case CSRSscratch:
		cpu.Sscratch = val
	case CSRSepc:
		cpu.Sepc = val & ^uint64(1) // Must be aligned
	case CSRScause:
		cpu.Scause = val
	case CSRStval:
		cpu.Stval = val
	case CSRSip:
		// Only SSIP is writable
		cpu.Mip = (cpu.Mip &^ MipSSIP) | (val & MipSSIP)
	case CSRSatp:
		cpu.Satp = val

	// Machine CSRs
	case CSRMstatus:
		cpu.writeMstatus(val)
	case CSRMisa:
		// Read-only in this implementation
	case CSRMedeleg:
		cpu.Medeleg = val & 0xb3ff // Only certain bits are writable
	case CSRMideleg:
		cpu.Mideleg = val & (MipSSIP | MipSTIP | MipSEIP)
	case CSRMie:
		cpu.Mie = val & (MipSSIP | MipMSIP | MipSTIP | MipMTIP | MipSEIP | MipMEIP)
	case CSRMtvec:
		cpu.Mtvec = val
	case CSRMcounteren:
		cpu.Mcounteren = val
	case CSRMscratch:
		cpu.Mscratch = val
	case CSRMepc:
		cpu.Mepc = val & ^uint64(1) // Must be aligned
	case CSRMcause:
		cpu.Mcause = val
	case CSRMtval:
		cpu.Mtval = val
	case CSRMip:
		// Only SSIP, STIP, SEIP are writable via mip
		mask := uint64(MipSSIP | MipSTIP | MipSEIP)
		cpu.Mip = (cpu.Mip &^ mask) | (val & mask)
	}

	return nil
}

// Sstatus mask - bits visible in sstatus
const sstatusMask = MstatusSIE | MstatusSPIE | MstatusSPP | MstatusFS |
	MstatusSUM | MstatusMXR | MstatusSD

// readSstatus reads the sstatus view of mstatus
func (cpu *CPU) readSstatus() uint64 {
	return cpu.Mstatus & sstatusMask
}

// writeSstatus writes the sstatus view of mstatus
func (cpu *CPU) writeSstatus(val uint64) {
	cpu.Mstatus = (cpu.Mstatus &^ sstatusMask) | (val & sstatusMask)
}

// writeMstatus writes mstatus with proper masking
func (cpu *CPU) writeMstatus(val uint64) {
	// Writable bits in mstatus
	const mstatusMask = MstatusSIE | MstatusMIE | MstatusSPIE | MstatusMPIE |
		MstatusSPP | MstatusMPP | MstatusFS | MstatusMPRV | MstatusSUM |
		MstatusMXR | MstatusTVM | MstatusTW | MstatusTSR

	cpu.Mstatus = (cpu.Mstatus &^ mstatusMask) | (val & mstatusMask)

	// Update SD bit based on FS
	if (cpu.Mstatus & MstatusFS) == MstatusFS {
		cpu.Mstatus |= MstatusSD
	} else {
		cpu.Mstatus &^= MstatusSD
	}
}

// CheckInterrupt checks if there's a pending interrupt that should be taken
func (cpu *CPU) CheckInterrupt() (bool, uint64) {
	// Get pending and enabled interrupts
	pending := cpu.Mip & cpu.Mie

	if pending == 0 {
		return false, 0
	}

	// Check if interrupts are globally enabled
	if cpu.Priv == PrivMachine {
		if (cpu.Mstatus & MstatusMIE) == 0 {
			return false, 0
		}
	} else if cpu.Priv == PrivSupervisor {
		if (cpu.Mstatus & MstatusSIE) == 0 {
			// Still check for M-mode interrupts
			mInt := pending &^ cpu.Mideleg
			if mInt == 0 {
				return false, 0
			}
			pending = mInt
		}
	}
	// U-mode always has interrupts enabled

	// Find highest priority interrupt
	// Machine interrupts have higher priority than supervisor
	// External > Software > Timer

	// Machine external interrupt
	if pending&MipMEIP != 0 && (cpu.Priv < PrivMachine || (cpu.Mstatus&MstatusMIE != 0)) {
		return true, CauseMExternalInt
	}
	// Machine software interrupt
	if pending&MipMSIP != 0 && (cpu.Priv < PrivMachine || (cpu.Mstatus&MstatusMIE != 0)) {
		return true, CauseMSoftwareInt
	}
	// Machine timer interrupt
	if pending&MipMTIP != 0 && (cpu.Priv < PrivMachine || (cpu.Mstatus&MstatusMIE != 0)) {
		return true, CauseMTimerInt
	}
	// Supervisor external interrupt
	if pending&MipSEIP != 0 {
		if cpu.Priv < PrivSupervisor || (cpu.Priv == PrivSupervisor && (cpu.Mstatus&MstatusSIE != 0)) {
			return true, CauseSExternalInt
		}
	}
	// Supervisor software interrupt
	if pending&MipSSIP != 0 {
		if cpu.Priv < PrivSupervisor || (cpu.Priv == PrivSupervisor && (cpu.Mstatus&MstatusSIE != 0)) {
			return true, CauseSSoftwareInt
		}
	}
	// Supervisor timer interrupt
	if pending&MipSTIP != 0 {
		if cpu.Priv < PrivSupervisor || (cpu.Priv == PrivSupervisor && (cpu.Mstatus&MstatusSIE != 0)) {
			return true, CauseSTimerInt
		}
	}

	return false, 0
}

// HandleTrap handles a trap (exception or interrupt)
func (cpu *CPU) HandleTrap(cause uint64, tval uint64) {
	isInterrupt := (cause >> 63) != 0
	exceptionCode := cause & 0x7fffffffffffffff

	// Determine if trap should be delegated to S-mode
	delegateToS := false
	if cpu.Priv <= PrivSupervisor {
		if isInterrupt {
			if (cpu.Mideleg & (1 << exceptionCode)) != 0 {
				delegateToS = true
			}
		} else {
			if (cpu.Medeleg & (1 << exceptionCode)) != 0 {
				delegateToS = true
			}
		}
	}

	if delegateToS {
		// Trap to S-mode
		cpu.Sepc = cpu.PC
		cpu.Scause = cause
		cpu.Stval = tval

		// Save current SIE to SPIE
		if cpu.Mstatus&MstatusSIE != 0 {
			cpu.Mstatus |= MstatusSPIE
		} else {
			cpu.Mstatus &^= MstatusSPIE
		}

		// Clear SIE
		cpu.Mstatus &^= MstatusSIE

		// Save current privilege to SPP
		if cpu.Priv == PrivSupervisor {
			cpu.Mstatus |= MstatusSPP
		} else {
			cpu.Mstatus &^= MstatusSPP
		}

		// Set privilege to Supervisor
		cpu.Priv = PrivSupervisor

		// Jump to stvec
		if (cpu.Stvec & 1) == 1 && isInterrupt {
			// Vectored mode for interrupts
			cpu.PC = (cpu.Stvec &^ 1) + 4*exceptionCode
		} else {
			cpu.PC = cpu.Stvec &^ 3
		}
	} else {
		// Trap to M-mode
		cpu.Mepc = cpu.PC
		cpu.Mcause = cause
		cpu.Mtval = tval

		// Save current MIE to MPIE
		if cpu.Mstatus&MstatusMIE != 0 {
			cpu.Mstatus |= MstatusMPIE
		} else {
			cpu.Mstatus &^= MstatusMPIE
		}

		// Clear MIE
		cpu.Mstatus &^= MstatusMIE

		// Save current privilege to MPP
		cpu.Mstatus &^= MstatusMPP
		cpu.Mstatus |= uint64(cpu.Priv) << MstatusMPPShift

		// Set privilege to Machine
		cpu.Priv = PrivMachine

		// Jump to mtvec
		if (cpu.Mtvec & 1) == 1 && isInterrupt {
			// Vectored mode for interrupts
			cpu.PC = (cpu.Mtvec &^ 1) + 4*exceptionCode
		} else {
			cpu.PC = cpu.Mtvec &^ 3
		}
	}
}
