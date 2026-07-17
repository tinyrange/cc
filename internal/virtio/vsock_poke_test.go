package virtio

import "testing"

func TestVsockPokeRaisesVringIRQ(t *testing.T) {
	irq := &recordingIRQ{}
	dev := &Vsock{IRQ: 17, irq: irq}
	if err := dev.Poke(); err != nil {
		t.Fatalf("Poke: %v", err)
	}
	if len(irq.levels) != 1 || !irq.levels[0] {
		t.Fatalf("IRQ levels = %v, want [true]", irq.levels)
	}
	if !dev.irqHigh || dev.interruptStatus&vsockInterruptVring == 0 {
		t.Fatalf("vring IRQ was not raised: high=%t status=%#x", dev.irqHigh, dev.interruptStatus)
	}
}

func TestVsockPokePreservesPendingIRQ(t *testing.T) {
	irq := &recordingIRQ{}
	dev := &Vsock{IRQ: 17, irq: irq, interruptStatus: vsockInterruptVring}
	if err := dev.Poke(); err != nil {
		t.Fatalf("Poke: %v", err)
	}
	if len(irq.levels) != 1 || !irq.levels[0] {
		t.Fatalf("IRQ levels = %v, want [true]", irq.levels)
	}
}

func TestVsockPokeRepulsesAssertedPendingIRQ(t *testing.T) {
	irq := &recordingIRQ{}
	dev := &Vsock{IRQ: 17, irq: irq, interruptStatus: vsockInterruptVring, irqHigh: true}
	if err := dev.Poke(); err != nil {
		t.Fatalf("Poke: %v", err)
	}
	if len(irq.levels) != 2 || irq.levels[0] || !irq.levels[1] {
		t.Fatalf("IRQ levels = %v, want [false true]", irq.levels)
	}
	if !dev.irqHigh || dev.interruptStatus == 0 {
		t.Fatalf("pending IRQ was not preserved: high=%t status=%#x", dev.irqHigh, dev.interruptStatus)
	}
}

type recordingIRQ struct {
	levels []bool
}

func (i *recordingIRQ) SetIRQ(_ uint32, level bool) error {
	i.levels = append(i.levels, level)
	return nil
}
