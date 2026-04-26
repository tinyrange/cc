package main

import (
	"os"
	"testing"

	"j5.nz/cc/client"
)

func TestStreamHostStdinReadsPipedInput(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	defer r.Close()

	if _, err := w.Write([]byte("hello\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	ch := make(chan client.ExecInput, 4)
	err = streamHostStdin(r, ch)
	if err != nil {
		t.Fatalf("streamHostStdin() error = %v", err)
	}
	got := <-ch
	if got.Kind != "stdin" || string(got.Data) != "hello\n" {
		t.Fatalf("first input = %#v, want stdin hello", got)
	}
	closed := <-ch
	if closed.Kind != "stdin_close" {
		t.Fatalf("close input = %#v, want stdin_close", closed)
	}
}

func TestSignalName(t *testing.T) {
	tests := []struct {
		sig  os.Signal
		want string
		ok   bool
	}{
		{sig: os.Interrupt, want: "INT", ok: true},
		{sig: unsupportedSignalForTest(), want: "", ok: false},
	}
	for _, tt := range tests {
		got, ok := signalName(tt.sig)
		if ok != tt.ok || got != tt.want {
			t.Fatalf("signalName(%v) = %q, %v; want %q, %v", tt.sig, got, ok, tt.want, tt.ok)
		}
	}
}
