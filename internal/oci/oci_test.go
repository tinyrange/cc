package oci

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"j5.nz/cc/internal/imagefs"
)

func TestStorePullSIMGFixtureAndOpenPreservesMetadata(t *testing.T) {
	t.Setenv(sharedCacheEnv, filepath.Join(t.TempDir(), "shared"))
	store := NewStore(filepath.Join(t.TempDir(), "store"))

	state, err := store.Pull(context.Background(), "alpine", alpineFixture(t), PullOptions{Architecture: "amd64"})
	if err != nil {
		t.Fatalf("pull fixture: %v", err)
	}
	if state.Name != "alpine" || state.Status != "downloaded" || state.SourceKind != SourceKindSIMG {
		t.Fatalf("state = %+v", state)
	}

	image, err := store.Open("alpine")
	if err != nil {
		t.Fatalf("open pulled image: %v", err)
	}
	if image.Architecture != "amd64" {
		t.Fatalf("architecture = %q, want amd64", image.Architecture)
	}
	if image.SourceKind != SourceKindSIMG || !strings.HasSuffix(image.Source, "alpine.simg") {
		t.Fatalf("source metadata = kind %q source %q", image.SourceKind, image.Source)
	}
	if _, err := imagefs.LookupPath(image.RootFS, "/etc/alpine-release"); err != nil {
		t.Fatalf("lookup alpine release: %v", err)
	}
}

func TestStorePullRestoresSIMGFromSharedCache(t *testing.T) {
	shared := filepath.Join(t.TempDir(), "shared")
	t.Setenv(sharedCacheEnv, shared)
	source := alpineFixture(t)

	first := NewStore(filepath.Join(t.TempDir(), "first"))
	if _, err := first.Pull(context.Background(), "alpine", source, PullOptions{Architecture: "amd64"}); err != nil {
		t.Fatalf("initial pull: %v", err)
	}

	second := NewStore(filepath.Join(t.TempDir(), "second"))
	state, err := second.Pull(context.Background(), "restored", source, PullOptions{Architecture: "amd64"})
	if err != nil {
		t.Fatalf("restore pull: %v", err)
	}
	if state.Name != "restored" || state.Status != "downloaded" {
		t.Fatalf("restored state = %+v", state)
	}
	if _, err := second.Open("restored"); err != nil {
		t.Fatalf("open restored image: %v", err)
	}
}

func TestStoreRecordsSIMGArchitectureAndSupportsInternalScratch(t *testing.T) {
	t.Setenv(sharedCacheEnv, filepath.Join(t.TempDir(), "shared"))
	store := NewStore(filepath.Join(t.TempDir(), "store"))

	if _, err := store.Pull(context.Background(), "simg-arch", alpineFixture(t), PullOptions{Architecture: "arm64"}); err != nil {
		t.Fatalf("pull simg with requested architecture: %v", err)
	}
	simgImage, err := store.Open("simg-arch")
	if err != nil {
		t.Fatalf("open simg image: %v", err)
	}
	if simgImage.Architecture != "amd64" {
		t.Fatalf("SIMG architecture = %q, want header architecture amd64", simgImage.Architecture)
	}

	if err := store.EnsureInternalScratch(context.Background(), "scratch", "arm64"); err != nil {
		t.Fatalf("ensure scratch: %v", err)
	}
	img, err := store.Open("scratch")
	if err != nil {
		t.Fatalf("open scratch: %v", err)
	}
	if img.SourceKind != SourceKindInternal || img.Architecture != "arm64" {
		t.Fatalf("scratch metadata = kind %q arch %q", img.SourceKind, img.Architecture)
	}
}

func alpineFixture(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("resolve caller")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "fixtures", "alpine.simg")
}
