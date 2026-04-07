package serial

import (
	"bytes"
	"testing"
)

func TestUART8250TransmitAndStatus(t *testing.T) {
	var out bytes.Buffer
	uart := NewUART8250(0x09000000, 0, &out)

	if err := uart.WriteValue(0x09000000, 1, 'A'); err != nil {
		t.Fatalf("WriteValue() error = %v", err)
	}
	if got := out.String(); got != "A" {
		t.Fatalf("output = %q, want %q", got, "A")
	}

	lsr, err := uart.ReadValue(0x09000005, 1)
	if err != nil {
		t.Fatalf("ReadValue(LSR) error = %v", err)
	}
	const wantLSR = uartLSRTHRE | uartLSRTEMT
	if byte(lsr) != wantLSR {
		t.Fatalf("LSR = %#x, want %#x", byte(lsr), wantLSR)
	}
}

func TestUART8250Loopback(t *testing.T) {
	uart := NewUART8250(0x09000000, 0, nil)

	if err := uart.WriteValue(0x09000004, 1, uartMCRLoop); err != nil {
		t.Fatalf("WriteValue(MCR) error = %v", err)
	}
	if err := uart.WriteValue(0x09000000, 1, 'Z'); err != nil {
		t.Fatalf("WriteValue(THR) error = %v", err)
	}

	lsr, err := uart.ReadValue(0x09000005, 1)
	if err != nil {
		t.Fatalf("ReadValue(LSR) error = %v", err)
	}
	if byte(lsr)&uartLSRDataReady == 0 {
		t.Fatalf("LSR = %#x, want data ready bit set", byte(lsr))
	}

	value, err := uart.ReadValue(0x09000000, 1)
	if err != nil {
		t.Fatalf("ReadValue(RBR) error = %v", err)
	}
	if byte(value) != 'Z' {
		t.Fatalf("RBR = %q, want %q", byte(value), byte('Z'))
	}
}
