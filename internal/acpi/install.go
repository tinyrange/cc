package acpi

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/tinyrange/cc/internal/hv"
)

// Install writes ACPI tables into guest memory using the provided config.
func Install(vm hv.VirtualMachine, cfg Config) error {
	cfg.normalize(vm)

	if cfg.TablesBase < cfg.MemoryBase || cfg.TablesBase+cfg.TablesSize > cfg.MemoryBase+cfg.MemorySize {
		return fmt.Errorf("acpi: table region out of guest RAM")
	}
	if cfg.RSDPBase < cfg.MemoryBase || cfg.RSDPBase+36 > cfg.MemoryBase+cfg.MemorySize {
		return fmt.Errorf("acpi: RSDP location out of guest RAM")
	}

	writer := newTableWriter(cfg.TablesBase, cfg.OEM)

	dsdtAddr := writer.Append(tableParams{
		Signature:  sig("DSDT"),
		Revision:   2,
		OEMTableID: tableID("TINYRDSD"),
		Body:       buildMinimalDSDT(cfg),
	})

	madtBody := buildMADTBody(cfg)
	madtAddr := writer.Append(tableParams{
		Signature:  sig("APIC"),
		Revision:   1,
		OEMTableID: tableID("TINYRAPC"),
		Body:       madtBody,
	})

	var hpetAddr uint64
	if cfg.HPET != nil {
		hpetBody := buildHPETBody(cfg.HPET)
		hpetAddr = writer.Append(tableParams{
			Signature:  sig("HPET"),
			Revision:   1,
			OEMTableID: tableID("TINYRHPT"),
			Body:       hpetBody,
		})
	}

	fadtBody := buildFADTBody(dsdtAddr)
	fadtAddr := writer.Append(tableParams{
		Signature:  sig("FACP"),
		Revision:   5,
		OEMTableID: tableID("TINYRFAC"),
		Body:       fadtBody,
	})

	xsdtEntries := []uint64{fadtAddr, madtAddr}
	if hpetAddr != 0 {
		xsdtEntries = append(xsdtEntries, hpetAddr)
	}

	xsdtAddr := writer.Append(tableParams{
		Signature:  sig("XSDT"),
		Revision:   1,
		OEMTableID: tableID("TINYRXSD"),
		Body:       buildXSDTBody(xsdtEntries),
	})

	tables := writer.Bytes()
	if uint64(len(tables)) > cfg.TablesSize {
		return fmt.Errorf("acpi: tables require %d bytes, region only %d bytes", len(tables), cfg.TablesSize)
	}

	if _, err := vm.WriteAt(tables, int64(cfg.TablesBase)); err != nil {
		return fmt.Errorf("acpi: write tables: %w", err)
	}

	rsdp := buildRSDP(xsdtAddr, cfg.OEM)
	if _, err := vm.WriteAt(rsdp, int64(cfg.RSDPBase)); err != nil {
		return fmt.Errorf("acpi: write RSDP: %w", err)
	}

	return nil
}

