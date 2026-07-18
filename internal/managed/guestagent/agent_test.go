//go:build !windows

package guestagent

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"j5.nz/cc/client"
)

func TestRootUserRequestClassification(t *testing.T) {
	for _, user := range []string{"", "root", "0", "0:0", "root:wheel"} {
		if !isRootUserRequest(user) {
			t.Errorf("root user request %q was rejected", user)
		}
	}
	for _, user := range []string{"1000", "1000:1000", "nobody", "root:nobody"} {
		if isRootUserRequest(user) {
			t.Errorf("non-root user request %q was accepted", user)
		}
	}
}

func TestRootPathCleansRootAndName(t *testing.T) {
	if got := rootPath("/mnt/root", "../etc/passwd"); got != "/mnt/root/etc/passwd" {
		t.Fatalf("rootPath = %q", got)
	}
	if got := rootPath("/", "bin/sh"); got != "/bin/sh" {
		t.Fatalf("rootPath at root = %q", got)
	}
}

func TestValidateExecWorkDir(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "work"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "file"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validateExecWorkDir(root, "/work"); err != nil {
		t.Fatalf("validate directory workdir: %v", err)
	}
	if err := validateExecWorkDir(root, "/missing"); err == nil {
		t.Fatal("accepted a missing workdir")
	}
	if err := validateExecWorkDir(root, "/file"); err == nil {
		t.Fatal("accepted a regular file workdir")
	}
}

func TestExecCommandUsesRequestPATH(t *testing.T) {
	binDir := t.TempDir()
	command := filepath.Join(binDir, "guest-command")
	if err := os.WriteFile(command, []byte("#!/bin/sh\nprintf '%s' \"$RESULT\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())

	cmd := execCommand(request{
		Command: []string{"guest-command"},
		Env:     []string{"PATH=" + binDir, "RESULT=request-environment"},
	})
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("run command from request PATH: %v", err)
	}
	if string(output) != "request-environment" {
		t.Fatalf("command output = %q", output)
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

func TestWriteAndExtractTarPreservesHardLink(t *testing.T) {
	srcDir := t.TempDir()
	first := filepath.Join(srcDir, "first")
	second := filepath.Join(srcDir, "second")
	if err := os.WriteFile(first, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(first, second); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(srcDir)
	if err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if err := WritePathTar(&archive, srcDir, filepath.Base(srcDir), info); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "out")
	if err := ExtractTarToPath(bytes.NewReader(archive.Bytes()), "", destination, true); err != nil {
		t.Fatal(err)
	}
	firstInfo, err := os.Stat(filepath.Join(destination, filepath.Base(srcDir), "first"))
	if err != nil {
		t.Fatal(err)
	}
	secondInfo, err := os.Stat(filepath.Join(destination, filepath.Base(srcDir), "second"))
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(firstInfo, secondInfo) {
		t.Fatal("extracted files do not share an inode")
	}
}

func TestArchiveHardlinkIdentityDoesNotDependOnReportedLinkCount(t *testing.T) {
	info := fakeArchiveFileInfo{sys: struct {
		Dev   uint64
		Ino   uint64
		Nlink uint64
	}{Dev: 7, Ino: 42, Nlink: 1}}
	if key, ok := archiveHardlinkKey(info); !ok || key != "7:42" {
		t.Fatalf("archiveHardlinkKey = %q, %v; want 7:42, true", key, ok)
	}
}

func TestManagedExecSignalIsNotBlockedByStdinBackpressure(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	unblock := make(chan struct{})
	managed := &managedExec{stdin: blockingWriteCloser{unblock: unblock}, process: cmd.Process}
	writeStarted := make(chan struct{})
	go func() {
		close(writeStarted)
		_ = managed.write([]byte("pending input"))
	}()
	<-writeStarted

	signaled := make(chan error, 1)
	go func() { signaled <- managed.signal("KILL") }()
	select {
	case err := <-signaled:
		if err != nil {
			t.Fatalf("signal: %v", err)
		}
	case <-time.After(time.Second):
		close(unblock)
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatal("signal waited behind a blocked stdin write")
	}
	close(unblock)
	_ = cmd.Wait()
}

type fakeArchiveFileInfo struct{ sys any }

func (fakeArchiveFileInfo) Name() string       { return "file" }
func (fakeArchiveFileInfo) Size() int64        { return 0 }
func (fakeArchiveFileInfo) Mode() os.FileMode  { return 0o644 }
func (fakeArchiveFileInfo) ModTime() time.Time { return time.Time{} }
func (fakeArchiveFileInfo) IsDir() bool        { return false }
func (f fakeArchiveFileInfo) Sys() any         { return f.sys }

type blockingWriteCloser struct{ unblock <-chan struct{} }

func (w blockingWriteCloser) Write(p []byte) (int, error) {
	<-w.unblock
	return len(p), nil
}
func (blockingWriteCloser) Close() error { return nil }

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

func TestExtractTarToPathEnforcesCallerBudgets(t *testing.T) {
	var archive bytes.Buffer
	if err := writeSingleFileTar(&archive, "payload", "0123456789"); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "payload")
	err := ExtractTarToPathContext(context.Background(), bytes.NewReader(archive.Bytes()), "/", dst, false, &client.ArchiveLimits{
		MaxEntries:       1,
		MaxFileBytes:     9,
		MaxExpandedBytes: 100,
	})
	var limitErr *ArchiveLimitError
	if !errors.As(err, &limitErr) || limitErr.Resource != "file bytes" || limitErr.Limit != 9 || limitErr.Actual != 10 {
		t.Fatalf("limit error = %#v", err)
	}
	if _, statErr := os.Stat(dst); !os.IsNotExist(statErr) {
		t.Fatalf("oversized archive published destination: %v", statErr)
	}
}

func TestExtractTarToPathCancellationPreservesExistingFile(t *testing.T) {
	var archive bytes.Buffer
	if err := writeSingleFileTar(&archive, "payload", strings.Repeat("x", 4096)); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "payload")
	if err := os.WriteFile(dst, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}

	reader, writer := io.Pipe()
	wrotePrefix := make(chan struct{})
	go func() {
		_, _ = writer.Write(archive.Bytes()[:1024])
		close(wrotePrefix)
	}()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- ExtractTarToPathContext(ctx, reader, "/", dst, false, &client.ArchiveLimits{MaxFileBytes: 4096, MaxExpandedBytes: 4096})
	}()
	<-wrotePrefix
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("extraction error = %v, want context cancellation", err)
	}
	if got := readTestFile(t, dst); got != "keep" {
		t.Fatalf("existing file after canceled extraction = %q", got)
	}
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

func TestExtractTarToPathRejectsSymlinkDestinationParent(t *testing.T) {
	root := t.TempDir()
	destination := filepath.Join(root, "destination")
	outside := filepath.Join(root, "outside")
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, destination); err != nil {
		t.Fatal(err)
	}

	var archive bytes.Buffer
	tw := tar.NewWriter(&archive)
	if err := tw.WriteHeader(&tar.Header{Name: "payload", Typeflag: tar.TypeReg, Mode: 0o644, Size: 4}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("data")); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	err := ExtractTarToPath(bytes.NewReader(archive.Bytes()), "", filepath.Join(destination, "injected"), true)
	if !errors.Is(err, ErrUnsafeTarExtractionPath) {
		t.Fatalf("ExtractTarToPath error = %v, want unsafe path", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "injected")); !os.IsNotExist(err) {
		t.Fatalf("outside destination was modified: %v", err)
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
