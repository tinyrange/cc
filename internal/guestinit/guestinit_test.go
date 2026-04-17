package guestinit

import (
	"context"
	"testing"
)

func TestBuildReturnsEmbeddedLinuxBinary(t *testing.T) {
	data, err := Build(context.Background(), "")
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if len(data) < 4 {
		t.Fatalf("Build() returned %d bytes, want ELF payload", len(data))
	}
	if string(data[:4]) != "\x7fELF" {
		t.Fatalf("Build() header = %q, want ELF magic", string(data[:4]))
	}
}
