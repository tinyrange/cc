package chipset

import (
	"encoding/binary"
	"testing"
)

type capturedInterrupt struct {
	vector       uint8
	dest         uint8
	destMode     uint8
	deliveryMode uint8
	level        bool
}

type captureRouter struct {
	events []capturedInterrupt
}

func (c *captureRouter) Assert(vector uint8, dest uint8, destMode uint8, deliveryMode uint8, level bool) {
	c.events = append(c.events, capturedInterrupt{
		vector:       vector,
		dest:         dest,
		destMode:     destMode,
		deliveryMode: deliveryMode,
		level:        level,
	})
}

func programRedirection(t *testing.T, io *IOAPIC, line uint8, vector uint8, dest uint8, level bool, masked bool) {
	t.Helper()

	lowIndex := uint8(0x10 + line*2)
	highIndex := lowIndex + 1

	var low uint32
	low |= uint32(vector)
	// Delivery mode fixed, dest mode physical by default.
	if level {
		low |= 1 << 15
	}
	if masked {
		low |= 1 << 16
	}

	high := uint32(dest) << 24

	writeRegister := func(index uint8, value uint32) {
		if err := io.WriteMMIO(IOAPICBaseAddress+0x00, []byte{index}); err != nil {
			t.Fatalf("write select %d: %v", index, err)
		}
		buf := make([]byte, 4)
		binary.LittleEndian.PutUint32(buf, value)
		if err := io.WriteMMIO(IOAPICBaseAddress+0x10, buf); err != nil {
			t.Fatalf("write data idx=%d: %v", index, err)
		}
	}

	writeRegister(lowIndex, low)
	writeRegister(highIndex, high)
}

func readRedirectionLow(t *testing.T, io *IOAPIC, line uint8) uint32 {
	t.Helper()
	index := uint8(0x10 + line*2)
	if err := io.WriteMMIO(IOAPICBaseAddress+0x00, []byte{index}); err != nil {
		t.Fatalf("write select %d: %v", index, err)
	}
	buf := make([]byte, 4)
	if err := io.ReadMMIO(IOAPICBaseAddress+0x10, buf); err != nil {
		t.Fatalf("read data idx=%d: %v", index, err)
	}
	return binary.LittleEndian.Uint32(buf)
}

func TestIOAPICEdgeTriggeredInterrupt(t *testing.T) {
	io := NewIOAPIC(4)
	router := &captureRouter{}
	io.SetRouting(router)
	programRedirection(t, io, 0, 0x30, 1, false, false)

	io.SetIRQ(0, true)

	if len(router.events) != 1 {
		t.Fatalf("expected 1 interrupt, got %d", len(router.events))
	}
	ev := router.events[0]
	if ev.vector != 0x30 || ev.dest != 1 || ev.level {
		t.Fatalf("unexpected interrupt %+v", ev)
	}
}

func TestIOAPICLevelReassertAfterEOI(t *testing.T) {
	io := NewIOAPIC(4)
	router := &captureRouter{}
	io.SetRouting(router)
	programRedirection(t, io, 1, 0x41, 0, true, false)

	io.SetIRQ(1, true)
	if len(router.events) != 1 {
		t.Fatalf("expected 1 interrupt after assert, got %d", len(router.events))
	}

	// EOI should clear remote-IRR and re-evaluate since line still high.
	io.HandleEOI(0x41)
	if len(router.events) != 2 {
		t.Fatalf("expected reassert after EOI, got %d events", len(router.events))
	}
	if !router.events[1].level {
		t.Fatalf("expected level-triggered reassert, got %+v", router.events[1])
	}

	io.SetIRQ(1, false)
}

func TestIOAPICMaskUnmaskEdge(t *testing.T) {
	io := NewIOAPIC(4)
	router := &captureRouter{}
	io.SetRouting(router)

	// Start masked.
	programRedirection(t, io, 2, 0x55, 2, false, true)
	io.SetIRQ(2, true) // masked, should not trigger
	if len(router.events) != 0 {
		t.Fatalf("expected no interrupts while masked, got %d", len(router.events))
	}

	// Unmask should treat existing high level as an edge.
	programRedirection(t, io, 2, 0x55, 2, false, false)
	if len(router.events) != 1 {
		t.Fatalf("expected 1 interrupt on unmask edge, got %d", len(router.events))
	}
}

func TestIOAPICRedirectionProgramming(t *testing.T) {
	io := NewIOAPIC(4)
	router := &captureRouter{}
	io.SetRouting(router)

	programRedirection(t, io, 3, 0x70, 0x7, true, false)
	low := readRedirectionLow(t, io, 3)
	if gotVec := uint8(low & 0xFF); gotVec != 0x70 {
		t.Fatalf("vector mismatch: got 0x%x", gotVec)
	}
	if (low>>15)&1 != 1 {
		t.Fatalf("trigger mode not set to level: low=0x%x", low)
	}
	if (low>>16)&1 != 0 {
		t.Fatalf("mask bit set unexpectedly: low=0x%x", low)
	}
}

func TestIOAPICEOIClearsRemoteIRR(t *testing.T) {
	io := NewIOAPIC(4)
	router := &captureRouter{}
	io.SetRouting(router)

	programRedirection(t, io, 0, 0x44, 1, true, false)

	io.SetIRQ(0, true)
	if len(router.events) != 1 {
		t.Fatalf("expected 1 interrupt, got %d", len(router.events))
	}
	low := readRedirectionLow(t, io, 0)
	if (low>>14)&1 == 0 {
		t.Fatalf("expected remote-IRR set after level assert, low=0x%x", low)
	}

	io.HandleEOI(0x44)
	low = readRedirectionLow(t, io, 0)
	if (low>>14)&1 != 0 {
		t.Fatalf("expected remote-IRR cleared after EOI, low=0x%x", low)
	}
}
