package amd64

import (
	"encoding/binary"
	"fmt"
)

const (
	acpiRSDPAddress  = 0x000e0000
	acpiTableAddress = 0x000e1000

	acpiLocalAPICAddress = 0xfee00000
	acpiIOAPICAddress    = 0xfec00000
	acpiHPETAddress      = 0xfed00000
)

func installBootACPI(memory []byte, numCPUs int) (uint64, error) {
	if numCPUs < 1 {
		numCPUs = 1
	}
	writer := acpiTableWriter{base: acpiTableAddress}
	facs := writer.appendRaw(buildBootFACS())
	dsdt := writer.append("DSDT", 2, "CCKVMDSD", buildBootDSDT())
	fadt := writer.append("FACP", 6, "CCKVMFAD", buildBootFADT(facs, dsdt))
	madt := writer.append("APIC", 3, "CCKVMAPC", buildBootMADT(numCPUs))
	xsdt := writer.append("XSDT", 1, "CCKVMXSD", buildBootXSDT([]uint64{fadt, madt}))
	if err := writeAt(memory, acpiTableAddress, writer.data); err != nil {
		return 0, fmt.Errorf("write ACPI tables: %w", err)
	}
	if err := writeAt(memory, acpiRSDPAddress, buildBootRSDP(xsdt)); err != nil {
		return 0, fmt.Errorf("write ACPI RSDP: %w", err)
	}
	return acpiRSDPAddress, nil
}

type acpiTableWriter struct {
	base uint64
	data []byte
}

func (w *acpiTableWriter) append(signature string, revision byte, tableID string, body []byte) uint64 {
	addr := w.base + uint64(len(w.data))
	table := buildACPITable(signature, revision, tableID, body)
	w.data = append(w.data, table...)
	for len(w.data)%16 != 0 {
		w.data = append(w.data, 0)
	}
	return addr
}

func (w *acpiTableWriter) appendRaw(table []byte) uint64 {
	addr := w.base + uint64(len(w.data))
	w.data = append(w.data, table...)
	for len(w.data)%16 != 0 {
		w.data = append(w.data, 0)
	}
	return addr
}

func buildACPITable(signature string, revision byte, tableID string, body []byte) []byte {
	table := make([]byte, 36+len(body))
	copy(table[0:4], acpiFixedString(signature, 4))
	binary.LittleEndian.PutUint32(table[4:], uint32(len(table)))
	table[8] = revision
	copy(table[10:16], acpiFixedString("CCKVM ", 6))
	copy(table[16:24], acpiFixedString(tableID, 8))
	binary.LittleEndian.PutUint32(table[24:], 1)
	copy(table[28:32], acpiFixedString("CC  ", 4))
	binary.LittleEndian.PutUint32(table[32:], 1)
	copy(table[36:], body)
	table[9] = checksum(table)
	return table
}

func buildBootXSDT(entries []uint64) []byte {
	body := make([]byte, 8*len(entries))
	for i, entry := range entries {
		binary.LittleEndian.PutUint64(body[i*8:], entry)
	}
	return body
}

func buildBootDSDT() []byte {
	pciBody := appendNameDWord(nil, "_HID", 0x030ad041) // PNP0A03 PCI bus.
	pciBody = appendNameIntegerZero(pciBody, "_ADR")
	pciBody = appendNameIntegerZero(pciBody, "_BBN")
	pciBody = appendNameBuffer(pciBody, "_CRS", buildBootPCIResources())
	device := append([]byte{0x5b, 0x82}, amlPkg(4+len(pciBody))...)
	device = append(device, 'P', 'C', 'I', '0')
	device = append(device, pciBody...)

	scopeBody := device
	scope := append([]byte{0x10}, amlPkg(5+len(scopeBody))...)
	scope = append(scope, '\\', '_', 'S', 'B', '_')
	scope = append(scope, scopeBody...)
	return scope
}

