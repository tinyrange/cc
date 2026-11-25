//go:build linux && amd64

package kvm

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

const (
	kvmGetStatsFD       = 0xaece
	kvmCapBinaryStatsFD = 203
)

// ExecutionCounters reads scalar KVM statistics for the VM and selected vCPU.
// Read while the owning vCPU is stopped for a consistent measurement endpoint.
func (v *VM) ExecutionCounters(index int) (map[string]uint64, error) {
	supported, err := checkExtension(v.kvm.fd, kvmCapBinaryStatsFD)
	if err != nil || supported == 0 {
		return nil, err
	}
	cpu, err := kvmVCPU(v, index)
	if err != nil {
		return nil, err
	}
	values := make(map[string]uint64)
	for _, source := range []struct {
		fd     int
		prefix string
	}{{v.vmfd, "vm."}, {cpu.fd, "vcpu."}} {
		fd, err := ioctl(uintptr(source.fd), kvmGetStatsFD, 0)
		if err != nil {
			return nil, fmt.Errorf("KVM_GET_STATS_FD: %w", err)
		}
		file := os.NewFile(fd, "kvm-stats")
		err = readScalarStats(file, source.prefix, values)
		_ = file.Close()
		if err != nil {
			return nil, err
		}
	}
	return values, nil
}

func readScalarStats(reader io.ReaderAt, prefix string, result map[string]uint64) error {
	const maxBlock = 1 << 20
	var header [24]byte
	if _, err := reader.ReadAt(header[:], 0); err != nil {
		return err
	}
	nameSize := uint64(binary.LittleEndian.Uint32(header[4:]))
	count := uint64(binary.LittleEndian.Uint32(header[8:]))
	descOffset := int64(binary.LittleEndian.Uint32(header[16:]))
	dataOffset := int64(binary.LittleEndian.Uint32(header[20:]))
	stride := 16 + nameSize
	if nameSize < 2 || nameSize > 256 || count > maxBlock/stride || descOffset < 24 || dataOffset < 24 {
		return fmt.Errorf("invalid KVM statistics header")
	}
	descriptors := make([]byte, count*stride)
	if len(descriptors) == 0 {
		return nil
	}
	if _, err := reader.ReadAt(descriptors, descOffset); err != nil {
		return err
	}
	var dataSize uint64
	for offset := uint64(0); offset < uint64(len(descriptors)); offset += stride {
		desc := descriptors[offset : offset+stride]
		end := uint64(binary.LittleEndian.Uint32(desc[8:])) + uint64(binary.LittleEndian.Uint16(desc[6:]))*8
		dataSize = max(dataSize, end)
	}
	if dataSize == 0 || dataSize > maxBlock {
		return fmt.Errorf("invalid KVM statistics data size")
	}
	data := make([]byte, dataSize)
	if _, err := reader.ReadAt(data, dataOffset); err != nil {
		return err
	}
	for offset := uint64(0); offset < uint64(len(descriptors)); offset += stride {
		desc := descriptors[offset : offset+stride]
		if binary.LittleEndian.Uint16(desc[6:]) != 1 {
			continue
		}
		name, _, ok := bytes.Cut(desc[16:], []byte{0})
		if !ok || len(name) == 0 {
			return fmt.Errorf("invalid KVM statistic name")
		}
		start := binary.LittleEndian.Uint32(desc[8:])
		result[prefix+string(name)] = binary.LittleEndian.Uint64(data[start:])
	}
	return nil
}
