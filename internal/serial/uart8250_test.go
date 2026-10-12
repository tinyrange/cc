package serial

import (
	"bytes"
	"testing"
)

type irqRecorder struct{ level bool }

func (r *irqRecorder) SetIRQ(_ uint32, level bool) error { r.level = level; return nil }

// The 16550-family interrupt control table specifies IIR-read acknowledgement
// for THRE, without clearing the transmitter-empty bit in LSR.
func TestTHREInterruptAcknowledgementAndReceivePriority(t *testing.T) {
	var out bytes.Buffer
	uart := NewUART8250(0x3f8, 0, &out)
	irq := &irqRecorder{}
	uart.AttachIRQ(irq, 4)
	write := func(register uint64, value uint64) {
		t.Helper()
		if err := uart.WriteValue(0x3f8+register, 1, value); err != nil {
			t.Fatal(err)
		}
	}
	read := func(register uint64) uint64 {
		t.Helper()
		value, err := uart.ReadValue(0x3f8+register, 1)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	write(1, 2)
	if !irq.level || read(2) != 2 {
		t.Fatal("missing initial THRE interrupt")
	}
	if irq.level || read(2) != 1 {
		t.Fatal("THRE interrupt not acknowledged by IIR read")
	}
	if read(5)&0x60 != 0x60 {
		t.Fatal("acknowledgement cleared transmitter empty status")
	}
	write(0, 'X')
	if !irq.level || read(2) != 2 || irq.level {
		t.Fatal("transmission did not rearm one THRE interrupt")
	}
	write(1, 0)
	write(1, 3)
	uart.InjectRXByte('R')
	if read(2) != 4 {
		t.Fatal("receive interrupt must outrank THRE")
	}
	if read(0) != 'R' || read(2) != 2 || read(2) != 1 {
		t.Fatal("receive acknowledgement lost pending THRE")
	}
	if out.String() != "X" {
		t.Fatalf("transmit: %q", out.String())
	}
}
