package rv64

import (
	"bytes"
	"encoding/binary"
)

// FDT constants
const (
	FDTMagic       = 0xd00dfeed
	FDTBeginNode   = 0x00000001
	FDTEndNode     = 0x00000002
	FDTProp        = 0x00000003
	FDTNOP         = 0x00000004
	FDTEnd         = 0x00000009
	FDTVersion     = 17
	FDTLastCompVer = 16
)

// FDTBuilder builds a flattened device tree
type FDTBuilder struct {
	structure bytes.Buffer
	strings   bytes.Buffer
	stringMap map[string]uint32
}

// NewFDTBuilder creates a new FDT builder
func NewFDTBuilder() *FDTBuilder {
	return &FDTBuilder{
		stringMap: make(map[string]uint32),
	}
}

// putU32 writes a big-endian uint32
func (f *FDTBuilder) putU32(v uint32) {
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], v)
	f.structure.Write(buf[:])
}

// addString adds a string to the string table and returns its offset
func (f *FDTBuilder) addString(s string) uint32 {
	if off, ok := f.stringMap[s]; ok {
		return off
	}
	off := uint32(f.strings.Len())
	f.strings.WriteString(s)
	f.strings.WriteByte(0)
	f.stringMap[s] = off
	return off
}

// BeginNode starts a new node
func (f *FDTBuilder) BeginNode(name string) {
	f.putU32(FDTBeginNode)
	f.structure.WriteString(name)
	f.structure.WriteByte(0)
	// Align to 4 bytes
	for f.structure.Len()%4 != 0 {
		f.structure.WriteByte(0)
	}
}

// EndNode ends the current node
func (f *FDTBuilder) EndNode() {
	f.putU32(FDTEndNode)
}

// AddPropertyString adds a string property
func (f *FDTBuilder) AddPropertyString(name, value string) {
	f.putU32(FDTProp)
	f.putU32(uint32(len(value) + 1))
	f.putU32(f.addString(name))
	f.structure.WriteString(value)
	f.structure.WriteByte(0)
	for f.structure.Len()%4 != 0 {
		f.structure.WriteByte(0)
	}
}

// AddPropertyStringList adds a string list property
func (f *FDTBuilder) AddPropertyStringList(name string, values []string) {
	var buf bytes.Buffer
	for _, v := range values {
		buf.WriteString(v)
		buf.WriteByte(0)
	}
	f.putU32(FDTProp)
	f.putU32(uint32(buf.Len()))
	f.putU32(f.addString(name))
	f.structure.Write(buf.Bytes())
	for f.structure.Len()%4 != 0 {
		f.structure.WriteByte(0)
	}
}

// AddPropertyU32 adds a u32 property
func (f *FDTBuilder) AddPropertyU32(name string, value uint32) {
	f.putU32(FDTProp)
	f.putU32(4)
	f.putU32(f.addString(name))
	f.putU32(value)
}

// AddPropertyU64 adds a u64 property
func (f *FDTBuilder) AddPropertyU64(name string, value uint64) {
	f.putU32(FDTProp)
	f.putU32(8)
	f.putU32(f.addString(name))
	f.putU32(uint32(value >> 32))
	f.putU32(uint32(value))
}

// AddPropertyU32Array adds a u32 array property
func (f *FDTBuilder) AddPropertyU32Array(name string, values []uint32) {
	f.putU32(FDTProp)
	f.putU32(uint32(len(values) * 4))
	f.putU32(f.addString(name))
	for _, v := range values {
		f.putU32(v)
	}
}

// AddPropertyEmpty adds an empty property (marker)
func (f *FDTBuilder) AddPropertyEmpty(name string) {
	f.putU32(FDTProp)
	f.putU32(0)
	f.putU32(f.addString(name))
}

// AddPropertyBytes adds a raw bytes property
func (f *FDTBuilder) AddPropertyBytes(name string, data []byte) {
	f.putU32(FDTProp)
	f.putU32(uint32(len(data)))
	f.putU32(f.addString(name))
	f.structure.Write(data)
	for f.structure.Len()%4 != 0 {
		f.structure.WriteByte(0)
	}
}

