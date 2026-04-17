package main

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/json"
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
	mux := newMux(&server{}, nil)

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
	mux := newMux(srv, nil)

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
