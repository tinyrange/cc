package main

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/websocket"
	"j5.nz/cc/client"
	"j5.nz/cc/internal/oci"
	"j5.nz/cc/internal/vm"
)

func TestWantsExecEventStream(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/vm/run?stream=1", nil)
	if !wantsExecEventStream(req) {
		t.Fatal("wantsExecEventStream(query) = false, want true")
	}

	req = httptest.NewRequest(http.MethodPost, "/vm/run", nil)
	req.Header.Set("Accept", "application/x-ndjson")
	if !wantsExecEventStream(req) {
		t.Fatal("wantsExecEventStream(accept) = false, want true")
	}

	req = httptest.NewRequest(http.MethodPost, "/vm/run", nil)
	if wantsExecEventStream(req) {
		t.Fatal("wantsExecEventStream(default) = true, want false")
	}
}

func TestWantsProgressStream(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/kernel/download?stream=1", nil)
	if !wantsProgressStream(req) {
		t.Fatal("wantsProgressStream(query) = false, want true")
	}

	req = httptest.NewRequest(http.MethodPost, "/kernel/download", nil)
	req.Header.Set("Accept", "application/x-ndjson")
	if !wantsProgressStream(req) {
		t.Fatal("wantsProgressStream(accept) = false, want true")
	}

	req = httptest.NewRequest(http.MethodPost, "/kernel/download", nil)
	if wantsProgressStream(req) {
		t.Fatal("wantsProgressStream(default) = true, want false")
	}
}

func TestResolveVMBootTimeout(t *testing.T) {
	t.Setenv("CCX3_VM_BOOT_TIMEOUT", "")
	if got := resolveVMBootTimeout(); got != 5*time.Second {
		t.Fatalf("resolveVMBootTimeout(default) = %s, want 5s", got)
	}

	t.Setenv("CCX3_VM_BOOT_TIMEOUT", "1.5")
	if got := resolveVMBootTimeout(); got != 1500*time.Millisecond {
		t.Fatalf("resolveVMBootTimeout(env) = %s, want 1.5s", got)
	}

	t.Setenv("CCX3_VM_BOOT_TIMEOUT", "nope")
	if got := resolveVMBootTimeout(); got != 5*time.Second {
		t.Fatalf("resolveVMBootTimeout(invalid) = %s, want 5s", got)
	}
}

func TestWriteStartupError(t *testing.T) {
	var buf bytes.Buffer
	if err := writeStartupError(&buf, errors.New("listen on localhost: bind failed")); err != nil {
		t.Fatalf("writeStartupError() error = %v", err)
	}
	var hello client.ServerHello
	if err := json.Unmarshal(buf.Bytes(), &hello); err != nil {
		t.Fatalf("Unmarshal(startup error) error = %v", err)
	}
	if hello.Kind != "error" || hello.Error != "ccvm failed to start" {
		t.Fatalf("startup error = %#v", hello)
	}
	if !strings.Contains(hello.Detail, "bind failed") {
		t.Fatalf("startup detail = %q", hello.Detail)
	}
}

func TestBootTimeoutFromRequest(t *testing.T) {
	t.Setenv("CCX3_VM_BOOT_TIMEOUT", "2.5")
	if got := bootTimeoutFromRequest(0); got != 2500*time.Millisecond {
		t.Fatalf("bootTimeoutFromRequest(0) = %s, want 2.5s", got)
	}
	if got := bootTimeoutFromRequest(300); got != 5*time.Minute {
		t.Fatalf("bootTimeoutFromRequest(300) = %s, want 5m", got)
	}
}

func TestRunRequestContextUsesRequestTimeout(t *testing.T) {
	ctx, cancel := runRequestContext(context.Background(), client.RunRequest{TimeoutSeconds: 0.01})
	defer cancel()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("run request context did not time out")
	}
	if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("run request context error = %v, want deadline exceeded", ctx.Err())
	}
}

