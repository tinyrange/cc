package timeslice

import (
	"bytes"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

var (
	timesliceA = RegisterKind("a", 0)
	timesliceB = RegisterKind("b", 0)
)

func TestTimeslice(t *testing.T) {
	var buf bytes.Buffer
	func() {
		writer, err := StartRecording(&buf)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		defer writer.Close()

		Record(timesliceA, 100*time.Millisecond)
		Record(timesliceB, 200*time.Millisecond)
	}()

	r := bytes.NewReader(buf.Bytes())

	var seen []string
	if err := ReadAllRecords(r, func(id string, flags SliceFlags, duration time.Duration) error {
		seen = append(seen, id)
		return nil
	}); err != nil {
		t.Fatalf("ReadAllRecords: %v", err)
	}
	if len(seen) != 2 {
		t.Fatalf("expected 2 records, got %d", len(seen))
	}
}

func BenchmarkTimeslice(b *testing.B) {
	var buf bytes.Buffer
	var count uint64
	func() {
		writer, err := StartRecording(&buf)
		if err != nil {
			b.Fatalf("Open: %v", err)
		}
		defer writer.Close()

		b.ResetTimer()

		for b.Loop() {
			Record(timesliceA, 100*time.Millisecond)
			Record(timesliceB, 200*time.Millisecond)
			atomic.AddUint64(&count, 2)
		}
	}()

	b.ReportMetric(float64(count), "records")
	b.StopTimer()

	r := bytes.NewReader(buf.Bytes())

	var seen uint64
	if err := ReadAllRecords(r, func(id string, flags SliceFlags, duration time.Duration) error {
		atomic.AddUint64(&seen, 1)
		return nil
	}); err != nil {
		b.Fatalf("ReadAllRecords: %v", err)
	}
	if seen != count {
		b.Fatalf("expected %d records, got %d", count, seen)
	}
}

func BenchmarkTimesliceTempFile(b *testing.B) {
	dir := b.TempDir()
	tmpfile := filepath.Join(dir, "timeslice.log")

	var count uint64

	func() {
		f, err := os.Create(tmpfile)
		if err != nil {
			b.Fatalf("Create: %v", err)
		}
		defer f.Close()

		writer, err := StartRecording(f)
		if err != nil {
			b.Fatalf("Open: %v", err)
		}
		defer writer.Close()

		b.ResetTimer()

		for b.Loop() {
			Record(timesliceA, 100*time.Millisecond)
			Record(timesliceB, 200*time.Millisecond)
			atomic.AddUint64(&count, 2)
		}
	}()

	b.ReportMetric(float64(count), "records")
	b.StopTimer()

	r, err := os.Open(tmpfile)
	if err != nil {
		b.Fatalf("Open: %v", err)
	}
	defer r.Close()

	var seen uint64
	if err := ReadAllRecords(r, func(id string, flags SliceFlags, duration time.Duration) error {
		atomic.AddUint64(&seen, 1)
		return nil
	}); err != nil {
		b.Fatalf("ReadAllRecords: %v", err)
	}
	if seen != count {
		b.Fatalf("expected %d records, got %d", count, seen)
	}
}
