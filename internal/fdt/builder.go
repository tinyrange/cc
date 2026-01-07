// Package fdt provides utilities for building Flattened Device Trees (FDT).
package fdt

import (
	"encoding/binary"
)

const (
	fdtBuilderMagic      = 0xd00dfeed
	fdtBuilderVersion    = 17
	fdtBuilderCompatible = 16

	fdtBuilderBeginNode = 0x00000001
	fdtBuilderEndNode   = 0x00000002
	fdtBuilderProp      = 0x00000003
	fdtBuilderNop       = 0x00000004
	fdtBuilderEnd       = 0x00000009
)

// Builder constructs a Flattened Device Tree blob.
type Builder struct {
	structure []byte
	strings   []byte
	stringOff map[string]uint32
}

// NewBuilder creates a new FDT builder.
func NewBuilder() *Builder {
	return &Builder{
		stringOff: make(map[string]uint32),
	}
}

// BeginNode starts a new node with the given name.
func (b *Builder) BeginNode(name string) {
	b.appendU32(fdtBuilderBeginNode)
	b.appendString(name)
}

// EndNode ends the current node.
func (b *Builder) EndNode() {
	b.appendU32(fdtBuilderEndNode)
}

// AddPropertyEmpty adds an empty property.
func (b *Builder) AddPropertyEmpty(name string) {
	b.appendU32(fdtBuilderProp)
	b.appendU32(0) // length
	b.appendU32(b.addString(name))
}

// AddPropertyString adds a string property.
func (b *Builder) AddPropertyString(name, value string) {
	data := append([]byte(value), 0)
	b.appendU32(fdtBuilderProp)
	b.appendU32(uint32(len(data)))
	b.appendU32(b.addString(name))
	b.appendBytes(data)
}

// AddPropertyStringList adds a string list property.
func (b *Builder) AddPropertyStringList(name string, values []string) {
	var data []byte
	for _, v := range values {
		data = append(data, v...)
		data = append(data, 0)
	}
	b.appendU32(fdtBuilderProp)
	b.appendU32(uint32(len(data)))
	b.appendU32(b.addString(name))
	b.appendBytes(data)
}

// AddPropertyU32 adds a 32-bit unsigned integer property.
func (b *Builder) AddPropertyU32(name string, value uint32) {
	b.appendU32(fdtBuilderProp)
	b.appendU32(4)
	b.appendU32(b.addString(name))
	b.appendU32(value)
}

// AddPropertyU32Array adds an array of 32-bit unsigned integers.
func (b *Builder) AddPropertyU32Array(name string, values []uint32) {
	b.appendU32(fdtBuilderProp)
	b.appendU32(uint32(len(values) * 4))
	b.appendU32(b.addString(name))
	for _, v := range values {
		b.appendU32(v)
	}
}

// AddPropertyU64 adds a 64-bit unsigned integer property.
func (b *Builder) AddPropertyU64(name string, value uint64) {
	b.appendU32(fdtBuilderProp)
	b.appendU32(8)
	b.appendU32(b.addString(name))
	b.appendU64(value)
}

// AddPropertyU64Pair adds a pair of 64-bit values (e.g., for reg properties).
func (b *Builder) AddPropertyU64Pair(name string, addr, size uint64) {
	b.appendU32(fdtBuilderProp)
	b.appendU32(16)
	b.appendU32(b.addString(name))
	b.appendU64(addr)
	b.appendU64(size)
}

// AddPropertyBytes adds a raw bytes property.
func (b *Builder) AddPropertyBytes(name string, data []byte) {
	b.appendU32(fdtBuilderProp)
	b.appendU32(uint32(len(data)))
	b.appendU32(b.addString(name))
	b.appendBytes(data)
}

// Build generates the final FDT blob.
func (b *Builder) Build() []byte {
	// End the structure block
	b.appendU32(fdtBuilderEnd)

	// Calculate offsets
	headerSize := uint32(40)
	memRsvmapOff := headerSize
	memRsvmapSize := uint32(16) // empty reservation map (2 x 8 bytes of zeros)
	structOff := memRsvmapOff + memRsvmapSize
	structSize := uint32(len(b.structure))
	stringsOff := structOff + structSize
	stringsSize := uint32(len(b.strings))
	totalSize := stringsOff + stringsSize

	// Build header
	header := make([]byte, headerSize)
	binary.BigEndian.PutUint32(header[0:], fdtBuilderMagic)
	binary.BigEndian.PutUint32(header[4:], totalSize)
	binary.BigEndian.PutUint32(header[8:], structOff)
	binary.BigEndian.PutUint32(header[12:], stringsOff)
	binary.BigEndian.PutUint32(header[16:], memRsvmapOff)
	binary.BigEndian.PutUint32(header[20:], fdtBuilderVersion)
	binary.BigEndian.PutUint32(header[24:], fdtBuilderCompatible)
	binary.BigEndian.PutUint32(header[28:], 0) // boot_cpuid_phys
	binary.BigEndian.PutUint32(header[32:], stringsSize)
	binary.BigEndian.PutUint32(header[36:], structSize)

	// Assemble the blob
	blob := make([]byte, totalSize)
	copy(blob, header)
	// Memory reservation map is already zeros
	copy(blob[structOff:], b.structure)
	copy(blob[stringsOff:], b.strings)

	return blob
}

func (b *Builder) appendU32(v uint32) {
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, v)
	b.structure = append(b.structure, buf...)
}

func (b *Builder) appendU64(v uint64) {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, v)
	b.structure = append(b.structure, buf...)
}

func (b *Builder) appendString(s string) {
	data := append([]byte(s), 0)
	b.structure = append(b.structure, data...)
	// Align to 4 bytes
	for len(b.structure)%4 != 0 {
		b.structure = append(b.structure, 0)
	}
}

func (b *Builder) appendBytes(data []byte) {
	b.structure = append(b.structure, data...)
	// Align to 4 bytes
	for len(b.structure)%4 != 0 {
		b.structure = append(b.structure, 0)
	}
}

func (b *Builder) addString(name string) uint32 {
	if off, ok := b.stringOff[name]; ok {
		return off
	}
	off := uint32(len(b.strings))
	b.stringOff[name] = off
	b.strings = append(b.strings, name...)
	b.strings = append(b.strings, 0)
	return off
}
