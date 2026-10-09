//go:build linux

package guestinit

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"j5.nz/cc/internal/managed/guestagent"
	"j5.nz/cc/internal/vmruntime"
)

func TestSandboxProcessModes(t *testing.T) {
	for _, mode := range []string{"single", "group", ""} {
		for _, cancel := range []bool{false, true} {
			t.Run(mode+"/cancel="+strconv.FormatBool(cancel), func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "child")
				script := "sleep 30 & echo $! > " + path
				if cancel {
					script += "; wait"
				}
				cfg := config{Protocol: 1, WorkDir: "/", Env: []string{"PATH=/usr/bin:/bin"}, BeginMarker: vmruntime.CommandBeginMarker, OutputMarkerPref: vmruntime.CommandOutputMarker, ErrorMarkerPref: vmruntime.CommandErrorMarker, ControlMarkerPref: vmruntime.CommandControlMarker, ExitMarkerPrefix: vmruntime.CommandExitMarkerPref}
				managed := &managedExec{mode: mode}
				var output bytes.Buffer
				done := make(chan struct{})
				go func() {
					runManagedExec(cfg, &output, "mode", []string{"/bin/sh", "-c", script}, cfg.Env, "", "/", "", nil, managed, false, false, 0, 0, nil, func() {})
					close(done)
				}()
				var child int
				deadline := time.Now().Add(3 * time.Second)
				for time.Now().Before(deadline) {
					data, _ := os.ReadFile(path)
					child, _ = strconv.Atoi(strings.TrimSpace(string(data)))
					managed.stdinMu.Lock()
					started := managed.process != nil
					managed.stdinMu.Unlock()
					if child > 0 && started {
						break
					}
					time.Sleep(5 * time.Millisecond)
				}
				if child <= 0 {
					t.Fatal("child did not start")
				}
				defer syscall.Kill(child, syscall.SIGKILL)
				if cancel {
					if err := managed.signal("KILL"); err != nil {
						t.Fatal(err)
					}
				}
				select {
				case <-done:
				case <-time.After(4 * time.Second):
					t.Fatal("exec hung on descendant descriptors")
				}
				state, _, ok := procProcessState("/proc/" + strconv.Itoa(child) + "/stat")
				alive := ok && state != 'Z'
				if alive != (mode == "single") {
					t.Fatalf("child alive=%v mode=%q cancel=%v", alive, mode, cancel)
				}
				found := false
				for _, line := range strings.Split(output.String(), "\n") {
					event, _, ok, err := vmruntime.ParseManagedExecEventLine(line, "mode")
					if err != nil {
						t.Fatal(err)
					}
					if ok && event.Kind == "started" {
						found = true
					}
				}
				if !found {
					t.Fatal("missing started event")
				}
			})
		}
	}
}

func TestSandboxLaunchErrorBeforeExit(t *testing.T) {
	var output bytes.Buffer
	cfg := config{Protocol: 1, ControlMarkerPref: vmruntime.CommandControlMarker, ExitMarkerPrefix: vmruntime.CommandExitMarkerPref}
	runManagedExec(cfg, &output, "bad", []string{"/no/such/executable"}, nil, "", "/", "", nil, &managedExec{mode: "single"}, false, false, 0, 0, nil, func() {})
	found := false
	for _, line := range strings.Split(output.String(), "\n") {
		event, _, ok, err := vmruntime.ParseManagedExecEventLine(line, "bad")
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			continue
		}
		if event.Kind == "started" {
			t.Fatal("failed launch emitted started")
		}
		if event.Kind == "launch_error" {
			found = true
			if event.Error == "" {
				t.Fatal("missing error")
			}
		}
		if event.Kind == "exit" && !found {
			t.Fatal("exit preceded launch_error")
		}
	}
	if !found {
		t.Fatal("missing launch_error")
	}
}

