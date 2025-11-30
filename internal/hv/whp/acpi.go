package whp

import (
	"bytes"
	"encoding/binary"
)

// makeSDT creates the Standard Description Table Header (36 bytes).
func makeSDT(sig string, revision uint8, oemTableID string) *bytes.Buffer {
	buf := &bytes.Buffer{}

	// Signature (4 bytes)
	buf.WriteString(sig)

	// Length (4 bytes) - placeholder
	binary.Write(buf, binary.LittleEndian, uint32(0))

	// Revision (1 byte)
	buf.WriteByte(revision)

	// Checksum (1 byte) - placeholder
	buf.WriteByte(0)

	// OEMID (6 bytes)
	oemID := []byte("TINYR ")
	if len(oemID) != 6 {
		panic("OEMID must be 6 bytes")
	}
	buf.Write(oemID)

	// OEM Table ID (8 bytes)
	ot := []byte(oemTableID)
	outOT := make([]byte, 8)
	copy(outOT, ot)
	for i := len(ot); i < 8; i++ {
		outOT[i] = ' '
	}
	buf.Write(outOT)

	// OEM Revision (4 bytes)
	binary.Write(buf, binary.LittleEndian, uint32(1))
	// Creator ID (4 bytes)
	binary.Write(buf, binary.LittleEndian, uint32(0x5452594E)) // "TRYN"
	// Creator Revision (4 bytes)
	binary.Write(buf, binary.LittleEndian, uint32(1))

	return buf
}

// finalizeSDT updates the Length field and calculates the Checksum.
func finalizeSDT(buf *bytes.Buffer) []byte {
	b := buf.Bytes()

	// 1. Update Length (at offset 4)
	length := uint32(len(b))
	binary.LittleEndian.PutUint32(b[4:], length)

	// 2. Calculate Checksum (at offset 9)
	var sum uint8
	for i, v := range b {
		if i == 9 {
			continue
		}
		sum += v
	}
	b[9] = uint8(0 - sum)

	return b
}

// ----------------- RSDP (v2) -----------------

func BuildRSDP(xsdtAddr uint64) []byte {
	b := make([]byte, 36)

	copy(b[0:], []byte("RSD PTR ")) // Signature
	copy(b[9:], []byte("TINYR "))   // OEMID

	b[15] = 2 // Revision (ACPI 2.0+)

	binary.LittleEndian.PutUint32(b[16:], 0)              // RSDT address
	binary.LittleEndian.PutUint32(b[20:], uint32(len(b))) // Length
	binary.LittleEndian.PutUint64(b[24:], xsdtAddr)       // XSDT address

	// 1. Checksum (byte 8) over first 20 bytes
	var sum20 uint8
	for i := 0; i < 20; i++ {
		if i == 8 {
			continue
		}
		sum20 += b[i]
	}
	b[8] = uint8(0 - sum20)

	// 2. Extended Checksum (byte 32) over full table
	var sumAll uint8
	for i, v := range b {
		if i == 32 {
			continue
		}
		sumAll += v
	}
	b[32] = uint8(0 - sumAll)

	return b
}

// ----------------- XSDT -----------------

func BuildXSDT(entries []uint64) []byte {
	buf := makeSDT("XSDT", 1, "TINYRXSD")

	for _, e := range entries {
		binary.Write(buf, binary.LittleEndian, e)
	}

	return finalizeSDT(buf)
}

// ----------------- FADT (Fixed ACPI Description Table) -----------------

