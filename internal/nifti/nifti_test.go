package nifti

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestReadHeaderAndShape(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	nii := filepath.Join(root, "a.nii")
	if err := os.WriteFile(nii, buildHeader(t, [8]int16{3, 10, 20, 30}), 0o644); err != nil {
		t.Fatal(err)
	}
	hdr, err := ReadHeader(nii)
	if err != nil {
		t.Fatalf("ReadHeader() error = %v", err)
	}
	got := Shape(hdr)
	want := []int{10, 20, 30}
	if len(got) != len(want) {
		t.Fatalf("Shape() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Shape() = %v, want %v", got, want)
		}
	}
	if !Is3D(hdr) {
		t.Fatalf("Is3D() = false, want true")
	}
}

func TestReadHeaderGZ(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "a.nii.gz")
	raw := buildHeader(t, [8]int16{4, 64, 64, 33, 300})
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	hdr, err := ReadHeader(path)
	if err != nil {
		t.Fatalf("ReadHeader() error = %v", err)
	}
	if Is3D(hdr) {
		t.Fatalf("Is3D() = true, want false")
	}
}

func buildHeader(t *testing.T, dims [8]int16) []byte {
	t.Helper()
	buf := make([]byte, 348)
	binary.LittleEndian.PutUint32(buf[0:4], 348)
	for i := 0; i < 8; i++ {
		binary.LittleEndian.PutUint16(buf[40+i*2:42+i*2], uint16(dims[i]))
	}
	return buf
}