func TestSandboxProtocolValidation(t *testing.T) {
	for _, request := range []execRequest{
		{SandboxProtocol: 2}, {ProcessMode: "family", SandboxProtocol: 1}, {ProcessMode: "single"},
	} {
		if validateSandboxRequest(request) == nil {
			t.Fatalf("accepted %+v", request)
		}
	}
	for _, request := range []execRequest{{}, {SandboxProtocol: 1, ProcessMode: "single"}, {SandboxProtocol: 1, ProcessMode: "group"}} {
		if err := validateSandboxRequest(request); err != nil {
			t.Fatal(err)
		}
	}
	if validateSandboxConfig(config{Protocol: 2}) == nil {
		t.Fatal("accepted unsupported config")
	}
}

type failingSandboxInput struct{}

func (failingSandboxInput) Read(p []byte) (int, error) {
	copy(p, "partial")
	return 7, errors.New("transfer interrupted")
}

func TestSandboxFileTransferAtomicAndBinary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file")
	data := []byte{0, 255, 1, 10, 0}
	if err := sandboxFilePut(context.Background(), path, false, bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := sandboxFileGet(path, &output); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(output.Bytes(), data) {
		t.Fatal("binary data changed")
	}
	if err := sandboxFilePut(context.Background(), path, false, strings.NewReader("overwrite")); !errors.Is(err, os.ErrExist) {
		t.Fatalf("no-clobber: %v", err)
	}
	if err := sandboxFilePut(context.Background(), path, true, failingSandboxInput{}); err == nil {
		t.Fatal("accepted failed transfer")
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, data) {
		t.Fatal("failed transfer replaced destination")
	}
	stages, _ := filepath.Glob(filepath.Join(dir, ".sa2-file-put-*"))
	if len(stages) != 0 {
		t.Fatal("leaked staging file")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sandboxFilePut(ctx, path, true, strings.NewReader("cancelled")); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	if err := sandboxFilePut(context.Background(), path, true, strings.NewReader("new")); err != nil {
		t.Fatal(err)
	}
	if err := sandboxFileGet(dir, io.Discard); err == nil {
		t.Fatal("read directory")
	}
	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	if err := sandboxFileGet(fifo, io.Discard); err == nil {
		t.Fatal("read fifo")
	}
}

func TestSandboxProxyRelayAndCancellation(t *testing.T) {
	target, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	go func() {
		conn, err := target.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = io.Copy(conn, conn)
	}()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stop := serveSandboxProxy(ctx, listener)
	defer stop()
	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, port, _ := net.SplitHostPort(target.Addr().String())
	// Pipeline binary payload with the header to verify buffered bytes are relayed.
	_, err = conn.Write([]byte(`{"version":1,"host":"localhost","port":"` + port + `"}` + "\n" + "hello\x00"))
	if err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(conn)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		Version int
		Error   string
	}
	if err := json.Unmarshal(line, &response); err != nil {
		t.Fatal(err)
	}
	if response.Version != 1 || response.Error != "" {
		t.Fatalf("response: %s", line)
	}
	got := make([]byte, 6)
	if _, err := io.ReadFull(reader, got); err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello\x00" {
		t.Fatalf("relay %q", got)
	}
	cancel()
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := reader.ReadByte(); err == nil {
		t.Fatal("cancel did not close connection")
	}
}

func TestSandboxProxyRejectsBadHeaders(t *testing.T) {
	for _, header := range []string{`{"version":2,"host":"localhost","port":"80"}` + "\n", strings.Repeat("x", sandboxProxyHeaderLimit+1) + "\n"} {
		server, conn := net.Pipe()
		done := make(chan struct{})
		go func() { handleSandboxProxy(context.Background(), server); close(done) }()
		go func() { _, _ = io.WriteString(conn, header) }()
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		line, err := bufio.NewReader(conn).ReadBytes('\n')
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(line, []byte(`"error"`)) {
			t.Fatalf("response %s", line)
		}
		_ = conn.Close()
		<-done
	}
}

