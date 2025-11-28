package amd64

import (
	"bytes"
	"fmt"
	"io"
)

type kernelFormat int

const (
	kernelFormatBzImage kernelFormat = iota
	kernelFormatELF
)

type elfSegment struct {
	physAddr uint64
	fileSize uint64
	memSize  uint64
	data     []byte
}

// KernelImage represents a loaded Linux kernel image on the host side.
type KernelImage struct {
	format kernelFormat

	// bzImage-specific fields.
	Data          []byte
	Header        SetupHeader
	HeaderBytes   []byte
	PayloadOffset int

	// ELF-specific fields.
	elfSegments []elfSegment
	elfEntry    uint64
	elfMinPhys  uint64
	elfMaxPhys  uint64
}

// LoadKernel detects the format of kernelPath and returns a parsed KernelImage.
func LoadKernel(kernel io.ReaderAt, kernelSize int64) (*KernelImage, error) {
	var magic [4]byte
	if _, err := kernel.ReadAt(magic[:], 0); err != nil {
		return nil, fmt.Errorf("read kernel image header: %w", err)
	}
	if bytes.Equal(magic[:], []byte{0x7f, 'E', 'L', 'F'}) {
		return loadELFKernel(kernel)
	}
	return LoadBzImage(kernel, kernelSize)
}
