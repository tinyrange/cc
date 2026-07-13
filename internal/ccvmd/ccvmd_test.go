package ccvmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/vm"
)

func TestMuxHealthStatusWatchdogAndShutdown(t *testing.T) {
	shutdownCalled := make(chan struct{}, 1)
	watchdog := newWatchdogController(func() {})
	defer watchdog.Stop()
	mux := newMux(&server{vms: vm.NewManagerWithHost(nil)}, watchdog, func() {
		shutdownCalled <- struct{}{}
	}, ServerOptions{})

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("health status = %d body=%s", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/vm/status?id=alpha", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("vm status code = %d body=%s", rr.Code, rr.Body.String())
	}
	var state client.InstanceState
	if err := json.NewDecoder(rr.Body).Decode(&state); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if state.ID != "alpha" || state.Status != "stopped" {
		t.Fatalf("state = %+v", state)
	}

	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/watchdog/feed", nil))
	if rr.Code != http.StatusConflict {
		t.Fatalf("feed before create status = %d body=%s", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/watchdog/lease", jsonBody(t, client.WatchdogLeaseRequest{TimeoutSeconds: 0.25})))
	if rr.Code != http.StatusOK {
		t.Fatalf("lease status = %d body=%s", rr.Code, rr.Body.String())
	}
	var lease client.WatchdogLeaseResponse
	if err := json.NewDecoder(rr.Body).Decode(&lease); err != nil {
		t.Fatalf("decode lease: %v", err)
	}
	if lease.LeaseID == "" || lease.TimeoutSeconds != 0.25 {
		t.Fatalf("lease = %+v", lease)
	}

	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/watchdog/lease/release", jsonBody(t, client.WatchdogLeaseRequest{LeaseID: lease.LeaseID})))
	if rr.Code != http.StatusOK {
		t.Fatalf("release status = %d body=%s", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/shutdown", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("shutdown status = %d body=%s", rr.Code, rr.Body.String())
	}
	select {
	case <-shutdownCalled:
	case <-time.After(time.Second):
		t.Fatalf("shutdown callback was not called")
	}
}

func TestWorkerControlSocketPreparationProtectsExistingPaths(t *testing.T) {
	t.Run("regular file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "control.sock")
		if err := os.WriteFile(path, []byte("keep"), 0o600); err != nil {
			t.Fatalf("write regular file: %v", err)
		}
		if _, _, err := workerControlListenEndpoint(path); err == nil {
			t.Fatal("regular worker path was accepted")
		}
		if data, err := os.ReadFile(path); err != nil || string(data) != "keep" {
			t.Fatalf("regular worker path changed: data=%q err=%v", data, err)
		}
	})

	t.Run("directory", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "control.sock")
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatalf("create directory: %v", err)
		}
		if _, _, err := workerControlListenEndpoint(path); err == nil {
			t.Fatal("directory worker path was accepted")
		}
		if info, err := os.Stat(path); err != nil || !info.IsDir() {
			t.Fatalf("worker directory changed: info=%v err=%v", info, err)
		}
	})

	t.Run("symlink", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("symlink creation requires additional Windows privileges")
		}
		dir := t.TempDir()
		target := filepath.Join(dir, "target")
		if err := os.WriteFile(target, []byte("keep"), 0o600); err != nil {
			t.Fatalf("write symlink target: %v", err)
		}
		path := filepath.Join(dir, "control.sock")
		if err := os.Symlink(target, path); err != nil {
			t.Fatalf("create symlink: %v", err)
		}
		if _, _, err := workerControlListenEndpoint(path); err == nil {
			t.Fatal("symlink worker path was accepted")
		}
		if data, err := os.ReadFile(target); err != nil || string(data) != "keep" {
			t.Fatalf("symlink target changed: data=%q err=%v", data, err)
		}
		if info, err := os.Lstat(path); err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("worker symlink changed: info=%v err=%v", info, err)
		}
	})
}

func TestWorkerControlSocketDetectsLiveAndRecoversStaleEndpoint(t *testing.T) {
	dir := shortWorkerSocketTempDir(t)
	livePath := filepath.Join(dir, "live.sock")
	live := listenUnixWithoutAutomaticUnlink(t, livePath)
	defer func() {
		_ = live.Close()
		_ = os.Remove(livePath)
	}()
	if _, _, err := workerControlListenEndpoint(livePath); err == nil {
		t.Fatal("live worker socket was treated as stale")
	}
	if info, err := os.Lstat(livePath); err != nil || info.Mode()&os.ModeSocket == 0 {
		t.Fatalf("live worker socket changed: info=%v err=%v", info, err)
	}

	stalePath := filepath.Join(dir, "stale.sock")
	stale := listenUnixWithoutAutomaticUnlink(t, stalePath)
	if err := stale.Close(); err != nil {
		t.Fatalf("close stale listener: %v", err)
	}
	network, address, err := workerControlListenEndpoint(stalePath)
	if err != nil {
		t.Fatalf("recover stale worker socket: %v", err)
	}
	if network != "unix" || address != stalePath {
		t.Fatalf("recovered endpoint = %q %q", network, address)
	}
	if _, err := os.Lstat(stalePath); !os.IsNotExist(err) {
		t.Fatalf("stale socket still exists: %v", err)
	}
	recovered := listenUnixWithoutAutomaticUnlink(t, stalePath)
	_ = recovered.Close()
	_ = os.Remove(stalePath)
}

