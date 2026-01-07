// Package riscv64 provides RISC-V 64-bit Linux kernel loading.
package riscv64

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
)

// KernelImage represents a RISC-V Linux kernel image.
// RISC-V kernels are typically flat binary images that run at 0x8020_0000.
type KernelImage struct {
	payload []byte
}

// LoadKernel loads a RISC-V Linux kernel image.
// The kernel is expected to be a flat binary (Image file) or gzip-compressed.
func LoadKernel(reader io.ReaderAt, size int64) (*KernelImage, error) {
	if size <= 0 {
		return nil, fmt.Errorf("invalid kernel size: %d", size)
	}

	payload := make([]byte, size)
	n, err := reader.ReadAt(payload, 0)
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("read kernel: %w", err)
	}
	if n != int(size) && n != len(payload) {
		payload = payload[:n]
	}

	// Check if gzip compressed
	if len(payload) >= 2 && payload[0] == 0x1f && payload[1] == 0x8b {
		decompressed, err := decompressGzip(payload)
		if err != nil {
			return nil, fmt.Errorf("decompress kernel: %w", err)
		}
		payload = decompressed
	}

	return &KernelImage{
		payload: payload,
	}, nil
}

// Payload returns the raw kernel bytes.
func (k *KernelImage) Payload() []byte {
	if k == nil {
		return nil
	}
	return k.payload
}

// Size returns the size of the kernel payload.
func (k *KernelImage) Size() int64 {
	if k == nil {
		return 0
	}
	return int64(len(k.payload))
}

// decompressGzip decompresses a gzip-compressed byte slice.
func decompressGzip(data []byte) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(reader)
}
