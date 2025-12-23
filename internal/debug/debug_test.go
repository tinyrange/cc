package debug

import (
	"fmt"
	"io"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type write struct {
	off  int64
	data []byte
}

type logStructuredBuffer struct {
	data    sync.Map
	maxSize atomic.Int64
}

func (b *logStructuredBuffer) WriteAt(p []byte, off int64) (n int, err error) {
	b.data.Store(off, write{
		off:  off,
		data: append([]byte{}, p...),
	})
	val := b.maxSize.Load()
	if val < int64(len(p))+off {
		for {
			if b.maxSize.CompareAndSwap(val, int64(len(p))+off) {
				break
			}
			val = b.maxSize.Load()
		}
	}
	return len(p), nil
}

func (b *logStructuredBuffer) Close() error {
	return nil
}

type compiledBuffer []byte

func (b *compiledBuffer) ReadAt(p []byte, off int64) (n int, err error) {
	if off < 0 || off >= int64(len(*b)) {
		return 0, io.EOF
	}
	return copy(p, (*b)[off:]), nil
}

func (b *logStructuredBuffer) Compile() (compiledBuffer, error) {
	data := make([]byte, b.maxSize.Load())
	b.data.Range(func(key, value any) bool {
		off := key.(int64)
		write := value.(write)
		copy(data[off:off+int64(len(write.data))], write.data)
		return true
	})

	return compiledBuffer(data), nil
}

func TestDebug(t *testing.T) {
	buf := new(logStructuredBuffer)
	func() {
		Open(buf)
		defer Close()

		Write("test", "hello, world")
	}()

	r, err := buf.Compile()
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	reader, err := NewReader(&r)
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}

	var seen []string

	if err := reader.Each(func(ts time.Time, kind DebugKind, source string, data []byte) error {
		seen = append(seen, source)
		return nil
	}); err != nil {
		t.Fatalf("Each: %v", err)
	}

	if len(seen) != 1 {
		t.Fatalf("expected 1 source, got %d", len(seen))
	}
	if seen[0] != "test" {
		t.Fatalf("expected source to be 'test', got %s", seen[0])
	}
}

func TestDebugTempFile(t *testing.T) {
	dir := t.TempDir()
	func() {
		OpenFile(filepath.Join(dir, "test.log"))
		defer Close()

		Write("test", "hello, world")
	}()

	r, closer, err := NewReaderFromFile(filepath.Join(dir, "test.log"))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	defer closer.Close()

	var seen []string

	if err := r.Each(func(ts time.Time, kind DebugKind, source string, data []byte) error {
		seen = append(seen, source)
		return nil
	}); err != nil {
		t.Fatalf("Each: %v", err)
	}

	if len(seen) != 1 {
		t.Fatalf("expected 1 source, got %d", len(seen))
	}
	if seen[0] != "test" {
		t.Fatalf("expected source to be 'test', got %s", seen[0])
	}
}

func TestDebugMessageOrdering(t *testing.T) {
	buf := new(logStructuredBuffer)
	Open(buf)
	defer Close()

	for i := 0; i < 10; i++ {
		Write("test", fmt.Sprintf("hello, world %d", i))
	}

	r, err := buf.Compile()
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	reader, err := NewReader(&r)
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}

	var seen []string

	if err := reader.Each(func(ts time.Time, kind DebugKind, source string, data []byte) error {
		seen = append(seen, source)
		return nil
	}); err != nil {
		t.Fatalf("Each: %v", err)
	}

	if len(seen) != 10 {
		t.Fatalf("expected 10 sources, got %d", len(seen))
	}
	for i := range 10 {
		if seen[i] != "test" {
			t.Fatalf("expected source to be 'test', got %s at index %d", seen[i], i)
		}
	}
}

func TestDebugTimestampOrdering(t *testing.T) {
	buf := new(logStructuredBuffer)
	Open(buf)
	defer Close()

	// create 4 goroutines that write to the buffer
	var wg sync.WaitGroup
	for i := range 4 {
		wg.Go(func() {
			for range 10 {
				time.Sleep(time.Millisecond * time.Duration(i))
				Write("test", fmt.Sprintf("hello, world %d", i))
			}
		})
	}
	wg.Wait()

	r, err := buf.Compile()
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	reader, err := NewReader(&r)
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}

	var timestamps []time.Time

	if err := reader.Each(func(ts time.Time, kind DebugKind, source string, data []byte) error {
		timestamps = append(timestamps, ts)
		return nil
	}); err != nil {
		t.Fatalf("Each: %v", err)
	}

	if len(timestamps) != 40 {
		t.Fatalf("expected 40 timestamps, got %d", len(timestamps))
	}
	for i := range len(timestamps) - 1 {
		if timestamps[i].After(timestamps[i+1]) {
			t.Fatalf("expected timestamps to be in order, got %v at index %d and %d: %v", timestamps, i, i+1, timestamps[i].After(timestamps[i+1]))
		}
	}
}

func BenchmarkWriteString(b *testing.B) {
	buf := new(logStructuredBuffer)
	Open(buf)
	defer Close()

	for b.Loop() {
		Write("test", "hello, world")
	}
}

func BenchmarkReadString(b *testing.B) {
	buf := new(logStructuredBuffer)
	func() {
		Open(buf)
		defer Close()

		for range 10 {
			Write("test", "hello, world")
		}
	}()

	for b.Loop() {
		r, err := buf.Compile()
		if err != nil {
			b.Fatalf("Compile: %v", err)
		}
		reader, err := NewReader(&r)
		if err != nil {
			b.Fatalf("NewReader: %v", err)
		}

		if err := reader.Each(func(ts time.Time, kind DebugKind, source string, data []byte) error {
			return nil
		}); err != nil {
			b.Fatalf("Each: %v", err)
		}
	}
}
