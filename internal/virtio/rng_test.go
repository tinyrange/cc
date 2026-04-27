package virtio

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestRNGDeviceFeatures(t *testing.T) {
	r := NewRNG(0x1000, 0x1000, 44)

	low, err := r.Read(0x1000+regDeviceFeatures, 4)
	if err != nil {
		t.Fatalf("Read(device_features low) error = %v", err)
	}
	if low != 0 {
		t.Fatalf("device features low = %#x, want 0", low)
	}

	if err := r.Write(0x1000+regDeviceFeatSel, 4, 1); err != nil {
		t.Fatalf("Write(device_feature_sel high) error = %v", err)
	}
	high, err := r.Read(0x1000+regDeviceFeatures, 4)
	if err != nil {
		t.Fatalf("Read(device_features high) error = %v", err)
	}
	if high != 1 {
		t.Fatalf("device features high = %#x, want %#x", high, uint64(1))
	}
}

func TestRNGQueueFillsWritableDescriptorAndRaisesIRQ(t *testing.T) {
	mem := &testGuestMemory{data: make([]byte, 0x4000)}
	irq := &testIRQController{}

	r := NewRNG(0x1000, 0x1000, 44)
	r.Reader = bytes.NewReader([]byte("abcdefghijklmnopqrstuvwxyz"))
	r.Attach(mem, irq)

	const (
		descAddr  = 0x2000
		availAddr = 0x2100
		usedAddr  = 0x2200
		dataAddr  = 0x2300
	)
	binary.LittleEndian.PutUint64(mem.data[descAddr:descAddr+8], dataAddr)
	binary.LittleEndian.PutUint32(mem.data[descAddr+8:descAddr+12], 8)
	binary.LittleEndian.PutUint16(mem.data[descAddr+12:descAddr+14], descFWrite)
	binary.LittleEndian.PutUint16(mem.data[availAddr+2:availAddr+4], 1)
	binary.LittleEndian.PutUint16(mem.data[availAddr+4:availAddr+6], 0)

	for _, write := range []struct {
		reg   uint64
		value uint64
	}{
		{regQueueSel, rngQueue},
		{regQueueNum, 8},
		{regQueueDescLow, descAddr},
		{regQueueAvailLow, availAddr},
		{regQueueUsedLow, usedAddr},
		{regQueueReady, 1},
		{regQueueNotify, rngQueue},
	} {
		if err := r.Write(0x1000+write.reg, 4, write.value); err != nil {
			t.Fatalf("Write(reg=%#x) error = %v", write.reg, err)
		}
	}

	if got := string(mem.data[dataAddr : dataAddr+8]); got != "abcdefgh" {
		t.Fatalf("random bytes = %q, want %q", got, "abcdefgh")
	}
	if irq.calls == 0 || !irq.level || irq.irq != 44 {
		t.Fatalf("irq state = irq=%d level=%v calls=%d, want irq=44 asserted", irq.irq, irq.level, irq.calls)
	}
	if usedIdx := binary.LittleEndian.Uint16(mem.data[usedAddr+2 : usedAddr+4]); usedIdx != 1 {
		t.Fatalf("used idx = %d, want 1", usedIdx)
	}
	if usedLen := binary.LittleEndian.Uint32(mem.data[usedAddr+8 : usedAddr+12]); usedLen != 8 {
		t.Fatalf("used len = %d, want 8", usedLen)
	}
}
