package amd64

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/tinyrange/cc/internal/hv"
)

type stubVM struct {
	mem        []byte
	memoryBase uint64
}

func (s *stubVM) CaptureSnapshot() (hv.Snapshot, error)                  { panic("unimplemented") }
func (s *stubVM) RestoreSnapshot(snap hv.Snapshot) error                 { panic("unimplemented") }
func (s *stubVM) AddDevice(dev hv.Device) error                          { panic("unimplemented") }
func (s *stubVM) AddDeviceFromTemplate(template hv.DeviceTemplate) error { panic("unimplemented") }
func (s *stubVM) Close() error                                           { panic("unimplemented") }
func (s *stubVM) Hypervisor() hv.Hypervisor                              { panic("unimplemented") }
func (s *stubVM) Run(ctx context.Context, cfg hv.RunConfig) error        { panic("unimplemented") }
func (s *stubVM) VirtualCPUCall(id int, f func(vcpu hv.VirtualCPU) error) error {
	panic("unimplemented")
}
func (s *stubVM) AllocateMemory(physAddr uint64, size uint64) (hv.MemoryRegion, error) {
	panic("unimplemented")
}

func (s *stubVM) MemorySize() uint64 {
	return uint64(len(s.mem))
}

func (s *stubVM) MemoryBase() uint64 {
	return s.memoryBase
}

// ReadAt implements hv.VirtualMachine.
func (s *stubVM) ReadAt(p []byte, off int64) (n int, err error) {
	off = off - int64(s.memoryBase)

	if off < 0 || int(off) >= len(s.mem) {
		return 0, os.ErrInvalid
	}

	n = copy(p, s.mem[off:])
	if n < len(p) {
		err = os.ErrInvalid
	}

	return n, err
}

// WriteAt implements hv.VirtualMachine.
func (s *stubVM) WriteAt(p []byte, off int64) (n int, err error) {
	off = off - int64(s.memoryBase)

	if off < 0 || int(off) >= len(s.mem) {
		return 0, os.ErrInvalid
	}

	n = copy(s.mem[off:], p)
	if n < len(p) {
		err = os.ErrInvalid
	}

	return n, err
}

var (
	_ hv.VirtualMachine = &stubVM{}
)

func TestPrepareSetsE820ToFullGuestRAM(t *testing.T) {
	kernelPath := filepath.Join("..", "local", "vmlinux_amd64")
	if _, err := os.Stat(kernelPath); err != nil {
		t.Skipf("skipping: %v", err)
	}

	f, err := os.Open(kernelPath)
	if err != nil {
		t.Fatalf("Open kernel: %v", err)
	}
	defer f.Close()

	kernel, err := LoadKernel(f, func() int64 {
		info, err := f.Stat()
		if err != nil {
			t.Fatalf("Stat kernel: %v", err)
		}
		return info.Size()
	}())
	if err != nil {
		t.Fatalf("LoadKernel: %v", err)
	}

	// Allocate a reasonably sized guest memory region (256 MiB) which should
	// comfortably cover the kernel payload plus boot parameters.
	const memSize = 256 << 20
	vm := &stubVM{
		mem:        make([]byte, memSize),
		memoryBase: 0x0,
	}

	plan, err := kernel.Prepare(vm, BootOptions{})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	zpOff := int(plan.ZeroPageGPA - vm.MemoryBase())
	if zpOff < 0 || zpOff+zeroPageSize > len(vm.mem) {
		t.Fatalf("zero page outside guest memory: off=%#x", zpOff)
	}
	zp := make([]byte, zeroPageSize)
	if _, err := vm.ReadAt(zp, int64(zpOff)); err != nil {
		t.Fatalf("ReadAt zero page: %v", err)
	}

	gotEntries := int(zp[zeroPageE820Entries])
	if gotEntries < 2 {
		t.Fatalf("e820 entries = %d, want at least 2", gotEntries)
	}

	type entry struct {
		addr uint64
		size uint64
		typ  uint32
	}
	entries := make([]entry, 0, gotEntries)
	for i := 0; i < gotEntries; i++ {
		base := zeroPageE820Table + i*e820EntrySize
		addr := binary.LittleEndian.Uint64(zp[base:])
		size := binary.LittleEndian.Uint64(zp[base+8:])
		typ := binary.LittleEndian.Uint32(zp[base+16:])
		entries = append(entries, entry{addr: addr, size: size, typ: typ})
	}

	first := entries[0]
	if first.typ != 1 {
		t.Fatalf("e820[0].type = %d, want 1 (usable RAM)", first.typ)
	}
	if first.addr != vm.MemoryBase() {
		t.Fatalf("e820[0].addr = %#x, want %#x", first.addr, vm.MemoryBase())
	}

	last := entries[len(entries)-1]
	if last.typ != 1 {
		t.Fatalf("e820[last].type = %d, want 1 (usable RAM)", last.typ)
	}
	if end := last.addr + last.size; end != vm.MemoryBase()+memSize {
		t.Fatalf("e820[last] end = %#x, want %#x", end, vm.MemoryBase()+memSize)
	}

	var hasReserved bool
	for _, ent := range entries {
		if ent.typ == 2 {
			hasReserved = true
		}
	}
	if memSize > 1<<20 && !hasReserved {
		t.Fatalf("expected reserved ISA/BIOs hole entry in e820 map, got %+v", entries)
	}
}
