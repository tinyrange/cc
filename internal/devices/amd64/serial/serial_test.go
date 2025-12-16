package serial

import (
	"bytes"
	"context"
	"sync"
	"testing"
)

// testIRQLine captures interrupt line state changes
type testIRQLine struct {
	mu     sync.Mutex
	level  bool
	events []bool
}

func (t *testIRQLine) SetLevel(level bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.level = level
	t.events = append(t.events, level)
}

func (t *testIRQLine) PulseInterrupt() {
	t.SetLevel(true)
	t.SetLevel(false)
}

func (t *testIRQLine) getLevel() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.level
}

func (t *testIRQLine) getEvents() []bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	result := make([]bool, len(t.events))
	copy(result, t.events)
	return result
}

func (t *testIRQLine) reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.level = false
	t.events = t.events[:0]
}

// testReader provides controllable input for testing
type testReader struct {
	mu    sync.Mutex
	data  []byte
	index int
}

func (t *testReader) Read(buf []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.index >= len(t.data) {
		return 0, nil
	}
	n := copy(buf, t.data[t.index:])
	t.index += n
	return n, nil
}

func (t *testReader) addData(data []byte) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.data = append(t.data, data...)
}

func (t *testReader) reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.data = t.data[:0]
	t.index = 0
}

// testWriter captures output for testing
type testWriter struct {
	mu   sync.Mutex
	data []byte
}

func (t *testWriter) Write(buf []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.data = append(t.data, buf...)
	return len(buf), nil
}

func (t *testWriter) getData() []byte {
	t.mu.Lock()
	defer t.mu.Unlock()
	result := make([]byte, len(t.data))
	copy(result, t.data)
	return result
}

func (t *testWriter) reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.data = t.data[:0]
}

// TestSerialTHRRHRFIFO tests transmit/receive FIFO functionality
func TestSerialTHRRHRFIFO(t *testing.T) {
	irqLine := &testIRQLine{}
	writer := &testWriter{}
	reader := &testReader{}
	serial := NewSerial16550(0x3F8, irqLine, writer, reader)

	// Enable FIFO mode
	// FCR: enable FIFO (bit 0), clear RX FIFO (bit 1), clear TX FIFO (bit 2)
	if err := serial.WriteIOPort(0x3F8+2, []byte{0x07}); err != nil {
		t.Fatalf("write FCR: %v", err)
	}

	// Enable RX interrupt (IER bit 0)
	if err := serial.WriteIOPort(0x3F8+1, []byte{0x01}); err != nil {
		t.Fatalf("write IER: %v", err)
	}

	// Test RX FIFO: add data to reader
	testData := []byte{'A', 'B', 'C', 'D', 'E'}
	reader.addData(testData)

	// Poll multiple times to read all data into FIFO (Poll reads one byte at a time)
	ctx := context.Background()
	for i := 0; i < len(testData); i++ {
		if err := serial.Poll(ctx); err != nil {
			t.Fatalf("poll[%d]: %v", i, err)
		}
	}

	// Read data from RHR
	readBuf := make([]byte, len(testData))
	for i := range readBuf {
		buf := []byte{0}
		if err := serial.ReadIOPort(0x3F8, buf); err != nil {
			t.Fatalf("read RHR[%d]: %v", i, err)
		}
		readBuf[i] = buf[0]
	}

	// Verify data matches
	if !bytes.Equal(readBuf, testData) {
		t.Fatalf("RX data mismatch: got %v, want %v", readBuf, testData)
	}

	// Test TX FIFO: write data to THR
	txData := []byte{'X', 'Y', 'Z'}
	for _, b := range txData {
		if err := serial.WriteIOPort(0x3F8, []byte{b}); err != nil {
			t.Fatalf("write THR: %v", err)
		}
	}

	// Poll to process TX FIFO
	if err := serial.Poll(ctx); err != nil {
		t.Fatalf("poll TX: %v", err)
	}

	// Verify data was written
	written := writer.getData()
	if !bytes.Equal(written, txData) {
		t.Fatalf("TX data mismatch: got %v, want %v", written, txData)
	}
}