func buildBootPCIResources() []byte {
	var out []byte
	out = appendWordAddressSpace(out, 2, 0, 0, 0, 0xff, 0, 0x100)  // Bus 0.
	out = appendIOPortDescriptor(out, 0x0cf8, 0x0cf8, 1, 8)        // PCI config address.
	out = appendWordIOAddressSpace(out, 0x0000, 0x0cf7, 0, 0x0cf8) // Low I/O below PCI config.
	out = appendWordIOAddressSpace(out, 0x0d00, 0xffff, 0, 0xf300) // I/O window above PCI config.
	out = appendDWordMemoryAddressSpace(out, 0x80000000, 0xfebfffff, 0, 0x7ec00000)
	out = append(out, 0x79, 0x00) // End tag.
	return out
}

func buildBootMADT(numCPUs int) []byte {
	var body []byte
	body = binary.LittleEndian.AppendUint32(body, acpiLocalAPICAddress)
	body = binary.LittleEndian.AppendUint32(body, 1)
	for cpu := 0; cpu < numCPUs && cpu < 255; cpu++ {
		body = append(body, 0, 8, byte(cpu), byte(cpu))
		body = binary.LittleEndian.AppendUint32(body, 1)
	}
	body = append(body, 1, 12, 0, 0)
	body = binary.LittleEndian.AppendUint32(body, acpiIOAPICAddress)
	body = binary.LittleEndian.AppendUint32(body, 0)
	body = appendMADTInterruptOverride(body, 0, 2, 0)
	body = appendMADTLocalAPICNMI(body)
	return body
}

func appendNameDWord(out []byte, name string, value uint32) []byte {
	out = append(out, 0x08)
	out = append(out, acpiFixedString(name, 4)...)
	out = append(out, 0x0c)
	return binary.LittleEndian.AppendUint32(out, value)
}

func appendNameIntegerZero(out []byte, name string) []byte {
	out = append(out, 0x08)
	out = append(out, acpiFixedString(name, 4)...)
	return append(out, 0x00)
}

func appendNameBuffer(out []byte, name string, data []byte) []byte {
	out = append(out, 0x08)
	out = append(out, acpiFixedString(name, 4)...)
	out = append(out, 0x11)
	body := append([]byte{0x0a, byte(len(data))}, data...)
	out = append(out, amlPkg(len(body))...)
	return append(out, body...)
}

func amlPkg(length int) []byte {
	encodedLen := 1
	for {
		total := length + encodedLen
		nextEncodedLen := 1
		switch {
		case total < 0x40:
			nextEncodedLen = 1
		case total < 0x1000:
			nextEncodedLen = 2
		case total < 0x100000:
			nextEncodedLen = 3
		default:
			nextEncodedLen = 4
		}
		if nextEncodedLen == encodedLen {
			length = total
			break
		}
		encodedLen = nextEncodedLen
	}
	if length < 0x40 {
		return []byte{byte(length)}
	}
	if length < 0x1000 {
		return []byte{byte(0x40 | (length & 0x0f)), byte(length >> 4)}
	}
	if length < 0x100000 {
		return []byte{byte(0x80 | (length & 0x0f)), byte(length >> 4), byte(length >> 12)}
	}
	return []byte{byte(0xc0 | (length & 0x0f)), byte(length >> 4), byte(length >> 12), byte(length >> 20)}
}

func appendIOPortDescriptor(out []byte, min, max uint16, align, length byte) []byte {
	out = append(out, 0x47, 0x01)
	out = binary.LittleEndian.AppendUint16(out, min)
	out = binary.LittleEndian.AppendUint16(out, max)
	out = append(out, align, length)
	return out
}

func appendWordAddressSpace(out []byte, resourceType, flags, typeFlags byte, min, max, translation, length uint16) []byte {
	out = append(out, 0x88, 0x0d, 0x00, resourceType, flags, typeFlags)
	out = binary.LittleEndian.AppendUint16(out, 0)
	out = binary.LittleEndian.AppendUint16(out, min)
	out = binary.LittleEndian.AppendUint16(out, max)
	out = binary.LittleEndian.AppendUint16(out, translation)
	out = binary.LittleEndian.AppendUint16(out, length)
	return out
}

func appendWordIOAddressSpace(out []byte, min, max, translation, length uint16) []byte {
	return appendWordAddressSpace(out, 1, 0x00, 0x03, min, max, translation, length)
}

