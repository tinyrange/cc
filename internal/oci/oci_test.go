package oci

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ulikunitz/xz"
	"j5.nz/cc/client"
	"j5.nz/cc/internal/imagefs"
)

func TestWriteAndApplyIndexedLayerUsesOneStream(t *testing.T) {
	var compressed bytes.Buffer
	gzw := gzip.NewWriter(&compressed)
	tw := tar.NewWriter(gzw)
	body := []byte("hello from one pass")
	if err := tw.WriteHeader(&tar.Header{
		Name:     "usr/share/message",
		Mode:     0o644,
		Size:     int64(len(body)),
		Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatal(err)
	}

	build := newIndexedBuildState()
	layerPath := filepath.Join(t.TempDir(), "layers", "test.tar")
	var finalProgress int64
	if err := writeAndApplyIndexedLayer(
		layerPath,
		"application/vnd.oci.image.layer.v1.tar+gzip",
		bytes.NewReader(compressed.Bytes()),
		"layers/test.tar",
		build.merged,
		build.fsEntries,
		func(current int64) { finalProgress = current },
	); err != nil {
		t.Fatal(err)
	}
	node := build.merged["/usr/share/message"]
	if node == nil || node.TarPath != "layers/test.tar" || node.Size != uint64(len(body)) {
		t.Fatalf("indexed node = %+v", node)
	}
	file, err := os.Open(layerPath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	got := make([]byte, len(body))
	if _, err := file.ReadAt(got, int64(node.TarOffset)); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("indexed contents = %q", got)
	}
	if finalProgress != int64(compressed.Len()) {
		t.Fatalf("progress = %d, want %d", finalProgress, compressed.Len())
	}
	if _, err := os.Stat(layerPath + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temporary layer remains: %v", err)
	}
}

func TestAggregateLayerProgressReportsWholeImageBytes(t *testing.T) {
	var events []client.ProgressEvent
	reader := newAggregateDownloadProgressReader(
		bytes.NewReader(make([]byte, 50)),
		"neurodesktop",
		"sha256:layer",
		100,
		200,
		func(event client.ProgressEvent) {
			events = append(events, event)
		},
	)
	if _, err := io.Copy(io.Discard, reader); err != nil {
		t.Fatal(err)
	}
	reader.finish()
	if len(events) == 0 {
		t.Fatal("download did not report progress")
	}
	final := events[len(events)-1]
	if final.BytesDownloaded != 150 || final.BytesTotal != 200 || final.Progress != 0.75 {
		t.Fatalf("final aggregate progress = %+v", final)
	}
}

func TestPlanPullReportsOnlyUncachedOCILayerBytes(t *testing.T) {
	const (
		firstDigest  = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
		secondDigest = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
	)
	manifestData, err := json.Marshal(manifest{
		SchemaVersion: 2,
		MediaType:     "application/vnd.oci.image.manifest.v1+json",
		Config: descriptor{
			MediaType: "application/vnd.oci.image.config.v1+json",
			Digest:    "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Size:      32,
		},
		Layers: []descriptor{
			{MediaType: "application/vnd.oci.image.layer.v1.tar+gzip", Digest: firstDigest, Size: 100},
			{MediaType: "application/vnd.oci.image.layer.v1.tar+gzip", Digest: secondDigest, Size: 250},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/manifests/edge") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
		_, _ = w.Write(manifestData)
	}))
	defer server.Close()

	shared := filepath.Join(t.TempDir(), "shared")
	t.Setenv(sharedCacheEnv, shared)
	if err := os.MkdirAll(filepath.Join(shared, "_blobs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shared, "_blobs", digestToFileName(firstDigest)), make([]byte, 100), 0o644); err != nil {
		t.Fatal(err)
	}
	store := NewStore(filepath.Join(t.TempDir(), "store"))
	store.httpClient = server.Client()
	source := strings.TrimPrefix(server.URL, "https://") + "/team/squad:edge"
	plan, err := store.PlanPull(t.Context(), "squadvm", source, PullOptions{Architecture: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Available || plan.BytesTotal != 350 || plan.BytesCached != 100 || plan.BytesToDownload != 250 {
		t.Fatalf("pull plan = %+v", plan)
	}
	if plan.LayersTotal != 2 || plan.LayersCached != 1 {
		t.Fatalf("pull plan layers = %+v", plan)
	}

	spec, err := ParseSource(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.writeMetadata("squadvm", metadata{
		Name:           "squadvm",
		Source:         spec.Raw,
		SourceKind:     spec.Kind,
		Architecture:   "amd64",
		RootFSDir:      store.imageDir("squadvm"),
		ResolvedSource: "oci:old",
	}); err != nil {
		t.Fatal(err)
	}
	update, err := store.PlanPull(t.Context(), "squadvm", source, PullOptions{Architecture: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	if !update.Installed || update.Available || update.BytesToDownload != 250 {
		t.Fatalf("update plan = %+v", update)
	}

	sum := sha256.Sum256(manifestData)
	currentSource := resolvedOCISource(server.URL, "team/squad", "sha256:"+hex.EncodeToString(sum[:]))
	meta, err := store.readMetadata("squadvm")
	if err != nil {
		t.Fatal(err)
	}
	meta.ResolvedSource = currentSource
	if err := store.writeMetadata("squadvm", meta); err != nil {
		t.Fatal(err)
	}
	current, err := store.PlanPull(t.Context(), "squadvm", source, PullOptions{Architecture: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	if !current.Installed || !current.Available || current.BytesToDownload != 0 {
		t.Fatalf("current plan = %+v", current)
	}
}

func TestCacheLayerBlobsDownloadsConcurrently(t *testing.T) {
	const layerCount = 4
	layers := make([]descriptor, 0, layerCount)
	contents := make(map[string][]byte, layerCount)
	for index := range layerCount {
		data := bytes.Repeat([]byte{byte(index + 1)}, 64<<10)
		sum := sha256.Sum256(data)
		digest := "sha256:" + hex.EncodeToString(sum[:])
		layers = append(layers, descriptor{Digest: digest, Size: int64(len(data))})
		contents[digest] = data
	}

	var active atomic.Int32
	var maximum atomic.Int32
	var releaseOnce sync.Once
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := active.Add(1)
		defer active.Add(-1)
		for {
			previous := maximum.Load()
			if current <= previous || maximum.CompareAndSwap(previous, current) {
				break
			}
		}
		if current >= 2 {
			releaseOnce.Do(func() { close(release) })
		}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		digest := path.Base(r.URL.Path)
		data, ok := contents[digest]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		_, _ = w.Write(data)
	}))
	defer server.Close()

	store := NewStore(t.TempDir())
	reg := &registryContext{client: server.Client(), registry: server.URL}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := store.cacheLayerBlobs(ctx, reg, "test/image", "parallel", layers, nil); err != nil {
		t.Fatalf("cache layer blobs: %v", err)
	}
	if maximum.Load() < 2 {
		t.Fatalf("maximum concurrent blob requests = %d, want at least 2", maximum.Load())
	}
}

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

	firstRoot := filepath.Join(t.TempDir(), "first")
	first := NewStore(firstRoot)
	if _, err := first.Pull(context.Background(), "alpine", source, PullOptions{Architecture: "amd64"}); err != nil {
		t.Fatalf("initial pull: %v", err)
	}

	secondRoot := filepath.Join(t.TempDir(), "second")
	second := NewStore(secondRoot)
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

	spec, err := ParseSource(source)
	if err != nil {
		t.Fatal(err)
	}
	sharedImage := filepath.Join(shared, sharedImageKey(spec, "amd64"))
	sharedRootFS, err := os.Stat(filepath.Join(sharedImage, "rootfs.simg"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(firstRoot, "alpine", "rootfs.simg"),
		filepath.Join(secondRoot, "restored", "rootfs.simg"),
	} {
		localRootFS, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if !os.SameFile(sharedRootFS, localRootFS) {
			t.Fatalf("cached image artifact %q occupies a second copy", path)
		}
	}
	sharedMetadata, err := os.Stat(filepath.Join(sharedImage, "image.json"))
	if err != nil {
		t.Fatal(err)
	}
	localMetadata, err := os.Stat(filepath.Join(secondRoot, "restored", "image.json"))
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(sharedMetadata, localMetadata) {
		t.Fatal("named image metadata aliases shared-cache metadata")
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
