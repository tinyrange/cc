package vm

import (
	"io"
	"strings"
	"testing"
)

type discardAt struct{}

func (d discardAt) WriteAt(p []byte, off int64) (n int, err error) {
	return len(p), nil
}

var (
	_ io.WriterAt = (*discardAt)(nil)
)

type countingRegion struct {
	data  RawRegion
	reads int
}

func (r *countingRegion) ReadAt(p []byte, off int64) (int, error) {
	r.reads++
	return r.data.ReadAt(p, off)
}

func (r *countingRegion) WriteAt([]byte, int64) (int, error) {
	return 0, io.ErrClosedPipe
}

func (r *countingRegion) Size() int64 {
	return r.data.Size()
}

func TestVirtualMemoryWritesOverlayMappedRegion(t *testing.T) {
	source := &countingRegion{data: RawRegion([]byte("abcdefghijklmnop"))}
	vm := NewVirtualMemory(16, 8)
	if err := vm.Map(source, 0); err != nil {
		t.Fatalf("map source: %v", err)
	}

	if _, err := vm.WriteAt([]byte("XY"), 2); err != nil {
		t.Fatalf("write overlay: %v", err)
	}

	buf := make([]byte, 16)
	if _, err := vm.ReadAt(buf, 0); err != nil {
		t.Fatalf("read merged data: %v", err)
	}
	if got, want := string(buf), "abXYefghijklmnop"; got != want {
		t.Fatalf("merged data = %q, want %q", got, want)
	}

	before := source.reads
	if _, err := vm.WriteAt([]byte("Z"), 3); err != nil {
		t.Fatalf("rewrite overlay: %v", err)
	}
	if source.reads != before {
		t.Fatalf("rewrite read source %d times, want %d", source.reads-before, 0)
	}

	out := &recordingWriterAt{}
	if _, err := vm.WriteSparseTo(out); err != nil {
		t.Fatalf("write sparse: %v", err)
	}
	if got, want := out.String(), "abXZefghijklmnop"; got != want {
		t.Fatalf("sparse output = %q, want %q", got, want)
	}
}

type recordingWriterAt struct {
	buf []byte
}

func (w *recordingWriterAt) WriteAt(p []byte, off int64) (int, error) {
	end := int(off) + len(p)
	if end > len(w.buf) {
		grown := make([]byte, end)
		copy(grown, w.buf)
		w.buf = grown
	}
	return copy(w.buf[off:], p), nil
}

func (w *recordingWriterAt) String() string {
	return strings.TrimRight(string(w.buf), "\x00")
}

func BenchmarkNewVM1GB(b *testing.B) {
	for b.Loop() {
		vm := NewVirtualMemory(1*1024*1024*1024, 4096)
		_ = vm
	}
}

func BenchmarkNewVM128GB(b *testing.B) {
	for b.Loop() {
		vm := NewVirtualMemory(128*1024*1024*1024, 4096)
		_ = vm
	}
}

func BenchmarkVMWriteSparseTo(b *testing.B) {
	for b.Loop() {
		vm := NewVirtualMemory(1*1024*1024*1024, 4096)
		if _, err := vm.WriteSparseTo(discardAt{}); err != nil {
			_ = err
		}
	}
}