// TestSerialFIFOTriggerLevel tests FIFO trigger level behavior
func TestSerialFIFOTriggerLevel(t *testing.T) {
	irqLine := &testIRQLine{}
	reader := &testReader{}
	serial := NewSerial16550(0x3F8, irqLine, nil, reader)

	// Enable FIFO with trigger level 4 (FCR bits 6-7 = 0x40)
	if err := serial.WriteIOPort(0x3F8+2, []byte{0x41}); err != nil {
		t.Fatalf("write FCR: %v", err)
	}

	// Enable OUT2 for interrupts
	if err := serial.WriteIOPort(0x3F8+4, []byte{0x08}); err != nil {
		t.Fatalf("write MCR: %v", err)
	}

	// Enable RX interrupt
	if err := serial.WriteIOPort(0x3F8+1, []byte{0x01}); err != nil {
		t.Fatalf("write IER: %v", err)
	}

	// Add 3 bytes (below trigger)
	reader.addData([]byte{'A', 'B', 'C'})
	irqLine.reset()
	// Poll 3 times to read all 3 bytes
	for i := 0; i < 3; i++ {
		if err := serial.Poll(context.Background()); err != nil {
			t.Fatalf("poll[%d]: %v", i, err)
		}
	}

	// Should not trigger interrupt yet (only 3 bytes, trigger is 4)
	events := irqLine.getEvents()
	hasHigh := false
	for _, e := range events {
		if e {
			hasHigh = true
			break
		}
	}
	if hasHigh {
		t.Fatalf("unexpected interrupt with 3 bytes, events: %v", events)
	}

	// Add 1 more byte to reach trigger level 4
	reader.addData([]byte{'D'})
	// Poll to read the 4th byte - this should trigger interrupt
	if err := serial.Poll(context.Background()); err != nil {
		t.Fatalf("poll: %v", err)
	}

	// Should trigger interrupt now
	events = irqLine.getEvents()
	hasHigh = false
	for _, e := range events {
		if e {
			hasHigh = true
			break
		}
	}
	if !hasHigh {
		t.Fatalf("expected interrupt at trigger level, events: %v", events)
	}
}

// TestSerialInterruptGeneration tests interrupt generation based on IER
func TestSerialInterruptGeneration(t *testing.T) {
	irqLine := &testIRQLine{}
	writer := &testWriter{}
	reader := &testReader{}
	serial := NewSerial16550(0x3F8, irqLine, writer, reader)

	// Enable OUT2 (MCR bit 3) - required for interrupts
	if err := serial.WriteIOPort(0x3F8+4, []byte{0x08}); err != nil {
		t.Fatalf("write MCR: %v", err)
	}

	// Test RX data available interrupt (IER bit 0)
	irqLine.reset()
	if err := serial.WriteIOPort(0x3F8+1, []byte{0x01}); err != nil {
		t.Fatalf("write IER: %v", err)
	}

	reader.addData([]byte{'X'})
	if err := serial.Poll(context.Background()); err != nil {
		t.Fatalf("poll: %v", err)
	}

	events := irqLine.getEvents()
	if len(events) == 0 || !events[len(events)-1] {
		t.Fatalf("expected RX interrupt, events: %v", events)
	}

	// Read IIR to verify interrupt type
	buf := []byte{0}
	if err := serial.ReadIOPort(0x3F8+2, buf); err != nil {
		t.Fatalf("read IIR: %v", err)
	}
	iir := buf[0]
	// IIR bit 0 = 0 means interrupt pending, bits 1-2 = 00 means RX data available
	if iir&0x07 != 0x04 {
		t.Fatalf("unexpected IIR value: 0x%02x (expected 0x04 for RX data)", iir)
	}

	// Test TX holding register empty interrupt (IER bit 1)
	irqLine.reset()
	if err := serial.WriteIOPort(0x3F8+1, []byte{0x02}); err != nil {
		t.Fatalf("write IER: %v", err)
	}

	// Clear RX data by reading
	if err := serial.ReadIOPort(0x3F8, buf); err != nil {
		t.Fatalf("read RHR: %v", err)
	}

	// THRE should be set, triggering interrupt
	events = irqLine.getEvents()
	if len(events) == 0 || !events[len(events)-1] {
		t.Fatalf("expected THRE interrupt, events: %v", events)
	}

	// Read IIR to verify interrupt type
	if err := serial.ReadIOPort(0x3F8+2, buf); err != nil {
		t.Fatalf("read IIR: %v", err)
	}
	iir = buf[0]
	// IIR bits 1-2 = 01 means THRE
	if iir&0x07 != 0x02 {
		t.Fatalf("unexpected IIR value: 0x%02x (expected 0x02 for THRE)", iir)
	}
}

