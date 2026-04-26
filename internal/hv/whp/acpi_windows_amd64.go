//go:build windows && amd64

package whp

import (
	"encoding/binary"
	"fmt"
)

const (
	acpiRSDPAddress  = 0x000e0000
	acpiTableAddress = 0x000f0000
)

func installBootACPI(memory []byte) error {
	if len(memory) < acpiTableAddress+0x10000 {
		return fmt.Errorf("guest memory too small for ACPI tables")
	}
	writer := acpiTableWriter{base: acpiTableAddress}
	madt := writer.append("APIC", 1, "CCWHPAPC", buildBootMADT())
	hpet := writer.append("HPET", 1, "CCWHPHPT", buildBootHPET())
	xsdt := writer.append("XSDT", 1, "CCWHPXSD", buildBootXSDT([]uint64{madt, hpet}))
	copy(memory[acpiTableAddress:], writer.data)
	copy(memory[acpiRSDPAddress:], buildBootRSDP(xsdt))
	return nil
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

func buildACPITable(signature string, revision byte, tableID string, body []byte) []byte {
	table := make([]byte, 36+len(body))
	copy(table[0:4], acpiFixedString(signature, 4))
	binary.LittleEndian.PutUint32(table[4:], uint32(len(table)))
	table[8] = revision
	copy(table[10:16], acpiFixedString("CCWHP ", 6))
	copy(table[16:24], acpiFixedString(tableID, 8))
	binary.LittleEndian.PutUint32(table[24:], 1)
	copy(table[28:32], acpiFixedString("CC  ", 4))
	binary.LittleEndian.PutUint32(table[32:], 1)
	copy(table[36:], body)
	table[9] = acpiChecksum(table)
	return table
}

func buildBootXSDT(entries []uint64) []byte {
	body := make([]byte, 8*len(entries))
	for i, entry := range entries {
		binary.LittleEndian.PutUint64(body[i*8:], entry)
	}
	return body
}

func buildBootMADT() []byte {
	var body []byte
	body = binary.LittleEndian.AppendUint32(body, 0xfee00000)
	body = binary.LittleEndian.AppendUint32(body, 1)

	body = append(body, 0, 8, 0, 0)
	body = binary.LittleEndian.AppendUint32(body, 1)

	body = append(body, 1, 12, 0, 0)
	body = binary.LittleEndian.AppendUint32(body, ioapicBaseAddress)
	body = binary.LittleEndian.AppendUint32(body, 0)

	body = appendMADTInterruptOverride(body, 0, 2, 0)
	return body
}

func appendMADTInterruptOverride(body []byte, irq byte, gsi uint32, flags uint16) []byte {
	body = append(body, 2, 10, 0, irq)
	body = binary.LittleEndian.AppendUint32(body, gsi)
	body = binary.LittleEndian.AppendUint16(body, flags)
	return body
}

func buildBootHPET() []byte {
	var body []byte
	id := uint32(1)
	id |= uint32(hpetNumTimers-1) << 8
	id |= 1 << 13
	id |= uint32(hpetVendorID) << 16
	body = binary.LittleEndian.AppendUint32(body, id)
	body = append(body, 0, 64, 0, 0)
	body = binary.LittleEndian.AppendUint64(body, hpetBaseAddress)
	body = append(body, 0)
	body = binary.LittleEndian.AppendUint16(body, 0x0080)
	body = append(body, 0)
	return body
}

func buildBootRSDP(xsdt uint64) []byte {
	rsdp := make([]byte, 36)
	copy(rsdp[0:8], "RSD PTR ")
	copy(rsdp[9:15], "CCWHP ")
	rsdp[15] = 2
	binary.LittleEndian.PutUint32(rsdp[20:], uint32(len(rsdp)))
	binary.LittleEndian.PutUint64(rsdp[24:], xsdt)
	rsdp[8] = acpiChecksum(rsdp[:20])
	rsdp[32] = acpiChecksum(rsdp)
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

func acpiChecksum(data []byte) byte {
	var sum byte
	for _, b := range data {
		sum += b
	}
	return byte(0 - sum)
}
