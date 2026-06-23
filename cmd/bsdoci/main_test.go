package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ulikunitz/xz"
)

func TestRunWritesOCILayoutForAllFamilies(t *testing.T) {
	tests := []struct {
		name       string
		family     string
		version    string
		arch       string
		sourceDir  string
		wantKernel string
		wantRoot   string
	}{
		{
			name:       "openbsd",
			family:     "openbsd",
			version:    "7.9",
			arch:       "amd64",
			sourceDir:  writeOpenBSDFixtures(t),
			wantKernel: "openbsd-kernel",
			wantRoot:   "bin/sh",
		},
		{
			name:       "freebsd",
			family:     "freebsd",
			version:    "15.1",
			arch:       "amd64",
			sourceDir:  writeFreeBSDFixtures(t),
			wantKernel: "freebsd-kernel",
			wantRoot:   "bin/sh",
		},
		{
			name:       "netbsd",
			family:     "netbsd",
			version:    "10.1",
			arch:       "amd64",
			sourceDir:  writeNetBSDFixtures(t),
			wantKernel: "netbsd-kernel",
			wantRoot:   "bin/sh",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			outDir := filepath.Join(t.TempDir(), "oci")
			var stdout strings.Builder

			err := run(t.Context(), []string{
				"--family", tc.family,
				"--version", tc.version,
				"--arch", tc.arch,
				"--source-dir", tc.sourceDir,
				"--cache-dir", filepath.Join(t.TempDir(), "cache"),
				"--out", outDir,
			}, &stdout)
			if err != nil {
				t.Fatalf("run: %v", err)
			}

			m := readLayoutManifest(t, outDir)
			if len(m.Layers) != 2 {
				t.Fatalf("layers = %d, want rootfs and kernel", len(m.Layers))
			}
			if m.Layers[0].MediaType != ociLayerTarGzipMediaType {
				t.Fatalf("root layer media type = %q", m.Layers[0].MediaType)
			}
			if m.Layers[1].MediaType != bsdKernelMediaType {
				t.Fatalf("kernel layer media type = %q", m.Layers[1].MediaType)
			}
			if m.Annotations["io.tinyrange.bsd.family"] != tc.family {
				t.Fatalf("family annotation = %q", m.Annotations["io.tinyrange.bsd.family"])
			}
			if m.Annotations["io.tinyrange.bsd.guest_arch"] != tc.arch {
				t.Fatalf("arch annotation = %q", m.Annotations["io.tinyrange.bsd.guest_arch"])
			}
			if !strings.HasPrefix(m.Config.Digest, "sha256:") {
				t.Fatalf("config digest = %q", m.Config.Digest)
			}

			rootLayer := blobPath(outDir, m.Layers[0].Digest)
			if !tarGzipContains(t, rootLayer, tc.wantRoot) {
				t.Fatalf("root layer does not contain %s", tc.wantRoot)
			}
			kernelData, err := os.ReadFile(blobPath(outDir, m.Layers[1].Digest))
			if err != nil {
				t.Fatalf("read kernel blob: %v", err)
			}
			if string(kernelData) != tc.wantKernel {
				t.Fatalf("kernel blob = %q", kernelData)
			}
		})
	}
}

func TestRunDryRunPushesToGHCR(t *testing.T) {
	sourceDir := writeOpenBSDFixtures(t)
	var stdout strings.Builder
	err := run(t.Context(), []string{
		"--family", "openbsd",
		"--version", "7.9",
		"--arch", "amd64",
		"--source-dir", sourceDir,
		"--cache-dir", filepath.Join(t.TempDir(), "cache"),
		"--push", "ghcr.io/tinyrange/cc-openbsd:7.9",
		"--dry-run",
	}, &stdout)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !textHasFields(stdout.String(), "dry-run", "push", "manifest", "ghcr.io/tinyrange/cc-openbsd:7.9") {
		t.Fatalf("dry-run output missing GHCR manifest push:\n%s", stdout.String())
	}
	if !textHasFields(stdout.String(), "dry-run", "push", "blob", bsdKernelMediaType) {
		t.Fatalf("dry-run output missing kernel blob media type:\n%s", stdout.String())
	}
}

