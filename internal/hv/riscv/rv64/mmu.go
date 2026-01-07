package rv64

// SATP modes
const (
	SatpModeOff  = 0
	SatpModeSv39 = 8
	SatpModeSv48 = 9
)

// Page table entry flags
const (
	PteV = 1 << 0 // Valid
	PteR = 1 << 1 // Readable
	PteW = 1 << 2 // Writable
	PteX = 1 << 3 // Executable
	PteU = 1 << 4 // User accessible
	PteG = 1 << 5 // Global
	PteA = 1 << 6 // Accessed
	PteD = 1 << 7 // Dirty
)

// Page sizes
const (
	PageSize     = 4096
	PageShift    = 12
	PteLevels39  = 3
	PteLevels48  = 4
	VpnBits      = 9
	PpnBits      = 44
)

// TLB entry
type TLBEntry struct {
	Valid    bool
	VPN      uint64 // Virtual page number
	PPN      uint64 // Physical page number
	Flags    uint64
	PageSize uint64 // For superpages
	ASID     uint16
}

// MMU handles virtual to physical address translation
type MMU struct {
	cpu *CPU

	// TLB caches
	tlb [512]TLBEntry
}

// NewMMU creates a new MMU
func NewMMU(cpu *CPU) *MMU {
	return &MMU{
		cpu: cpu,
	}
}

// FlushTLB invalidates all TLB entries
func (mmu *MMU) FlushTLB() {
	for i := range mmu.tlb {
		mmu.tlb[i].Valid = false
	}
}

// FlushTLBEntry invalidates a specific TLB entry
func (mmu *MMU) FlushTLBEntry(vaddr uint64, asid uint16) {
	vpn := vaddr >> PageShift
	idx := vpn & uint64(len(mmu.tlb)-1)
	entry := &mmu.tlb[idx]
	if entry.Valid && (asid == 0 || entry.ASID == asid) && entry.VPN == vpn {
		entry.Valid = false
	}
}

// Translate translates a virtual address to a physical address
// access: 0=read, 1=write, 2=execute
func (mmu *MMU) Translate(vaddr uint64, access int) (uint64, error) {
	// Check if address translation is enabled
	mode := (mmu.cpu.Satp >> 60) & 0xf

	if mode == SatpModeOff {
		// No translation - bare mode
		return vaddr, nil
	}

	// Check privilege level
	priv := mmu.cpu.Priv

	// Handle MPRV bit
	if mmu.cpu.Priv == PrivMachine && access != 2 && (mmu.cpu.Mstatus&MstatusMPRV) != 0 {
		priv = uint8((mmu.cpu.Mstatus >> MstatusMPPShift) & 3)
	}

	// M-mode doesn't use translation (except with MPRV)
	if priv == PrivMachine {
		return vaddr, nil
	}

	// Check TLB first
	vpn := vaddr >> PageShift
	idx := vpn & uint64(len(mmu.tlb)-1)
	entry := &mmu.tlb[idx]

	asid := uint16((mmu.cpu.Satp >> 44) & 0xffff)

	if entry.Valid && entry.VPN == vpn && (entry.ASID == asid || entry.Flags&PteG != 0) {
		// TLB hit - check permissions
		if err := mmu.checkPermissions(entry.Flags, access, priv); err != nil {
			return 0, err
		}

		// Check and set A/D bits
		if entry.Flags&PteA == 0 {
			entry.Valid = false // Force page walk to set A bit
		} else if access == 1 && entry.Flags&PteD == 0 {
			entry.Valid = false // Force page walk to set D bit
		} else {
			pageOffset := vaddr & (entry.PageSize - 1)
			return (entry.PPN << PageShift) | pageOffset, nil
		}
	}

	// TLB miss - do page table walk
	paddr, flags, pageSize, err := mmu.walkPageTable(vaddr, access, priv, mode)
	if err != nil {
		return 0, err
	}

	// Update TLB
	entry.Valid = true
	entry.VPN = vpn
	entry.PPN = paddr >> PageShift
	entry.Flags = flags
	entry.PageSize = pageSize
	entry.ASID = asid

	return paddr, nil
}

