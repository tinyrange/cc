package oci

import (
	"archive/tar"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ulikunitz/xz"
	"j5.nz/cc/client"
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

func TestStoreOpenSharesImmutableSIMGTree(t *testing.T) {
	t.Setenv(sharedCacheEnv, filepath.Join(t.TempDir(), "shared"))
	store := NewStore(filepath.Join(t.TempDir(), "store"))
	if _, err := store.Pull(context.Background(), "alpine", alpineFixture(t)); err != nil {
		t.Fatalf("pull fixture: %v", err)
	}

	const count = 16
	images := make(chan *Image, count)
	errs := make(chan error, count)
	for range count {
		go func() {
			image, err := store.Open("alpine")
			images <- image
			errs <- err
		}()
	}
	opened := make([]*Image, 0, count)
	for range count {
		if err := <-errs; err != nil {
			t.Fatalf("open image: %v", err)
		}
		opened = append(opened, <-images)
	}
	for _, image := range opened[1:] {
		if image.RootFS != opened[0].RootFS {
			t.Fatal("concurrent opens rebuilt the immutable SIMG tree")
		}
	}
	opened[0].Config.Env = append(opened[0].Config.Env, "ONLY_FIRST=1")
	if len(opened[0].Config.Env) == len(opened[1].Config.Env) {
		t.Fatal("open images share mutable runtime configuration")
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

func TestStorePullRootFSTarXZ(t *testing.T) {
	t.Setenv(sharedCacheEnv, filepath.Join(t.TempDir(), "shared"))
	source := writeRootFSTarXZFixture(t)
	store := NewStore(filepath.Join(t.TempDir(), "store"))

	state, err := store.Pull(context.Background(), "ubuntu", "rootfs-tar:"+source, PullOptions{Architecture: "arm64"})
	if err != nil {
		t.Fatalf("pull rootfs tar: %v", err)
	}
	if state.Name != "ubuntu" || state.Status != "downloaded" || state.SourceKind != SourceKindRootFSTar {
		t.Fatalf("state = %+v", state)
	}

	image, err := store.Open("ubuntu")
	if err != nil {
		t.Fatalf("open rootfs image: %v", err)
	}
	if image.Architecture != "arm64" {
		t.Fatalf("architecture = %q, want arm64", image.Architecture)
	}
	if !containsEnv(image.Config.Env, "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin") {
		t.Fatalf("env = %v, want default PATH", image.Config.Env)
	}
	if _, err := imagefs.LookupPath(image.RootFS, "/etc/os-release"); err != nil {
		t.Fatalf("lookup os-release: %v", err)
	}
}

func TestStorePullRootFSTarReportsDownloadProgress(t *testing.T) {
	t.Setenv(sharedCacheEnv, filepath.Join(t.TempDir(), "shared"))
	source := writeRootFSTarXZFixture(t)
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatalf("read rootfs fixture: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		_, _ = w.Write(data)
	}))
	defer server.Close()
	store := NewStore(filepath.Join(t.TempDir(), "store"))
	var events []client.ProgressEvent

	_, err = store.Pull(context.Background(), "ubuntu", "rootfs-tar:"+server.URL+"/rootfs.tar.xz", PullOptions{
		Architecture: "arm64",
		Report: func(event client.ProgressEvent) {
			if event.Status == "downloading" && event.Blob == "rootfs" {
				events = append(events, event)
			}
		},
	})
	if err != nil {
		t.Fatalf("pull rootfs tar: %v", err)
	}
	if len(events) == 0 {
		t.Fatalf("no download progress events reported")
	}
	last := events[len(events)-1]
	if last.BytesDownloaded != int64(len(data)) || last.BytesTotal != int64(len(data)) {
		t.Fatalf("last progress bytes = %d/%d, want %d/%d", last.BytesDownloaded, last.BytesTotal, len(data), len(data))
	}
	if last.Progress != 1 {
		t.Fatalf("last progress = %v, want 1", last.Progress)
	}
}

func TestResolvedOCISourceUsesImmutableDigest(t *testing.T) {
	got := resolvedOCISource(defaultRegistry, "library/ubuntu", "sha256:abc123")
	if got != "library/ubuntu@sha256:abc123" {
		t.Fatalf("default registry source = %q", got)
	}
	got = resolvedOCISource("https://registry.example.com/v2", "team/image", "sha256:def456")
	if got != "registry.example.com/team/image@sha256:def456" {
		t.Fatalf("custom registry source = %q", got)
	}
}

func TestRegistryAuthorizeEncodesChallengeParams(t *testing.T) {
	var gotService, gotScope string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotService = r.URL.Query().Get("service")
		gotScope = r.URL.Query().Get("scope")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"ok"}`))
	}))
	defer server.Close()

	reg := &registryContext{client: server.Client()}
	header := `Bearer realm="` + server.URL + `/token",service="SUSE Linux Docker Registry",scope="repository:bci/bci-base:pull"`
	if err := reg.authorize(context.Background(), header); err != nil {
		t.Fatalf("authorize: %v", err)
	}
	if gotService != "SUSE Linux Docker Registry" {
		t.Fatalf("service query = %q", gotService)
	}
	if gotScope != "repository:bci/bci-base:pull" {
		t.Fatalf("scope query = %q", gotScope)
	}
	if reg.token != "ok" {
		t.Fatalf("token = %q", reg.token)
	}
}

func TestRegistryAuthorizeAcceptsChunkedTokenResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		_, _ = w.Write([]byte(`{"token":"chunked"}`))
	}))
	defer server.Close()

	reg := &registryContext{client: server.Client()}
	header := `Bearer realm="` + server.URL + `/token"`
	if err := reg.authorize(context.Background(), header); err != nil {
		t.Fatalf("authorize chunked token response: %v", err)
	}
	if reg.token != "chunked" {
		t.Fatalf("token = %q, want chunked", reg.token)
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

func writeRootFSTarXZFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rootfs.tar.xz")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create rootfs tar: %v", err)
	}
	xzw, err := xz.NewWriter(file)
	if err != nil {
		t.Fatalf("create xz writer: %v", err)
	}
	tw := tar.NewWriter(xzw)
	entries := []struct {
		name     string
		mode     int64
		body     string
		typeflag byte
	}{
		{name: "etc/os-release", mode: 0o644, body: "ID=ubuntu\nVERSION_ID=\"24.04\"\n", typeflag: tar.TypeReg},
		{name: "usr/bin/tool", mode: 0o755, body: "#!/bin/sh\nexit 0\n", typeflag: tar.TypeReg},
		{name: "run/initctl", mode: 0o600, typeflag: tar.TypeFifo},
	}
	for _, entry := range entries {
		hdr := &tar.Header{
			Name:     entry.name,
			Mode:     entry.mode,
			Size:     int64(len(entry.body)),
			ModTime:  time.Unix(1, 0),
			Typeflag: entry.typeflag,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("write tar header: %v", err)
		}
		if _, err := tw.Write([]byte(entry.body)); err != nil {
			t.Fatalf("write tar body: %v", err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := xzw.Close(); err != nil {
		t.Fatalf("close xz: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close rootfs tar: %v", err)
	}
	return path
}

func containsEnv(env []string, want string) bool {
	for _, entry := range env {
		if entry == want {
			return true
		}
	}
	return false
}
