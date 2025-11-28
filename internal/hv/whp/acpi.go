package whp

import (
	"bytes"
	"encoding/binary"
)

// Common ACPI SDT header (36 bytes)
func makeSDT(sig string, length uint32, revision uint8, oemTableID string) []byte {
	buf := &bytes.Buffer{}

	// Signature
	buf.WriteString(sig)
	// Length (we'll overwrite this later if needed)
	binary.Write(buf, binary.LittleEndian, length)
	// Revision
	buf.WriteByte(revision)
	// Checksum (placeholder 0 for now)
	buf.WriteByte(0)

	// OEMID (6 bytes)
	oemID := []byte("TINYR ")
	if len(oemID) != 6 {
		panic("OEMID must be 6 bytes")
	}
	buf.Write(oemID)

	// OEM Table ID (8 bytes)
	ot := []byte(oemTableID)
	if len(ot) > 8 {
		ot = ot[:8]
	}
	for len(ot) < 8 {
		ot = append(ot, ' ')
	}
	buf.Write(ot)

	// OEM Revision
	binary.Write(buf, binary.LittleEndian, uint32(1))
	// Creator ID
	binary.Write(buf, binary.LittleEndian, uint32(0x5452594E)) // "TRYN"
	// Creator Revision
	binary.Write(buf, binary.LittleEndian, uint32(1))

	// Ensure length at least header
	if length < uint32(buf.Len()) {
		length = uint32(buf.Len())
	}
	// fix length
	b := buf.Bytes()
	binary.LittleEndian.PutUint32(b[4:], length)

	b = append(b, make([]byte, length-uint32(len(b)))...) // pad to length

	return b
}

func acpiChecksum(b []byte) {
	var sum uint8
	for _, v := range b {
		sum += v
	}
	b[9] = uint8(0 - sum) // SDT checksum at offset 9
}

// ----------------- RSDP (v2) -----------------

func BuildRSDP(xsdtAddr uint64) []byte {
	// ACPI 2.0+ RSDP is 36 bytes
	b := make([]byte, 36)

	copy(b[0:], []byte("RSD PTR ")) // signature

	// checksum over first 20 bytes
	// we'll fill at [8]
	// OEMID (6)
	copy(b[9:], []byte("TINYR "))

	b[15] = 2 // revision (ACPI 2.0+)

	// RSDT address (ACPI 1.0) – we can set 0 if we only use XSDT
	binary.LittleEndian.PutUint32(b[16:], 0)

	// Length
	binary.LittleEndian.PutUint32(b[20:], uint32(len(b)))

	// XSDT address
	binary.LittleEndian.PutUint64(b[24:], xsdtAddr)

	// extended checksum (byte 32) over full table
	// first compute 1.0 checksum over first 20 bytes
	var sum20 uint8
	for i := 0; i < 20; i++ {
		sum20 += b[i]
	}
	b[8] = uint8(0 - sum20)

	var sumAll uint8
	for _, v := range b {
		sumAll += v
	}
	b[32] = uint8(0 - sumAll)

	return b
}

// ----------------- XSDT -----------------

func BuildXSDT(entries []uint64) []byte {
	// header (36 bytes) + 8 bytes per entry
	length := uint32(36 + 8*len(entries))
	b := makeSDT("XSDT", length, 1, "TINYRXSD")

	// append entries
	buf := bytes.NewBuffer(b)
	for _, e := range entries {
		binary.Write(buf, binary.LittleEndian, e)
	}

	out := buf.Bytes()
	// fix length
	binary.LittleEndian.PutUint32(out[4:], uint32(len(out)))
	acpiChecksum(out)
	return out
}

// ----------------- MADT (APIC) -----------------

func BuildMADT(lapicBase uint32, ioapicID uint8, ioapicAddr uint32, gsiBase uint32) []byte {
	// We'll build into a buffer then fix length/checksum
	h := makeSDT("APIC", 44, 1, "TINYRAPC")
	buf := bytes.NewBuffer(h)

	// MADT fields
	binary.Write(buf, binary.LittleEndian, lapicBase) // Local APIC Address
	// Flags: PC-AT compat (bit0)
	binary.Write(buf, binary.LittleEndian, uint32(1))

	// --- Processor Local APIC (type 0) for CPU0 only ---
	buf.WriteByte(0)                                  // Type
	buf.WriteByte(8)                                  // Length
	buf.WriteByte(0)                                  // ACPI Processor ID
	buf.WriteByte(0)                                  // APIC ID
	binary.Write(buf, binary.LittleEndian, uint32(1)) // Flags: enabled

	// --- IOAPIC (type 1) ---
	buf.WriteByte(1)  // Type
	buf.WriteByte(12) // Length
	buf.WriteByte(ioapicID)
	buf.WriteByte(0) // reserved
	binary.Write(buf, binary.LittleEndian, ioapicAddr)
	binary.Write(buf, binary.LittleEndian, gsiBase)

	out := buf.Bytes()
	binary.LittleEndian.PutUint32(out[4:], uint32(len(out)))
	acpiChecksum(out)
	return out
}

// ----------------- HPET table -----------------

func BuildHPET(acpiHPETAddr uint64, hpetGSI uint32) []byte {
	// HPET table is 56 bytes for minimal implementation
	b := makeSDT("HPET", 56, 1, "TINYRHPT")

	if len(b) != 56 {
		panic("HPET table must be 56 bytes")
	}

	// offset 36: Hardware ID (UINT32)
	//   Bits 0-15: vendor id, 16-31: device id. We'll use Intel/1.
	binary.LittleEndian.PutUint32(b[36:], 0x80860001)

	// offset 40: base address (ACPI Generic Address Structure, 12 bytes)
	// GAS: [SpaceID(1), BitWidth(1), BitOffset(1), AccessSize(1), Address(8)]
	// We'll say system memory, 64-bit, address HPET_BASE
	gas := make([]byte, 12)
	gas[0] = 0  // System Memory
	gas[1] = 64 // bit width
	gas[2] = 0  // bit offset
	gas[3] = 0  // access size (undefined or 0)
	binary.LittleEndian.PutUint64(gas[4:], acpiHPETAddr)
	copy(b[40:], gas)

	// offset 52: HPET Number (uint8)
	b[52] = 0

	// offset 53: Minimum Clock Tick in periodic mode (uint16 at 53..54)
	binary.LittleEndian.PutUint16(b[53:], 0x0080) // arbitrary small-ish value

	// offset 55: Page Protection (uint8)
	b[55] = 0

	acpiChecksum(b)
	return b
}