// BuildFADT is required for Windows to boot. It defines power state and DSDT location.
func BuildFADT(dsdtAddr uint64) []byte {
	// ACPI 5.0 FADT is 268 bytes, but 5.0+ rev usually 244 or 268.
	// We use Revision 5.
	buf := makeSDT("FACP", 5, "TINYRFACP")

	// Payload starts at offset 36

	// FIRMWARE_CTRL (4 bytes) - 36
	binary.Write(buf, binary.LittleEndian, uint32(0))
	// DSDT (4 bytes) - 40 (Legacy 32-bit pointer)
	binary.Write(buf, binary.LittleEndian, uint32(dsdtAddr))

	// Reserved (1 byte) - 44
	buf.WriteByte(0)

	// Preferred_PM_Profile (1 byte) - 45 (0 = Unspecified, 1 = Desktop)
	buf.WriteByte(1)

	// SCI_INT (2 bytes) - 46 (System Control Interrupt vector, usually 9)
	binary.Write(buf, binary.LittleEndian, uint16(9))

	// SMI_CMD (4 bytes) - 48 (System Management Mode command port)
	binary.Write(buf, binary.LittleEndian, uint32(0))

	// ACPI_ENABLE (1 byte) - 52
	buf.WriteByte(0)
	// ACPI_DISABLE (1 byte) - 53
	buf.WriteByte(0)
	// S4BIOS_REQ (1 byte) - 54
	buf.WriteByte(0)
	// PSTATE_CNT (1 byte) - 55
	buf.WriteByte(0)

	// PM1a_EVT_BLK (4 bytes) - 56
	binary.Write(buf, binary.LittleEndian, uint32(0))
	// PM1b_EVT_BLK (4 bytes) - 60
	binary.Write(buf, binary.LittleEndian, uint32(0))
	// PM1a_CNT_BLK (4 bytes) - 64
	binary.Write(buf, binary.LittleEndian, uint32(0))
	// PM1b_CNT_BLK (4 bytes) - 68
	binary.Write(buf, binary.LittleEndian, uint32(0))
	// PM2_CNT_BLK (4 bytes) - 72
	binary.Write(buf, binary.LittleEndian, uint32(0))
	// PM_TMR_BLK (4 bytes) - 76
	binary.Write(buf, binary.LittleEndian, uint32(0))

	// GPE0_BLK (4 bytes) - 80
	binary.Write(buf, binary.LittleEndian, uint32(0))
	// GPE1_BLK (4 bytes) - 84
	binary.Write(buf, binary.LittleEndian, uint32(0))

	// PM1_EVT_LEN (1 byte) - 88
	buf.WriteByte(0)
	// PM1_CNT_LEN (1 byte) - 89
	buf.WriteByte(0)
	// PM2_CNT_LEN (1 byte) - 90
	buf.WriteByte(0)
	// PM_TMR_LEN (1 byte) - 91
	buf.WriteByte(0)
	// GPE0_BLK_LEN (1 byte) - 92
	buf.WriteByte(0)
	// GPE1_BLK_LEN (1 byte) - 93
	buf.WriteByte(0)
	// GPE1_BASE (1 byte) - 94
	buf.WriteByte(0)

	// CST_CNT (1 byte) - 95
	buf.WriteByte(0)
	// P_LVL2_LAT (2 bytes) - 96
	binary.Write(buf, binary.LittleEndian, uint16(0))
	// P_LVL3_LAT (2 bytes) - 98
	binary.Write(buf, binary.LittleEndian, uint16(0))
	// FLUSH_SIZE (2 bytes) - 100
	binary.Write(buf, binary.LittleEndian, uint16(0))
	// FLUSH_STRIDE (2 bytes) - 102
	binary.Write(buf, binary.LittleEndian, uint16(0))
	// DUTY_OFFSET (1 byte) - 104
	buf.WriteByte(0)
	// DUTY_WIDTH (1 byte) - 105
	buf.WriteByte(0)
	// DAY_ALRM (1 byte) - 106
	buf.WriteByte(0)
	// MON_ALRM (1 byte) - 107
	buf.WriteByte(0)
	// CENTURY (1 byte) - 108
	buf.WriteByte(0)

	// IAPC_BOOT_ARCH (2 bytes) - 109
	// Bit 0: LEGACY_DEVICES (has 8042 kbd controller)
	// Bit 1: 8042 (1)
	// Bit 4: VGA Not Present (0 - we have VGA usually)
	binary.Write(buf, binary.LittleEndian, uint16(3)) // Legacy + 8042

	// Reserved (1 byte) - 111
	buf.WriteByte(0)

	// Flags (4 bytes) - 112
	// Bit 20: HW_REDUCED_ACPI (1) - Since we have no PM timer/buttons
	binary.Write(buf, binary.LittleEndian, uint32(1<<20))

	// RESET_REG (12 bytes, GAS) - 116
	// Reset via IO port 0xCF9 (standard PCI reset) or 0x64 (KBC)
	buf.Write([]byte{1, 8, 0, 0}) // IO space
	binary.Write(buf, binary.LittleEndian, uint64(0xCF9))

	// RESET_VALUE (1 byte) - 128
	buf.WriteByte(6) // Warm reset

	// ARM_BOOT_ARCH (2 bytes) - 129
	binary.Write(buf, binary.LittleEndian, uint16(0))

	// FADT Minor Version (1 byte) - 131
	buf.WriteByte(1)

	// X_FIRMWARE_CTRL (8 bytes) - 132
	binary.Write(buf, binary.LittleEndian, uint64(0))

	// X_DSDT (8 bytes) - 140 (64-bit pointer)
	binary.Write(buf, binary.LittleEndian, uint64(dsdtAddr))

	// ... P_BLK fields would go here, simplified ...

	// We need to pad to at least 244 bytes for ACPI 5.0
	currentLen := buf.Len()
	if currentLen < 244 {
		padding := make([]byte, 244-currentLen)
		buf.Write(padding)
	}

	return finalizeSDT(buf)
}

