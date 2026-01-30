package acpi

import "github.com/tinyrange/cc/internal/hv"

// Config controls how ACPI tables are laid out and populated inside guest
// memory. All addresses are physical guest addresses.
type Config struct {
	MemoryBase uint64
	MemorySize uint64
	TablesBase uint64
	TablesSize uint64
	RSDPBase   uint64

	NumCPUs   int
	LAPICBase uint32

	IOAPIC IOAPICConfig

	HPET *HPETConfig

	// VirtioDevices describes virtio-mmio devices to add to the DSDT.
	VirtioDevices []VirtioMMIODevice

	// ISAOverrides emits MADT interrupt source overrides for legacy ISA IRQs.
	ISAOverrides []InterruptOverride

	OEM OEMInfo
}

// IOAPICConfig describes the IO-APIC entry that will be emitted into MADT.
type IOAPICConfig struct {
	ID      uint8
	Address uint32
	GSIBase uint32
}

// VirtioMMIODevice describes a virtio-mmio device for DSDT generation.
type VirtioMMIODevice struct {
	Name     string // 4-char ACPI name (e.g., "VIO0")
	BaseAddr uint64
	Size     uint64
	GSI      uint32 // Global System Interrupt number
}

// HPETConfig describes the optional HPET ACPI table.
type HPETConfig struct {
	Address uint64
}

// InterruptOverride describes a single MADT INT_SRC_OVR entry.
type InterruptOverride struct {
	Bus   uint8  // typically 0 (ISA)
	IRQ   uint8  // source IRQ
	GSI   uint32 // destination GSI
	Flags uint16 // polarity/trigger encoding per ACPI spec
}

// OEMInfo mirrors the ACPI table header OEM fields.
type OEMInfo struct {
	OEMID           [6]byte
	OEMTableID      [8]byte
	OEMRevision     uint32
	CreatorID       [4]byte
	CreatorRevision uint32
}

// DefaultOEMInfo returns the default table header metadata used by the VMM.
func DefaultOEMInfo() OEMInfo {
	return OEMInfo{
		OEMID:           [6]byte{'T', 'I', 'N', 'Y', 'R', ' '},
		OEMTableID:      [8]byte{'T', 'I', 'N', 'Y', 'R', 'D', 'E', 'F'},
		OEMRevision:     1,
		CreatorID:       [4]byte{'T', 'R', 'Y', 'N'},
		CreatorRevision: 1,
	}
}

// x86_64 memory layout constant
const x86PCIHoleStart uint64 = 0xC0000000 // 3GB - start of PCI/MMIO hole

func (c *Config) normalize(vm hv.VirtualMachine) {
	if c.MemoryBase == 0 {
		c.MemoryBase = vm.MemoryBase()
	}
	if c.MemorySize == 0 {
		c.MemorySize = vm.MemorySize()
	}
	if c.TablesSize == 0 {
		c.TablesSize = 0x10000
	}
	if c.TablesBase == 0 {
		memEnd := c.MemoryBase + c.MemorySize
		c.TablesBase = memEnd - c.TablesSize
		// On x86_64, if memory extends into the PCI hole (above 3GB),
		// place tables just below the PCI hole to avoid MMIO region (3GB-4GB).
		if memEnd > x86PCIHoleStart {
			c.TablesBase = x86PCIHoleStart - c.TablesSize
		}
	}
	if c.RSDPBase == 0 {
		c.RSDPBase = c.MemoryBase + 0x000E0000
	}
	if c.NumCPUs <= 0 {
		c.NumCPUs = 1
	}
	if c.LAPICBase == 0 {
		c.LAPICBase = 0xFEE00000
	}
	if c.IOAPIC.Address == 0 {
		c.IOAPIC.Address = 0xFEC00000
	}
	if c.OEM == (OEMInfo{}) {
		c.OEM = DefaultOEMInfo()
	}
}