func TestRunRuntimeTrimPreservesCompilerAndPackageManager(t *testing.T) {
	sourceDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sourceDir, "bsd"), []byte("openbsd-kernel"), 0o644); err != nil {
		t.Fatalf("write kernel fixture: %v", err)
	}
	writeTarGzip(t, filepath.Join(sourceDir, "base79.tgz"), []tarFixture{
		{name: "usr", mode: 0o755, typeflag: tar.TypeDir},
		{name: "usr/bin", mode: 0o755, typeflag: tar.TypeDir},
		{name: "usr/sbin", mode: 0o755, typeflag: tar.TypeDir},
		{name: "usr/sbin/pkg_add", mode: 0o555, typeflag: tar.TypeReg, data: []byte("pkg")},
		{name: "usr/share", mode: 0o755, typeflag: tar.TypeDir},
		{name: "usr/share/man/man1/cc.1", mode: 0o444, typeflag: tar.TypeReg, data: []byte("man")},
		{name: "usr/share/locale/en_US.UTF-8/LC_CTYPE", mode: 0o444, typeflag: tar.TypeReg, data: []byte("locale")},
		{name: "usr/share/relink/usr/lib/libc.so.a", mode: 0o444, typeflag: tar.TypeReg, data: []byte("relink")},
	})
	writeTarGzip(t, filepath.Join(sourceDir, "comp79.tgz"), []tarFixture{
		{name: "usr/bin/cc", mode: 0o555, typeflag: tar.TypeReg, data: []byte("compiler")},
	})
	outDir := filepath.Join(t.TempDir(), "oci")
	sourceCache := filepath.Join(t.TempDir(), "source-cache")
	var stdout strings.Builder
	if err := run(t.Context(), []string{
		"--family", "openbsd",
		"--version", "7.9",
		"--source-dir", sourceDir,
		"--cache-dir", filepath.Join(t.TempDir(), "cache"),
		"--trim-profile", "runtime",
		"--source-cache-out", sourceCache,
		"--out", outDir,
	}, &stdout); err != nil {
		t.Fatalf("run: %v", err)
	}

	m := readLayoutManifest(t, outDir)
	rootLayer := blobPath(outDir, m.Layers[0].Digest)
	for _, want := range []string{"usr/bin/cc", "usr/sbin/pkg_add"} {
		if !tarGzipContains(t, rootLayer, want) {
			t.Fatalf("trimmed root layer missing %s", want)
		}
	}
	for _, removed := range []string{"usr/share/man/man1/cc.1", "usr/share/locale/en_US.UTF-8/LC_CTYPE", "usr/share/relink/usr/lib/libc.so.a"} {
		if tarGzipContains(t, rootLayer, removed) {
			t.Fatalf("trimmed root layer still contains %s", removed)
		}
	}
	sourceRoot := filepath.Join(sourceCache, "openbsd", "7.9", "amd64", "base79.tgz")
	if !tarGzipContains(t, sourceRoot, "usr/bin/cc") || tarGzipContains(t, sourceRoot, "usr/share/man/man1/cc.1") {
		t.Fatalf("source cache trim did not match OCI trim")
	}
}

func TestRunPushesToLocalRegistry(t *testing.T) {
	sourceDir := writeOpenBSDFixtures(t)
	reg := newFakeRegistry(t)
	defer reg.Close()

	ref := strings.TrimPrefix(reg.URL, "http://") + "/tinyrange/cc-openbsd:7.9"
	var stdout strings.Builder
	err := run(t.Context(), []string{
		"--family", "openbsd",
		"--version", "7.9",
		"--source-dir", sourceDir,
		"--cache-dir", filepath.Join(t.TempDir(), "cache"),
		"--push", ref,
		"--plain-http",
	}, &stdout)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if reg.blobs != 3 {
		t.Fatalf("pushed blobs = %d, want 3", reg.blobs)
	}
	if reg.manifests != 1 {
		t.Fatalf("pushed manifests = %d, want 1", reg.manifests)
	}
	if reg.lastManifestMediaType != ociImageManifestMediaType {
		t.Fatalf("manifest content type = %q", reg.lastManifestMediaType)
	}
}

