package virtio

import "testing"

func TestVsockPokePulsesIdleIRQ(t *testing.T) {
	irq := &recordingIRQ{}
	dev := &Vsock{IRQ: 17, irq: irq}
	if err := dev.Poke(); err != nil {
		t.Fatalf("Poke: %v", err)
	}
	if len(irq.levels) != 2 || !irq.levels[0] || irq.levels[1] {
		t.Fatalf("IRQ levels = %v, want [true false]", irq.levels)
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

type recordingIRQ struct {
	levels []bool
}

func (i *recordingIRQ) SetIRQ(_ uint32, level bool) error {
	i.levels = append(i.levels, level)
	return nil
}
