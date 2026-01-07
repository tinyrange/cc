package rv64

import (
	"os"
	"testing"
)

func TestFDTDump(t *testing.T) {
	m := NewMachine(128*1024*1024, nil, nil)
	fdt := GenerateFDT(m, "console=ttyS0")

	// Write to file for inspection
	if err := os.WriteFile("/tmp/test.dtb", fdt, 0644); err != nil {
		t.Fatalf("Write FDT: %v", err)
	}
	t.Logf("Wrote %d bytes to /tmp/test.dtb", len(fdt))
	t.Log("Run: dtc -I dtb -O dts /tmp/test.dtb")
}
