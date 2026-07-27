package ccvmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/oci"
	"j5.nz/cc/internal/vm"
)

type orderedResponseRecorder struct {
	*httptest.ResponseRecorder
	operations []string
}

func (r *orderedResponseRecorder) Write(body []byte) (int, error) {
	r.operations = append(r.operations, "write")
	return r.ResponseRecorder.Write(body)
}

func (r *orderedResponseRecorder) Flush() {
	r.operations = append(r.operations, "flush")
	r.ResponseRecorder.Flush()
}

func TestImagePullStreamFlushesHeadersBeforeFirstProgressEvent(t *testing.T) {
	watchdog := newWatchdogController(func() {})
	defer watchdog.Stop()
	handler := newMux(&server{
		images: oci.NewStore(filepath.Join(t.TempDir(), "images")),
		vms:    vm.NewManagerWithHost(nil),
	}, watchdog, func() {}, ServerOptions{})

	body, err := json.Marshal(client.PullImageRequest{
		Source: "docker-archive:" + filepath.Join(t.TempDir(), "missing.tar") + "#test:latest",
	})
	if err != nil {
		t.Fatalf("encode request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/image/test?stream=1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	recorder := &orderedResponseRecorder{ResponseRecorder: httptest.NewRecorder()}
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("response status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if len(recorder.operations) < 2 {
		t.Fatalf("stream operations = %v, want header flush followed by an event", recorder.operations)
	}
	if recorder.operations[0] != "flush" || recorder.operations[1] != "write" {
		t.Fatalf("stream operations = %v, want flush before first event write", recorder.operations)
	}
}
