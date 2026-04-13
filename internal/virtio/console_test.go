package virtio

import (
	"bytes"
	"encoding/binary"
	"testing"
)

type testGuestMemory struct {
	data []byte
}

func (m *testGuestMemory) ReadIPA(addr uint64, size int) ([]byte, error) {
	return append([]byte(nil), m.data[addr:addr+uint64(size)]...), nil
}

func (m *testGuestMemory) WriteIPA(addr uint64, data []byte) error {
	copy(m.data[addr:addr+uint64(len(data))], data)
	return nil
}

type testIRQController struct {
	irq   uint32
	level bool
	calls int
}

func (c *testIRQController) SetIRQ(irq uint32, level bool) error {
	c.irq = irq
	c.level = level
	c.calls++
	return nil
}

func TestConsoleDeviceFeaturesAndConfig(t *testing.T) {
	c := NewConsole(0x1000, 0x1000, 40, nil)

	if err := c.Write(0x1000+regDeviceFeatSel, 4, 0); err != nil {
		t.Fatalf("Write(device_feature_sel low) error = %v", err)
	}
	low, err := c.Read(0x1000+regDeviceFeatures, 4)
	if err != nil {
		t.Fatalf("Read(device_features low) error = %v", err)
	}
	if low != featureSize {
		t.Fatalf("device features low = %#x, want %#x", low, featureSize)
	}

	if err := c.Write(0x1000+regDeviceFeatSel, 4, 1); err != nil {
		t.Fatalf("Write(device_feature_sel high) error = %v", err)
	}
	high, err := c.Read(0x1000+regDeviceFeatures, 4)
	if err != nil {
		t.Fatalf("Read(device_features high) error = %v", err)
	}
	if high != 1 {
		t.Fatalf("device features high = %#x, want %#x", high, uint64(1))
	}

	cfg, err := c.Read(0x1000+regConfig, 4)
	if err != nil {
		t.Fatalf("Read(config) error = %v", err)
	}
	if cfg != 0x00190050 {
		t.Fatalf("config word = %#x, want %#x", cfg, uint64(0x00190050))
	}
}

func TestConsoleTransmitQueueWritesOutputAndRaisesIRQ(t *testing.T) {
	mem := &testGuestMemory{data: make([]byte, 0x4000)}
	irq := &testIRQController{}
	var out bytes.Buffer

	c := NewConsole(0x1000, 0x1000, 40, &out)
	c.Attach(mem, irq)

	const (
		descAddr  = 0x2000
		availAddr = 0x2100
		usedAddr  = 0x2200
		dataAddr  = 0x2300
	)
	copy(mem.data[dataAddr:], []byte("hello\n"))
	binary.LittleEndian.PutUint64(mem.data[descAddr:descAddr+8], dataAddr)
	binary.LittleEndian.PutUint32(mem.data[descAddr+8:descAddr+12], 6)
	binary.LittleEndian.PutUint16(mem.data[descAddr+12:descAddr+14], 0)
	binary.LittleEndian.PutUint16(mem.data[availAddr+2:availAddr+4], 1)
	binary.LittleEndian.PutUint16(mem.data[availAddr+4:availAddr+6], 0)

	for _, write := range []struct {
		reg   uint64
		value uint64
	}{
		{regQueueSel, queueTx},
		{regQueueNum, 8},
		{regQueueDescLow, descAddr},
		{regQueueAvailLow, availAddr},
		{regQueueUsedLow, usedAddr},
		{regQueueReady, 1},
		{regQueueNotify, queueTx},
	} {
		if err := c.Write(0x1000+write.reg, 4, write.value); err != nil {
			t.Fatalf("Write(reg=%#x) error = %v", write.reg, err)
		}
	}

	if got := out.String(); got != "hello\n" {
		t.Fatalf("console output = %q, want %q", got, "hello\n")
	}
	if irq.calls == 0 || !irq.level || irq.irq != 40 {
		t.Fatalf("irq state = irq=%d level=%v calls=%d, want irq=40 asserted", irq.irq, irq.level, irq.calls)
	}
	usedIdx := binary.LittleEndian.Uint16(mem.data[usedAddr+2 : usedAddr+4])
	if usedIdx != 1 {
		t.Fatalf("used idx = %d, want 1", usedIdx)
	}
}
