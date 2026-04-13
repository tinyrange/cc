package initramfs

import (
	"bytes"
	"testing"
)

func TestBuildIncludesFileAndTrailer(t *testing.T) {
	data, err := Build([]File{{
		Path: "/init",
		Mode: 0o755,
		Data: []byte("hello"),
	}})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if !bytes.Contains(data, []byte("init\x00")) {
		t.Fatalf("archive missing init entry")
	}
	if !bytes.Contains(data, []byte("TRAILER!!!\x00")) {
		t.Fatalf("archive missing trailer entry")
	}
}
