package arm64

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareBootOpenBSD79BSD(t *testing.T) {
	kernel, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "local", "openbsd79-arm64", "bsd"))
	if err != nil {
		if os.IsNotExist(err) {
			t.Skip("OpenBSD arm64 fixture not present")
		}
		t.Fatalf("read fixture: %v", err)
	}
	mem := make([]byte, 128<<20)
	const memoryBase = 0xa0000000
	plan, err := PrepareBoot(mem, kernel, BootOptions{
		MemoryBase: memoryBase,
		MemorySize: uint64(len(mem)),
		Console:    true,
	})
	if err != nil {
		t.Fatalf("prepare boot: %v", err)
	}
	if plan.EntryGPA != memoryBase+0x200000 {
		t.Fatalf("EntryGPA = %#x, want %#x", plan.EntryGPA, uint64(memoryBase+0x200000))
	}
	if plan.KernelEndVA <= kernelBaseVA {
		t.Fatalf("KernelEndVA = %#x, want above kernel base", plan.KernelEndVA)
	}
	if plan.KernelEndGPA <= plan.EntryGPA {
		t.Fatalf("KernelEndGPA = %#x, want above entry %#x", plan.KernelEndGPA, plan.EntryGPA)
	}
	if plan.DeviceTreeGPA != memoryBase+dtbLoadOffset {
		t.Fatalf("DeviceTreeGPA = %#x, want %#x", plan.DeviceTreeGPA, uint64(memoryBase+dtbLoadOffset))
	}
	if plan.DeviceTreeGPA >= plan.EntryGPA {
		t.Fatalf("DeviceTreeGPA = %#x, want below entry %#x", plan.DeviceTreeGPA, plan.EntryGPA)
	}
	dtbOff := plan.DeviceTreeGPA - memoryBase
	if dtbOff+4 > uint64(len(mem)) {
		t.Fatalf("DeviceTreeGPA = %#x outside guest memory", plan.DeviceTreeGPA)
	}
	if got := binary.BigEndian.Uint32(mem[dtbOff : dtbOff+4]); got != 0xd00dfeed {
		t.Fatalf("device tree magic = %#x, want %#x", got, uint32(0xd00dfeed))
	}
}