func TestWriteExecEventStream(t *testing.T) {
	rec := httptest.NewRecorder()
	writeExecEventStream(rec, client.ExecResponse{ExitCode: 7, Output: "hello"})

	if got := rec.Header().Get("Content-Type"); got != "application/x-ndjson" {
		t.Fatalf("Content-Type = %q, want application/x-ndjson", got)
	}

	lines := strings.Split(strings.TrimSpace(rec.Body.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("event line count = %d, want 2", len(lines))
	}

	var outputEvent client.ExecEvent
	if err := json.Unmarshal([]byte(lines[0]), &outputEvent); err != nil {
		t.Fatalf("Unmarshal(output event) error = %v", err)
	}
	if outputEvent.Kind != "output" || outputEvent.Output != "hello" {
		t.Fatalf("output event = %#v, want kind=output output=hello", outputEvent)
	}

	var exitEvent client.ExecEvent
	if err := json.Unmarshal([]byte(lines[1]), &exitEvent); err != nil {
		t.Fatalf("Unmarshal(exit event) error = %v", err)
	}
	if exitEvent.Kind != "exit" || exitEvent.ExitCode != 7 {
		t.Fatalf("exit event = %#v, want kind=exit exit_code=7", exitEvent)
	}
}

func TestSanitizeExecEventForJSONClearsInvalidTextOutput(t *testing.T) {
	event := sanitizeExecEventForJSON(client.ExecEvent{
		Kind:   "stdout",
		Stream: "stdout",
		Output: string([]byte{0xff, 0x00, 'x'}),
		Data:   []byte{0xff, 0x00, 'x'},
	})

	if event.Output != "" {
		t.Fatalf("Output = %q, want empty for non-UTF-8 binary data", event.Output)
	}
	if string(event.Data) != string([]byte{0xff, 0x00, 'x'}) {
		t.Fatalf("Data = %v, want preserved binary data", event.Data)
	}
}

func TestSanitizeExecEventForJSONKeepsTextOutput(t *testing.T) {
	event := sanitizeExecEventForJSON(client.ExecEvent{
		Kind:   "stdout",
		Stream: "stdout",
		Output: "hello\n",
		Data:   []byte("hello\n"),
	})

	if event.Output != "hello\n" {
		t.Fatalf("Output = %q, want preserved text output", event.Output)
	}
}

func TestWriteProgressEvent(t *testing.T) {
	rec := httptest.NewRecorder()
	if err := writeProgressEvent(rec, client.ProgressEvent{
		Status:             "downloading",
		Artifact:           "linux-virt.apk",
		BytesDownloaded:    1024,
		BytesTotal:         4096,
		RateBytesPerSecond: 512,
		ETASeconds:         6,
	}); err != nil {
		t.Fatalf("writeProgressEvent() error = %v", err)
	}

	if got := rec.Header().Get("Content-Type"); got != "application/x-ndjson" {
		t.Fatalf("Content-Type = %q, want application/x-ndjson", got)
	}

	var event client.ProgressEvent
	if err := json.Unmarshal(bytes.TrimSpace(rec.Body.Bytes()), &event); err != nil {
		t.Fatalf("Unmarshal(progress event) error = %v", err)
	}
	if event.Status != "downloading" || event.Artifact != "linux-virt.apk" {
		t.Fatalf("progress event = %#v", event)
	}
	if event.BytesDownloaded != 1024 || event.BytesTotal != 4096 {
		t.Fatalf("progress bytes = %#v", event)
	}
}

func TestCapabilitiesEndpoint(t *testing.T) {
	srv := &server{vms: vm.NewManager()}
	mux := newMux(srv, nil, func() {})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/capabilities", nil)

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /capabilities status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var caps client.CapabilitiesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &caps); err != nil {
		t.Fatalf("Unmarshal(capabilities) error = %v", err)
	}
	if caps.Host == "" || caps.Backend == "" || caps.MaxInstances == 0 || !caps.SupportsMultiImageExec {
		t.Fatalf("capabilities = %#v", caps)
	}
}