type fakeRegistry struct {
	*httptest.Server
	blobs                 int
	manifests             int
	lastManifestMediaType string
}

func newFakeRegistry(t *testing.T) *fakeRegistry {
	t.Helper()
	reg := &fakeRegistry{}
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/tinyrange/cc-openbsd/blobs/uploads/", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			w.Header().Set("Location", "/v2/tinyrange/cc-openbsd/blobs/uploads/test-upload")
			w.WriteHeader(http.StatusAccepted)
		case http.MethodPut:
			if r.URL.Query().Get("digest") == "" {
				http.Error(w, "missing digest", http.StatusBadRequest)
				return
			}
			if _, err := io.Copy(io.Discard, r.Body); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			reg.blobs++
			w.WriteHeader(http.StatusCreated)
		default:
			http.Error(w, "method", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/v2/tinyrange/cc-openbsd/manifests/7.9", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		reg.lastManifestMediaType = r.Header.Get("Content-Type")
		var m manifest
		if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if len(m.Layers) != 2 {
			http.Error(w, fmt.Sprintf("layers=%d", len(m.Layers)), http.StatusBadRequest)
			return
		}
		reg.manifests++
		w.WriteHeader(http.StatusCreated)
	})
	reg.Server = httptest.NewServer(mux)
	return reg
}

func writeOpenBSDFixtures(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bsd"), []byte("openbsd-kernel"), 0o644); err != nil {
		t.Fatalf("write kernel fixture: %v", err)
	}
	writeTarGzip(t, filepath.Join(dir, "base79.tgz"), []tarFixture{
		{name: "bin", mode: 0o755, typeflag: tar.TypeDir},
		{name: "bin/sh", mode: 0o555, typeflag: tar.TypeReg, data: []byte("#!/bin/sh\n")},
	})
	writeTarGzip(t, filepath.Join(dir, "comp79.tgz"), []tarFixture{
		{name: "usr", mode: 0o755, typeflag: tar.TypeDir},
		{name: "usr/bin", mode: 0o755, typeflag: tar.TypeDir},
		{name: "usr/bin/cc", mode: 0o555, typeflag: tar.TypeReg, data: []byte("compiler")},
	})
	return dir
}

func writeFreeBSDFixtures(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeTarXZ(t, filepath.Join(dir, "kernel.txz"), []tarFixture{
		{name: "boot", mode: 0o755, typeflag: tar.TypeDir},
		{name: "boot/kernel", mode: 0o755, typeflag: tar.TypeDir},
		{name: "boot/kernel/kernel", mode: 0o555, typeflag: tar.TypeReg, data: []byte("freebsd-kernel")},
	})
	writeTarXZ(t, filepath.Join(dir, "base.txz"), []tarFixture{
		{name: "bin", mode: 0o755, typeflag: tar.TypeDir},
		{name: "bin/sh", mode: 0o555, typeflag: tar.TypeReg, data: []byte("#!/bin/sh\n")},
	})
	return dir
}

func writeNetBSDFixtures(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeGzipFile(t, filepath.Join(dir, "netbsd-GENERIC.gz"), []byte("netbsd-kernel"))
	writeTarXZ(t, filepath.Join(dir, "base.tar.xz"), []tarFixture{
		{name: "bin", mode: 0o755, typeflag: tar.TypeDir},
		{name: "bin/sh", mode: 0o555, typeflag: tar.TypeReg, data: []byte("#!/bin/sh\n")},
	})
	writeTarXZ(t, filepath.Join(dir, "comp.tar.xz"), []tarFixture{
		{name: "usr", mode: 0o755, typeflag: tar.TypeDir},
		{name: "usr/bin", mode: 0o755, typeflag: tar.TypeDir},
		{name: "usr/bin/cc", mode: 0o555, typeflag: tar.TypeReg, data: []byte("compiler")},
	})
	return dir
}

