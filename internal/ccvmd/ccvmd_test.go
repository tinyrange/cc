package ccvmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync"
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

func TestMuxPprofEnvironmentCombinations(t *testing.T) {
	for _, tc := range []struct {
		name  string
		debug string
		plain string
		want  int
	}{
		{name: "disabled", want: http.StatusNotFound},
		{name: "debug", debug: "1", want: http.StatusOK},
		{name: "plain", plain: "1", want: http.StatusOK},
		{name: "both", debug: "1", plain: "1", want: http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CCX3_DEBUG_PPROF", tc.debug)
			t.Setenv("CCX3_PPROF", tc.plain)
			watchdog := newWatchdogController(func() {})
			defer watchdog.Stop()

			mux := newMux(&server{vms: vm.NewManagerWithHost(nil)}, watchdog, func() {}, ServerOptions{})
			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil))
			if rr.Code != tc.want {
				t.Fatalf("pprof status = %d, want %d", rr.Code, tc.want)
			}
		})
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

func TestWorkerControlEndpointPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix ownership and mode semantics are not available on Windows")
	}
	t.Run("private parent and socket", func(t *testing.T) {
		root := shortWorkerSocketTempDir(t)
		path := filepath.Join(root, "private", "control.sock")
		network, address, err := workerControlListenEndpoint(path)
		if err != nil {
			t.Fatalf("prepare endpoint: %v", err)
		}
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
		parent := filepath.Join(shortWorkerSocketTempDir(t), "open")
		if err := os.Mkdir(parent, 0o700); err != nil {
			t.Fatalf("create parent: %v", err)
		}
		if err := os.Chmod(parent, 0o755); err != nil {
			t.Fatalf("make parent insecure: %v", err)
		}
		path := filepath.Join(parent, "control.sock")
		if _, _, err := workerControlListenEndpoint(path); err == nil {
			t.Fatal("insecure worker parent was accepted")
		}
		if info, err := os.Stat(parent); err != nil || info.Mode().Perm() != 0o755 {
			t.Fatalf("insecure parent was modified: info=%v err=%v", info, err)
		}
	})

	t.Run("symlink parent", func(t *testing.T) {
		root := shortWorkerSocketTempDir(t)
		target := filepath.Join(root, "target")
		if err := os.Mkdir(target, 0o700); err != nil {
			t.Fatalf("create target: %v", err)
		}
		parent := filepath.Join(root, "linked")
		if err := os.Symlink(target, parent); err != nil {
			t.Fatalf("create parent symlink: %v", err)
		}
		if _, _, err := workerControlListenEndpoint(filepath.Join(parent, "control.sock")); err == nil {
			t.Fatal("symlink worker parent was accepted")
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

func TestWorkerControlRejectsMalformedPayloadsAndRemainsUsable(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	clientCodec := vm.NewWorkerCodec(clientConn)
	serverCodec := vm.NewWorkerCodec(serverConn)
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- serveWorkerControl(serverCodec, &server{vms: vm.NewManagerWithHost(nil)}, ServerOptions{})
	}()
	defer serverConn.Close()

	frameTypes := []string{
		vm.WorkerFrameStart,
		vm.WorkerFrameStartBlank,
		vm.WorkerFrameStatus,
		vm.WorkerFrameStop,
		vm.WorkerFrameWait,
		vm.WorkerFrameFlush,
		vm.WorkerFrameAddShare,
		vm.WorkerFrameConsole,
		vm.WorkerFrameExec,
		vm.WorkerFrameExecInput,
		vm.WorkerFrameCancel,
	}
	for i, frameType := range frameTypes {
		id := uint64(i + 1)
		if err := clientCodec.Send(vm.WorkerFrame{
			ID:      id,
			Service: vm.WorkerServiceControl,
			Type:    frameType,
			Payload: json.RawMessage(`"invalid"`),
		}); err != nil {
			t.Fatalf("send malformed %s frame: %v", frameType, err)
		}
		assertWorkerRequestError(t, clientCodec, id, frameType)
	}

	emptyIDTypes := []string{
		vm.WorkerFrameStart,
		vm.WorkerFrameStartBlank,
		vm.WorkerFrameStatus,
		vm.WorkerFrameStop,
		vm.WorkerFrameWait,
		vm.WorkerFrameFlush,
		vm.WorkerFrameAddShare,
		vm.WorkerFrameConsole,
		vm.WorkerFrameExec,
	}
	for i, frameType := range emptyIDTypes {
		id := uint64(100 + i)
		frame, err := vm.NewWorkerFrame(id, vm.WorkerServiceControl, frameType, map[string]any{})
		if err != nil {
			t.Fatalf("create empty-id %s frame: %v", frameType, err)
		}
		if err := clientCodec.Send(frame); err != nil {
			t.Fatalf("send empty-id %s frame: %v", frameType, err)
		}
		assertWorkerRequestError(t, clientCodec, id, frameType)
	}

	valid, err := vm.NewWorkerFrame(999, vm.WorkerServiceControl, vm.WorkerFrameStatus, vm.WorkerStatusRequest{ID: "named"})
	if err != nil {
		t.Fatalf("create valid status frame: %v", err)
	}
	if err := clientCodec.Send(valid); err != nil {
		t.Fatalf("send valid status frame: %v", err)
	}
	response, err := clientCodec.Receive()
	if err != nil {
		t.Fatalf("receive valid status response: %v", err)
	}
	if response.ID != 999 || response.Type != vm.WorkerFrameDone {
		t.Fatalf("valid response = %+v", response)
	}
	var status vm.WorkerStatusResponse
	if err := response.DecodePayload(&status); err != nil {
		t.Fatalf("decode valid status response: %v", err)
	}
	if status.State.ID != "named" {
		t.Fatalf("valid status targeted %q, want named", status.State.ID)
	}

	if err := clientConn.Close(); err != nil {
		t.Fatalf("close worker client: %v", err)
	}
	select {
	case err := <-serveDone:
		if err != nil {
			t.Fatalf("worker server after client close: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("worker server did not stop after client close")
	}
}

func assertWorkerRequestError(t *testing.T, codec *vm.WorkerCodec, id uint64, requestType string) {
	t.Helper()
	response, err := codec.Receive()
	if err != nil {
		t.Fatalf("receive %s error: %v", requestType, err)
	}
	if response.ID != id || response.Type != vm.WorkerFrameError {
		t.Fatalf("%s error envelope = %+v", requestType, response)
	}
	var workerErr vm.WorkerError
	if err := response.DecodePayload(&workerErr); err != nil {
		t.Fatalf("decode %s error: %v", requestType, err)
	}
	if workerErr.Error == "" || workerErr.RequestID != id || workerErr.RequestType != requestType {
		t.Fatalf("%s structured error = %+v", requestType, workerErr)
	}
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

func TestWatchdogLeaseFeedsPreserveCreatedTimeout(t *testing.T) {
	clock := &fakeWatchdogClock{now: time.Unix(100, 0)}
	expired := 0
	watchdog := newWatchdogController(func() { expired++ })
	watchdog.now = clock.Now
	watchdog.afterFunc = clock.AfterFunc
	defer watchdog.Stop()

	id, err := watchdog.CreateLease(5 * time.Second)
	if err != nil {
		t.Fatalf("create lease: %v", err)
	}
	clock.Advance(4 * time.Second)
	if !watchdog.FeedLease(id) {
		t.Fatal("first lease feed failed")
	}
	clock.Advance(4 * time.Second)
	if expired != 0 {
		t.Fatalf("lease expired before its created timeout after first feed")
	}
	if !watchdog.FeedLease(id) {
		t.Fatal("second lease feed failed")
	}
	clock.Advance(4 * time.Second)
	if expired != 0 {
		t.Fatalf("lease expired before its created timeout after second feed")
	}
	clock.Advance(time.Second)
	if expired != 1 {
		t.Fatalf("expiry callbacks = %d, want 1 at the preserved deadline", expired)
	}
}

type fakeWatchdogClock struct {
	now   time.Time
	timer *fakeWatchdogTimer
}

func (c *fakeWatchdogClock) Now() time.Time { return c.now }

func (c *fakeWatchdogClock) AfterFunc(delay time.Duration, fn func()) watchdogTimer {
	timer := &fakeWatchdogTimer{clock: c, deadline: c.now.Add(delay), fn: fn}
	c.timer = timer
	return timer
}

func (c *fakeWatchdogClock) Advance(delay time.Duration) {
	c.now = c.now.Add(delay)
	if c.timer == nil || c.timer.stopped || c.timer.deadline.After(c.now) {
		return
	}
	c.timer.stopped = true
	c.timer.fn()
}

type fakeWatchdogTimer struct {
	clock    *fakeWatchdogClock
	deadline time.Time
	fn       func()
	stopped  bool
}

func (t *fakeWatchdogTimer) Stop() bool {
	wasActive := !t.stopped
	t.stopped = true
	return wasActive
}

func (t *fakeWatchdogTimer) Reset(delay time.Duration) bool {
	wasActive := !t.stopped
	t.deadline = t.clock.now.Add(delay)
	t.stopped = false
	return wasActive
}

func TestWatchdogLeaseDurationRejectsInvalidValues(t *testing.T) {
	for _, seconds := range []float64{
		-1,
		0,
		math.SmallestNonzeroFloat64,
		math.Inf(1),
		math.Inf(-1),
		math.NaN(),
		1e100,
	} {
		if _, err := watchdogLeaseDuration(seconds); err == nil {
			t.Fatalf("watchdogLeaseDuration(%v) returned no error", seconds)
		}
	}
	if got, err := watchdogLeaseDuration(0.25); err != nil || got != 250*time.Millisecond {
		t.Fatalf("watchdogLeaseDuration(0.25) = %s, %v", got, err)
	}
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
		{name: "overflowing lease timeout", method: http.MethodPost, target: "/watchdog/lease", body: []byte(`{"timeout_seconds":1e100}`), status: http.StatusBadRequest},
		{name: "trailing lease json", method: http.MethodPost, target: "/watchdog/lease", body: []byte(`{"timeout_seconds":1}{}`), status: http.StatusBadRequest},
		{name: "unknown lease field", method: http.MethodPost, target: "/watchdog/lease", body: []byte(`{"timeout_seconds":1,"unexpected":true}`), status: http.StatusBadRequest},
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

func TestMuxRejectsRequestBeforeReadingPastBodyLimit(t *testing.T) {
	watchdog := newWatchdogController(func() {})
	defer watchdog.Stop()
	mux := newMux(&server{vms: vm.NewManagerWithHost(nil)}, watchdog, func() {}, ServerOptions{})
	handler := http.MaxBytesHandler(mux, 8)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/watchdog/lease", bytes.NewReader([]byte(`{"timeout_seconds":1}`))))

	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusRequestEntityTooLarge)
	}
	var apiErr client.ErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&apiErr); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if apiErr.Error == "" {
		t.Fatal("oversized request returned an empty structured error")
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

func TestWaitForWorkerStateReturnsTerminalState(t *testing.T) {
	want := client.InstanceState{ID: "vm", Status: "stopped"}
	got, completed := waitForWorkerState(t.Context(), func() client.InstanceState { return want })
	if !completed || got != want {
		t.Fatalf("wait result = %+v, %t", got, completed)
	}
}

func TestWaitForWorkerStateCancellationReleasesAllWaiters(t *testing.T) {
	const waiterCount = 100
	ctx, cancel := context.WithCancel(t.Context())
	started := make(chan struct{}, waiterCount)
	done := make(chan struct{}, waiterCount)
	for range waiterCount {
		go func() {
			var once sync.Once
			_, completed := waitForWorkerState(ctx, func() client.InstanceState {
				once.Do(func() { started <- struct{}{} })
				return client.InstanceState{Status: "running"}
			})
			if completed {
				t.Errorf("canceled wait reported completion")
			}
			done <- struct{}{}
		}()
	}
	for range waiterCount {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("waiter did not start")
		}
	}
	cancel()
	for range waiterCount {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("waiter did not stop after cancellation")
		}
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
