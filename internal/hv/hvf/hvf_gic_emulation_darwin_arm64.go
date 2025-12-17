//go:build darwin && arm64

package hvf

import (
	"encoding/binary"
	"fmt"
	"sync"

	"github.com/tinyrange/cc/internal/hv"
)

// GICv3 register offsets within the redistributor (per-CPU region)
const (
	// RD_base (first 64KB of each redistributor)
	gicrCtlr      = 0x0000 // Redistributor Control Register
	gicrIidr      = 0x0004 // Implementer Identification Register
	gicrTyper     = 0x0008 // Redistributor Type Register
	gicrStatusr   = 0x0010 // Error Reporting Status Register
	gicrWaker     = 0x0014 // Redistributor Wake Register
	gicrPropbaser = 0x0070 // LPI Configuration Table Address
	gicrPendbaser = 0x0078 // LPI Pending Table Address

	// SGI_base (second 64KB of each redistributor)
	gicrSGIOffset  = 0x10000
	gicrIgroupr0   = gicrSGIOffset + 0x0080 // Interrupt Group Register 0
	gicrIsenabler0 = gicrSGIOffset + 0x0100 // Interrupt Set-Enable Register 0
	gicrIcenabler0 = gicrSGIOffset + 0x0180 // Interrupt Clear-Enable Register 0
	gicrIspendr0   = gicrSGIOffset + 0x0200 // Interrupt Set-Pending Register 0
	gicrIcpendr0   = gicrSGIOffset + 0x0280 // Interrupt Clear-Pending Register 0
	gicrIsactiver0 = gicrSGIOffset + 0x0300 // Interrupt Set-Active Register 0
	gicrIcactiver0 = gicrSGIOffset + 0x0380 // Interrupt Clear-Active Register 0
	gicrIpriorityr = gicrSGIOffset + 0x0400 // Interrupt Priority Registers (0-7)
	gicrIcfgr0     = gicrSGIOffset + 0x0C00 // Interrupt Configuration Register 0
	gicrIcfgr1     = gicrSGIOffset + 0x0C04 // Interrupt Configuration Register 1
	gicrIgrpmodr0  = gicrSGIOffset + 0x0D00 // Interrupt Group Modifier Register 0
	gicrNsacr      = gicrSGIOffset + 0x0E00 // Non-secure Access Control Register

	// Peripheral ID registers (at the end of each 64KB block)
	gicrPidr2RDBase  = 0xFFE8                 // Peripheral ID 2 (RD_base)
	gicrPidr2SGIBase = gicrSGIOffset + 0xFFE8 // Peripheral ID 2 (SGI_base)

	// GIC Distributor offsets
	gicdCtlr       = 0x0000 // Distributor Control Register
	gicdTyper      = 0x0004 // Interrupt Controller Type Register
	gicdIidr       = 0x0008 // Distributor Implementer Identification Register
	gicdTyper2     = 0x000C // Interrupt Controller Type Register 2
	gicdStatusr    = 0x0010 // Error Reporting Status Register
	gicdSetspi_nsr = 0x0040 // Set SPI Register (Non-secure)
	gicdClrspi_nsr = 0x0048 // Clear SPI Register (Non-secure)
	gicdIgroupr    = 0x0080 // Interrupt Group Registers
	gicdIsenabler  = 0x0100 // Interrupt Set-Enable Registers
	gicdIcenabler  = 0x0180 // Interrupt Clear-Enable Registers
	gicdIspendr    = 0x0200 // Interrupt Set-Pending Registers
	gicdIcpendr    = 0x0280 // Interrupt Clear-Pending Registers
	gicdIsactiver  = 0x0300 // Interrupt Set-Active Registers
	gicdIcactiver  = 0x0380 // Interrupt Clear-Active Registers
	gicdIpriorityr = 0x0400 // Interrupt Priority Registers
	gicdItargetsr  = 0x0800 // Interrupt Processor Targets Registers (GICv2 compat)
	gicdIcfgr      = 0x0C00 // Interrupt Configuration Registers
	gicdIgrpmodr   = 0x0D00 // Interrupt Group Modifier Registers
	gicdNsacr      = 0x0E00 // Non-secure Access Control Registers
	gicdIrouter    = 0x6000 // Interrupt Routing Registers
	gicdPidr2      = 0xFFE8 // Peripheral ID 2

	// Architecture version in PIDR2
	gicArchRevGICv1 = 0x10
	gicArchRevGICv2 = 0x20
	gicArchRevGICv3 = 0x30
	gicArchRevGICv4 = 0x40
)