func TestWatchdogEndpointsCreateAndFeed(t *testing.T) {
	expired := make(chan struct{}, 1)
	watchdog := newWatchdogController(func() { expired <- struct{}{} })
	defer watchdog.Stop()
	mux := newMux(&server{}, watchdog, func() {})

	createReq := httptest.NewRequest(http.MethodPost, "/watchdog", strings.NewReader(`{"timeout_seconds":10}`))
	createRec := httptest.NewRecorder()
	mux.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("POST /watchdog status = %d, body = %s", createRec.Code, createRec.Body.String())
	}

	feedReq := httptest.NewRequest(http.MethodPost, "/watchdog/feed", nil)
	feedRec := httptest.NewRecorder()
	mux.ServeHTTP(feedRec, feedReq)
	if feedRec.Code != http.StatusOK {
		t.Fatalf("POST /watchdog/feed status = %d, body = %s", feedRec.Code, feedRec.Body.String())
	}
}

func TestWatchdogExpiresWithoutFeed(t *testing.T) {
	expired := make(chan struct{}, 1)
	watchdog := newWatchdogController(func() { expired <- struct{}{} })
	defer watchdog.Stop()

	watchdog.Create(20 * time.Millisecond)

	select {
	case <-expired:
	case <-time.After(time.Second):
		t.Fatal("watchdog did not expire")
	}
	if watchdog.Feed() {
		t.Fatal("Feed() after expiry = true, want false")
	}
}

func TestServeRunWebSocket(t *testing.T) {
	ts := httptest.NewServer(websocket.Server{
		Handshake: func(*websocket.Config, *http.Request) error { return nil },
		Handler: func(ws *websocket.Conn) {
			serveRunWebSocket(ws, func(_ context.Context, req client.ExecRequest, inputs <-chan client.ExecInput, onEvent func(client.ExecEvent) error) error {
				if got := strings.Join(req.Command, " "); got != "echo hi" {
					t.Fatalf("command = %q, want %q", got, "echo hi")
				}
				var gotInput client.ExecInput
				select {
				case gotInput = <-inputs:
				case <-time.After(time.Second):
					t.Fatal("timed out waiting for stdin input")
				}
				if gotInput.Kind != "stdin" || string(gotInput.Data) != "yo" {
					t.Fatalf("input = %#v, want stdin yo", gotInput)
				}
				if err := onEvent(client.ExecEvent{Kind: "stdout", Stream: "stdout", Output: "hi", Data: []byte("hi")}); err != nil {
					return err
				}
				return onEvent(client.ExecEvent{Kind: "exit", ExitCode: 0})
			})
		},
	})
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")
	ws, err := websocket.Dial(wsURL, "", ts.URL)
	if err != nil {
		t.Fatalf("websocket.Dial() error = %v", err)
	}
	defer ws.Close()

	if err := websocket.JSON.Send(ws, client.ExecRequest{Command: []string{"echo", "hi"}}); err != nil {
		t.Fatalf("JSON.Send(req) error = %v", err)
	}
	if err := websocket.JSON.Send(ws, client.ExecInput{Kind: "stdin", Data: []byte("yo")}); err != nil {
		t.Fatalf("JSON.Send(input) error = %v", err)
	}

	var outputEvent client.ExecEvent
	if err := websocket.JSON.Receive(ws, &outputEvent); err != nil {
		t.Fatalf("JSON.Receive(output) error = %v", err)
	}
	if outputEvent.Kind != "stdout" || outputEvent.Output != "hi" {
		t.Fatalf("output event = %#v, want output hi", outputEvent)
	}

	var exitEvent client.ExecEvent
	if err := websocket.JSON.Receive(ws, &exitEvent); err != nil {
		t.Fatalf("JSON.Receive(exit) error = %v", err)
	}
	if exitEvent.Kind != "exit" || exitEvent.ExitCode != 0 {
		t.Fatalf("exit event = %#v, want exit 0", exitEvent)
	}
}