func TestSandboxPutHelperSignalChild(t *testing.T) {
	path := os.Getenv("CC_TEST_SA2_PUT_PATH")
	if path == "" {
		return
	}
	_, err := runSandboxHelper([]string{"--sa2-file-put", path, "false"})
	if err == nil {
		os.Exit(2)
	}
	os.Exit(0)
}

func TestSandboxPutSignalRemovesStage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "destination")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, "-test.run=^TestSandboxPutHelperSignalChild$")
	cmd.Env = append(os.Environ(), "CC_TEST_SA2_PUT_PATH="+path)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	_, _ = stdin.Write([]byte("partial"))
	deadline := time.Now().Add(3 * time.Second)
	ready := false
	for time.Now().Before(deadline) {
		stages, _ := filepath.Glob(filepath.Join(dir, ".sa2-file-put-*"))
		if len(stages) > 0 {
			ready = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !ready {
		t.Fatal("helper did not create stage")
	}
	// Exercise the same signal dispatcher used by host guest-exec cancellation.
	managed := &managedExec{mode: "single"}
	managed.setProcess(cmd.Process, false, nil)
	active := guestagent.NewActiveExecSet()
	active.Add("put", managed)
	var control bytes.Buffer
	if !handleInitControlRequest(config{}, &control, active, execRequest{Kind: "signal", ID: "put", Signal: "TERM"}) {
		t.Fatal("signal request not handled")
	}
	// KVM escalates TERM to KILL after 500 ms. Verify cleanup before escalation,
	// independently of the race runtime's additional process-exit delay.
	deadline = time.Now().Add(500 * time.Millisecond)
	cleaned := false
	for time.Now().Before(deadline) {
		stages, _ := filepath.Glob(filepath.Join(dir, ".sa2-file-put-*"))
		if len(stages) == 0 {
			cleaned = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !cleaned {
		t.Fatal("TERM failed to remove staging within host kill grace")
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("signal did not unblock helper")
	}
	stages, _ := filepath.Glob(filepath.Join(dir, ".sa2-file-put-*"))
	if len(stages) != 0 {
		t.Fatal("signal leaked staging file")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("signal installed incomplete file: %v", err)
	}
}

func TestSandboxFileHelpersRequireAbsolutePath(t *testing.T) {
	for _, path := range []string{"", ".", "relative", "../relative"} {
		for _, args := range [][]string{{"--sa2-file-get", path}, {"--sa2-file-put", path, "false"}, {"--sa2-file-put", path, "true"}} {
			handled, err := runSandboxHelper(args)
			if !handled || err == nil || !strings.Contains(err.Error(), "absolute path") {
				t.Fatalf("dispatch %q: handled=%v err=%v", args, handled, err)
			}
		}
	}
}

type sandboxConcurrentCreateReader struct {
	path    string
	reader  io.Reader
	created bool
}

func (r *sandboxConcurrentCreateReader) Read(p []byte) (int, error) {
	if !r.created {
		r.created = true
		if err := os.WriteFile(r.path, []byte("competing writer"), 0600); err != nil {
			return 0, err
		}
	}
	return r.reader.Read(p)
}

func TestSandboxPutNoClobberAtAtomicInstall(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "raced-destination")
	// Create the destination after file-put's preflight check, while staging input.
	input := &sandboxConcurrentCreateReader{path: path, reader: strings.NewReader("upload")}
	if err := sandboxFilePut(context.Background(), path, false, input); !errors.Is(err, os.ErrExist) {
		t.Fatalf("atomic no-clobber: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "competing writer" {
		t.Fatalf("concurrent destination overwritten: %q", got)
	}
	stages, _ := filepath.Glob(filepath.Join(dir, ".sa2-file-put-*"))
	if len(stages) != 0 {
		t.Fatal("failed install leaked staging file")
	}
}
