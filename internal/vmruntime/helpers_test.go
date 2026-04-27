package vmruntime

import (
	"errors"
	"testing"

	"j5.nz/cc/client"
)

func TestBootEventWriterCloseIsIdempotent(t *testing.T) {
	w := NewBootEventWriter(func(client.BootEvent) error {
		return nil
	})
	if err := w.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}

func TestBootEventWriterDropsWritesAfterClose(t *testing.T) {
	w := NewBootEventWriter(func(client.BootEvent) error {
		return errors.New("callback should not run after close")
	})
	if err := w.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("Write after Close() panicked: %v", recovered)
		}
	}()
	n, err := w.Write([]byte("late serial data"))
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if n != len("late serial data") {
		t.Fatalf("Write() = %d, want %d", n, len("late serial data"))
	}
}