func TestServeRunWebSocketCancelsRunnerOnClientDisconnect(t *testing.T) {
	cancelled := make(chan struct{}, 1)
	runnerDone := make(chan struct{}, 1)
	ts := httptest.NewServer(websocket.Server{
		Handshake: func(*websocket.Config, *http.Request) error { return nil },
		Handler: func(ws *websocket.Conn) {
			serveRunWebSocket(ws, func(ctx context.Context, req client.ExecRequest, inputs <-chan client.ExecInput, onEvent func(client.ExecEvent) error) error {
				_ = req
				_ = inputs
				_ = onEvent
				defer func() { runnerDone <- struct{}{} }()
				select {
				case <-ctx.Done():
					cancelled <- struct{}{}
					return ctx.Err()
				case <-time.After(time.Second):
					t.Error("runner context was not cancelled")
					return nil
				}
			})
		},
	})
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")
	ws, err := websocket.Dial(wsURL, "", ts.URL)
	if err != nil {
		t.Fatalf("websocket.Dial() error = %v", err)
	}
	if err := websocket.JSON.Send(ws, client.ExecRequest{Command: []string{"sleep", "1000"}}); err != nil {
		t.Fatalf("JSON.Send(req) error = %v", err)
	}
	if err := ws.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("runner context was not cancelled after client disconnect")
	}
	select {
	case <-runnerDone:
	case <-time.After(time.Second):
		t.Fatal("runner did not return after client disconnect")
	}
}

