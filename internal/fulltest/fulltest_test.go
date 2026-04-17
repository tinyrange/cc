package fulltest

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestSubstituteVariables(t *testing.T) {
	t.Parallel()

	got := substituteVariables("niimath ${t1w} $output_dir", map[string]string{
		"t1w":        "/work/a.nii.gz",
		"output_dir": "/work/test_output",
	})
	want := "niimath /work/a.nii.gz /work/test_output"
	if got != want {
		t.Fatalf("substituteVariables() = %q, want %q", got, want)
	}
}

func TestValidateOutputs(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	a := filepath.Join(root, "a.nii.gz")
	b := filepath.Join(root, "b.nii.gz")
	c := filepath.Join(root, "c.nii.gz")
	writeNiftiGZ(t, a, [8]int16{3, 10, 20, 30})
	writeNiftiGZ(t, b, [8]int16{3, 10, 20, 30})
	writeNiftiGZ(t, c, [8]int16{4, 10, 20, 30, 2})

	if msg := validateOutputs(map[string]string{}, []map[string]any{
		{"output_exists": a},
		{"same_dimensions": []any{a, b}},
		{"is_3d": a},
	}); msg != "" {
		t.Fatalf("validateOutputs() = %q, want success", msg)
	}
	if msg := validateOutputs(map[string]string{}, []map[string]any{
		{"is_3d": c},
	}); msg == "" {
		t.Fatalf("validateOutputs() unexpectedly succeeded for 4D file")
	}
}

func writeNiftiGZ(t *testing.T, path string, dims [8]int16) {
	t.Helper()
	var header [348]byte
	binary.LittleEndian.PutUint32(header[0:4], 348)
	for i := 0; i < 8; i++ {
		binary.LittleEndian.PutUint16(header[40+i*2:42+i*2], uint16(dims[i]))
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(header[:]); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}
