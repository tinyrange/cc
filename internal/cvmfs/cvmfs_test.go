package cvmfs

import (
	"bytes"
	"compress/zlib"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseTarget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		raw    string
		remote bool
		mirror string
		repo   string
		path   string
		local  string
	}{
		{
			raw:   "/cvmfs/neurodesk.ardc.edu.au/containers/niimath/bin",
			repo:  "neurodesk.ardc.edu.au",
			path:  "/containers/niimath/bin",
			local: "/cvmfs/neurodesk.ardc.edu.au/containers/niimath/bin",
		},
		{
			raw:    "cvmfs://neurodesk.ardc.edu.au/containers/niimath/bin",
			remote: true,
			mirror: DefaultMirror,
			repo:   "neurodesk.ardc.edu.au",
			path:   "/containers/niimath/bin",
		},
		{
			raw:    "https://cvmfs.neurodesk.org/cvmfs/neurodesk.ardc.edu.au/containers/niimath/bin",
			remote: true,
			mirror: "https://cvmfs.neurodesk.org/cvmfs",
			repo:   "neurodesk.ardc.edu.au",
			path:   "/containers/niimath/bin",
		},
	}

	for _, tt := range tests {
		got, err := ParseTarget(tt.raw)
		if err != nil {
			t.Fatalf("ParseTarget(%q) error = %v", tt.raw, err)
		}
		if got.Remote != tt.remote || got.Mirror != tt.mirror || got.Repo != tt.repo || got.Path != tt.path || got.LocalPath != tt.local {
			t.Fatalf("ParseTarget(%q) = %#v", tt.raw, got)
		}
	}
}

func TestRemoteReadDirAndFile(t *testing.T) {
	t.Parallel()

	server := newTestRepoServer(t)
	client := &Client{HTTPClient: server.Client()}

	entries, err := client.ReadDir(server.URL + "/cvmfs/test.repo/containers/niimath")
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	if len(entries) != 1 || entries[0].Name != "niimath" {
		t.Fatalf("ReadDir() = %#v, want single niimath entry", entries)
	}
	data, err := client.ReadFile(server.URL + "/cvmfs/test.repo/containers/niimath/niimath")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(data) != "niimath-data" {
		t.Fatalf("ReadFile() = %q, want %q", string(data), "niimath-data")
	}
	chunked, err := client.ReadFile(server.URL + "/cvmfs/test.repo/chunked.txt")
	if err != nil {
		t.Fatalf("ReadFile(chunked) error = %v", err)
	}
	if string(chunked) != "hello chunked world" {
		t.Fatalf("ReadFile(chunked) = %q", string(chunked))
	}
}

func TestLocalReadDirAndFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dir := filepath.Join(root, "repo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	client := NewClient()
	entries, err := client.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(local) error = %v", err)
	}
	if len(entries) != 1 || entries[0].Name != "hello.txt" {
		t.Fatalf("ReadDir(local) = %#v", entries)
	}
	data, err := client.ReadFile(filepath.Join(dir, "hello.txt"))
	if err != nil {
		t.Fatalf("ReadFile(local) error = %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("ReadFile(local) = %q", string(data))
	}
}

type testRepoServer struct {
	*httptest.Server
}

func newTestRepoServer(t *testing.T) *testRepoServer {
	t.Helper()

	rootCatalogHash := strings.Repeat("1", 40)
	nestedCatalogHash := strings.Repeat("2", 40)
	fileHash := strings.Repeat("3", 40)
	chunk1Hash := strings.Repeat("4", 40)
	chunk2Hash := strings.Repeat("5", 40)

	rootCatalog := createCatalogDB(t, []string{
		`CREATE TABLE catalog (md5path_1 INTEGER, md5path_2 INTEGER, parent_1 INTEGER, parent_2 INTEGER, hardlinks INTEGER, hash BLOB, size INTEGER, mode INTEGER, mtime INTEGER, mtimens INTEGER, flags INTEGER, name TEXT, symlink TEXT, uid INTEGER, gid INTEGER, xattr BLOB);`,
		`CREATE TABLE nested_catalogs (path TEXT, sha1 TEXT, size INTEGER);`,
		`CREATE TABLE chunks (md5path_1 INTEGER, md5path_2 INTEGER, offset INTEGER, size INTEGER, hash BLOB);`,
		`INSERT INTO catalog VALUES (1, 1, 0, 0, 1, NULL, 0, 16877, 0, 0, 1, 'containers', '', 0, 0, NULL);`,
		`INSERT INTO catalog VALUES (9, 9, 0, 0, 1, NULL, 19, 33188, 0, 0, 196, 'chunked.txt', '', 0, 0, NULL);`,
		`INSERT INTO nested_catalogs VALUES ('containers/niimath', '` + nestedCatalogHash + `', 0);`,
		`INSERT INTO chunks VALUES (9, 9, 0, 6, X'` + strings.ToUpper(chunk1Hash) + `');`,
		`INSERT INTO chunks VALUES (9, 9, 6, 13, X'` + strings.ToUpper(chunk2Hash) + `');`,
	})
	nestedCatalog := createCatalogDB(t, []string{
		`CREATE TABLE catalog (md5path_1 INTEGER, md5path_2 INTEGER, parent_1 INTEGER, parent_2 INTEGER, hardlinks INTEGER, hash BLOB, size INTEGER, mode INTEGER, mtime INTEGER, mtimens INTEGER, flags INTEGER, name TEXT, symlink TEXT, uid INTEGER, gid INTEGER, xattr BLOB);`,
		`INSERT INTO catalog VALUES (2, 2, 1, 1, 1, NULL, 0, 16877, 0, 0, 1, 'niimath', '', 0, 0, NULL);`,
		`INSERT INTO catalog VALUES (3, 3, 2, 2, 1, X'` + strings.ToUpper(fileHash) + `', 12, 33261, 0, 0, 4, 'niimath', '', 0, 0, NULL);`,
	})

	objects := map[string][]byte{
		"/cvmfs/test.repo/.cvmfspublished": []byte("C" + rootCatalogHash + "\nNtest.repo\n--\n"),
		objectPath(rootCatalogHash, "C"):   compressZlib(t, rootCatalog),
		objectPath(nestedCatalogHash, "C"): compressZlib(t, nestedCatalog),
		objectPath(fileHash, ""):           compressZlib(t, []byte("niimath-data")),
		objectPath(chunk1Hash, "P"):        compressZlib(t, []byte("hello ")),
		objectPath(chunk2Hash, "P"):        compressZlib(t, []byte("chunked world")),
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := objects[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	return &testRepoServer{Server: server}
}

func createCatalogDB(t *testing.T, statements []string) []byte {
	t.Helper()
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 not available")
	}
	file, err := os.CreateTemp("", "ccx3-cvmfs-test-*.sqlite")
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

func decodeHex(t *testing.T, hexHash string) []byte {
	t.Helper()
	out, err := hex.DecodeString(hexHash)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func compressZlib(t *testing.T, data []byte) []byte {
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

func objectPath(hash, suffix string) string {
	return "/cvmfs/test.repo/data/" + hash[:2] + "/" + hash[2:] + suffix
}

func TestFetchDataObjectIsZlib(t *testing.T) {
	t.Parallel()
	content := compressZlib(t, []byte("hello"))
	reader, err := zlib.NewReader(bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello" {
		t.Fatalf("decoded = %q", string(data))
	}
}