func TestPullImageRequestAcceptsStructuredSource(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/image/niimath", strings.NewReader(`{
		"source": {
			"type": "cvmfs",
			"mirror": "https://cvmfs.neurodesk.org",
			"repo": "neurodesk.ardc.edu.au",
			"path": "/containers/niimath_1.0.20250804_20251016"
		},
		"cache_dir": "/tmp/cvmfs-cache"
	}`))

	var parsed client.PullImageRequest
	if err := decodeRequiredJSON(req, &parsed); err != nil {
		t.Fatalf("decodeRequiredJSON() error = %v", err)
	}
	source, err := parsed.SourceString()
	if err != nil {
		t.Fatalf("SourceString() error = %v", err)
	}
	if source != "https://cvmfs.neurodesk.org/cvmfs/neurodesk.ardc.edu.au/containers/niimath_1.0.20250804_20251016" {
		t.Fatalf("SourceString() = %q", source)
	}
	if parsed.CacheDir != "/tmp/cvmfs-cache" {
		t.Fatalf("CacheDir = %q, want /tmp/cvmfs-cache", parsed.CacheDir)
	}
}

func TestCVMFSEndpointsSupportCacheDir(t *testing.T) {
	repoServer := newHTTPTestRepoServer(t)
	cacheDir := t.TempDir()
	mux := newMux(&server{}, nil, func() {})

	listBody := `{"mirror":"` + repoServer.URL + `/cvmfs","repo":"test.repo","path":"/containers/niimath","cache_dir":"` + cacheDir + `"}`
	listReq := httptest.NewRequest(http.MethodPost, "/cvmfs/list", strings.NewReader(listBody))
	listRec := httptest.NewRecorder()
	mux.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("POST /cvmfs/list status = %d, body = %s", listRec.Code, listRec.Body.String())
	}
	var listResp client.CVMFSListResponse
	if err := json.Unmarshal(listRec.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("Unmarshal(list response) error = %v", err)
	}
	if len(listResp.Entries) != 1 || listResp.Entries[0].Path != "/containers/niimath/niimath" {
		t.Fatalf("list response = %#v", listResp)
	}

	fullReadBody := `{"mirror":"` + repoServer.URL + `/cvmfs","repo":"test.repo","path":"/containers/niimath/niimath","cache_dir":"` + cacheDir + `"}`
	fullReadReq := httptest.NewRequest(http.MethodPost, "/cvmfs/read", strings.NewReader(fullReadBody))
	fullReadRec := httptest.NewRecorder()
	mux.ServeHTTP(fullReadRec, fullReadReq)
	if fullReadRec.Code != http.StatusOK {
		t.Fatalf("full POST /cvmfs/read status = %d, body = %s", fullReadRec.Code, fullReadRec.Body.String())
	}

	readBody := `{"mirror":"` + repoServer.URL + `/cvmfs","repo":"test.repo","path":"/containers/niimath/niimath","offset":0,"length":6,"cache_dir":"` + cacheDir + `"}`
	readReq := httptest.NewRequest(http.MethodPost, "/cvmfs/read", strings.NewReader(readBody))
	readRec := httptest.NewRecorder()
	mux.ServeHTTP(readRec, readReq)
	if readRec.Code != http.StatusOK {
		t.Fatalf("POST /cvmfs/read status = %d, body = %s", readRec.Code, readRec.Body.String())
	}
	var readResp client.CVMFSReadResponse
	if err := json.Unmarshal(readRec.Body.Bytes(), &readResp); err != nil {
		t.Fatalf("Unmarshal(read response) error = %v", err)
	}
	if string(readResp.Data) != "niimat" {
		t.Fatalf("read data = %q, want %q", string(readResp.Data), "niimat")
	}

	if _, err := os.Stat(filepath.Join(cacheDir, "state", "test.repo", "manifest")); err != nil {
		t.Fatalf("manifest cache missing: %v", err)
	}

	repoServer.Close()

	cachedReq := httptest.NewRequest(http.MethodPost, "/cvmfs/read", strings.NewReader(readBody))
	cachedRec := httptest.NewRecorder()
	mux.ServeHTTP(cachedRec, cachedReq)
	if cachedRec.Code != http.StatusOK {
		t.Fatalf("cached POST /cvmfs/read status = %d, body = %s", cachedRec.Code, cachedRec.Body.String())
	}
	var cachedResp client.CVMFSReadResponse
	if err := json.Unmarshal(cachedRec.Body.Bytes(), &cachedResp); err != nil {
		t.Fatalf("Unmarshal(cached read response) error = %v", err)
	}
	if string(cachedResp.Data) != "niimat" {
		t.Fatalf("cached read data = %q, want %q", string(cachedResp.Data), "niimat")
	}
}

func TestPostImageImportsDirectoryBackedCVMFSContainer(t *testing.T) {
	repoServer := newHTTPDirectoryRepoServer(t)
	srv := &server{images: oci.NewStore(t.TempDir())}
	mux := newMux(srv, nil, func() {})

	req := httptest.NewRequest(http.MethodPost, "/image/niimath", strings.NewReader(`{
		"source": {
			"type": "cvmfs",
			"mirror": "`+repoServer.URL+`/cvmfs",
			"repo": "test.repo",
			"path": "/containers/niimath_1.0.20250804_20251016"
		}
	}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /image/niimath status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var state client.ImageState
	if err := json.Unmarshal(rec.Body.Bytes(), &state); err != nil {
		t.Fatalf("Unmarshal(image state) error = %v", err)
	}
	if state.SourceKind != "cvmfs" || state.Status != "downloaded" {
		t.Fatalf("image state = %#v", state)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/image/niimath", nil)
	getRec := httptest.NewRecorder()
	mux.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET /image/niimath status = %d, body = %s", getRec.Code, getRec.Body.String())
	}
}

func TestPostImageStreamsProgressForDirectoryBackedCVMFSContainer(t *testing.T) {
	repoServer := newHTTPDirectoryRepoServer(t)
	srv := &server{images: oci.NewStore(t.TempDir())}
	mux := newMux(srv, nil, func() {})

	req := httptest.NewRequest(http.MethodPost, "/image/niimath?stream=1", strings.NewReader(`{
		"source": {
			"type": "cvmfs",
			"mirror": "`+repoServer.URL+`/cvmfs",
			"repo": "test.repo",
			"path": "/containers/niimath_1.0.20250804_20251016"
		},
		"prefetch": true,
		"prefetch_workers": 2
	}`))
	req.Header.Set("Accept", "application/x-ndjson")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /image/niimath?stream=1 status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/x-ndjson" {
		t.Fatalf("Content-Type = %q, want application/x-ndjson", got)
	}
	lines := strings.Split(strings.TrimSpace(rec.Body.String()), "\n")
	if len(lines) < 2 {
		t.Fatalf("streamed event line count = %d, want at least 2", len(lines))
	}
	var last client.ProgressEvent
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &last); err != nil {
		t.Fatalf("Unmarshal(last progress event) error = %v", err)
	}
	if last.Status != "downloaded" {
		t.Fatalf("last progress event = %#v", last)
	}
}