// walkPageTable performs a page table walk
func (mmu *MMU) walkPageTable(vaddr uint64, access int, priv uint8, mode uint64) (uint64, uint64, uint64, error) {
	var levels int
	var vpnMask uint64

	switch mode {
	case SatpModeSv39:
		levels = 3
		vpnMask = 0x1ff // 9 bits
		// Check if address is canonical (sign-extended from bit 38)
		if vaddr >= (1 << 38) && vaddr < (^uint64(0) - (1 << 38)) {
			return 0, 0, 0, mmu.pageFault(access, vaddr)
		}
	case SatpModeSv48:
		levels = 4
		vpnMask = 0x1ff // 9 bits
		// Check if address is canonical (sign-extended from bit 47)
		if vaddr >= (1 << 47) && vaddr < (^uint64(0) - (1 << 47)) {
			return 0, 0, 0, mmu.pageFault(access, vaddr)
		}
	default:
		return vaddr, PteR | PteW | PteX, PageSize, nil // Unknown mode - identity map
	}

	// Get root page table address from satp
	ppn := mmu.cpu.Satp & ((1 << PpnBits) - 1)
	pteAddr := ppn << PageShift

	var pte uint64
	var pageSize uint64 = PageSize

	for level := levels - 1; level >= 0; level-- {
		// Calculate VPN for this level
		vpnShift := PageShift + level*VpnBits
		vpn := (vaddr >> vpnShift) & vpnMask

		// Read PTE
		pteAddr = pteAddr + vpn*8
		val, err := mmu.cpu.Bus.Read64(pteAddr)
		if err != nil {
			return 0, 0, 0, mmu.pageFault(access, vaddr)
		}
		pte = val

		// Check valid bit
		if pte&PteV == 0 {
			return 0, 0, 0, mmu.pageFault(access, vaddr)
		}

		// Check for invalid combinations
		if pte&PteR == 0 && pte&PteW != 0 {
			return 0, 0, 0, mmu.pageFault(access, vaddr)
		}

		// Check if this is a leaf PTE
		if pte&PteR != 0 || pte&PteX != 0 {
			// Leaf PTE
			// Check for misaligned superpage
			if level > 0 {
				mask := uint64((1 << (level * VpnBits)) - 1)
				if ((pte >> 10) & mask) != 0 {
					return 0, 0, 0, mmu.pageFault(access, vaddr)
				}
				pageSize = 1 << (PageShift + level*VpnBits)
			}

			// Check permissions
			if err := mmu.checkPermissions(pte, access, priv); err != nil {
				return 0, 0, 0, err
			}

			// Check and set A/D bits
			if pte&PteA == 0 || (access == 1 && pte&PteD == 0) {
				// Set A bit, and D bit for writes
				newPte := pte | PteA
				if access == 1 {
					newPte |= PteD
				}
				if err := mmu.cpu.Bus.Write64(pteAddr, newPte); err != nil {
					return 0, 0, 0, mmu.pageFault(access, vaddr)
				}
				pte = newPte
			}

			// Calculate physical address
			ppn := (pte >> 10) & ((1 << PpnBits) - 1)
			pageOffset := vaddr & (pageSize - 1)

			// For superpages, include VPN bits in physical address
			if level > 0 {
				mask := uint64((1 << (level * VpnBits)) - 1)
				vpnBits := (vaddr >> PageShift) & mask
				ppn = (ppn &^ mask) | vpnBits
			}

			paddr := (ppn << PageShift) | pageOffset
			return paddr, pte, pageSize, nil
		}

		// Non-leaf PTE - continue to next level
		ppn := (pte >> 10) & ((1 << PpnBits) - 1)
		pteAddr = ppn << PageShift
	}

	// Shouldn't reach here
	return 0, 0, 0, mmu.pageFault(access, vaddr)
}

// checkPermissions checks if access is allowed
func (mmu *MMU) checkPermissions(pte uint64, access int, priv uint8) error {
	// Check user/supervisor access
	if priv == PrivUser {
		if pte&PteU == 0 {
			return mmu.pageFault(access, 0)
		}
	} else {
		// Supervisor mode
		if pte&PteU != 0 {
			// User page accessed from supervisor
			if (mmu.cpu.Mstatus & MstatusSUM) == 0 {
				return mmu.pageFault(access, 0)
			}
		}
	}

	// Check read/write/execute permissions
	switch access {
	case 0: // Read
		if pte&PteR == 0 {
			// Check MXR bit for execute-only pages
			if (mmu.cpu.Mstatus&MstatusMXR) != 0 && (pte&PteX) != 0 {
				return nil
			}
			return mmu.pageFault(access, 0)
		}
	case 1: // Write
		if pte&PteW == 0 {
			return mmu.pageFault(access, 0)
		}
	case 2: // Execute
		if pte&PteX == 0 {
			return mmu.pageFault(access, 0)
		}
	}

	return nil
}

// pageFault returns the appropriate page fault exception
func (mmu *MMU) pageFault(access int, vaddr uint64) error {
	switch access {
	case 0:
		return Exception(CauseLoadPageFault, vaddr)
	case 1:
		return Exception(CauseStorePageFault, vaddr)
	case 2:
		return Exception(CauseInsnPageFault, vaddr)
	}
	return Exception(CauseLoadPageFault, vaddr)
}

// TranslateRead translates a read access
func (mmu *MMU) TranslateRead(vaddr uint64) (uint64, error) {
	return mmu.Translate(vaddr, 0)
}

// TranslateWrite translates a write access
func (mmu *MMU) TranslateWrite(vaddr uint64) (uint64, error) {
	return mmu.Translate(vaddr, 1)
}

// TranslateFetch translates an instruction fetch
func (mmu *MMU) TranslateFetch(vaddr uint64) (uint64, error) {
	return mmu.Translate(vaddr, 2)
}