// TestSerialModemControlLoopback tests loopback mode
func TestSerialModemControlLoopback(t *testing.T) {
	irqLine := &testIRQLine{}
	writer := &testWriter{}
	serial := NewSerial16550(0x3F8, irqLine, writer, nil)

	// Enable OUT2 for interrupts
	if err := serial.WriteIOPort(0x3F8+4, []byte{0x08}); err != nil {
		t.Fatalf("write MCR: %v", err)
	}

	// Enable FIFO mode for proper loopback behavior
	if err := serial.WriteIOPort(0x3F8+2, []byte{0x01}); err != nil {
		t.Fatalf("write FCR: %v", err)
	}

	// Enable loopback mode (MCR bit 4) and OUT2
	if err := serial.WriteIOPort(0x3F8+4, []byte{0x18}); err != nil {
		t.Fatalf("write MCR loopback: %v", err)
	}

	// Enable RX interrupt
	if err := serial.WriteIOPort(0x3F8+1, []byte{0x01}); err != nil {
		t.Fatalf("write IER: %v", err)
	}

	// Write to THR - should loop back to RX
	txData := []byte{'L', 'O', 'O', 'P'}
	for _, b := range txData {
		if err := serial.WriteIOPort(0x3F8, []byte{b}); err != nil {
			t.Fatalf("write THR: %v", err)
		}
	}

	// Poll multiple times to process all TX bytes (each poll processes one TX byte from FIFO)
	for i := 0; i < len(txData); i++ {
		if err := serial.Poll(context.Background()); err != nil {
			t.Fatalf("poll[%d]: %v", i, err)
		}
	}

	// Verify data looped back (not written to output)
	written := writer.getData()
	if len(written) != 0 {
		t.Fatalf("unexpected output in loopback mode: %v", written)
	}

	// Read looped back data
	readBuf := make([]byte, len(txData))
	for i := range readBuf {
		buf := []byte{0}
		if err := serial.ReadIOPort(0x3F8, buf); err != nil {
			t.Fatalf("read RHR: %v", err)
		}
		readBuf[i] = buf[0]
	}

	if !bytes.Equal(readBuf, txData) {
		t.Fatalf("loopback data mismatch: got %v, want %v", readBuf, txData)
	}

	// Test MSR reflects MCR in loopback mode
	// Set DTR (MCR bit 0) and RTS (MCR bit 1)
	if err := serial.WriteIOPort(0x3F8+4, []byte{0x1B}); err != nil {
		t.Fatalf("write MCR with DTR/RTS: %v", err)
	}

	// Read MSR
	buf := []byte{0}
	if err := serial.ReadIOPort(0x3F8+6, buf); err != nil {
		t.Fatalf("read MSR: %v", err)
	}
	msr := buf[0]

	// In loopback: DTR -> DSR (bit 5), RTS -> CTS (bit 4)
	if msr&0x30 != 0x30 {
		t.Fatalf("MSR mismatch in loopback: got 0x%02x, expected DSR and CTS set", msr)
	}
}

// TestSerialOUT2InterruptGate tests OUT2 interrupt gating
func TestSerialOUT2InterruptGate(t *testing.T) {
	irqLine := &testIRQLine{}
	reader := &testReader{}
	serial := NewSerial16550(0x3F8, irqLine, nil, reader)

	// Enable RX interrupt
	if err := serial.WriteIOPort(0x3F8+1, []byte{0x01}); err != nil {
		t.Fatalf("write IER: %v", err)
	}

	// OUT2 disabled (MCR bit 3 = 0) - interrupts should be gated
	// First ensure MCR is 0 (OUT2 disabled)
	if err := serial.WriteIOPort(0x3F8+4, []byte{0x00}); err != nil {
		t.Fatalf("write MCR disable OUT2: %v", err)
	}
	irqLine.reset()
	reader.addData([]byte{'X'})
	if err := serial.Poll(context.Background()); err != nil {
		t.Fatalf("poll: %v", err)
	}

	events := irqLine.getEvents()
	// Note: initial state might set level to false, filter that out
	hasHigh := false
	for _, e := range events {
		if e {
			hasHigh = true
			break
		}
	}
	if hasHigh {
		t.Fatalf("unexpected interrupt with OUT2 disabled, events: %v", events)
	}

	// Enable OUT2
	if err := serial.WriteIOPort(0x3F8+4, []byte{0x08}); err != nil {
		t.Fatalf("write MCR with OUT2: %v", err)
	}

	// Interrupt should now be asserted
	events = irqLine.getEvents()
	if len(events) == 0 || !events[len(events)-1] {
		t.Fatalf("expected interrupt with OUT2 enabled, events: %v", events)
	}
}