// gicEmulator handles GIC MMIO emulation for HVF
type gicEmulator struct {
	vm *virtualMachine

	// Distributor state
	distCtlr uint32

	// Per-redistributor state (indexed by CPU)
	redistWaker []uint32

	// Software GIC state tracking (for interrupt delivery)
	mu sync.Mutex

	// SPI state (SPIs start at INTID 32)
	// Each bit represents one SPI (bit 0 = SPI 0 = INTID 32)
	spiPending []uint32 // Pending SPIs
	spiActive  []uint32 // Active SPIs
	spiEnabled []uint32 // Enabled SPIs

	// Per-CPU state for interrupt acknowledgment
	// Maps CPU index to the currently active interrupt INTID (1023 = none)
	cpuActiveIntid []uint32
}

func newGICEmulator(vm *virtualMachine) *gicEmulator {
	cpuCount := len(vm.vcpus)
	if cpuCount == 0 {
		cpuCount = 1
	}

	// Allocate bitmasks for SPIs (max 988 SPIs = 32 words)
	spiWordCount := 32 // Enough for 1024 SPIs
	return &gicEmulator{
		vm:            vm,
		redistWaker:   make([]uint32, cpuCount),
		spiPending:    make([]uint32, spiWordCount),
		spiActive:      make([]uint32, spiWordCount),
		spiEnabled:     make([]uint32, spiWordCount),
		cpuActiveIntid: make([]uint32, cpuCount),
	}
}

func (g *gicEmulator) Init(vm hv.VirtualMachine) error {
	return nil
}

func (g *gicEmulator) MMIORegions() []hv.MMIORegion {
	info := g.vm.arm64GICInfo
	if info.Version == hv.Arm64GICVersionUnknown {
		return nil
	}

	cpuCount := len(g.vm.vcpus)
	if cpuCount == 0 {
		cpuCount = 1
	}

	return []hv.MMIORegion{
		{Address: info.DistributorBase, Size: info.DistributorSize},
		{Address: info.RedistributorBase, Size: info.RedistributorSize * uint64(cpuCount)},
	}
}

func (g *gicEmulator) ReadMMIO(addr uint64, data []byte) error {
	info := g.vm.arm64GICInfo

	// Determine if this is distributor or redistributor access
	if addr >= info.DistributorBase && addr < info.DistributorBase+info.DistributorSize {
		return g.readDistributor(addr-info.DistributorBase, data)
	}

	cpuCount := len(g.vm.vcpus)
	if cpuCount == 0 {
		cpuCount = 1
	}
	redistEnd := info.RedistributorBase + info.RedistributorSize*uint64(cpuCount)
	if addr >= info.RedistributorBase && addr < redistEnd {
		// Determine which CPU's redistributor
		offset := addr - info.RedistributorBase
		cpuIdx := int(offset / info.RedistributorSize)
		regOffset := offset % info.RedistributorSize
		return g.readRedistributor(cpuIdx, regOffset, data)
	}

	// Unknown GIC region
	for i := range data {
		data[i] = 0
	}
	return nil
}

func (g *gicEmulator) WriteMMIO(addr uint64, data []byte) error {
	info := g.vm.arm64GICInfo

	// Determine if this is distributor or redistributor access
	if addr >= info.DistributorBase && addr < info.DistributorBase+info.DistributorSize {
		return g.writeDistributor(addr-info.DistributorBase, data)
	}

	cpuCount := len(g.vm.vcpus)
	if cpuCount == 0 {
		cpuCount = 1
	}
	redistEnd := info.RedistributorBase + info.RedistributorSize*uint64(cpuCount)
	if addr >= info.RedistributorBase && addr < redistEnd {
		// Determine which CPU's redistributor
		offset := addr - info.RedistributorBase
		cpuIdx := int(offset / info.RedistributorSize)
		regOffset := offset % info.RedistributorSize
		return g.writeRedistributor(cpuIdx, regOffset, data)
	}

	// Unknown GIC region - ignore writes
	return nil
}

