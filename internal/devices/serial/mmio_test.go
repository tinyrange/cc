package serial

import (
	"bytes"
	"context"
	"sync"
	"testing"

	"github.com/tinyrange/cc/internal/chipset"
)

// testIRQLineMMIO captures interrupt line state changes for MMIO tests
type testIRQLineMMIO struct {
	mu     sync.Mutex
	level  bool
	events []bool
}

func (t *testIRQLineMMIO) SetLevel(level bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.level = level
	t.events = append(t.events, level)
}

func (t *testIRQLineMMIO) PulseInterrupt() {
	t.SetLevel(true)
	t.SetLevel(false)
}

func (t *testIRQLineMMIO) reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.level = false
	t.events = t.events[:0]
}

// testReaderMMIO provides controllable input for MMIO tests
type testReaderMMIO struct {
	mu    sync.Mutex
	data  []byte
	index int
}

func (t *testReaderMMIO) Read(buf []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.index >= len(t.data) {
		return 0, nil
	}
	n := copy(buf, t.data[t.index:])
	t.index += n
	return n, nil
}

func (t *testReaderMMIO) addData(data []byte) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.data = append(t.data, data...)
}

func (t *testReaderMMIO) reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.data = t.data[:0]
	t.index = 0
}

// testWriterMMIO captures output for MMIO tests
type testWriterMMIO struct {
	mu   sync.Mutex
	data []byte
}

func (t *testWriterMMIO) Write(buf []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.data = append(t.data, buf...)
	return len(buf), nil
}

func (t *testWriterMMIO) getData() []byte {
	t.mu.Lock()
	defer t.mu.Unlock()
	result := make([]byte, len(t.data))
	copy(result, t.data)
	return result
}

func (t *testWriterMMIO) reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.data = t.data[:0]
}

// TestSerialMMIOAccessPatterns tests MMIO wrapper with different strides
func TestSerialMMIOAccessPatterns(t *testing.T) {
	irqLine := &testIRQLineMMIO{}
	writer := &testWriterMMIO{}
	reader := &testReaderMMIO{}

	// Test stride 1 (byte-aligned)
	testMMIOStride(t, 0, irqLine, writer, reader)
	// Test stride 2 (16-bit aligned)
	testMMIOStride(t, 1, irqLine, writer, reader)
	// Test stride 4 (32-bit aligned)
	testMMIOStride(t, 2, irqLine, writer, reader)
}

func testMMIOStride(t *testing.T, regShift uint32, irqLine chipset.LineInterrupt, writer *testWriterMMIO, reader *testReaderMMIO) {
	t.Helper()

	mmioSerial := NewSerial16550MMIO(0x1000, regShift, irqLine, writer, reader)
	stride := uint64(1) << regShift
	if stride == 0 {
		stride = 1
	}

	// Reset serial device and reader/writer for clean test
	if err := mmioSerial.Reset(); err != nil {
		t.Fatalf("reset serial stride=%d: %v", stride, err)
	}
	reader.reset()
	writer.reset()

	// Enable FIFO mode for proper operation
	// FCR: enable FIFO (bit 0)
	fcrAddr := uint64(0x1000) + 2*stride
	if err := mmioSerial.WriteMMIO(fcrAddr, []byte{0x01}); err != nil {
		t.Fatalf("write FCR stride=%d: %v", stride, err)
	}

	// Get PollDevice handler
	pollDevice := mmioSerial.SupportsPollDevice()
	if pollDevice == nil {
		t.Fatalf("stride=%d: expected PollDevice support", stride)
	}

	// Test write to THR (register 0) via MMIO
	// Write bytes one at a time since THR is a single-byte register
	thrAddr := uint64(0x1000)
	txData := []byte{'M', 'M', 'I', 'O'}
	for _, b := range txData {
		if err := mmioSerial.WriteMMIO(thrAddr, []byte{b}); err != nil {
			t.Fatalf("write MMIO THR stride=%d: %v", stride, err)
		}
		// Poll after each write to process TX FIFO
		if err := pollDevice.Handler.Poll(context.Background()); err != nil {
			t.Fatalf("poll stride=%d: %v", stride, err)
		}
	}

	// Verify data written
	written := writer.getData()
	if len(written) != len(txData) {
		t.Fatalf("stride=%d: expected %d bytes written, got %d", stride, len(txData), len(written))
	}
	if !bytes.Equal(written, txData) {
		t.Fatalf("stride=%d: TX data mismatch: got %v, want %v", stride, written, txData)
	}
	writer.reset()

	// Test read from RHR via MMIO
	rxData := []byte{'R', 'X'}
	reader.addData(rxData)
	// Poll multiple times to read all data (Poll reads one byte at a time)
	for i := 0; i < len(rxData); i++ {
		if err := pollDevice.Handler.Poll(context.Background()); err != nil {
			t.Fatalf("poll RX stride=%d [%d]: %v", stride, i, err)
		}
	}

	rhrAddr := uint64(0x1000)
	readBuf := make([]byte, len(rxData))
	for i := range readBuf {
		buf := []byte{0}
		if err := mmioSerial.ReadMMIO(rhrAddr, buf); err != nil {
			t.Fatalf("read MMIO RHR stride=%d [%d]: %v", stride, i, err)
		}
		readBuf[i] = buf[0]
	}

	if !bytes.Equal(readBuf, rxData) {
		t.Fatalf("stride=%d: RX data mismatch: got %v, want %v", stride, readBuf, rxData)
	}

	// Test register access with stride
	// Write to IER (register 1)
	ierAddr := uint64(0x1000) + stride
	if err := mmioSerial.WriteMMIO(ierAddr, []byte{0x03}); err != nil {
		t.Fatalf("write MMIO IER stride=%d: %v", stride, err)
	}

	// Read back IER
	ierBuf := []byte{0}
	if err := mmioSerial.ReadMMIO(ierAddr, ierBuf); err != nil {
		t.Fatalf("read MMIO IER stride=%d: %v", stride, err)
	}
	if ierBuf[0] != 0x03 {
		t.Fatalf("stride=%d: IER mismatch: got 0x%02x, want 0x03", stride, ierBuf[0])
	}

	// Test unaligned access (should return 0 or be ignored)
	if stride > 1 {
		unalignedAddr := uint64(0x1000) + 1
		unalignedBuf := []byte{0xFF}
		if err := mmioSerial.WriteMMIO(unalignedAddr, unalignedBuf); err != nil {
			t.Fatalf("write unaligned stride=%d: %v", stride, err)
		}

		// Verify unaligned read returns 0
		readUnaligned := []byte{0xFF}
		if err := mmioSerial.ReadMMIO(unalignedAddr, readUnaligned); err != nil {
			t.Fatalf("read unaligned stride=%d: %v", stride, err)
		}
		if readUnaligned[0] != 0 {
			t.Fatalf("stride=%d: unaligned read should return 0, got 0x%02x", stride, readUnaligned[0])
		}
	}

	// Test out-of-bounds access
	outOfBoundsAddr := uint64(0x1000) + 0x2000
	outBuf := []byte{0xFF}
	if err := mmioSerial.WriteMMIO(outOfBoundsAddr, outBuf); err == nil {
		t.Fatalf("stride=%d: expected error for out-of-bounds write", stride)
	}

	readOutBuf := []byte{0xFF}
	if err := mmioSerial.ReadMMIO(outOfBoundsAddr, readOutBuf); err == nil {
		t.Fatalf("stride=%d: expected error for out-of-bounds read", stride)
	}
}

