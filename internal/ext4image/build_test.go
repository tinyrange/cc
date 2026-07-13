package ext4image

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"j5.nz/cc/internal/imagefs"
)

type writeFunc func([]byte) (int, error)

func (f writeFunc) Write(p []byte) (int, error) {
	return f(p)
}

type readDirErrorDirectory struct {
	imagefs.Directory
	err error
}

func (d readDirErrorDirectory) ReadDir() ([]imagefs.DirEnt, error) {
	return nil, d.err
}

func TestWriteBuildsExt4Image(t *testing.T) {
	overlay := imagefs.NewOverlay(nil)
	if err := overlay.AddFile("/etc/issue", 0o644, []byte("hello\n")); err != nil {
		t.Fatal(err)
	}
	if err := overlay.AddSymlink("/etc/issue.link", "issue"); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := Write(context.Background(), &buf, overlay.Root(), Options{}); err != nil {
		t.Fatal(err)
	}
	if got := buf.Len(); got != defaultImageSize {
		t.Fatalf("image size = %d, want %d", got, defaultImageSize)
	}
	if got := buf.Bytes()[1024+56 : 1024+58]; got[0] != 0x53 || got[1] != 0xef {
		t.Fatalf("ext4 magic = % x, want 53 ef", got)
	}
}

func TestWriteReturnsOutputFailure(t *testing.T) {
	backingErr := errors.New("output unavailable")
	err := Write(context.Background(), writeFunc(func([]byte) (int, error) {
		return 0, backingErr
	}), imagefs.NewOverlay(nil).Root(), Options{})
	if !errors.Is(err, backingErr) {
		t.Fatalf("Write error = %v, want wrapped %v", err, backingErr)
	}
}

func TestWriteFilePreservesDestinationAndPrimaryError(t *testing.T) {
	dir := t.TempDir()
	destination := filepath.Join(dir, "root.ext4")
	want := []byte("existing image")
	if err := os.WriteFile(destination, want, 0o644); err != nil {
		t.Fatalf("write existing image: %v", err)
	}
	primaryErr := errors.New("source filesystem unavailable")
	err := WriteFile(context.Background(), destination, readDirErrorDirectory{err: primaryErr}, Options{})
	if !errors.Is(err, primaryErr) {
		t.Fatalf("WriteFile error = %v, want wrapped %v", err, primaryErr)
	}
	got, err := os.ReadFile(destination)
	if err != nil {
		t.Fatalf("read existing image: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("destination bytes = %q, want %q", got, want)
	}
	if _, err := os.Stat(destination + ".tmp"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary image remains: %v", err)
	}
}
