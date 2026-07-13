//go:build !windows

package guestagent

import (
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRootPathCleansRootAndName(t *testing.T) {
	if got := rootPath("/mnt/root", "../etc/passwd"); got != "/mnt/root/etc/passwd" {
		t.Fatalf("rootPath = %q", got)
	}
	if got := rootPath("/", "bin/sh"); got != "/bin/sh" {
		t.Fatalf("rootPath at root = %q", got)
	}
}

func TestTarTargetRejectsTraversal(t *testing.T) {
	if _, err := tarTarget("/tmp/out", true, "../escape"); err == nil {
		t.Fatalf("tarTarget traversal error = %v", err)
	}
	if got, err := tarTarget("/tmp/out", false, "root/file"); err != nil || got != "/tmp/out/file" {
		t.Fatalf("tarTarget single destination = %q, %v", got, err)
	}
}

func TestWriteAndExtractTarPreservesSymlink(t *testing.T) {
	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "target"), []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target", filepath.Join(srcDir, "link")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(srcDir)
	if err != nil {
		t.Fatal(err)
	}
	rootName := filepath.Base(srcDir)
	var buf bytes.Buffer
	if err := WritePathTar(&buf, srcDir, rootName, info); err != nil {
		t.Fatalf("WritePathTar: %v", err)
	}

	var sawLink bool
	tr := tar.NewReader(bytes.NewReader(buf.Bytes()))
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read tar: %v", err)
		}
		if header.Name == rootName+"/link" {
			sawLink = true
			if header.Typeflag != tar.TypeSymlink || header.Linkname != "target" {
				t.Fatalf("link header = type %d target %q", header.Typeflag, header.Linkname)
			}
		}
	}
	if !sawLink {
		t.Fatalf("tar did not contain symlink")
	}

	dstDir := t.TempDir()
	if err := ExtractTarToPath(bytes.NewReader(buf.Bytes()), "/", filepath.Join(dstDir, "out"), true); err != nil {
		t.Fatalf("ExtractTarToPath: %v", err)
	}
	link, err := os.Readlink(filepath.Join(dstDir, "out", rootName, "link"))
	if err != nil {
		t.Fatalf("read extracted link: %v", err)
	}
	if link != "target" {
		t.Fatalf("extracted link = %q", link)
	}
}