func (g *gicEmulator) readDistributor(offset uint64, data []byte) error {
	var value uint32

	switch offset {
	case gicdCtlr:
		value = g.distCtlr
	case gicdTyper:
		// ITLinesNumber = (SPIs / 32) - 1
		// For 988 SPIs: (988/32) - 1 = 30 (but clamp to max)
		itLines := uint32(30) // Max value
		cpuNum := uint32(0)   // Single CPU
		// SecurityExtn=1, MBIS=0, LPIS=0
		value = itLines | (cpuNum << 5) | (1 << 10)
	case gicdIidr:
		// ARM implementation
		value = 0x0200043B
	case gicdTyper2:
		value = 0
	case gicdPidr2:
		value = gicArchRevGICv3
	default:
		// Default to 0 for unhandled registers
		value = 0
	}

	writeU32LE(data, value)
	return nil
}

func (g *gicEmulator) writeDistributor(offset uint64, data []byte) error {
	value := readU32LE(data)

	switch {
	case offset == gicdCtlr:
		g.distCtlr = value
	case offset >= gicdIsenabler && offset < gicdIsenabler+0x80:
		// Interrupt Set-Enable Registers: enable SPIs
		regIdx := (offset - gicdIsenabler) / 4
		if regIdx < uint64(len(g.spiEnabled)) {
			g.mu.Lock()
			g.spiEnabled[regIdx] |= value
			g.mu.Unlock()
		}
	case offset >= gicdIcenabler && offset < gicdIcenabler+0x80:
		// Interrupt Clear-Enable Registers: disable SPIs
		regIdx := (offset - gicdIcenabler) / 4
		if regIdx < uint64(len(g.spiEnabled)) {
			g.mu.Lock()
			g.spiEnabled[regIdx] &^= value
			g.mu.Unlock()
		}
	default:
		// Ignore writes to unhandled registers
	}

	return nil
}

func (g *gicEmulator) readRedistributor(cpuIdx int, offset uint64, data []byte) error {
	var value uint32

	cpuCount := len(g.vm.vcpus)
	if cpuCount == 0 {
		cpuCount = 1
	}

	switch offset {
	case gicrCtlr:
		value = 0
	case gicrIidr:
		// ARM implementation
		value = 0x0200043B
	case gicrTyper:
		// Affinity bits in upper 32 bits, processor number in bits [23:8]
		// Last bit indicates this is the last redistributor
		procNum := uint32(cpuIdx) << 8
		last := uint32(0)
		if cpuIdx == cpuCount-1 {
			last = 1 << 4 // GICR_TYPER.Last
		}
		value = procNum | last
	case gicrTyper + 4:
		// Upper 32 bits of TYPER - affinity
		value = uint32(cpuIdx) << 8 // Aff1 = cpuIdx
	case gicrWaker:
		if cpuIdx < len(g.redistWaker) {
			value = g.redistWaker[cpuIdx]
		}
	case gicrPidr2RDBase:
		value = gicArchRevGICv3
	case gicrPidr2SGIBase:
		value = gicArchRevGICv3
	default:
		// Default to 0 for unhandled registers
		value = 0
	}

	writeU32LE(data, value)
	return nil
}

func (g *gicEmulator) writeRedistributor(cpuIdx int, offset uint64, data []byte) error {
	value := readU32LE(data)

	switch offset {
	case gicrWaker:
		if cpuIdx < len(g.redistWaker) {
			// ChildrenAsleep is read-only, ProcessorSleep is writable
			// When ProcessorSleep is cleared, ChildrenAsleep should also clear
			if value&0x2 == 0 { // ProcessorSleep cleared
				g.redistWaker[cpuIdx] = 0 // Clear both bits
			} else {
				g.redistWaker[cpuIdx] = value & 0x6 // Preserve writable bits
			}
		}
	default:
		// Ignore writes to unhandled registers
	}

	return nil
}

func readU32LE(data []byte) uint32 {
	if len(data) < 4 {
		var tmp [4]byte
		copy(tmp[:], data)
		return binary.LittleEndian.Uint32(tmp[:])
	}
	return binary.LittleEndian.Uint32(data)
}

func writeU32LE(data []byte, value uint32) {
	if len(data) >= 4 {
		binary.LittleEndian.PutUint32(data, value)
	} else {
		var tmp [4]byte
		binary.LittleEndian.PutUint32(tmp[:], value)
		copy(data, tmp[:len(data)])
	}
}

