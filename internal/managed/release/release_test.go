package release

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFirstExistingReturnsFirstNonEmptyFile(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty")
	first := filepath.Join(dir, "first")
	second := filepath.Join(dir, "second")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(first, []byte("first"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("second"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := FirstExisting(filepath.Join(dir, "missing"), empty, first, second); got != first {
		t.Fatalf("FirstExisting = %q, want %q", got, first)
	}
}

func TestEnsureArtifactUsesLocalCandidate(t *testing.T) {
	dir := t.TempDir()
	local := filepath.Join(dir, "local.tgz")
	if err := os.WriteFile(local, []byte("fixture"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := EnsureArtifact(context.Background(), Artifact{
		CacheDir:        filepath.Join(dir, "cache"),
		Family:          "openbsd",
		Version:         "7.9",
		Arch:            "amd64",
		Name:            "base79.tgz",
		LocalCandidates: []string{filepath.Join(dir, "missing"), local},
	})
	if err != nil {
		t.Fatalf("EnsureArtifact: %v", err)
	}
	if got != local {
		t.Fatalf("artifact = %q, want local %q", got, local)
	}
}

func TestEnsureArtifactDownloadsToVersionedCache(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/pub/OpenBSD/7.9/amd64/base79.tgz" {
			t.Fatalf("request path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte("artifact"))
	}))
	defer server.Close()
	cache := filepath.Join(t.TempDir(), "cache")
	got, err := EnsureArtifact(context.Background(), Artifact{
		CacheDir: cache,
		Family:   "openbsd",
		Version:  "7.9",
		Arch:     "amd64",
		Mirror:   server.URL + "/pub/OpenBSD",
		Name:     "base79.tgz",
		URLPath:  "7.9/amd64/base79.tgz",
	})
	if err != nil {
		t.Fatalf("EnsureArtifact: %v", err)
	}
	want := filepath.Join(cache, "openbsd", "7.9", "amd64", "base79.tgz")
	if got != want {
		t.Fatalf("artifact path = %q, want %q", got, want)
	}
	data, err := os.ReadFile(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "artifact" {
		t.Fatalf("artifact data = %q", data)
	}
}

func TestEnsureDecompressedUsesFreshCachedTarget(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "base.tgz")
	target := filepath.Join(dir, "base.tar")
	if err := os.WriteFile(source, []byte("source"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("cached"), 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := os.Chtimes(source, now.Add(-time.Hour), now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(target, now, now); err != nil {
		t.Fatal(err)
	}
	called := false
	got, err := EnsureDecompressed(context.Background(), source, target, func(r io.Reader) (io.ReadCloser, error) {
		called = true
		return io.NopCloser(r), nil
	})
	if err != nil {
		t.Fatalf("EnsureDecompressed: %v", err)
	}
	if got != target {
		t.Fatalf("target = %q, want %q", got, target)
	}
	if called {
		t.Fatalf("reader factory called for fresh cached target")
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "cached" {
		t.Fatalf("cached data = %q", data)
	}
}

func TestEnsureDecompressedWritesAtomically(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "base.tgz")
	target := filepath.Join(dir, "base.tar")
	if err := os.WriteFile(source, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := EnsureDecompressed(context.Background(), source, target, func(r io.Reader) (io.ReadCloser, error) {
		data, err := io.ReadAll(r)
		if err != nil {
			return nil, err
		}
		return io.NopCloser(strings.NewReader(strings.ToUpper(string(data)))), nil
	})
	if err != nil {
		t.Fatalf("EnsureDecompressed: %v", err)
	}
	if got != target {
		t.Fatalf("target = %q, want %q", got, target)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "PAYLOAD" {
		t.Fatalf("decompressed data = %q", data)
	}
	if _, err := os.Stat(target + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temporary file still exists: %v", err)
	}
}

func TestDownloadWritesResponseAtomically(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("downloaded"))
	}))
	defer server.Close()
	target := filepath.Join(t.TempDir(), "artifact")
	if err := Download(context.Background(), server.URL, target); err != nil {
		t.Fatalf("Download: %v", err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "downloaded" {
		t.Fatalf("downloaded data = %q", data)
	}
}