func TestExtractTarToPathConflictSemantics(t *testing.T) {
	t.Run("file over file overwrites", func(t *testing.T) {
		var archive bytes.Buffer
		if err := writeSingleFileTar(&archive, "src.txt", "new"); err != nil {
			t.Fatalf("write archive: %v", err)
		}
		dst := filepath.Join(t.TempDir(), "dst.txt")
		if err := os.WriteFile(dst, []byte("old"), 0o644); err != nil {
			t.Fatalf("write dst: %v", err)
		}

		if err := ExtractTarToPath(bytes.NewReader(archive.Bytes()), "/", dst, false); err != nil {
			t.Fatalf("extract file over file: %v", err)
		}
		if got := readTestFile(t, dst); got != "new" {
			t.Fatalf("dst content = %q, want new", got)
		}
	})

	t.Run("file into directory copies under source name", func(t *testing.T) {
		var archive bytes.Buffer
		if err := writeSingleFileTar(&archive, "src.txt", "payload"); err != nil {
			t.Fatalf("write archive: %v", err)
		}
		dst := t.TempDir()

		if err := ExtractTarToPath(bytes.NewReader(archive.Bytes()), "/", dst, false); err != nil {
			t.Fatalf("extract file into directory: %v", err)
		}
		if got := readTestFile(t, filepath.Join(dst, "src.txt")); got != "payload" {
			t.Fatalf("copied file content = %q, want payload", got)
		}
	})

	t.Run("directory over file fails", func(t *testing.T) {
		var archive bytes.Buffer
		if err := writeTestPathTar(&archive, makeTestCopyTree(t), "tree"); err != nil {
			t.Fatalf("write archive: %v", err)
		}
		dst := filepath.Join(t.TempDir(), "dst")
		if err := os.WriteFile(dst, []byte("keep"), 0o644); err != nil {
			t.Fatalf("write dst: %v", err)
		}

		err := ExtractTarToPath(bytes.NewReader(archive.Bytes()), "/", dst, false)
		if err == nil {
			t.Fatalf("extract directory over file error = %v", err)
		}
		if got := readTestFile(t, dst); got != "keep" {
			t.Fatalf("dst content = %q, want keep", got)
		}
	})

	t.Run("directory into directory merges under source name", func(t *testing.T) {
		var archive bytes.Buffer
		if err := writeTestPathTar(&archive, makeTestCopyTree(t), "tree"); err != nil {
			t.Fatalf("write archive: %v", err)
		}
		dst := filepath.Join(t.TempDir(), "dst")
		if err := os.MkdirAll(filepath.Join(dst, "tree", "nested"), 0o755); err != nil {
			t.Fatalf("make dst nested: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dst, "tree", "nested", "old.txt"), []byte("old"), 0o644); err != nil {
			t.Fatalf("write old file: %v", err)
		}

		if err := ExtractTarToPath(bytes.NewReader(archive.Bytes()), "/", dst, false); err != nil {
			t.Fatalf("extract directory into directory: %v", err)
		}
		if got := readTestFile(t, filepath.Join(dst, "tree", "nested", "file.txt")); got != "payload" {
			t.Fatalf("new nested file = %q, want payload", got)
		}
		if got := readTestFile(t, filepath.Join(dst, "tree", "nested", "old.txt")); got != "old" {
			t.Fatalf("old nested file = %q, want old", got)
		}
	})

	t.Run("non-directory over directory fails when forced exact", func(t *testing.T) {
		dst := filepath.Join(t.TempDir(), "dst")
		if err := os.Mkdir(dst, 0o755); err != nil {
			t.Fatalf("make dst dir: %v", err)
		}

		err := ensureTarTargetCompatible(dst, false)
		if err == nil {
			t.Fatalf("exact file over directory error = %v", err)
		}
		if info, err := os.Stat(dst); err != nil || !info.IsDir() {
			t.Fatalf("dst dir stat = %v info=%v", err, info)
		}
	})
}

func TestExtractTarToPathRejectsArchiveSymlinkEscape(t *testing.T) {
	base := t.TempDir()
	dst := filepath.Join(base, "dst")
	outside := filepath.Join(base, "outside")
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatalf("make outside dir: %v", err)
	}
	outPath := filepath.Join(outside, "marker")
	if err := os.WriteFile(outPath, []byte("unchanged"), 0o600); err != nil {
		t.Fatalf("write outside marker: %v", err)
	}
	archive := maliciousSymlinkTar(t, true)
	err := ExtractTarToPath(bytes.NewReader(archive), "", dst, true)
	if !errors.Is(err, ErrUnsafeTarExtractionPath) {
		t.Fatalf("extract error = %v, want unsafe path", err)
	}
	if got := readTestFile(t, outPath); got != "unchanged" {
		t.Fatalf("outside content = %q", got)
	}
}

func TestExtractTarToPathRejectsPreexistingSymlinkParent(t *testing.T) {
	base := t.TempDir()
	dst := filepath.Join(base, "dst")
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(filepath.Join(dst, "root"), 0o755); err != nil {
		t.Fatalf("make destination: %v", err)
	}
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatalf("make outside dir: %v", err)
	}
	outPath := filepath.Join(outside, "marker")
	if err := os.WriteFile(outPath, []byte("unchanged"), 0o600); err != nil {
		t.Fatalf("write outside marker: %v", err)
	}
	if err := os.Symlink("../../outside", filepath.Join(dst, "root", "link")); err != nil {
		t.Fatalf("make escaping symlink: %v", err)
	}
	archive := maliciousSymlinkTar(t, false)
	err := ExtractTarToPath(bytes.NewReader(archive), "", dst, true)
	if !errors.Is(err, ErrUnsafeTarExtractionPath) {
		t.Fatalf("extract error = %v, want unsafe path", err)
	}
	if got := readTestFile(t, outPath); got != "unchanged" {
		t.Fatalf("outside content = %q", got)
	}
}

