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
	clearExtractedImageCache()
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

func TestExtractImageCachesGzipPayload(t *testing.T) {
	clearExtractedImageCache()
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
	first, err := probe.ExtractImage(bytes.NewReader(prefix), int64(len(prefix)))
	if err != nil {
		t.Fatalf("first ExtractImage() error = %v", err)
	}
	second, err := probe.ExtractImage(bytes.NewReader(prefix), int64(len(prefix)))
	if err != nil {
		t.Fatalf("second ExtractImage() error = %v", err)
	}
	if len(first) == 0 || len(second) == 0 {
		t.Fatal("ExtractImage() returned empty image")
	}
	if &first[0] != &second[0] {
		t.Fatal("ExtractImage() did not reuse cached gzip payload")
	}
}

func BenchmarkExtractImageGzipCacheMiss(b *testing.B) {
	kernel := buildBenchmarkGzipImage(b)
	probe, err := ProbeKernelImage(bytes.NewReader(kernel), int64(len(kernel)))
	if err != nil {
		b.Fatalf("ProbeKernelImage() error = %v", err)
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		clearExtractedImageCache()
		if _, err := probe.ExtractImage(bytes.NewReader(kernel), int64(len(kernel))); err != nil {
			b.Fatalf("ExtractImage() error = %v", err)
		}
	}
}

func BenchmarkExtractImageGzipCacheHit(b *testing.B) {
	clearExtractedImageCache()
	kernel := buildBenchmarkGzipImage(b)
	probe, err := ProbeKernelImage(bytes.NewReader(kernel), int64(len(kernel)))
	if err != nil {
		b.Fatalf("ProbeKernelImage() error = %v", err)
	}
	if _, err := probe.ExtractImage(bytes.NewReader(kernel), int64(len(kernel))); err != nil {
		b.Fatalf("prime ExtractImage() error = %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := probe.ExtractImage(bytes.NewReader(kernel), int64(len(kernel))); err != nil {
			b.Fatalf("ExtractImage() error = %v", err)
		}
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

func buildBenchmarkGzipImage(tb testing.TB) []byte {
	tb.Helper()
	raw := make([]byte, 8<<20)
	binary.LittleEndian.PutUint64(raw[8:16], 0x80000)
	binary.LittleEndian.PutUint64(raw[16:24], uint64(len(raw)))
	binary.LittleEndian.PutUint32(raw[56:60], arm64ImageMagic)
	for i := imageHeaderSizeBytes; i < len(raw); i++ {
		raw[i] = byte(i * 251)
	}

	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	if _, err := zw.Write(raw); err != nil {
		tb.Fatalf("gzip write error = %v", err)
	}
	if err := zw.Close(); err != nil {
		tb.Fatalf("gzip close error = %v", err)
	}
	return append([]byte("stub"), gz.Bytes()...)
}

func clearExtractedImageCache() {
	extractedImageCache.Lock()
	defer extractedImageCache.Unlock()
	extractedImageCache.entries = map[imageCacheKey][]byte{}
	extractedImageCache.order = nil
}
