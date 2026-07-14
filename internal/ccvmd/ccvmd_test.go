package ccvmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
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