func TestExtractTarToPathPreservesRegularFileMetadata(t *testing.T) {
	mtime := time.Unix(1_700_000_000, 0)
	payload := []byte("payload")
	var archive bytes.Buffer
	tw := tar.NewWriter(&archive)
	if err := tw.WriteHeader(&tar.Header{Name: "file", Typeflag: tar.TypeReg, Mode: 0o750, Size: int64(len(payload)), ModTime: mtime}); err != nil {
		t.Fatalf("write header: %v", err)
	}
	if _, err := tw.Write(payload); err != nil {
		t.Fatalf("write payload: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close archive: %v", err)
	}
	dst := t.TempDir()
	if err := ExtractTarToPath(bytes.NewReader(archive.Bytes()), "", dst, true); err != nil {
		t.Fatalf("extract regular file: %v", err)
	}
	info, err := os.Stat(filepath.Join(dst, "file"))
	if err != nil {
		t.Fatalf("stat extracted file: %v", err)
	}
	if info.Mode().Perm() != 0o750 || info.ModTime().Unix() != mtime.Unix() {
		t.Fatalf("file metadata = mode %#o mtime %d", info.Mode().Perm(), info.ModTime().Unix())
	}
	if got := readTestFile(t, filepath.Join(dst, "file")); got != string(payload) {
		t.Fatalf("file content = %q", got)
	}
}

func maliciousSymlinkTar(t *testing.T, includeSymlink bool) []byte {
	t.Helper()
	var archive bytes.Buffer
	tw := tar.NewWriter(&archive)
	if includeSymlink {
		if err := tw.WriteHeader(&tar.Header{Name: "root/link", Typeflag: tar.TypeSymlink, Linkname: "../../outside", Mode: 0o777}); err != nil {
			t.Fatalf("write symlink header: %v", err)
		}
	}
	payload := []byte("overwritten")
	if err := tw.WriteHeader(&tar.Header{Name: "root/link/marker", Typeflag: tar.TypeReg, Mode: 0o600, Size: int64(len(payload))}); err != nil {
		t.Fatalf("write file header: %v", err)
	}
	if _, err := tw.Write(payload); err != nil {
		t.Fatalf("write file payload: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close archive: %v", err)
	}
	return archive.Bytes()
}

func TestParseSignal(t *testing.T) {
	tests := map[string]syscall.Signal{
		"SIGHUP":  syscall.SIGHUP,
		"INT":     syscall.SIGINT,
		"SIGQUIT": syscall.SIGQUIT,
		"":        syscall.SIGTERM,
		"TERM":    syscall.SIGTERM,
		"KILL":    syscall.SIGKILL,
		"USR1":    syscall.SIGUSR1,
		"SIGUSR2": syscall.SIGUSR2,
		"WINCH":   syscall.SIGWINCH,
	}
	for name, want := range tests {
		got, err := ParseSignal(name)
		if err != nil {
			t.Fatalf("ParseSignal(%q): %v", name, err)
		}
		if got != want {
			t.Fatalf("ParseSignal(%q) = %v, want %v", name, got, want)
		}
	}
	if _, err := ParseSignal("SIGBOGUS"); err == nil {
		t.Fatalf("ParseSignal accepted unsupported signal")
	}
}

