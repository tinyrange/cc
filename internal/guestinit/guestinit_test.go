package guestinit

import (
	"context"
	"testing"
)

func TestBuildForArchReturnsLinuxBinary(t *testing.T) {
	for _, goarch := range []string{"arm64", "amd64"} {
		t.Run(goarch, func(t *testing.T) {
			data, err := BuildForArch(context.Background(), t.TempDir(), goarch)
			if err != nil {
				t.Fatalf("BuildForArch() error = %v", err)
			}
			if len(data) < 4 {
				t.Fatalf("BuildForArch() returned %d bytes, want ELF payload", len(data))
			}
			if string(data[:4]) != "\x7fELF" {
				t.Fatalf("BuildForArch() header = %q, want ELF magic", string(data[:4]))
			}
		})
	}
}

func TestBuildReturnsLinuxBinaryForHostArch(t *testing.T) {
	data, err := Build(context.Background(), t.TempDir())
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

func TestBuildForArchRejectsUnsupportedArch(t *testing.T) {
	_, err := BuildForArch(context.Background(), t.TempDir(), "ppc64le")
	if err == nil {
		t.Fatal("BuildForArch() error = nil, want unsupported architecture error")
	}
}