type tarFixture struct {
	name     string
	mode     int64
	typeflag byte
	data     []byte
}

func writeTarGzip(t *testing.T, target string, entries []tarFixture) {
	t.Helper()
	out, err := os.Create(target)
	if err != nil {
		t.Fatalf("create tar gzip: %v", err)
	}
	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)
	writeTarEntries(t, tw, entries)
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	if err := out.Close(); err != nil {
		t.Fatalf("close file: %v", err)
	}
}

func writeTarXZ(t *testing.T, target string, entries []tarFixture) {
	t.Helper()
	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)
	writeTarEntries(t, tw, entries)
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	out, err := os.Create(target)
	if err != nil {
		t.Fatalf("create xz fixture: %v", err)
	}
	xzw, err := xz.NewWriter(out)
	if err != nil {
		t.Fatalf("create xz writer: %v", err)
	}
	if _, err := io.Copy(xzw, bytes.NewReader(tarBuf.Bytes())); err != nil {
		t.Fatalf("write xz fixture: %v", err)
	}
	if err := xzw.Close(); err != nil {
		t.Fatalf("close xz: %v", err)
	}
	if err := out.Close(); err != nil {
		t.Fatalf("close xz fixture: %v", err)
	}
}

func writeTarEntries(t *testing.T, tw *tar.Writer, entries []tarFixture) {
	t.Helper()
	for _, entry := range entries {
		hdr := &tar.Header{
			Name:     entry.name,
			Mode:     entry.mode,
			Typeflag: entry.typeflag,
			Size:     int64(len(entry.data)),
			ModTime:  time.Unix(1700000000, 0),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("write header: %v", err)
		}
		if len(entry.data) > 0 {
			if _, err := tw.Write(entry.data); err != nil {
				t.Fatalf("write entry: %v", err)
			}
		}
	}
}

func writeGzipFile(t *testing.T, target string, data []byte) {
	t.Helper()
	out, err := os.Create(target)
	if err != nil {
		t.Fatalf("create gzip file: %v", err)
	}
	gz := gzip.NewWriter(out)
	if _, err := gz.Write(data); err != nil {
		t.Fatalf("write gzip file: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip file: %v", err)
	}
	if err := out.Close(); err != nil {
		t.Fatalf("close gzip output: %v", err)
	}
}

func readLayoutManifest(t *testing.T, outDir string) manifest {
	t.Helper()
	indexData, err := os.ReadFile(filepath.Join(outDir, "index.json"))
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	var idx index
	if err := json.Unmarshal(indexData, &idx); err != nil {
		t.Fatalf("decode index: %v", err)
	}
	if len(idx.Manifests) != 1 {
		t.Fatalf("manifest count = %d, want 1", len(idx.Manifests))
	}
	manifestData, err := os.ReadFile(blobPath(outDir, idx.Manifests[0].Digest))
	if err != nil {
		t.Fatalf("read manifest blob: %v", err)
	}
	var m manifest
	if err := json.Unmarshal(manifestData, &m); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	return m
}

func blobPath(outDir, digest string) string {
	return filepath.Join(outDir, "blobs", "sha256", strings.TrimPrefix(digest, "sha256:"))
}

func textHasFields(text string, want ...string) bool {
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) < len(want) {
			continue
		}
		matched := true
		for i := range want {
			if fields[i] != want[i] {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

func tarGzipContains(t *testing.T, source, want string) bool {
	t.Helper()
	in, err := os.Open(source)
	if err != nil {
		t.Fatalf("open root layer: %v", err)
	}
	defer in.Close()
	gz, err := gzip.NewReader(in)
	if err != nil {
		t.Fatalf("open gzip: %v", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return false
		}
		if err != nil {
			t.Fatalf("read tar: %v", err)
		}
		if hdr.Name == want {
			return true
		}
	}
}
