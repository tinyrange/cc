package fs

import (
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// A zlib-compressed SQLite catalog with a root directory and hello.txt.
// Keeping this complete fixture in memory makes tests independent of host
// SQLite executables, real repositories, FUSE and a VM backend.
const fixtureCatalog = "eJzt1z1Lw0AYB/C7a8EL0kaXOnTwBgeFItjq5GIqQYSgWDO46WnSJJg0pTnBl6k4CO5+CXF0EMHFjyG4OAqCfgNTjCQ4uenw/3Ev/J977vbb3rIC5YpuPIikEi0yQSglK0KQL4zkaDrLP/KvVEbN+jhJBwAAAAAAAMBfGw7o2FS9Ts+XldwP3QOpZBh72cZWO6Zhm8I22pYpsqKYjZylvlT+7oJY37DNNbPTEN+lZl7qy4HbU8WmrFLo8WXii7a12W6IJDh1C+/FTjGpICrEbii9JI89mR7a5o6dPnIShUHvMEtHgZN3eXmYG/3NmX5H9Ld0AQAAAAAAAIB/pcWqlGp0kZY4na5wXiqzi8sH8fjUvL25qp297j3fv1x/vNfIcKbsu2EYz6tjNUmrGuWccMa1SnqJGdEnsGBmdA=="

func fixtureCVMFS(t *testing.T) (*CVMFS, *http.Client, *atomic.Int32) {
	t.Helper()
	catalog, err := base64.StdEncoding.DecodeString(fixtureCatalog)
	if err != nil {
		t.Fatal(err)
	}
	file, err := base64.StdEncoding.DecodeString("eJxzDvN1C1YoTsxLScqvUEjLrCgpLUrlAgBWVwfA")
	if err != nil {
		t.Fatal(err)
	}
	rootHash := fmt.Sprintf("%x", sha1.Sum(catalog))
	calls := &atomic.Int32{}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.URL.Path {
		case "/bad/cvmfs/repo.example/.cvmfspublished":
			http.Error(w, "mirror unavailable", 503)
		case "/cvmfs/repo.example/.cvmfspublished":
			_, _ = fmt.Fprintf(w, "C%s\n", rootHash)
		case "/cvmfs/repo.example/data/" + rootHash[:2] + "/" + rootHash[2:] + "C":
			_, _ = w.Write(catalog)
		case "/cvmfs/repo.example/data/87/8abd20c2da32b4ac95167bea60ddbae2aaf1f0":
			_, _ = w.Write(file)
		case "/cvmfs/repo.example/redirect":
			http.Redirect(w, r, "/cvmfs/other.example/private", 302)
		case "/cvmfs/repo.example/large":
			_, _ = io.WriteString(w, strings.Repeat("x", 1024))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	backend, err := NewCVMFS(t.Context(), CVMFSConfig{Repository: "repo.example", Mirrors: []string{server.URL + "/bad/cvmfs", server.URL + "/cvmfs"}, CacheBytes: 512, MaxObjectBytes: 1 << 20, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	return backend, &http.Client{Transport: backend.transport}, calls
}

func TestCVMFSReadOnlyLazyMountAndFailover(t *testing.T) {
	b, _, calls := fixtureCVMFS(t)
	if calls.Load() != 0 {
		t.Fatal("construction fetched data")
	}
	mount := b.Mount()
	if mount.Writable || mount.GuestPath != "/cvmfs/repo.example" {
		t.Fatal("invalid read-only mount", mount)
	}
	if _, ok := any(b).(interface {
		Write(uint64, uint64, uint64, []byte, uint32) (uint32, int32)
	}); ok {
		t.Fatal("mutation interface exposed")
	}
	node, attr, errno := b.Lookup(1, "hello.txt")
	if errno != 0 || attr.Size != uint64(len("CVMFS sandbox fixture\n")) {
		t.Fatalf("catalog lookup: node=%d attr=%+v errno=%d stats=%+v", node, attr, errno, b.Stats())
	}
	for _, flags := range []uint32{1, 2, 0x40, 0x200, 0x400} {
		if _, errno := b.Open(node, flags); errno != -30 {
			t.Fatalf("write open: %d", errno)
		}
	}
	fh, errno := b.Open(node, 0)
	if errno != 0 {
		t.Fatal("read open", errno)
	}
	defer b.Release(node, fh)
	data, errno := b.Read(node, fh, 0, 4096)
	if errno != 0 || string(data) != "CVMFS sandbox fixture\n" {
		t.Fatalf("read %q errno=%d stats=%+v", data, errno, b.Stats())
	}
	stats := b.Stats()
	if stats.Requests < 4 || stats.FetchedBytes == 0 || stats.ActiveRequests != 0 || stats.CacheBytes > stats.CacheLimitBytes {
		t.Fatalf("diagnostics: %+v", stats)
	}
}

func TestCVMFSBoundedCacheAndRedirectGrant(t *testing.T) {
	b, client, calls := fixtureCVMFS(t)
	base := strings.TrimSuffix(b.transport.prefixes[1], "/")
	get := func(path string) {
		t.Helper()
		r, err := client.Get(base + path)
		if err != nil {
			t.Fatal(err)
		}
		_, err = io.ReadAll(r.Body)
		_ = r.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	get("/.cvmfspublished")
	before := calls.Load()
	get("/.cvmfspublished")
	if calls.Load() != before {
		t.Fatal("complete response not cached")
	}
	get("/large") // Larger than the cache: stream it without retaining it.
	if b.Stats().CacheBytes > 512 {
		t.Fatal("cache quota exceeded")
	}
	before = calls.Load()
	for _, url := range []string{base + "/redirect", strings.Replace(base, "repo.example", "other.example", 1) + "/private", base + "/../other.example/private"} {
		if r, err := client.Get(url); err == nil {
			r.Body.Close()
			t.Fatal("escaped grant", url)
		}
	}
	if calls.Load() != before+1 {
		t.Fatal("redirect or invalid path reached ungranted repository")
	}
	if b.Stats().LastError == "" || b.Stats().ActiveRequests != 0 {
		t.Fatal("missing error diagnostics", b.Stats())
	}
	_ = b.Close()
	if r, err := client.Get(base + "/.cvmfspublished"); err == nil {
		r.Body.Close()
		t.Fatal("closed cache readable")
	}
}

func TestCVMFSCloseCancelsFetch(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done() }))
	defer server.Close()
	b, err := NewCVMFS(t.Context(), CVMFSConfig{Repository: "repo.example", Mirrors: []string{server.URL + "/cvmfs"}, MaxObjectBytes: 1 << 20, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	done := make(chan int32, 1)
	go func() { _, _, errno := b.Lookup(1, "hello.txt"); done <- errno }()
	<-started
	b.BeginClose()
	select {
	case errno := <-done:
		if errno == 0 {
			t.Fatal("canceled read succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("fetch did not cancel")
	}
}

func TestCVMFSObjectBounds(t *testing.T) {
	b, client, _ := fixtureCVMFS(t)
	b.transport.maxObject = 128
	r, err := client.Get(b.transport.prefixes[1] + "large")
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.ReadAll(r.Body)
	_ = r.Body.Close()
	if err == nil || !strings.Contains(err.Error(), "object limit") {
		t.Fatal("unbounded response", err)
	}
}

func TestCVMFSConfigurationRejectsUnsafeTargets(t *testing.T) {
	config := CVMFSConfig{Repository: "repo.example", Mirrors: []string{"https://mirror.example/cvmfs"}, MaxObjectBytes: 1024}
	for _, mirror := range []string{"http://mirror.example/cvmfs", "https://user:secret@mirror.example/cvmfs", "https://mirror.example/cvmfs?x=1", "https://mirror.example/other", "https://mirror.example/../cvmfs"} {
		c := config
		c.Mirrors = []string{mirror}
		if _, err := NewCVMFS(t.Context(), c); err == nil {
			t.Fatal("unsafe mirror accepted", mirror)
		}
	}
	for _, name := range []string{"../repo", "repo/other", "repo..example", "repo\x00example"} {
		c := config
		c.Repository = name
		if _, err := NewCVMFS(t.Context(), c); err == nil {
			t.Fatal("unsafe repository accepted", name)
		}
	}
}