// ----------------- MADT (APIC) -----------------

func BuildMADT(lapicBase uint32, ioapicID uint8, ioapicAddr uint32, gsiBase uint32) []byte {
	// Header
	buf := makeSDT("APIC", 1, "TINYRAPC")

	// --- MADT Body ---

	// Local APIC Address (4 bytes) - Offset 36
	binary.Write(buf, binary.LittleEndian, lapicBase)

	// Flags (4 bytes) - Offset 40. PC-AT compat (bit0)
	// We set this to 1 to indicate we support 8259A legacy PICs.
	binary.Write(buf, binary.LittleEndian, uint32(1))

	// --- Structures start at Offset 44 ---

	// 1. Processor Local APIC (Type 0) - generated outside typically, but we assume CPU0 here for example
	// In reality you loop this for all CPUs.
	buf.WriteByte(0)                                  // Type
	buf.WriteByte(8)                                  // Length
	buf.WriteByte(0)                                  // ACPI Processor ID
	buf.WriteByte(0)                                  // APIC ID
	binary.Write(buf, binary.LittleEndian, uint32(1)) // Flags: enabled

	// 2. I/O APIC (Type 1) [NEW]
	buf.WriteByte(1)                                   // Type
	buf.WriteByte(12)                                  // Length
	buf.WriteByte(ioapicID)                            // IOAPIC ID
	buf.WriteByte(0)                                   // Reserved
	binary.Write(buf, binary.LittleEndian, ioapicAddr) // Address (0xFEC00000)
	binary.Write(buf, binary.LittleEndian, gsiBase)    // GSI Base (0)

	// 3. Interrupt Source Override (Type 2) [NEW]
	// Critical: Remap ISA IRQ 0 (PIT) to GSI 2
	buf.WriteByte(2)                                  // Type
	buf.WriteByte(10)                                 // Length
	buf.WriteByte(0)                                  // Bus (0=ISA)
	buf.WriteByte(0)                                  // Source (IRQ 0)
	binary.Write(buf, binary.LittleEndian, uint32(2)) // Global System Interrupt
	binary.Write(buf, binary.LittleEndian, uint16(0)) // Flags (0=Conform to bus specs)

	return finalizeSDT(buf)
}

// ----------------- HPET table -----------------

func BuildHPET(acpiHPETAddr uint64, hpetGSI uint32) []byte {
	// HPET header
	buf := makeSDT("HPET", 1, "TINYRHPT")

	// Event Timer Block ID (offset 36) [UPDATED]
	// 31:16 = Vendor (0x8086)
	// 15 = Legacy Replacement Route Capable (1)
	// 13 = Count Size Cap (1 = 64-bit)
	// 12:8 = Num Comparators - 1 (2 => 3 timers)
	// 7:0 = Hardare Rev (1)
	// Value: 0x8086A201
	binary.Write(buf, binary.LittleEndian, uint32(0x8086A201))

	// Base Address (ACPI Generic Address Structure - 12 bytes) (offset 40)
	// [SpaceID(1), BitWidth(1), BitOffset(1), AccessSize(1), Address(8)]
	buf.WriteByte(0)  // 0=System Memory
	buf.WriteByte(64) // Bit Width
	buf.WriteByte(0)  // Bit Offset
	buf.WriteByte(0)  // Access Size
	binary.Write(buf, binary.LittleEndian, acpiHPETAddr)

	// HPET Number (offset 52)
	buf.WriteByte(0)

	// Main Counter Minimum Clock Tick (offset 53)
	binary.Write(buf, binary.LittleEndian, uint16(0x0080))

	// Page Protection (offset 55)
	buf.WriteByte(0) // No page protection

	return finalizeSDT(buf)
}
