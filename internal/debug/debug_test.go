package debug

import (
	"bytes"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

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

	reader, err := NewReader(&r, bytes.NewReader(r))
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

	reader, err := NewReader(&r, bytes.NewReader(r))
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
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 10 {
				time.Sleep(time.Millisecond * time.Duration(i))
				Write("test", fmt.Sprintf("hello, world %d", i))
			}
		}()
	}
	wg.Wait()

	r, err := buf.Compile()
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	reader, err := NewReader(&r, bytes.NewReader(r))
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
		reader, err := NewReader(&r, nil)
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
