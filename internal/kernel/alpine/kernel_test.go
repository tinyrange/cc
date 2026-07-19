package alpine

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestEnsureDownloadedRecoversFromRepositoryIndexPackageRace(t *testing.T) {
	arch := defaultArch()
	packageData := gzipTar(t, nil)
	actualDigest := sha1.Sum(packageData)
	staleDigest := sha1.Sum([]byte("previous package"))
	var mu sync.Mutex
	indexRequests, packageRequests := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cache-Control") != "no-cache" {
			t.Errorf("%s cache control = %q", r.URL.Path, r.Header.Get("Cache-Control"))
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/APKINDEX.tar.gz"):
			mu.Lock()
			indexRequests++
			request := indexRequests
			mu.Unlock()
			digest := actualDigest[:]
			if request == 1 {
				digest = staleDigest[:]
			}
			index := fmt.Sprintf("P:linux-virt\nV:1.0-r0\nA:%s\nS:%d\nC:Q1%s\n\n", arch, len(packageData), base64.StdEncoding.EncodeToString(digest))
			_, _ = w.Write(gzipTar(t, map[string][]byte{"APKINDEX": []byte(index)}))
		case strings.HasSuffix(r.URL.Path, "/linux-virt-1.0-r0.apk"):
			mu.Lock()
			packageRequests++
			request := packageRequests
			mu.Unlock()
			if request > 1 && r.URL.Query().Get("cc-repository-retry") == "" {
				t.Error("retry did not bypass a stale package cache key")
			}
			_, _ = w.Write(packageData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	manager := NewManager(t.TempDir())
	manager.mirror = server.URL
	manager.httpClient = server.Client()
	if err := manager.ensureDownloaded(context.Background(), nil); err != nil {
		t.Fatalf("ensure downloaded: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if indexRequests != 2 || packageRequests != 2 {
		t.Fatalf("requests = index %d package %d, want 2 each", indexRequests, packageRequests)
	}
}

func gzipTar(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, data := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(data))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
