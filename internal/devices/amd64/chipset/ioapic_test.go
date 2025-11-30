package chipset

import (
	"encoding/binary"
	"testing"
)

type ioapicTestRouter struct {
	calls []ioapicCall
}

type ioapicCall struct {
	line   uint8
	vector uint8
}

func (r *ioapicTestRouter) Assert(line uint8, vector uint8) {
	r.calls = append(r.calls, ioapicCall{line: line, vector: vector})
}

func TestIOAPICVersionRegister(t *testing.T) {
	dev := NewIOAPIC(24)

	writeIndex(t, dev, ioapicVersionRegister)
	value := readData(t, dev)
	if got, want := value&0xff, uint32(ioapicVersion); got != want {
		t.Fatalf("version register = 0x%x, want 0x%x", got, want)
	}
	if got, want := (value>>16)&0xff, uint32(len(dev.entries)-1); got != want {
		t.Fatalf("max redirection entry = %d, want %d", got, want)
	}
}

func TestIOAPICDeliversEdgeInterrupts(t *testing.T) {
	dev := NewIOAPIC(24)
	router := &ioapicTestRouter{}
	dev.SetRouting(router)

	programRedirection(t, dev, 0, 0x45, false, false)

	dev.SetIRQ(0, true)
	if len(router.calls) != 1 {
		t.Fatalf("expected one interrupt, got %d", len(router.calls))
	}
	if router.calls[0].vector != 0x45 {
		t.Fatalf("unexpected vector 0x%x", router.calls[0].vector)
	}

	// Keeping the line high should not retrigger.
	dev.SetIRQ(0, true)
	if len(router.calls) != 1 {
		t.Fatalf("unexpected retrigger while line high")
	}

	// Falling edge then rising edge should retrigger.
	dev.SetIRQ(0, false)
	dev.SetIRQ(0, true)
	if len(router.calls) != 2 {
		t.Fatalf("expected second interrupt, got %d", len(router.calls))
	}
}

func TestIOAPICLevelInterruptRequiresEOI(t *testing.T) {
	dev := NewIOAPIC(24)
	router := &ioapicTestRouter{}
	dev.SetRouting(router)

	const line = 5
	const vector = 0x55
	programRedirection(t, dev, line, vector, true, false)

	dev.SetIRQ(line, true)
	if len(router.calls) != 1 {
		t.Fatalf("expected first interrupt, got %d", len(router.calls))
	}

	dev.SetIRQ(line, false)
	dev.SetIRQ(line, true)
	if len(router.calls) != 1 {
		t.Fatalf("level interrupt fired without EOI")
	}

	dev.HandleEOI(vector)
	if len(router.calls) != 2 {
		t.Fatalf("expected second interrupt after EOI, got %d", len(router.calls))
	}
}

func programRedirection(t *testing.T, dev *IOAPIC, line uint32, vector byte, level bool, masked bool) {
	low := uint32(vector)
	if level {
		low |= 1 << 15
	}
	if masked {
		low |= 1 << 16
	}

	writeIndex(t, dev, ioapicRedirectionTableBase+uint8(line*2))
	writeData(t, dev, low)

	writeIndex(t, dev, ioapicRedirectionTableBase+uint8(line*2)+1)
	writeData(t, dev, 0)
}

func writeIndex(t *testing.T, dev *IOAPIC, index uint8) {
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, uint32(index))
	if err := dev.WriteMMIO(IOAPICBaseAddress+ioapicRegisterSelect, buf); err != nil {
		t.Fatalf("write select: %v", err)
	}
}

func writeData(t *testing.T, dev *IOAPIC, value uint32) {
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, value)
	if err := dev.WriteMMIO(IOAPICBaseAddress+ioapicRegisterData, buf); err != nil {
		t.Fatalf("write data: %v", err)
	}
}

func readData(t *testing.T, dev *IOAPIC) uint32 {
	buf := make([]byte, 4)
	if err := dev.ReadMMIO(IOAPICBaseAddress+ioapicRegisterData, buf); err != nil {
		t.Fatalf("read data: %v", err)
	}
	return binary.LittleEndian.Uint32(buf)
}
