package vm

import (
	"io"
	"testing"
)

type discardAt struct{}

func (d discardAt) WriteAt(p []byte, off int64) (n int, err error) {
	return len(p), nil
}

var (
	_ io.WriterAt = (*discardAt)(nil)
)

func BenchmarkNewVM1GB(b *testing.B) {
	for b.Loop() {
		vm := NewVirtualMemory(1*1024*1024*1024, 4096)
		_ = vm
	}
}

func BenchmarkNewVM128GB(b *testing.B) {
	for b.Loop() {
		vm := NewVirtualMemory(128*1024*1024*1024, 4096)
		_ = vm
	}
}

func BenchmarkVMWriteSparseTo(b *testing.B) {
	for b.Loop() {
		vm := NewVirtualMemory(1*1024*1024*1024, 4096)
		if _, err := vm.WriteSparseTo(discardAt{}); err != nil {
			_ = err
		}
	}
}