// setSPIPending sets or clears the pending state for an SPI
// intid is the full GIC INTID (e.g., 72 for SPI 40)
func (g *gicEmulator) setSPIPending(intid uint32, pending bool) {
	if intid < 32 {
		return // Not an SPI
	}
	spiNum := intid - 32
	if spiNum >= uint32(len(g.spiPending)*32) {
		return // Out of range
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	wordIdx := spiNum / 32
	bitIdx := spiNum % 32
	mask := uint32(1) << bitIdx

	if pending {
		g.spiPending[wordIdx] |= mask
		// Enable the SPI by default if not already enabled
		// This matches typical guest behavior where interrupts are enabled during setup
		g.spiEnabled[wordIdx] |= mask
	} else {
		g.spiPending[wordIdx] &^= mask
	}
}

// irqPending returns true if any enabled SPI is pending for the given CPU
func (g *gicEmulator) irqPending(cpuIdx int) bool {
	if cpuIdx < 0 || cpuIdx >= len(g.cpuActiveIntid) {
		return false
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	// Check if any enabled SPI is pending and not active
	for i := range g.spiPending {
		if (g.spiPending[i] &^ g.spiActive[i] & g.spiEnabled[i]) != 0 {
			return true
		}
	}

	return false
}

// acknowledgeInterrupt handles ICC_IAR1_EL1 read (interrupt acknowledge)
// Returns the INTID of the highest priority pending interrupt, or 1023 if none
func (g *gicEmulator) acknowledgeInterrupt(cpuIdx int) uint32 {
	if cpuIdx < 0 || cpuIdx >= len(g.cpuActiveIntid) {
		return 1023
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	// Find the highest priority pending+enabled SPI that's not active
	for wordIdx := range g.spiPending {
		pending := g.spiPending[wordIdx] &^ g.spiActive[wordIdx] & g.spiEnabled[wordIdx]
		if pending == 0 {
			continue
		}

		// Find the lowest bit (highest priority SPI in this word)
		bitIdx := uint32(0)
		for (pending & (1 << bitIdx)) == 0 {
			bitIdx++
		}

		// Calculate INTID (SPI number + 32)
		intid := uint32(wordIdx)*32 + bitIdx + 32

		// Mark as active
		g.spiActive[wordIdx] |= (1 << bitIdx)
		g.spiPending[wordIdx] &^= (1 << bitIdx) // Clear pending

		// Track active interrupt for this CPU
		g.cpuActiveIntid[cpuIdx] = intid

		return intid
	}

	return 1023 // No interrupt pending
}

// endOfInterrupt handles ICC_EOIR1_EL1 write (end of interrupt)
func (g *gicEmulator) endOfInterrupt(cpuIdx int, intid uint32) {
	if cpuIdx < 0 || cpuIdx >= len(g.cpuActiveIntid) {
		return
	}

	if intid < 32 {
		return // Not an SPI
	}
	spiNum := intid - 32
	if spiNum >= uint32(len(g.spiActive)*32) {
		return // Out of range
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	wordIdx := spiNum / 32
	bitIdx := spiNum % 32
	mask := uint32(1) << bitIdx

	// Clear active bit
	g.spiActive[wordIdx] &^= mask

	// Clear CPU's active interrupt if this was it
	if g.cpuActiveIntid[cpuIdx] == intid {
		g.cpuActiveIntid[cpuIdx] = 1023
	}
}

// enableSPI enables or disables an SPI
func (g *gicEmulator) enableSPI(intid uint32, enabled bool) {
	if intid < 32 {
		return // Not an SPI
	}
	spiNum := intid - 32
	if spiNum >= uint32(len(g.spiEnabled)*32) {
		return // Out of range
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	wordIdx := spiNum / 32
	bitIdx := spiNum % 32
	mask := uint32(1) << bitIdx

	if enabled {
		g.spiEnabled[wordIdx] |= mask
	} else {
		g.spiEnabled[wordIdx] &^= mask
	}
}

var (
	_ hv.MemoryMappedIODevice = (*gicEmulator)(nil)
)

// addGICEmulator adds a GIC emulator device to the VM if GIC is configured
func (vm *virtualMachine) addGICEmulator() error {
	if vm.arm64GICInfo.Version == hv.Arm64GICVersionUnknown {
		return nil
	}

	emulator := newGICEmulator(vm)
	vm.gicEmulator = emulator
	if err := vm.AddDevice(emulator); err != nil {
		return fmt.Errorf("add GIC emulator: %w", err)
	}

	return nil
}
