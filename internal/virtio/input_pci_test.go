package virtio

import (
	"encoding/binary"
	"testing"
)

func TestPCIInputDeliversKeyAndAcknowledgesINTx(t *testing.T) {
	mem := make(testGuestMemory, 64<<10)
	irq := &testIRQ{}
	i := NewKeyboardInput(0, 0, 47)
	i.Attach(mem, irq)
	p := &InputPCI{Input: i}
	write := func(offset uint64, size int, value uint64) {
		t.Helper()
		if err := p.WriteMMIO(offset, size, value); err != nil {
			t.Fatal(err)
		}
	}
	// Discover the modern capabilities and negotiate VERSION_1, as the Windows
	// VirtIO library does. Configure a split queue using mixed-width writes.
	var cfg [256]byte
	p.PCIConfig(cfg[:])
	for a, typ := byte(0x40), byte(1); a != 0; typ++ {
		if cfg[a] != 9 || cfg[a+3] != typ {
			t.Fatalf("invalid capability at %#x", a)
		}
		a = cfg[a+1]
	}
	write(0, 4, 1)
	if v, _ := p.ReadMMIO(4, 4); v != 1 {
		t.Fatalf("VERSION_1 = %#x", v)
	}
	write(8, 4, 1)
	write(12, 4, 1)
	write(22, 2, 0)
	write(24, 2, 2)
	write(32, 4, 0x2000)
	write(36, 4, 0)
	write(40, 8, 0x3000)
	write(48, 8, 0x3800)
	write(28, 2, 1)
	write(20, 1, 15)
	writeDesc(mem, 0x2000, 0x4000, 8, descFWrite, 0)
	writeDesc(mem, 0x2010, 0x4010, 8, descFWrite, 0)
	binary.LittleEndian.PutUint16(mem[0x3002:], 2)
	binary.LittleEndian.PutUint16(mem[0x3006:], 1)
	if err := i.Key(30, true); err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint32(mem[0x4000:]) != 30<<16|1 || binary.LittleEndian.Uint32(mem[0x4004:]) != 1 {
		t.Fatalf("wrong key event: %x", mem[0x4000:0x4008])
	}
	if !irq.level || irq.line != 47 {
		t.Fatalf("IRQ %#v", irq)
	}
	if v, err := p.ReadMMIO(0x2000, 1); err != nil || v != 1 || irq.level {
		t.Fatalf("ISR = %d, %v; IRQ %#v", v, err, irq)
	}
	if v, _ := p.ReadMMIO(0x2000, 1); v != 0 {
		t.Fatalf("ISR was not cleared: %d", v)
	}
	// Reset must retire queues and leave no stale events for a restarted driver.
	write(20, 1, 0)
	if v, _ := p.ReadMMIO(28, 2); v != 0 {
		t.Fatalf("queue ready after reset: %d", v)
	}
}

func TestAbsolutePointerRangeDoesNotCrossSignedHIDBoundary(t *testing.T) {
	i := NewAbsolutePointerInput(0, 0, 0, 2560, 1664)
	for _, extent := range []uint32{2560, 1664} {
		var previous uint32
		for pixel := uint32(0); pixel < extent; pixel++ {
			axis := scaleAbsolutePosition(pixel, extent)
			if axis < previous || int16(axis) < 0 {
				t.Fatalf("axis wraps at pixel %d/%d: %d", pixel, extent, axis)
			}
			previous = axis
		}
		if previous != 0x7fff {
			t.Fatalf("last pixel = %d", previous)
		}
	}
	i.configSelect = inputConfigABSInfo
	for _, axis := range []byte{inputAbsX, inputAbsY} {
		i.configSubsel = axis
		cfg := i.configBytesLocked()
		if binary.LittleEndian.Uint32(cfg[12:]) != 0x7fff {
			t.Fatal("reported range differs from emitted coordinates")
		}
	}
}