func buildMinimalDSDT(cfg Config) []byte {
	// Build AML:
	// Scope (\_SB) {
	//   Device (UAR0) { _HID "PNP0501"; _CRS (IO 0x3f8 len 8, IRQ4) }
	//   Device (RTC0) { _HID "PNP0B00"; _CRS (IO 0x70 len 2,  IRQ8) }
	//   Device (VIO0) { _HID "LNRO0005"; _UID 0; _CRS (Mem32 0xd0000000 len 0x200, ExtInt GSI) }
	// }
	scopeBody := bytes.Buffer{}
	scopeBody.WriteString("\\_SB_") // NameString for scope

	for _, dev := range []struct {
		name string
		hid  string
		io   ioRange
		irq  uint8
	}{
		{name: "UAR0", hid: "PNP0501", io: ioRange{base: 0x3f8, length: 8}, irq: 4},
		{name: "RTC0", hid: "PNP0B00", io: ioRange{base: 0x70, length: 2}, irq: 8},
	} {
		devBody := bytes.Buffer{}
		devBody.WriteString(dev.name) // Device NameString

		// Name(_HID, "<hid>")
		devBody.WriteByte(0x08)                        // NameOp
		devBody.WriteString("_HID")                    // NameString
		devBody.WriteByte(0x0d)                        // StringPrefix
		devBody.WriteString(dev.hid)                   // HID value
		devBody.WriteByte(0x00)                        // Null terminator
		emitCRS(&devBody, dev.io, dev.irq)             // _CRS
		device := wrapPkg(0x5b, 0x82, devBody.Bytes()) // DeviceOp
		scopeBody.Write(device)
	}

	// Add virtio-mmio devices
	for i, vdev := range cfg.VirtioDevices {
		devBody := bytes.Buffer{}
		name := vdev.Name
		if name == "" {
			name = fmt.Sprintf("VIO%d", i)
		}
		devBody.WriteString(name) // Device NameString

		// Name(_HID, "LNRO0005") - standard virtio-mmio HID
		devBody.WriteByte(0x08)         // NameOp
		devBody.WriteString("_HID")     // NameString
		devBody.WriteByte(0x0d)         // StringPrefix
		devBody.WriteString("LNRO0005") // virtio-mmio HID
		devBody.WriteByte(0x00)         // Null terminator

		// Name(_UID, <index>)
		devBody.WriteByte(0x08)     // NameOp
		devBody.WriteString("_UID") // NameString
		devBody.WriteByte(0x0a)     // BytePrefix
		devBody.WriteByte(byte(i))  // UID value

		// _CRS with Memory32Fixed and ExtendedInterrupt
		emitVirtioCRS(&devBody, vdev.BaseAddr, vdev.Size, vdev.GSI)

		device := wrapPkg(0x5b, 0x82, devBody.Bytes()) // DeviceOp
		scopeBody.Write(device)
	}

	scope := wrapPkg(0x10, 0x00, scopeBody.Bytes()) // ScopeOp (second byte unused)
	return scope
}

type ioRange struct {
	base   uint16
	length uint8
}

func emitCRS(buf *bytes.Buffer, io ioRange, irq uint8) {
	buf.WriteByte(0x08)     // NameOp
	buf.WriteString("_CRS") // NameString

	template := bytes.Buffer{}
	// I/O Port Descriptor (Decode16)
	template.WriteByte(0x47) // Descriptor type/length (0x8 IO | len 7)
	template.WriteByte(0x01) // Information: 16-bit decode
	binary.Write(&template, binary.LittleEndian, io.base)
	binary.Write(&template, binary.LittleEndian, io.base)
	template.WriteByte(0x00)      // Alignment
	template.WriteByte(io.length) // Length
	// IRQNoFlags descriptor
	template.WriteByte(0x22) // IRQ descriptor, length 2 bytes
	irqMask := uint16(1) << irq
	binary.Write(&template, binary.LittleEndian, irqMask)
	// End tag
	template.Write([]byte{0x79, 0x00})

	rt := template.Bytes()
	bufferBody := bytes.Buffer{}
	bufferBody.WriteByte(0x0a)          // BytePrefix for AML integer
	bufferBody.WriteByte(byte(len(rt))) // Buffer size
	bufferBody.Write(rt)

	buffer := wrapPkg(0x11, 0x00, bufferBody.Bytes()) // BufferOp
	buf.Write(buffer)
}

