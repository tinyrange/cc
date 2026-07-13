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

func TestWorkerControlEndpointPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix ownership and mode semantics are not available on Windows")
	}
	t.Run("private parent and socket", func(t *testing.T) {
		root := shortCCVMDSocketDir(t)
		path := filepath.Join(root, "private", "control.sock")
		network, address, cleanup, err := workerControlListenEndpoint(path)
		if err != nil {
			t.Fatalf("prepare endpoint: %v", err)
		}
		defer cleanup()
		parentInfo, err := os.Lstat(filepath.Dir(path))
		if err != nil {
			t.Fatalf("stat parent: %v", err)
		}
		if parentInfo.Mode().Perm() != 0o700 {
			t.Fatalf("parent permissions = %04o, want 0700", parentInfo.Mode().Perm())
		}

		listener, err := net.Listen(network, address)
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		defer listener.Close()
		if err := secureWorkerControlSocket(path); err != nil {
			t.Fatalf("secure socket: %v", err)
		}
		socketInfo, err := os.Lstat(path)
		if err != nil {
			t.Fatalf("stat socket: %v", err)
		}
		if socketInfo.Mode().Perm() != 0o600 || socketInfo.Mode()&os.ModeSocket == 0 {
			t.Fatalf("socket mode = %v, want owner-only socket", socketInfo.Mode())
		}
	})

	t.Run("insecure existing parent", func(t *testing.T) {
		parent := filepath.Join(shortCCVMDSocketDir(t), "open")
		if err := os.Mkdir(parent, 0o700); err != nil {
			t.Fatalf("create parent: %v", err)
		}
		if err := os.Chmod(parent, 0o755); err != nil {
			t.Fatalf("make parent insecure: %v", err)
		}
		path := filepath.Join(parent, "control.sock")
		if _, _, _, err := workerControlListenEndpoint(path); err == nil {
			t.Fatal("insecure worker parent was accepted")
		}
		if info, err := os.Stat(parent); err != nil || info.Mode().Perm() != 0o755 {
			t.Fatalf("insecure parent was modified: info=%v err=%v", info, err)
		}
	})

	t.Run("symlink parent", func(t *testing.T) {
		root := shortCCVMDSocketDir(t)
		target := filepath.Join(root, "target")
		if err := os.Mkdir(target, 0o700); err != nil {
			t.Fatalf("create target: %v", err)
		}
		parent := filepath.Join(root, "linked")
		if err := os.Symlink(target, parent); err != nil {
			t.Fatalf("create parent symlink: %v", err)
		}
		if _, _, _, err := workerControlListenEndpoint(filepath.Join(parent, "control.sock")); err == nil {
			t.Fatal("symlink worker parent was accepted")
		}
	})
}

func shortCCVMDSocketDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "ccvmd-")
	if err != nil {
		t.Fatalf("create short socket directory: %v", err)
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
