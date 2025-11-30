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
		Body:       buildMinimalDSDT(),
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

func buildMinimalDSDT() []byte {
	return nil
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

	buf.WriteByte(2)
	buf.WriteByte(10)
	buf.WriteByte(0)
	buf.WriteByte(0)
	binary.Write(buf, binary.LittleEndian, uint32(2))
	binary.Write(buf, binary.LittleEndian, uint16(0))

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
