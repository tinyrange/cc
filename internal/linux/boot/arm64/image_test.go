package arm64

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"testing"
)

func TestProbeKernelImageRaw(t *testing.T) {
	image := buildTestImage()
	probe, err := ProbeKernelImage(bytes.NewReader(image), int64(len(image)))
	if err != nil {
		t.Fatalf("ProbeKernelImage() error = %v", err)
	}
	if probe.NeedsDecompression {
		t.Fatal("ProbeKernelImage() reported decompression for raw image")
	}
	if probe.Header.Magic != arm64ImageMagic {
		t.Fatalf("probe header magic = %#x", probe.Header.Magic)
	}
}

func TestProbeKernelImageGzip(t *testing.T) {
	raw := buildTestImage()
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	if _, err := zw.Write(raw); err != nil {
		t.Fatalf("gzip write error = %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("gzip close error = %v", err)
	}

	prefix := append([]byte("stub"), gz.Bytes()...)
	probe, err := ProbeKernelImage(bytes.NewReader(prefix), int64(len(prefix)))
	if err != nil {
		t.Fatalf("ProbeKernelImage() error = %v", err)
	}
	if !probe.NeedsDecompression {
		t.Fatal("ProbeKernelImage() did not report decompression")
	}

	got, err := probe.ExtractImage(bytes.NewReader(prefix), int64(len(prefix)))
	if err != nil {
		t.Fatalf("ExtractImage() error = %v", err)
	}
	if !bytes.Equal(got, raw) {
		t.Fatal("ExtractImage() did not return raw image")
	}
}

func buildTestImage() []byte {
	buf := make([]byte, imageHeaderSizeBytes+16)
	binary.LittleEndian.PutUint64(buf[8:16], 0x80000)
	binary.LittleEndian.PutUint64(buf[16:24], uint64(len(buf)))
	binary.LittleEndian.PutUint32(buf[56:60], arm64ImageMagic)
	copy(buf[64:], []byte("payload"))
	return buf
}
