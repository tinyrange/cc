package arm64

import (
	"bytes"
	"testing"
)

func TestPrepareBootPlacesKernelAndDeviceTree(t *testing.T) {
	memBase := uint64(0x80000000)
	mem := make([]byte, 32<<20)
	kernel := buildTestImage()

	plan, err := PrepareBoot(mem, kernel, BootOptions{
		MemoryBase: memBase,
		MemorySize: uint64(len(mem)),
		Cmdline:    "console=ttyS0",
		Console:    true,
		NumCPUs:    1,
	})
	if err != nil {
		t.Fatalf("PrepareBoot() error = %v", err)
	}
	if plan.EntryGPA == 0 || plan.DeviceTreeGPA == 0 || plan.StackTopGPA == 0 {
		t.Fatalf("invalid boot plan: %+v", plan)
	}
	if !bytes.Contains(plan.DeviceTree, []byte("stdout-path\x00")) {
		t.Fatal("device tree missing stdout-path")
	}
	if !bytes.Contains(plan.DeviceTree, []byte("serial0\x00")) {
		t.Fatal("device tree missing serial alias")
	}
}
