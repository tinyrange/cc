package guestinit

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestBuildFindsModuleFromSourceLocation(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	otherModule := t.TempDir()
	if err := os.WriteFile(filepath.Join(otherModule, "go.mod"), []byte("module example.invalid/other\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(otherModule); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(wd); err != nil {
			t.Errorf("restore cwd before temp cleanup: %v", err)
		}
	})

	data, err := Build(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("empty guest init binary")
	}
}