func newHTTPTestRepoServer(t *testing.T) *httptest.Server {
	t.Helper()

	rootCatalogHash := strings.Repeat("1", 40)
	nestedCatalogHash := strings.Repeat("2", 40)
	fileHash := strings.Repeat("3", 40)

	rootCatalog := createHTTPTestCatalogDB(t, []string{
		`CREATE TABLE catalog (md5path_1 INTEGER, md5path_2 INTEGER, parent_1 INTEGER, parent_2 INTEGER, hardlinks INTEGER, hash BLOB, size INTEGER, mode INTEGER, mtime INTEGER, mtimens INTEGER, flags INTEGER, name TEXT, symlink TEXT, uid INTEGER, gid INTEGER, xattr BLOB);`,
		`CREATE TABLE nested_catalogs (path TEXT, sha1 TEXT, size INTEGER);`,
		`INSERT INTO catalog VALUES (1, 1, 0, 0, 1, NULL, 0, 16877, 0, 0, 1, 'containers', '', 0, 0, NULL);`,
		`INSERT INTO nested_catalogs VALUES ('containers/niimath', '` + nestedCatalogHash + `', 0);`,
	})
	nestedCatalog := createHTTPTestCatalogDB(t, []string{
		`CREATE TABLE catalog (md5path_1 INTEGER, md5path_2 INTEGER, parent_1 INTEGER, parent_2 INTEGER, hardlinks INTEGER, hash BLOB, size INTEGER, mode INTEGER, mtime INTEGER, mtimens INTEGER, flags INTEGER, name TEXT, symlink TEXT, uid INTEGER, gid INTEGER, xattr BLOB);`,
		`INSERT INTO catalog VALUES (2, 2, 1, 1, 1, NULL, 0, 16877, 0, 0, 1, 'niimath', '', 0, 0, NULL);`,
		`INSERT INTO catalog VALUES (3, 3, 2, 2, 1, X'` + strings.ToUpper(fileHash) + `', 12, 33261, 0, 0, 4, 'niimath', '', 0, 0, NULL);`,
	})

	objects := map[string][]byte{
		"/cvmfs/test.repo/.cvmfspublished":        []byte("C" + rootCatalogHash + "\nNtest.repo\n--\n"),
		objectPathForHTTP(rootCatalogHash, "C"):   compressZlibForHTTP(t, rootCatalog),
		objectPathForHTTP(nestedCatalogHash, "C"): compressZlibForHTTP(t, nestedCatalog),
		objectPathForHTTP(fileHash, ""):           compressZlibForHTTP(t, []byte("niimath-data")),
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := objects[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(ts.Close)
	return ts
}

func newHTTPDirectoryRepoServer(t *testing.T) *httptest.Server {
	t.Helper()

	rootCatalogHash := strings.Repeat("a", 40)
	nestedCatalogHash := strings.Repeat("b", 40)
	commandsHash := strings.Repeat("c", 40)
	releaseHash := strings.Repeat("d", 40)
	shHash := strings.Repeat("e", 40)

	rootCatalog := createHTTPTestCatalogDB(t, []string{
		`CREATE TABLE catalog (md5path_1 INTEGER, md5path_2 INTEGER, parent_1 INTEGER, parent_2 INTEGER, hardlinks INTEGER, hash BLOB, size INTEGER, mode INTEGER, mtime INTEGER, mtimens INTEGER, flags INTEGER, name TEXT, symlink TEXT, uid INTEGER, gid INTEGER, xattr BLOB);`,
		`CREATE TABLE nested_catalogs (path TEXT, sha1 TEXT, size INTEGER);`,
		`INSERT INTO catalog VALUES (1, 1, 0, 0, 1, NULL, 0, 16877, 0, 0, 1, 'containers', '', 0, 0, NULL);`,
		`INSERT INTO nested_catalogs VALUES ('containers/niimath_1.0.20250804_20251016', '` + nestedCatalogHash + `', 0);`,
	})
	nestedCatalog := createHTTPTestCatalogDB(t, []string{
		`CREATE TABLE catalog (md5path_1 INTEGER, md5path_2 INTEGER, parent_1 INTEGER, parent_2 INTEGER, hardlinks INTEGER, hash BLOB, size INTEGER, mode INTEGER, mtime INTEGER, mtimens INTEGER, flags INTEGER, name TEXT, symlink TEXT, uid INTEGER, gid INTEGER, xattr BLOB);`,
		`INSERT INTO catalog VALUES (2, 2, 1, 1, 1, NULL, 0, 16877, 0, 0, 1, 'niimath_1.0.20250804_20251016', '', 0, 0, NULL);`,
		`INSERT INTO catalog VALUES (3, 3, 2, 2, 1, X'` + strings.ToUpper(commandsHash) + `', 8, 33188, 0, 0, 4, 'commands.txt', '', 0, 0, NULL);`,
		`INSERT INTO catalog VALUES (4, 4, 2, 2, 1, NULL, 0, 16877, 0, 0, 1, 'niimath_1.0.20250804_20251016.simg', '', 0, 0, NULL);`,
		`INSERT INTO catalog VALUES (5, 5, 4, 4, 1, NULL, 0, 16877, 0, 0, 1, 'etc', '', 0, 0, NULL);`,
		`INSERT INTO catalog VALUES (6, 6, 5, 5, 1, X'` + strings.ToUpper(releaseHash) + `', 7, 33188, 0, 0, 4, 'alpine-release', '', 0, 0, NULL);`,
		`INSERT INTO catalog VALUES (7, 7, 4, 4, 1, NULL, 0, 16877, 0, 0, 1, 'bin', '', 0, 0, NULL);`,
		`INSERT INTO catalog VALUES (8, 8, 7, 7, 1, X'` + strings.ToUpper(shHash) + `', 64, 33261, 0, 0, 4, 'sh', '', 0, 0, NULL);`,
	})

	shData := make([]byte, 64)
	copy(shData, []byte{0x7f, 'E', 'L', 'F', 2, 1, 1, 0})
	shData[18] = 0x3e

	objects := map[string][]byte{
		"/cvmfs/test.repo/.cvmfspublished":        []byte("C" + rootCatalogHash + "\nNtest.repo\n--\n"),
		objectPathForHTTP(rootCatalogHash, "C"):   compressZlibForHTTP(t, rootCatalog),
		objectPathForHTTP(nestedCatalogHash, "C"): compressZlibForHTTP(t, nestedCatalog),
		objectPathForHTTP(commandsHash, ""):       compressZlibForHTTP(t, []byte("niimath\n")),
		objectPathForHTTP(releaseHash, ""):        compressZlibForHTTP(t, []byte("3.20.0\n")),
		objectPathForHTTP(shHash, ""):             compressZlibForHTTP(t, shData),
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := objects[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(ts.Close)
	return ts
}

func compressZlibForHTTP(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	if _, err := zw.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func objectPathForHTTP(hash, suffix string) string {
	return "/cvmfs/test.repo/data/" + hash[:2] + "/" + hash[2:] + suffix
}

func createHTTPTestCatalogDB(t *testing.T, statements []string) []byte {
	t.Helper()
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 not available")
	}
	file, err := os.CreateTemp("", "ccx3-cvmfs-http-test-*.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	_ = file.Close()
	t.Cleanup(func() { _ = os.Remove(file.Name()) })
	script := strings.Join(statements, "\n")
	cmd := exec.Command("sqlite3", file.Name())
	cmd.Stdin = strings.NewReader(script)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("sqlite3: %v\n%s", err, out)
	}
	data, err := os.ReadFile(file.Name())
	if err != nil {
		t.Fatal(err)
	}
	return data
}