func makeTestCopyTree(t *testing.T) string {
	t.Helper()
	src := filepath.Join(t.TempDir(), "tree")
	if err := os.MkdirAll(filepath.Join(src, "empty"), 0o755); err != nil {
		t.Fatalf("make empty dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(src, "nested"), 0o755); err != nil {
		t.Fatalf("make nested dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "nested", "file.txt"), []byte("payload"), 0o644); err != nil {
		t.Fatalf("write nested file: %v", err)
	}
	return src
}

func writeTestPathTar(w io.Writer, src, rootName string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	return WritePathTar(w, src, rootName, info)
}

func writeSingleFileTar(w io.Writer, name, content string) error {
	tw := tar.NewWriter(w)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(content))}); err != nil {
		return err
	}
	if _, err := tw.Write([]byte(content)); err != nil {
		return err
	}
	return tw.Close()
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func TestProtocolFraming(t *testing.T) {
	var buf bytes.Buffer
	WriteProtocolLine(&buf, "__TEST__\n")
	WriteProtocolBytes(&buf, OutputMarkerPrefix, "abc", []byte("hello"))
	WriteProtocolBytes(&buf, OutputMarkerPrefix, "abc", nil)
	WriteProtocolBytes(&buf, "", "abc", []byte("ignored"))
	WriteProtocolBytes(&buf, OutputMarkerPrefix, "", []byte("ignored"))

	const want = "__TEST__\n__CCX3_OUT__:abc:aGVsbG8=\n"
	if got := buf.String(); got != want {
		t.Fatalf("protocol framing mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestProtocolObjectWritesConfiguredMarkers(t *testing.T) {
	proto := Protocol{
		BeginMarkerPrefix:   "BEGIN:",
		OutputMarkerPrefix:  "OUT:",
		ErrorMarkerPrefix:   "ERR:",
		ControlMarkerPrefix: "CTL:",
		UsageMarkerPrefix:   "USE:",
		ExitMarkerPrefix:    "EXIT:",
	}
	var buf bytes.Buffer
	proto.WriteBegin(&buf, "id")
	proto.WriteStdout(&buf, "id", []byte("out"))
	proto.WriteStderr(&buf, "id", []byte("err"))
	proto.WriteControl(&buf, "id", []byte("ctl"))
	proto.WriteUsage(&buf, "id", "usage64")
	proto.WriteExit(&buf, "id", 7)

	const want = "BEGIN:id\nOUT:id:b3V0\nERR:id:ZXJy\nCTL:id:Y3Rs\nUSE:id:usage64\nEXIT:id:7\n"
	if got := buf.String(); got != want {
		t.Fatalf("protocol writes mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestExecReporterWritesConfiguredMarkers(t *testing.T) {
	proto := Protocol{
		BeginMarkerPrefix:   "BEGIN:",
		OutputMarkerPrefix:  "OUT:",
		ErrorMarkerPrefix:   "ERR:",
		ControlMarkerPrefix: "CTL:",
		UsageMarkerPrefix:   "USE:",
		ExitMarkerPrefix:    "EXIT:",
		TimingMarkerPrefix:  "TIME:",
	}
	var buf bytes.Buffer
	reporter := NewExecReporter(proto, &buf, "id", time.Now())
	if !reporter.HasExitMarker() {
		t.Fatalf("reporter did not detect exit marker")
	}
	reporter.Begin()
	reporter.Stdout([]byte("out"))
	reporter.Stderr([]byte("err"))
	reporter.ControlBytes([]byte("ctl"))
	reporter.Usage("usage64")
	reporter.Timing("phase")
	reporter.Exit(7)

	lines := strings.Split(buf.String(), "\n")
	want := []string{
		"BEGIN:id\n",
		"OUT:id:b3V0\n",
		"ERR:id:ZXJy\n",
		"CTL:id:Y3Rs\n",
		"USE:id:usage64\n",
		"EXIT:id:7\n",
	}
	if len(lines) != 8 {
		t.Fatalf("reporter lines = %#v", lines)
	}
	for i, wantLine := range want[:5] {
		if lines[i]+"\n" != wantLine {
			t.Fatalf("reporter line %d = %q, want %q", i, lines[i]+"\n", wantLine)
		}
	}
	timeFields := strings.Split(lines[5], ":")
	if len(timeFields) != 4 || timeFields[0] != "TIME" || timeFields[1] != "id" || timeFields[2] != "phase" || timeFields[3] == "" {
		t.Fatalf("time reporter line = %q", lines[5])
	}
	if lines[6]+"\n" != want[5] {
		t.Fatalf("reporter exit line = %q, want %q", lines[6]+"\n", want[5])
	}
}

func TestEncodeJSONBase64(t *testing.T) {
	got := EncodeJSONBase64(struct {
		Value string `json:"value"`
	}{Value: "ok"})
	if got != "eyJ2YWx1ZSI6Im9rIn0=" {
		t.Fatalf("EncodeJSONBase64 = %q", got)
	}
}
