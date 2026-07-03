//go:build arm64 && (darwin || linux)

package hv

import (
	"os"
	"syscall"
)

type unixBenchmarkGuestMapping struct {
	data []byte
}

func mapBenchmarkGuestFile(path string, size int) (benchmarkGuestMapping, error) {
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	data, err := syscall.Mmap(int(file.Fd()), 0, size, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_PRIVATE)
	if err != nil {
		return nil, err
	}
	return unixBenchmarkGuestMapping{data: data}, nil
}

func (m unixBenchmarkGuestMapping) Bytes() []byte {
	return m.data
}

func (m unixBenchmarkGuestMapping) Close() error {
	if len(m.data) == 0 {
		return nil
	}
	return syscall.Munmap(m.data)
}