// emitVirtioCRS emits a _CRS buffer for virtio-mmio devices with Memory32Fixed
// and Extended Interrupt descriptors.
func emitVirtioCRS(buf *bytes.Buffer, baseAddr, size uint64, gsi uint32) {
	buf.WriteByte(0x08)     // NameOp
	buf.WriteString("_CRS") // NameString

	template := bytes.Buffer{}

	// Memory32Fixed Descriptor (large resource, type 0x86)
	// Format: Tag(2) + Length(1) + ReadWrite(1) + BaseAddr(4) + Length(4)
	template.WriteByte(0x86) // Memory32Fixed tag
	template.WriteByte(0x09) // Length low byte (9 bytes follow)
	template.WriteByte(0x00) // Length high byte
	template.WriteByte(0x01) // Read/Write (1 = read-write)
	binary.Write(&template, binary.LittleEndian, uint32(baseAddr))
	binary.Write(&template, binary.LittleEndian, uint32(size))

	// Extended Interrupt Descriptor (large resource, type 0x89)
	// Format: Tag(2) + Length(2) + Flags(1) + Count(1) + Interrupts(4*count)
	template.WriteByte(0x89) // Extended Interrupt tag
	template.WriteByte(0x06) // Length low byte (6 bytes follow)
	template.WriteByte(0x00) // Length high byte
	template.WriteByte(0x09) // Flags: consumer, level, active-high, exclusive (for IOAPIC)
	template.WriteByte(0x01) // Interrupt count
	binary.Write(&template, binary.LittleEndian, gsi)

	// End tag
	template.Write([]byte{0x79, 0x00})

	rt := template.Bytes()
	bufferBody := bytes.Buffer{}
	bufferBody.WriteByte(0x0a)          // BytePrefix for AML integer
	bufferBody.WriteByte(byte(len(rt))) // Buffer size
	bufferBody.Write(rt)

	buffer := wrapPkg(0x11, 0x00, bufferBody.Bytes()) // BufferOp
	buf.Write(buffer)
}

// wrapPkg emits an AML opcode with a computed PkgLength and body.
func wrapPkg(opcode byte, opcode2 byte, body []byte) []byte {
	var out bytes.Buffer
	out.WriteByte(opcode)
	if opcode2 != 0x00 {
		out.WriteByte(opcode2)
	}
	out.Write(pkgLength(len(body)))
	out.Write(body)
	return out.Bytes()
}

// pkgLength encodes AML PkgLength for bodies that fit in 2 bytes.
func pkgLength(bodyLen int) []byte {
	// PkgLength encodes the length of following bytes (excluding opcode bytes).
	if bodyLen < 0x40 {
		return []byte{byte(bodyLen)}
	}
	// two-byte encoding: bits 6:0 hold low byte, bits 7-6 set count-1
	low := byte(bodyLen & 0xFF)
	high := byte((bodyLen >> 8) & 0x0F)
	return []byte{0x40 | (low & 0x3F), high}
}

func buildMADTBody(cfg Config) []byte {
	buf := &bytes.Buffer{}

	binary.Write(buf, binary.LittleEndian, cfg.LAPICBase)
	binary.Write(buf, binary.LittleEndian, uint32(1))

	for cpu := 0; cpu < cfg.NumCPUs; cpu++ {
		buf.WriteByte(0)
		buf.WriteByte(8)
		buf.WriteByte(uint8(cpu))
		buf.WriteByte(uint8(cpu))
		binary.Write(buf, binary.LittleEndian, uint32(1))
	}

	buf.WriteByte(1)
	buf.WriteByte(12)
	buf.WriteByte(cfg.IOAPIC.ID)
	buf.WriteByte(0)
	binary.Write(buf, binary.LittleEndian, cfg.IOAPIC.Address)
	binary.Write(buf, binary.LittleEndian, cfg.IOAPIC.GSIBase)

	for _, ovr := range cfg.ISAOverrides {
		buf.WriteByte(2)  // Type = Interrupt Source Override
		buf.WriteByte(10) // Length
		buf.WriteByte(ovr.Bus)
		buf.WriteByte(ovr.IRQ)
		binary.Write(buf, binary.LittleEndian, ovr.GSI)
		binary.Write(buf, binary.LittleEndian, ovr.Flags)
	}

	return buf.Bytes()
}

func buildHPETBody(cfg *HPETConfig) []byte {
	buf := &bytes.Buffer{}

	binary.Write(buf, binary.LittleEndian, uint32(0x8086A201))
	buf.WriteByte(0)
	buf.WriteByte(64)
	buf.WriteByte(0)
	buf.WriteByte(0)
	binary.Write(buf, binary.LittleEndian, cfg.Address)
	buf.WriteByte(0)
	binary.Write(buf, binary.LittleEndian, uint16(0x0080))
	buf.WriteByte(0)

	return buf.Bytes()
}

