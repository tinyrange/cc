//go:build !windows

package guestagent

import (
	"archive/tar"
	"bytes"
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
	if _, err := tarTarget("/tmp/out", true, "../escape"); err == nil || !strings.Contains(err.Error(), "unsafe tar path") {
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

	got := buf.String()
	for _, want := range []string{
		"BEGIN:id\n",
		"OUT:id:b3V0\n",
		"ERR:id:ZXJy\n",
		"CTL:id:Y3Rs\n",
		"USE:id:usage64\n",
		"TIME:id:phase:",
		"EXIT:id:7\n",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("reporter output missing %q in %q", want, got)
		}
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