// Build finalizes and returns the FDT blob
func (f *FDTBuilder) Build() []byte {
	f.putU32(FDTEnd)

	// Align strings
	for f.strings.Len()%4 != 0 {
		f.strings.WriteByte(0)
	}

	// Calculate offsets
	headerSize := uint32(40)
	memRsvmapOff := headerSize
	memRsvmapSize := uint32(16) // One empty entry
	structOff := memRsvmapOff + memRsvmapSize
	structSize := uint32(f.structure.Len())
	stringsOff := structOff + structSize
	stringsSize := uint32(f.strings.Len())
	totalSize := stringsOff + stringsSize

	// Build header
	var header bytes.Buffer
	hdr := func(v uint32) {
		var buf [4]byte
		binary.BigEndian.PutUint32(buf[:], v)
		header.Write(buf[:])
	}

	hdr(FDTMagic)
	hdr(totalSize)
	hdr(structOff)
	hdr(stringsOff)
	hdr(memRsvmapOff)
	hdr(FDTVersion)
	hdr(FDTLastCompVer)
	hdr(0) // boot_cpuid_phys
	hdr(stringsSize)
	hdr(structSize)

	// Build memory reservation map (empty)
	var memRsvmap [16]byte

	// Combine all parts
	result := make([]byte, totalSize)
	copy(result[0:], header.Bytes())
	copy(result[memRsvmapOff:], memRsvmap[:])
	copy(result[structOff:], f.structure.Bytes())
	copy(result[stringsOff:], f.strings.Bytes())

	return result
}

// GenerateFDT generates an FDT for the machine
func GenerateFDT(m *Machine, cmdline string) []byte {
	ramSize := m.MemorySize()

	f := NewFDTBuilder()

	// Root node
	f.BeginNode("")
	f.AddPropertyU32("#address-cells", 2)
	f.AddPropertyU32("#size-cells", 2)
	f.AddPropertyString("compatible", "riscv-virtio")
	f.AddPropertyString("model", "riscv-virtio,qemu")

	// Chosen node
	f.BeginNode("chosen")
	f.AddPropertyString("bootargs", cmdline)
	f.AddPropertyString("stdout-path", "/soc/serial@10000000")
	f.EndNode()

	// CPUs node
	f.BeginNode("cpus")
	f.AddPropertyU32("#address-cells", 1)
	f.AddPropertyU32("#size-cells", 0)
	f.AddPropertyU32("timebase-frequency", 10000000)

	// CPU 0
	f.BeginNode("cpu@0")
	f.AddPropertyString("device_type", "cpu")
	f.AddPropertyU32("reg", 0)
	f.AddPropertyString("status", "okay")
	f.AddPropertyString("compatible", "riscv")
	f.AddPropertyString("riscv,isa", "rv64imafdc_zicsr_zifencei")
	f.AddPropertyString("mmu-type", "riscv,sv48")

	// Interrupt controller
	f.BeginNode("interrupt-controller")
	f.AddPropertyU32("#interrupt-cells", 1)
	f.AddPropertyEmpty("interrupt-controller")
	f.AddPropertyString("compatible", "riscv,cpu-intc")
	f.AddPropertyU32("phandle", 1)
	f.EndNode()

	f.EndNode() // cpu@0
	f.EndNode() // cpus

	// Memory node
	f.BeginNode("memory@80000000")
	f.AddPropertyString("device_type", "memory")
	f.AddPropertyU32Array("reg", []uint32{
		uint32(RAMBase >> 32), uint32(RAMBase),
		uint32(ramSize >> 32), uint32(ramSize),
	})
	f.EndNode()

	// SOC node
	f.BeginNode("soc")
	f.AddPropertyU32("#address-cells", 2)
	f.AddPropertyU32("#size-cells", 2)
	f.AddPropertyStringList("compatible", []string{"simple-bus"})
	f.AddPropertyEmpty("ranges")

	// CLINT
	f.BeginNode("clint@2000000")
	f.AddPropertyStringList("compatible", []string{"sifive,clint0", "riscv,clint0"})
	f.AddPropertyU32Array("reg", []uint32{
		uint32(CLINTBase >> 32), uint32(CLINTBase),
		uint32(CLINTSize >> 32), uint32(CLINTSize),
	})
	f.AddPropertyU32Array("interrupts-extended", []uint32{1, 3, 1, 7})
	f.EndNode()

	// PLIC
	f.BeginNode("plic@c000000")
	f.AddPropertyString("compatible", "sifive,plic-1.0.0")
	f.AddPropertyU32("#interrupt-cells", 1)
	f.AddPropertyEmpty("interrupt-controller")
	f.AddPropertyU32Array("reg", []uint32{
		uint32(PLICBase >> 32), uint32(PLICBase),
		uint32(PLICSize >> 32), uint32(PLICSize),
	})
	f.AddPropertyU32Array("interrupts-extended", []uint32{1, 9, 1, 11})
	f.AddPropertyU32("riscv,ndev", 127)
	f.AddPropertyU32("phandle", 2)
	f.EndNode()

	// UART
	f.BeginNode("serial@10000000")
	f.AddPropertyString("compatible", "ns16550a")
	f.AddPropertyU32Array("reg", []uint32{
		uint32(UARTBase >> 32), uint32(UARTBase),
		uint32(UARTSize >> 32), uint32(UARTSize),
	})
	f.AddPropertyU32("clock-frequency", 3686400)
	f.AddPropertyU32("interrupts", 10)
	f.AddPropertyU32("interrupt-parent", 2)
	f.EndNode()

	f.EndNode() // soc
	f.EndNode() // root

	return f.Build()
}