func appendDWordMemoryAddressSpace(out []byte, min, max, translation, length uint32) []byte {
	out = append(out, 0x87, 0x17, 0x00, 0, 0x00, 0x06)
	out = binary.LittleEndian.AppendUint32(out, 0)
	out = binary.LittleEndian.AppendUint32(out, min)
	out = binary.LittleEndian.AppendUint32(out, max)
	out = binary.LittleEndian.AppendUint32(out, translation)
	out = binary.LittleEndian.AppendUint32(out, length)
	return out
}

func appendMADTInterruptOverride(body []byte, irq byte, gsi uint32, flags uint16) []byte {
	body = append(body, 2, 10, 0, irq)
	body = binary.LittleEndian.AppendUint32(body, gsi)
	body = binary.LittleEndian.AppendUint16(body, flags)
	return body
}

func appendMADTLocalAPICNMI(body []byte) []byte {
	body = append(body, 4, 6, 0xff)
	body = binary.LittleEndian.AppendUint16(body, 0)
	body = append(body, 1)
	return body
}

func buildBootHPET() []byte {
	const (
		hpetClockPeriodFemtoseconds = 10_000_000
		hpetVendorID                = 0x8086
		hpetNumTimers               = 3
	)
	var body []byte
	id := uint32(1)
	id |= uint32(hpetNumTimers-1) << 8
	id |= 1 << 13
	id |= uint32(hpetVendorID) << 16
	body = binary.LittleEndian.AppendUint32(body, id)
	body = append(body, 0, 64, 0, 0)
	body = binary.LittleEndian.AppendUint64(body, acpiHPETAddress)
	body = append(body, 0)
	body = binary.LittleEndian.AppendUint16(body, 0x0080)
	body = append(body, 0)
	_ = hpetClockPeriodFemtoseconds
	return body
}

func buildBootFACS() []byte {
	facs := make([]byte, 64)
	copy(facs[0:4], "FACS")
	binary.LittleEndian.PutUint32(facs[4:], uint32(len(facs)))
	binary.LittleEndian.PutUint32(facs[8:], 1)
	return facs
}

func buildBootFADT(facs, dsdt uint64) []byte {
	const (
		fadtBodyLength     = 276 - 36
		sciIRQ             = 9
		acpiPM1EventPort   = 0x400
		acpiPM1ControlPort = 0x404
		fadtFlagWBINVD     = 1 << 0
	)
	body := make([]byte, fadtBodyLength)
	binary.LittleEndian.PutUint32(body[0:], uint32(facs))
	binary.LittleEndian.PutUint32(body[4:], uint32(dsdt))
	body[9] = 0 // Unspecified preferred PM profile.
	binary.LittleEndian.PutUint16(body[10:], sciIRQ)
	binary.LittleEndian.PutUint32(body[20:], acpiPM1EventPort)
	binary.LittleEndian.PutUint32(body[28:], acpiPM1ControlPort)
	body[52] = 4
	body[53] = 2
	binary.LittleEndian.PutUint32(body[76:], fadtFlagWBINVD)
	binary.LittleEndian.PutUint64(body[96:], facs)
	binary.LittleEndian.PutUint64(body[104:], dsdt)
	return body
}

func buildBootRSDP(xsdt uint64) []byte {
	rsdp := make([]byte, 36)
	copy(rsdp[0:8], "RSD PTR ")
	copy(rsdp[9:15], "CCKVM ")
	rsdp[15] = 2
	binary.LittleEndian.PutUint32(rsdp[20:], uint32(len(rsdp)))
	binary.LittleEndian.PutUint64(rsdp[24:], xsdt)
	rsdp[8] = checksum(rsdp[:20])
	rsdp[32] = checksum(rsdp)
	return rsdp
}

func acpiFixedString(value string, size int) []byte {
	out := make([]byte, size)
	for i := range out {
		out[i] = ' '
	}
	copy(out, value)
	return out
}

func checksum(data []byte) byte {
	var sum byte
	for _, b := range data {
		sum += b
	}
	return -sum
}
