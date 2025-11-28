package arm64

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestKernelHeaderEntryPointUsesTextOffset(t *testing.T) {
	hdr := KernelHeader{TextOffset: 0x400000}
	entry, err := hdr.EntryPoint(0)
	if err != nil {
		t.Fatalf("EntryPoint returned error: %v", err)
	}
	if entry != 0x400000 {
		t.Fatalf("EntryPoint = %#x, want %#x", entry, hdr.TextOffset)
	}
}

func TestKernelHeaderEntryPointRequiresAlignment(t *testing.T) {
	hdr := KernelHeader{TextOffset: 0x100000}
	if _, err := hdr.EntryPoint(0x1000); err == nil {
		t.Fatalf("EntryPoint on unaligned base expected error")
	}
}

func TestProbeKernelImageParsesRawImageHeader(t *testing.T) {
	const textOffset = 0x80000
	img := buildTestHeader(t, textOffset)

	probe, err := ProbeKernelImage(bytes.NewReader(img), int64(len(img)))
	if err != nil {
		t.Fatalf("ProbeKernelImage returned error: %v", err)
	}
	if probe.NeedsDecompression {
		t.Fatalf("NeedsDecompression = true for raw Image")
	}
	if probe.CompressedOffset != 0 {
		t.Fatalf("CompressedOffset = %d, want 0", probe.CompressedOffset)
	}
	if probe.Header.TextOffset != textOffset {
		t.Fatalf("TextOffset = %#x, want %#x", probe.Header.TextOffset, textOffset)
	}
	entry, err := probe.Header.EntryPoint(0)
	if err != nil {
		t.Fatalf("EntryPoint returned error: %v", err)
	}
	if entry != textOffset {
		t.Fatalf("EntryPoint = %#x, want %#x", entry, textOffset)
	}
	payload, err := probe.ExtractImage(bytes.NewReader(img), int64(len(img)))
	if err != nil {
		t.Fatalf("ExtractImage returned error: %v", err)
	}
	if !bytes.Equal(payload, img) {
		t.Fatalf("Extracted payload does not match original raw image")
	}
}

func TestProbeKernelImageDetectsGzipCompression(t *testing.T) {
	const textOffset = 0x200000
	raw := buildTestHeader(t, textOffset)

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(raw); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}

	probe, err := ProbeKernelImage(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("ProbeKernelImage returned error: %v", err)
	}
	if !probe.NeedsDecompression {
		t.Fatalf("NeedsDecompression = false for gzip Image")
	}
	if probe.CompressedOffset != 0 {
		t.Fatalf("CompressedOffset = %d, want 0 for gzip-without-stub", probe.CompressedOffset)
	}
	if probe.Header.TextOffset != textOffset {
		t.Fatalf("TextOffset = %#x, want %#x", probe.Header.TextOffset, textOffset)
	}
	payload, err := probe.ExtractImage(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("ExtractImage returned error: %v", err)
	}
	if !bytes.Equal(payload[:len(raw)], raw) {
		t.Fatalf("ExtractImage payload prefix mismatch")
	}
}

func TestProbeKernelImageDetectsGzipAfterStub(t *testing.T) {
	const (
		textOffset = 0x100000
		stubSize   = 96
	)
	raw := buildTestHeader(t, textOffset)
	var gzBuf bytes.Buffer
	gz := gzip.NewWriter(&gzBuf)
	if _, err := gz.Write(raw); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}

	image := append(bytes.Repeat([]byte{0xaa}, stubSize), gzBuf.Bytes()...)

	probe, err := ProbeKernelImage(bytes.NewReader(image), int64(len(image)))
	if err != nil {
		t.Fatalf("ProbeKernelImage returned error: %v", err)
	}
	if !probe.NeedsDecompression {
		t.Fatalf("NeedsDecompression = false for stubbed gzip Image")
	}
	if probe.CompressedOffset != stubSize {
		t.Fatalf("CompressedOffset = %d, want %d", probe.CompressedOffset, stubSize)
	}
	if probe.Header.TextOffset != textOffset {
		t.Fatalf("TextOffset = %#x, want %#x", probe.Header.TextOffset, textOffset)
	}
	payload, err := probe.ExtractImage(bytes.NewReader(image), int64(len(image)))
	if err != nil {
		t.Fatalf("ExtractImage returned error: %v", err)
	}
	if !bytes.Equal(payload[:len(raw)], raw) {
		t.Fatalf("Extracted payload prefix mismatch")
	}
}

func TestProbeKernelImageWithLocalKernel(t *testing.T) {
	kernelPath := filepath.Join("local", "vmlinux_arm64")
	f, err := os.Open(kernelPath)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skipf("%s not present: %v", kernelPath, err)
		}
		t.Fatalf("open kernel: %v", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		t.Fatalf("stat kernel: %v", err)
	}

	probe, err := ProbeKernelImage(f, info.Size())
	if err != nil {
		t.Fatalf("ProbeKernelImage(local) returned error: %v", err)
	}
	if !probe.NeedsDecompression {
		t.Fatalf("NeedsDecompression = false, want true for compressed local kernel")
	}
	if probe.CompressedOffset <= 0 {
		t.Fatalf("CompressedOffset = %d, want > 0", probe.CompressedOffset)
	}
	if probe.Header.Magic != arm64ImageMagic {
		t.Fatalf("Magic = %#x, want %#x", probe.Header.Magic, arm64ImageMagic)
	}
	if probe.Header.ImageSize == 0 {
		t.Fatalf("ImageSize = 0, want non-zero")
	}
	if _, err := probe.Header.EntryPoint(0); err != nil {
		t.Fatalf("EntryPoint for local kernel: %v", err)
	}

	payload, err := probe.ExtractImage(f, info.Size())
	if err != nil {
		t.Fatalf("ExtractImage(local) returned error: %v", err)
	}
	if probe.Header.ImageSize > 0 && uint64(len(payload)) < probe.Header.ImageSize {
		t.Fatalf("payload len = %d, want >= %d", len(payload), probe.Header.ImageSize)
	}
	if len(payload) < 60 {
		t.Fatalf("payload too small (%d bytes) to contain header", len(payload))
	}
	if !bytes.Equal(payload[56:60], []byte{'A', 'R', 'M', 'd'}) {
		t.Fatalf("payload magic mismatch: got %q", payload[56:60])
	}
}

func buildTestHeader(t *testing.T, textOffset uint64) []byte {
	t.Helper()

	header := make([]byte, imageHeaderSizeBytes)
	binary.LittleEndian.PutUint32(header[0:4], 0xe59f0000) // pseudo opcodes
	binary.LittleEndian.PutUint32(header[4:8], 0xe59ff000)
	binary.LittleEndian.PutUint64(header[8:16], textOffset)
	binary.LittleEndian.PutUint64(header[16:24], 0x200000)
	binary.LittleEndian.PutUint64(header[24:32], 0x0)
	binary.LittleEndian.PutUint64(header[32:40], 0x0)
	binary.LittleEndian.PutUint64(header[40:48], 0x0)
	binary.LittleEndian.PutUint64(header[48:56], 0x0)
	binary.LittleEndian.PutUint32(header[56:60], arm64ImageMagic)
	binary.LittleEndian.PutUint32(header[60:64], 0)
	return header
}