// TestSerialLSRStatus tests LSR status bits
func TestSerialLSRStatus(t *testing.T) {
	irqLine := &testIRQLine{}
	reader := &testReader{}
	serial := NewSerial16550(0x3F8, irqLine, nil, reader)

	// Initially THRE and TEMT should be set
	buf := []byte{0}
	if err := serial.ReadIOPort(0x3F8+5, buf); err != nil {
		t.Fatalf("read LSR: %v", err)
	}
	lsr := buf[0]
	if lsr&0x60 != 0x60 {
		t.Fatalf("expected THRE and TEMT set initially, got 0x%02x", lsr)
	}

	// Enable FIFO mode to test FIFO behavior
	if err := serial.WriteIOPort(0x3F8+2, []byte{0x01}); err != nil {
		t.Fatalf("write FCR: %v", err)
	}

	// Write data - THRE should clear when FIFO is full
	// Fill FIFO (size 16)
	for i := 0; i < 16; i++ {
		if err := serial.WriteIOPort(0x3F8, []byte{'X'}); err != nil {
			t.Fatalf("write THR[%d]: %v", i, err)
		}
	}

	if err := serial.ReadIOPort(0x3F8+5, buf); err != nil {
		t.Fatalf("read LSR: %v", err)
	}
	lsr = buf[0]
	// THRE should be clear when FIFO is full
	if lsr&0x20 != 0 {
		t.Fatalf("expected THRE cleared when FIFO full, got 0x%02x", lsr)
	}

	// Add RX data
	reader.addData([]byte{'Y'})
	if err := serial.Poll(context.Background()); err != nil {
		t.Fatalf("poll: %v", err)
	}

	// Data ready bit should be set
	if err := serial.ReadIOPort(0x3F8+5, buf); err != nil {
		t.Fatalf("read LSR: %v", err)
	}
	lsr = buf[0]
	if lsr&0x01 == 0 {
		t.Fatalf("expected data ready bit set, got 0x%02x", lsr)
	}
}

// TestSerialFIFOClear tests FIFO clear operations
func TestSerialFIFOClear(t *testing.T) {
	irqLine := &testIRQLine{}
	reader := &testReader{}
	serial := NewSerial16550(0x3F8, irqLine, nil, reader)

	// Enable FIFO
	if err := serial.WriteIOPort(0x3F8+2, []byte{0x01}); err != nil {
		t.Fatalf("write FCR: %v", err)
	}

	// Add data to RX FIFO
	reader.addData([]byte{'A', 'B', 'C'})
	if err := serial.Poll(context.Background()); err != nil {
		t.Fatalf("poll: %v", err)
	}

	// Clear RX FIFO (FCR bit 1)
	if err := serial.WriteIOPort(0x3F8+2, []byte{0x03}); err != nil {
		t.Fatalf("write FCR clear RX: %v", err)
	}

	// Try to read - should get nothing
	buf := []byte{0}
	if err := serial.ReadIOPort(0x3F8, buf); err != nil {
		t.Fatalf("read RHR: %v", err)
	}
	if buf[0] != 0 {
		t.Fatalf("expected empty FIFO after clear, got 0x%02x", buf[0])
	}

	// LSR data ready should be clear
	lsrBuf := []byte{0}
	if err := serial.ReadIOPort(0x3F8+5, lsrBuf); err != nil {
		t.Fatalf("read LSR: %v", err)
	}
	if lsrBuf[0]&0x01 != 0 {
		t.Fatalf("expected data ready cleared, got 0x%02x", lsrBuf[0])
	}
}
