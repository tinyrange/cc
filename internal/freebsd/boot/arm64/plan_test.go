package arm64

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareBootFreeBSD151Kernel(t *testing.T) {
	kernel, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "local", "freebsd151-arm64", "kernel"))
	if err != nil {
		if os.IsNotExist(err) {
			t.Skip("FreeBSD arm64 fixture not present")
		}
		t.Fatalf("read fixture: %v", err)
	}
	mem := make([]byte, 256<<20)
	const memoryBase = 0xa0000000
	plan, err := PrepareBoot(mem, kernel, BootOptions{
		MemoryBase: memoryBase,
		MemorySize: uint64(len(mem)),
		Console:    true,
	})
	if err != nil {
		t.Fatalf("prepare boot: %v", err)
	}
	if plan.EntryGPA != memoryBase+0x800 {
		t.Fatalf("EntryGPA = %#x, want %#x", plan.EntryGPA, uint64(memoryBase+0x800))
	}
	if plan.KernelEndGPA <= plan.EntryGPA {
		t.Fatalf("KernelEndGPA = %#x, want above entry %#x", plan.KernelEndGPA, plan.EntryGPA)
	}
	if plan.DeviceTreeGPA < plan.KernelEndGPA {
		t.Fatalf("DeviceTreeGPA = %#x, want at or above KernelEndGPA %#x", plan.DeviceTreeGPA, plan.KernelEndGPA)
	}
	dtbOff := plan.DeviceTreeGPA - memoryBase
	if dtbOff+4 > uint64(len(mem)) {
		t.Fatalf("DeviceTreeGPA = %#x outside guest memory", plan.DeviceTreeGPA)
	}
	if got := binary.BigEndian.Uint32(mem[dtbOff : dtbOff+4]); got != 0xd00dfeed {
		t.Fatalf("device tree magic = %#x, want %#x", got, uint32(0xd00dfeed))
	}
}