func buildFADTBody(dsdtAddr uint64) []byte {
	buf := &bytes.Buffer{}

	// Firmware control structures and DSDT pointer
	binary.Write(buf, binary.LittleEndian, uint32(0))
	binary.Write(buf, binary.LittleEndian, uint32(dsdtAddr))

	buf.WriteByte(0)                                  // Reserved
	buf.WriteByte(1)                                  // Preferred_PM_Profile (desktop)
	binary.Write(buf, binary.LittleEndian, uint16(9)) // SCI interrupt
	binary.Write(buf, binary.LittleEndian, uint32(0)) // SMI_CMD
	buf.WriteByte(0)                                  // ACPI_ENABLE
	buf.WriteByte(0)                                  // ACPI_DISABLE
	buf.WriteByte(0)                                  // S4BIOS_REQ
	buf.WriteByte(0)                                  // PSTATE_CNT

	// PM block addresses (PM1a_EVT, PM1b_EVT, PM1a_CNT, PM1b_CNT, PM2_CNT, PM_TMR, GPE0, GPE1)
	for i := 0; i < 8; i++ {
		binary.Write(buf, binary.LittleEndian, uint32(0))
	}

	// PM block lengths
	for i := 0; i < 6; i++ {
		buf.WriteByte(0)
	}
	buf.WriteByte(0) // GPE1_BASE

	buf.WriteByte(0)                                  // CST_CNT
	binary.Write(buf, binary.LittleEndian, uint16(0)) // P_LVL2_LAT
	binary.Write(buf, binary.LittleEndian, uint16(0)) // P_LVL3_LAT
	binary.Write(buf, binary.LittleEndian, uint16(0)) // FLUSH_SIZE
	binary.Write(buf, binary.LittleEndian, uint16(0)) // FLUSH_STRIDE
	buf.WriteByte(0)                                  // DUTY_OFFSET
	buf.WriteByte(0)                                  // DUTY_WIDTH
	buf.WriteByte(0)                                  // DAY_ALRM
	buf.WriteByte(0)                                  // MON_ALRM
	buf.WriteByte(0)                                  // CENTURY

	binary.Write(buf, binary.LittleEndian, uint16(3)) // IAPC_BOOT_ARCH (legacy + 8042)
	buf.WriteByte(0)                                  // Reserved
	binary.Write(buf, binary.LittleEndian, uint32(1<<20))

	buf.Write([]byte{1, 8, 0, 0}) // RESET_REG GAS
	binary.Write(buf, binary.LittleEndian, uint64(0xCF9))
	buf.WriteByte(6)                                  // RESET_VALUE
	binary.Write(buf, binary.LittleEndian, uint16(0)) // ARM_BOOT_ARCH
	buf.WriteByte(1)                                  // FADT Minor Version
	binary.Write(buf, binary.LittleEndian, uint64(0))
	binary.Write(buf, binary.LittleEndian, dsdtAddr)

	for buf.Len()+36 < 244 {
		buf.WriteByte(0)
	}

	return buf.Bytes()
}

func buildXSDTBody(entries []uint64) []byte {
	buf := &bytes.Buffer{}
	for _, entry := range entries {
		binary.Write(buf, binary.LittleEndian, entry)
	}
	return buf.Bytes()
}

func buildRSDP(xsdtAddr uint64, oem OEMInfo) []byte {
	rsdp := make([]byte, 36)
	copy(rsdp[0:], []byte("RSD PTR "))
	copy(rsdp[9:], oem.OEMID[:])
	rsdp[15] = 2
	binary.LittleEndian.PutUint32(rsdp[16:], 0)
	binary.LittleEndian.PutUint32(rsdp[20:], uint32(len(rsdp)))
	binary.LittleEndian.PutUint64(rsdp[24:], xsdtAddr)

	rsdp[8] = checksum(rsdp[:20])
	rsdp[32] = checksum(rsdp)
	return rsdp
}
