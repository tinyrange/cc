package ccvmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

func TestBootEventWritersDoNotBlockOtherStreams(t *testing.T) {
	blockedResponse := &blockingResponseWriter{
		header:  http.Header{},
		started: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	blocked := newBootEventWriter(blockedResponse)
	blockedDone := make(chan error, 1)
	go func() {
		blockedDone <- blocked.Write(client.BootEvent{Kind: "status", Message: "blocked"})
	}()
	select {
	case <-blockedResponse.started:
	case <-time.After(time.Second):
		t.Fatal("first stream did not begin writing")
	}

	fast := newBootEventWriter(httptest.NewRecorder())
	fastDone := make(chan error, 1)
	go func() {
		fastDone <- fast.Write(client.BootEvent{Kind: "status", Message: "independent"})
	}()
	select {
	case err := <-fastDone:
		if err != nil {
			t.Fatalf("independent stream write: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("independent stream blocked behind another response")
	}
	close(blockedResponse.release)
	if err := <-blockedDone; err != nil {
		t.Fatalf("blocked stream write: %v", err)
	}
}

func TestBootEventWriterSerializesValidNDJSON(t *testing.T) {
	recorder := httptest.NewRecorder()
	writer := newBootEventWriter(recorder)
	const eventCount = 100
	var wg sync.WaitGroup
	errs := make(chan error, eventCount)
	for i := 0; i < eventCount; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- writer.Write(client.BootEvent{Kind: "status", Data: fmt.Sprintf("%d", i)})
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("write event: %v", err)
		}
	}
	seen := make(map[string]bool, eventCount)
	decoder := json.NewDecoder(recorder.Body)
	for {
		var event client.BootEvent
		err := decoder.Decode(&event)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("decode event: %v", err)
		}
		if event.Kind != "status" || seen[event.Data] {
			t.Fatalf("event = %+v", event)
		}
		seen[event.Data] = true
	}
	if len(seen) != eventCount {
		t.Fatalf("decoded events = %d, want %d", len(seen), eventCount)
	}
}

func TestBootEventWriterReturnsDisconnectError(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	response := &contextResponseWriter{header: http.Header{}, ctx: ctx}
	result := make(chan error, 1)
	go func() {
		result <- newBootEventWriter(response).Write(client.BootEvent{Kind: "status"})
	}()
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("write error = %v, want disconnect cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("producer did not return after disconnect")
	}
}

type blockingResponseWriter struct {
	header  http.Header
	started chan struct{}
	release chan struct{}
	buf     bytes.Buffer
}

func (w *blockingResponseWriter) Header() http.Header { return w.header }
func (w *blockingResponseWriter) WriteHeader(int)     {}
func (w *blockingResponseWriter) Write(p []byte) (int, error) {
	select {
	case w.started <- struct{}{}:
	default:
	}
	<-w.release
	return w.buf.Write(p)
}

type contextResponseWriter struct {
	header http.Header
	ctx    context.Context
}

func (w *contextResponseWriter) Header() http.Header { return w.header }
func (w *contextResponseWriter) WriteHeader(int)     {}
func (w *contextResponseWriter) Write([]byte) (int, error) {
	<-w.ctx.Done()
	return 0, w.ctx.Err()
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
