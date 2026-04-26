package cvmfs

import (
	"bytes"
	"compress/zlib"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	intsqlite "j5.nz/cc/internal/sqlite"
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

func TestFormatTargetEscapesLiteralPathCharacters(t *testing.T) {
	t.Parallel()

	raw, err := FormatTarget(Target{
		Remote: true,
		Mirror: "https://cvmfs.neurodesk.org/cvmfs",
		Repo:   "neurodesk.ardc.edu.au",
		Path:   "/containers/afni/bin/#nu_correct#",
	})
	if err != nil {
		t.Fatalf("FormatTarget(#) error = %v", err)
	}
	if !strings.Contains(raw, "%23nu_correct%23") {
		t.Fatalf("FormatTarget(#) = %q, want escaped fragment markers", raw)
	}
	got, err := ParseTarget(raw)
	if err != nil {
		t.Fatalf("ParseTarget(FormatTarget(#)) error = %v", err)
	}
	if got.Path != "/containers/afni/bin/#nu_correct#" {
		t.Fatalf("round trip path = %q, want %q", got.Path, "/containers/afni/bin/#nu_correct#")
	}

	raw, err = FormatTarget(Target{
		Remote: true,
		Mirror: "https://cvmfs.neurodesk.org/cvmfs",
		Repo:   "neurodesk.ardc.edu.au",
		Path:   "/usr/share/dcmtk/csmapper/ISO-8859/UCS%ISO-8859-2.mps",
	})
	if err != nil {
		t.Fatalf("FormatTarget(%%) error = %v", err)
	}
	if !strings.Contains(raw, "UCS%25ISO-8859-2.mps") {
		t.Fatalf("FormatTarget(%%) = %q, want escaped percent", raw)
	}
	got, err = ParseTarget(raw)
	if err != nil {
		t.Fatalf("ParseTarget(FormatTarget(%%)) error = %v", err)
	}
	if got.Path != "/usr/share/dcmtk/csmapper/ISO-8859/UCS%ISO-8859-2.mps" {
		t.Fatalf("round trip path = %q, want %q", got.Path, "/usr/share/dcmtk/csmapper/ISO-8859/UCS%ISO-8859-2.mps")
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

func TestRemoteWalkReturnsSubtreeMetadata(t *testing.T) {
	t.Parallel()

	server := newTestRepoServer(t)
	client := &Client{HTTPClient: server.Client()}

	var entries []WalkEntry
	err := client.Walk(server.URL+"/cvmfs/test.repo/containers/niimath", func(entry WalkEntry) error {
		entries = append(entries, entry)
		return nil
	})
	if err != nil {
		t.Fatalf("Walk() error = %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("Walk() returned %d entries, want 2", len(entries))
	}
	if entries[0].Path != "/containers/niimath" || !entries[0].Mode.IsDir() {
		t.Fatalf("Walk()[0] = %#v", entries[0])
	}
	if entries[1].Path != "/containers/niimath/niimath" || entries[1].Mode.IsDir() {
		t.Fatalf("Walk()[1] = %#v", entries[1])
	}
}

func TestWalkDescendsIntoDescendantNestedCatalogs(t *testing.T) {
	t.Parallel()

	rootCatalogHash := strings.Repeat("a", 40)
	containersCatalogHash := strings.Repeat("b", 40)
	descendantCatalogHash := strings.Repeat("c", 40)
	fileHash := strings.Repeat("d", 40)

	rootCatalog := createCatalogDB(t, []string{
		`CREATE TABLE catalog (md5path_1 INTEGER, md5path_2 INTEGER, parent_1 INTEGER, parent_2 INTEGER, hardlinks INTEGER, hash BLOB, size INTEGER, mode INTEGER, mtime INTEGER, mtimens INTEGER, flags INTEGER, name TEXT, symlink TEXT, uid INTEGER, gid INTEGER, xattr BLOB);`,
		`CREATE TABLE nested_catalogs (path TEXT, sha1 TEXT, size INTEGER);`,
		`INSERT INTO catalog VALUES (1, 1, 0, 0, 1, NULL, 0, 16877, 0, 0, 1, 'containers', '', 0, 0, NULL);`,
		`INSERT INTO nested_catalogs VALUES ('containers', '` + containersCatalogHash + `', 0);`,
	})
	containersCatalog := createCatalogDB(t, []string{
		`CREATE TABLE catalog (md5path_1 INTEGER, md5path_2 INTEGER, parent_1 INTEGER, parent_2 INTEGER, hardlinks INTEGER, hash BLOB, size INTEGER, mode INTEGER, mtime INTEGER, mtimens INTEGER, flags INTEGER, name TEXT, symlink TEXT, uid INTEGER, gid INTEGER, xattr BLOB);`,
		`CREATE TABLE nested_catalogs (path TEXT, sha1 TEXT, size INTEGER);`,
		`INSERT INTO catalog VALUES (2, 2, 0, 0, 1, NULL, 0, 16877, 0, 0, 1, 'containers', '', 0, 0, NULL);`,
		`INSERT INTO catalog VALUES (3, 3, 2, 2, 1, NULL, 0, 16877, 0, 0, 1, 'niimath', '', 0, 0, NULL);`,
		`INSERT INTO nested_catalogs VALUES ('containers/niimath', '` + descendantCatalogHash + `', 0);`,
	})
	descendantCatalog := createCatalogDB(t, []string{
		`CREATE TABLE catalog (md5path_1 INTEGER, md5path_2 INTEGER, parent_1 INTEGER, parent_2 INTEGER, hardlinks INTEGER, hash BLOB, size INTEGER, mode INTEGER, mtime INTEGER, mtimens INTEGER, flags INTEGER, name TEXT, symlink TEXT, uid INTEGER, gid INTEGER, xattr BLOB);`,
		`INSERT INTO catalog VALUES (4, 4, 3, 3, 1, NULL, 0, 16877, 0, 0, 1, 'usr', '', 0, 0, NULL);`,
		`INSERT INTO catalog VALUES (5, 5, 4, 4, 1, X'` + strings.ToUpper(fileHash) + `', 2, 33188, 0, 0, 4, 'ok', '', 0, 0, NULL);`,
	})

	objects := map[string][]byte{
		"/cvmfs/test.repo/.cvmfspublished":     []byte("C" + rootCatalogHash + "\nNtest.repo\n--\n"),
		objectPath(rootCatalogHash, "C"):       compressZlib(t, rootCatalog),
		objectPath(containersCatalogHash, "C"): compressZlib(t, containersCatalog),
		objectPath(descendantCatalogHash, "C"): compressZlib(t, descendantCatalog),
		objectPath(fileHash, ""):               compressZlib(t, []byte("ok")),
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

	client := &Client{HTTPClient: server.Client()}
	var got []string
	err := client.Walk(server.URL+"/cvmfs/test.repo/containers/niimath", func(entry WalkEntry) error {
		got = append(got, entry.Path)
		return nil
	})
	if err != nil {
		t.Fatalf("Walk() error = %v", err)
	}
	if !slices.Contains(got, "/containers/niimath/usr/ok") {
		t.Fatalf("Walk() paths = %#v, want descendant nested entry", got)
	}
}

func TestReadDirDoesNotWalkDescendantNestedCatalogs(t *testing.T) {
	t.Parallel()

	rootCatalogHash := strings.Repeat("a", 40)
	containersCatalogHash := strings.Repeat("b", 40)
	descendantCatalogHash := strings.Repeat("c", 40)

	rootCatalog := createCatalogDB(t, []string{
		`CREATE TABLE catalog (md5path_1 INTEGER, md5path_2 INTEGER, parent_1 INTEGER, parent_2 INTEGER, hardlinks INTEGER, hash BLOB, size INTEGER, mode INTEGER, mtime INTEGER, mtimens INTEGER, flags INTEGER, name TEXT, symlink TEXT, uid INTEGER, gid INTEGER, xattr BLOB);`,
		`CREATE TABLE nested_catalogs (path TEXT, sha1 TEXT, size INTEGER);`,
		`INSERT INTO catalog VALUES (1, 1, 0, 0, 1, NULL, 0, 16877, 0, 0, 1, 'containers', '', 0, 0, NULL);`,
		`INSERT INTO nested_catalogs VALUES ('containers', '` + containersCatalogHash + `', 0);`,
	})
	containersCatalog := createCatalogDB(t, []string{
		`CREATE TABLE catalog (md5path_1 INTEGER, md5path_2 INTEGER, parent_1 INTEGER, parent_2 INTEGER, hardlinks INTEGER, hash BLOB, size INTEGER, mode INTEGER, mtime INTEGER, mtimens INTEGER, flags INTEGER, name TEXT, symlink TEXT, uid INTEGER, gid INTEGER, xattr BLOB);`,
		`CREATE TABLE nested_catalogs (path TEXT, sha1 TEXT, size INTEGER);`,
		`INSERT INTO catalog VALUES (2, 2, 0, 0, 1, NULL, 0, 16877, 0, 0, 1, 'containers', '', 0, 0, NULL);`,
		`INSERT INTO catalog VALUES (3, 3, 2, 2, 1, NULL, 0, 16877, 0, 0, 1, 'niimath_1.0.20250804_20251016', '', 0, 0, NULL);`,
		`INSERT INTO nested_catalogs VALUES ('containers/niimath_1.0.20250804_20251016', '` + descendantCatalogHash + `', 0);`,
	})

	requested := make([]string, 0, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.URL.Path)
		switch r.URL.Path {
		case "/cvmfs/test.repo/.cvmfspublished":
			_, _ = w.Write([]byte("C" + rootCatalogHash + "\nNtest.repo\n--\n"))
		case objectPath(rootCatalogHash, "C"):
			_, _ = w.Write(compressZlib(t, rootCatalog))
		case objectPath(containersCatalogHash, "C"):
			_, _ = w.Write(compressZlib(t, containersCatalog))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	client := &Client{HTTPClient: server.Client()}
	entries, err := client.ReadDir(server.URL + "/cvmfs/test.repo/containers")
	if err != nil {
		t.Fatalf("ReadDir(/containers) error = %v", err)
	}
	if len(entries) != 1 || entries[0].Name != "niimath_1.0.20250804_20251016" {
		t.Fatalf("ReadDir(/containers) = %#v", entries)
	}
	for _, requestPath := range requested {
		if requestPath == objectPath(descendantCatalogHash, "C") {
			t.Fatalf("ReadDir(/containers) fetched descendant nested catalog %q", requestPath)
		}
	}
}

func TestRemoteReadUsesObjectCacheAfterServerShutdown(t *testing.T) {
	t.Parallel()

	server := newTestRepoServer(t)
	cacheDir := t.TempDir()
	client := &Client{
		HTTPClient: server.Client(),
		CacheDir:   cacheDir,
	}

	data, err := client.ReadFile(server.URL + "/cvmfs/test.repo/containers/niimath/niimath")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(data) != "niimath-data" {
		t.Fatalf("ReadFile() = %q, want %q", string(data), "niimath-data")
	}

	rootCatalogHash := strings.Repeat("1", 40)
	nestedCatalogHash := strings.Repeat("2", 40)
	fileHash := strings.Repeat("3", 40)
	for _, tc := range []struct {
		hash   string
		suffix string
	}{
		{hash: rootCatalogHash, suffix: "C"},
		{hash: nestedCatalogHash, suffix: "C"},
		{hash: fileHash, suffix: ""},
	} {
		cachePath := CVMFSObjectCachePath(cacheDir, tc.hash, tc.suffix)
		if _, err := os.Stat(cachePath); err != nil {
			t.Fatalf("Stat(%q) error = %v", cachePath, err)
		}
	}
	manifestPath := filepath.Join(cacheDir, "state", "test.repo", "manifest")
	if _, err := os.Stat(manifestPath); err != nil {
		t.Fatalf("Stat(%q) error = %v", manifestPath, err)
	}

	server.Close()

	data, err = client.ReadFile(server.URL + "/cvmfs/test.repo/containers/niimath/niimath")
	if err != nil {
		t.Fatalf("ReadFile() using cache error = %v", err)
	}
	if string(data) != "niimath-data" {
		t.Fatalf("cached ReadFile() = %q, want %q", string(data), "niimath-data")
	}
}

func TestChunkedFileUsesCachedCompressedChunks(t *testing.T) {
	t.Parallel()

	server := newTestRepoServer(t)
	cacheDir := t.TempDir()
	client := &Client{
		HTTPClient: server.Client(),
		CacheDir:   cacheDir,
	}

	data, err := client.ReadFile(server.URL + "/cvmfs/test.repo/chunked.txt")
	if err != nil {
		t.Fatalf("ReadFile(chunked) error = %v", err)
	}
	if string(data) != "hello chunked world" {
		t.Fatalf("ReadFile(chunked) = %q", string(data))
	}

	chunk1Hash := strings.Repeat("4", 40)
	chunk2Hash := strings.Repeat("5", 40)
	for _, tc := range []struct {
		hash   string
		suffix string
	}{
		{hash: chunk1Hash, suffix: "P"},
		{hash: chunk2Hash, suffix: "P"},
	} {
		cachePath := CVMFSObjectCachePath(cacheDir, tc.hash, tc.suffix)
		if _, err := os.Stat(cachePath); err != nil {
			t.Fatalf("Stat(%q) error = %v", cachePath, err)
		}
	}
	manifestPath := filepath.Join(cacheDir, "state", "test.repo", "manifest")
	if _, err := os.Stat(manifestPath); err != nil {
		t.Fatalf("Stat(%q) error = %v", manifestPath, err)
	}

	server.Close()

	data, err = client.ReadFile(server.URL + "/cvmfs/test.repo/chunked.txt")
	if err != nil {
		t.Fatalf("ReadFile(chunked) using cache error = %v", err)
	}
	if string(data) != "hello chunked world" {
		t.Fatalf("cached ReadFile(chunked) = %q", string(data))
	}
}

func TestPrefetchFilePopulatesFileCacheWithoutDataObjectCache(t *testing.T) {
	t.Parallel()

	server := newTestRepoServer(t)
	cacheDir := t.TempDir()
	client := &Client{
		HTTPClient: server.Client(),
		CacheDir:   cacheDir,
	}

	target := server.URL + "/cvmfs/test.repo/containers/niimath/niimath"
	size, err := client.PrefetchFile(target)
	if err != nil {
		t.Fatalf("PrefetchFile() error = %v", err)
	}
	if size != uint64(len("niimath-data")) {
		t.Fatalf("PrefetchFile() size = %d", size)
	}

	parsed, err := ParseTarget(target)
	if err != nil {
		t.Fatalf("ParseTarget() error = %v", err)
	}
	fileCachePath := cvmfsFileCachePath(cacheDir, parsed.Repo, parsed.Path)
	if _, err := os.Stat(fileCachePath); err != nil {
		t.Fatalf("Stat(file cache %q) error = %v", fileCachePath, err)
	}

	fileHash := strings.Repeat("3", 40)
	if _, err := os.Stat(CVMFSObjectCachePath(cacheDir, fileHash, "")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no data object cache entry, got err = %v", err)
	}

	server.Close()

	data, err := client.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile() using prefetched file cache error = %v", err)
	}
	if string(data) != "niimath-data" {
		t.Fatalf("ReadFile() = %q", string(data))
	}
}

func TestPrefetchChunkedFilePopulatesFileCacheWithoutChunkObjectCache(t *testing.T) {
	t.Parallel()

	server := newTestRepoServer(t)
	cacheDir := t.TempDir()
	client := &Client{
		HTTPClient: server.Client(),
		CacheDir:   cacheDir,
	}

	target := server.URL + "/cvmfs/test.repo/chunked.txt"
	size, err := client.PrefetchFile(target)
	if err != nil {
		t.Fatalf("PrefetchFile(chunked) error = %v", err)
	}
	if size != uint64(len("hello chunked world")) {
		t.Fatalf("PrefetchFile(chunked) size = %d", size)
	}

	parsed, err := ParseTarget(target)
	if err != nil {
		t.Fatalf("ParseTarget() error = %v", err)
	}
	fileCachePath := cvmfsFileCachePath(cacheDir, parsed.Repo, parsed.Path)
	if _, err := os.Stat(fileCachePath); err != nil {
		t.Fatalf("Stat(file cache %q) error = %v", fileCachePath, err)
	}

	for _, hash := range []string{strings.Repeat("4", 40), strings.Repeat("5", 40)} {
		if _, err := os.Stat(CVMFSObjectCachePath(cacheDir, hash, "P")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("expected no chunk object cache entry for %s, got err = %v", hash, err)
		}
	}

	server.Close()

	data, err := client.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile(chunked) using prefetched file cache error = %v", err)
	}
	if string(data) != "hello chunked world" {
		t.Fatalf("ReadFile(chunked) = %q", string(data))
	}
}

func TestCVMFSObjectCachePathUsesTwoLevelFanout(t *testing.T) {
	t.Parallel()

	got := CVMFSObjectCachePath("/cache", "aabbccddeeff", "P")
	want := filepath.Join("/cache", "objects", "aa", "bb", "ccddeeffP")
	if got != want {
		t.Fatalf("CVMFSObjectCachePath() = %q, want %q", got, want)
	}
}

func TestShouldWalkNestedCatalog(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		nestedPath string
		prefix     string
		want       bool
	}{
		{name: "exact match", nestedPath: "/containers", prefix: "/containers", want: true},
		{name: "prefix within nested", nestedPath: "/containers/niimath", prefix: "/containers/niimath/bin/niimath", want: true},
		{name: "parent listing skips descendants", nestedPath: "/containers/niimath", prefix: "/containers", want: false},
		{name: "unrelated path", nestedPath: "/apps", prefix: "/containers", want: false},
	}

	for _, tt := range tests {
		if got := shouldWalkNestedCatalog(tt.nestedPath, tt.prefix); got != tt.want {
			t.Fatalf("%s: shouldWalkNestedCatalog(%q, %q) = %v, want %v", tt.name, tt.nestedPath, tt.prefix, got, tt.want)
		}
	}
}

func TestLoadEntriesUsesSchemaColumnOrder(t *testing.T) {
	t.Parallel()

	dbBytes := createCatalogDB(t, []string{
		`CREATE TABLE catalog (md5path_1 INTEGER, md5path_2 INTEGER, parent_1 INTEGER, parent_2 INTEGER, hardlinks INTEGER, hash BLOB, size INTEGER, mode INTEGER, mtime INTEGER, flags INTEGER, name TEXT, symlink TEXT, uid INTEGER, gid INTEGER, xattr BLOB, mtimens INTEGER);`,
		`INSERT INTO catalog VALUES (1, 1, 0, 0, 1, NULL, 0, 16877, 123, 3, 'containers', '', 0, 0, NULL, 456);`,
	})

	db, err := intsqlite.ParseDatabase(dbBytes)
	if err != nil {
		t.Fatalf("ParseDatabase() error = %v", err)
	}
	entries, err := loadEntries(db, nil)
	if err != nil {
		t.Fatalf("loadEntries() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("loadEntries() got %d entries, want 1", len(entries))
	}
	entry := entries[0]
	if entry.Name != "containers" || entry.Flags != 3 || entry.Mtime != 123 || entry.MtimeNS != 456 {
		t.Fatalf("loadEntries() = %#v", entry)
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