func TestWorkerControlSocketCleanupDoesNotRemoveReplacement(t *testing.T) {
	path := filepath.Join(shortWorkerSocketTempDir(t), "control.sock")
	listener := listenUnixWithoutAutomaticUnlink(t, path)
	cleanup, err := workerControlSocketCleanup("unix", path)
	if err != nil {
		t.Fatalf("capture socket cleanup: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove owned socket path: %v", err)
	}
	if err := os.WriteFile(path, []byte("replacement"), 0o600); err != nil {
		t.Fatalf("write replacement: %v", err)
	}
	cleanup()
	if err := listener.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "replacement" {
		t.Fatalf("replacement changed by cleanup: data=%q err=%v", data, err)
	}
}

func listenUnixWithoutAutomaticUnlink(t *testing.T, path string) net.Listener {
	t.Helper()
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen on Unix socket: %v", err)
	}
	if unixListener, ok := listener.(*net.UnixListener); ok {
		unixListener.SetUnlinkOnClose(false)
	}
	return listener
}

func shortWorkerSocketTempDir(t *testing.T) string {
	t.Helper()
	base := os.TempDir()
	if runtime.GOOS != "windows" {
		base = "/tmp"
	}
	dir, err := os.MkdirTemp(base, "ccvmd-")
	if err != nil {
		t.Fatalf("create short socket temp directory: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func TestPersistentWatchdogDoesNotShutdownWhenLeasesEnd(t *testing.T) {
	shutdownCalled := make(chan struct{}, 1)
	watchdog := newPersistentWatchdogController(func() {
		shutdownCalled <- struct{}{}
	})
	defer watchdog.Stop()

	leaseID, err := watchdog.CreateLease(10 * time.Millisecond)
	if err != nil {
		t.Fatalf("create lease: %v", err)
	}
	if !watchdog.ReleaseLease(leaseID) {
		t.Fatalf("release lease failed")
	}
	assertNoShutdown(t, shutdownCalled)

	if _, err := watchdog.CreateLease(10 * time.Millisecond); err != nil {
		t.Fatalf("create expiring lease: %v", err)
	}
	time.Sleep(30 * time.Millisecond)
	assertNoShutdown(t, shutdownCalled)
}

func assertNoShutdown(t *testing.T, shutdownCalled <-chan struct{}) {
	t.Helper()
	select {
	case <-shutdownCalled:
		t.Fatalf("persistent watchdog called shutdown")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestMuxBadRequests(t *testing.T) {
	watchdog := newWatchdogController(func() {})
	defer watchdog.Stop()
	mux := newMux(&server{vms: vm.NewManagerWithHost(nil)}, watchdog, func() {}, ServerOptions{})

	for _, tc := range []struct {
		name   string
		method string
		target string
		body   []byte
		status int
	}{
		{name: "bad lease json", method: http.MethodPost, target: "/watchdog/lease", body: []byte("{"), status: http.StatusBadRequest},
		{name: "bad run json", method: http.MethodPost, target: "/vm/run", body: []byte("{"), status: http.StatusBadRequest},
		{name: "forward stopped vm", method: http.MethodPost, target: "/vm/forward?id=alpha", body: mustJSON(t, client.PortForward{HostPort: 8080, GuestPort: 80}), status: http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, httptest.NewRequest(tc.method, tc.target, bytes.NewReader(tc.body)))
			if rr.Code != tc.status {
				t.Fatalf("status = %d want %d body=%s", rr.Code, tc.status, rr.Body.String())
			}
			var apiErr client.ErrorResponse
			if err := json.NewDecoder(rr.Body).Decode(&apiErr); err != nil {
				t.Fatalf("decode error response: %v", err)
			}
			if apiErr.Error == "" {
				t.Fatalf("empty error response: %s", rr.Body.String())
			}
		})
	}
}

func TestMuxNormalizesRunRequestsAfterDecode(t *testing.T) {
	watchdog := newWatchdogController(func() {})
	defer watchdog.Stop()
	var normalized client.RunRequest
	mux := newMux(&server{vms: vm.NewManagerWithHost(nil)}, watchdog, func() {}, ServerOptions{
		NormalizeRunRequest: func(req *client.RunRequest, _ RuntimeView) error {
			req.MemoryMB = 4096
			req.BalloonMB = 512
			normalized = *req
			return fmt.Errorf("stop after normalize")
		},
	})

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/vm/run", jsonBody(t, client.RunRequest{Command: []string{"true"}})))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want bad request", rr.Code)
	}
	if len(normalized.Command) != 1 || normalized.Command[0] != "true" || normalized.MemoryMB != 4096 || normalized.BalloonMB != 512 {
		t.Fatalf("normalized request = %+v", normalized)
	}
}

func TestSanitizeExecEventForJSONUsesTextOrBinary(t *testing.T) {
	text := sanitizeExecEventForJSON(client.ExecEvent{
		Kind:   "stdout",
		Output: "hello\n",
		Data:   []byte("hello\n"),
	})
	if text.Output != "hello\n" {
		t.Fatalf("text output = %q", text.Output)
	}
	if len(text.Data) != 0 {
		t.Fatalf("text data was not cleared: %q", string(text.Data))
	}

	binary := sanitizeExecEventForJSON(client.ExecEvent{
		Kind:   "stdout",
		Output: "\xff\x00",
		Data:   []byte{0xff, 0x00},
	})
	if binary.Output != "" {
		t.Fatalf("binary output = %q", binary.Output)
	}
	if !bytes.Equal(binary.Data, []byte{0xff, 0x00}) {
		t.Fatalf("binary data = %v", binary.Data)
	}

	encoded := mustJSON(t, binary)
	var decoded client.ExecEvent
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("decode sanitized binary event: %v json=%s", err, string(encoded))
	}
	if !bytes.Equal(decoded.Data, []byte{0xff, 0x00}) {
		t.Fatalf("decoded data = %v", decoded.Data)
	}
}

func jsonBody(t *testing.T, value any) *bytes.Reader {
	t.Helper()
	return bytes.NewReader(mustJSON(t, value))
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return data
}
