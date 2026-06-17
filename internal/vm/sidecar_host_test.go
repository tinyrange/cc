package vm

import (
	"bytes"
	"context"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/imagefs"
	"j5.nz/cc/internal/oci"
	"j5.nz/cc/internal/virtio"
)

func TestSidecarCommandResolverUsesManagedResolver(t *testing.T) {
	root := sidecarResolverRoot(t)
	resolver := &sidecarCommandResolver{
		root:    imagefs.NewHostFS(root, nil),
		baseEnv: []string{"PATH=/bin", "BASE=1"},
		workDir: "/workspace",
	}
	got, err := resolver.resolve(client.ExecRequest{
		Command: []string{"tool", "arg"},
		Env:     []string{"EXTRA=1"},
		Stdin:   []byte("input"),
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if strings.Join(got.Command, " ") != "/bin/tool arg" {
		t.Fatalf("command = %#v", got.Command)
	}
	if !got.SkipResolve {
		t.Fatalf("SkipResolve = false")
	}
	if got.WorkDir != "/workspace" {
		t.Fatalf("WorkDir = %q", got.WorkDir)
	}
	if string(got.Stdin) != "input" {
		t.Fatalf("Stdin = %q", got.Stdin)
	}
	if !envHas(got.Env, "BASE=1") || !envHas(got.Env, "EXTRA=1") {
		t.Fatalf("Env = %#v", got.Env)
	}
}

func TestSidecarCommandResolverSkipsResolvedRequests(t *testing.T) {
	resolver := &sidecarCommandResolver{
		root:    imagefs.NewHostFS(sidecarResolverRoot(t), nil),
		baseEnv: []string{"PATH=/bin"},
		workDir: "/workspace",
	}
	req := client.ExecRequest{
		Command:     []string{"/already"},
		SkipResolve: true,
	}
	got, err := resolver.resolve(req)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got.WorkDir != "" {
		t.Fatalf("WorkDir = %q", got.WorkDir)
	}
	if strings.Join(got.Command, " ") != "/already" {
		t.Fatalf("Command = %#v", got.Command)
	}
}

func TestSidecarBlankRootPassthroughDecision(t *testing.T) {
	blankCore := newSidecarManagedCore(&sidecarManagedSession{}, nil)
	if !sidecarShouldPassthroughToWorker(blankCore, client.ExecRequest{Command: []string{"echo"}}) {
		t.Fatalf("blank unresolved exec should pass through to worker")
	}
	if sidecarShouldPassthroughToWorker(blankCore, client.ExecRequest{Kind: "fs_archive"}) {
		t.Fatalf("control request should use managed core")
	}
	if sidecarShouldPassthroughToWorker(blankCore, client.ExecRequest{Command: []string{"/bin/echo"}, SkipResolve: true}) {
		t.Fatalf("already-resolved request should use managed core")
	}
	imageCore := newSidecarManagedCore(&sidecarManagedSession{}, &sidecarCommandResolver{
		root:    imagefs.NewHostFS(sidecarResolverRoot(t), nil),
		baseEnv: []string{"PATH=/bin"},
	})
	if sidecarShouldPassthroughToWorker(imageCore, client.ExecRequest{Command: []string{"tool"}}) {
		t.Fatalf("image-backed exec should use managed core")
	}
}

func TestPrepareRunInInstanceExecAlternateImageUsesManagedResolver(t *testing.T) {
	baseImage := "@base"
	altImage := "alt"
	store := oci.NewStore(filepath.Join(t.TempDir(), "images"))
	root := imagefs.NewHostFS(sidecarResolverRoot(t), nil)
	_, err := store.SaveRootFS(context.Background(), altImage, root, oci.SaveOptions{
		Config: oci.RuntimeConfig{
			Env:        []string{"PATH=/bin", "BASE=1"},
			WorkingDir: "/workspace",
		},
	})
	if err != nil {
		t.Fatalf("SaveRootFS: %v", err)
	}
	host := &sidecarVMHost{images: store}
	instRoot := virtio.NewMountedFS(virtio.NewImageFS(imagefs.NewHostFS(t.TempDir(), nil), ""), nil)
	rootFS, ok := instRoot.(sidecarRootFS)
	if !ok {
		t.Fatalf("test rootfs does not implement sidecarRootFS")
	}
	inst := &sidecarInstance{
		rootFS: rootFS,
	}
	got, err := host.prepareRunInInstanceExec(context.Background(), inst, baseImage, client.RunRequest{
		Image:   altImage,
		Command: []string{"tool"},
		Env:     []string{"EXTRA=1"},
		Stdin:   []byte("input"),
	})
	if err != nil {
		t.Fatalf("prepareRunInInstanceExec: %v", err)
	}
	if strings.Join(got.Command, " ") != "/bin/tool" {
		t.Fatalf("Command = %#v", got.Command)
	}
	if got.RootDir != sidecarImageMountPath(altImage) {
		t.Fatalf("RootDir = %q", got.RootDir)
	}
	if got.WorkDir != "/workspace" {
		t.Fatalf("WorkDir = %q", got.WorkDir)
	}
	if !got.ReplaceEnv || !got.SkipResolve {
		t.Fatalf("ReplaceEnv/SkipResolve = %v/%v", got.ReplaceEnv, got.SkipResolve)
	}
	if !envHas(got.Env, "BASE=1") || !envHas(got.Env, "EXTRA=1") {
		t.Fatalf("Env = %#v", got.Env)
	}
	if string(got.Stdin) != "input" {
		t.Fatalf("Stdin = %q", got.Stdin)
	}
}

func TestSidecarExecResponseFromManagedSessionEvents(t *testing.T) {
	resp := sidecarExecResponse([]client.ExecEvent{
		{Kind: "stdout", Output: "out"},
		{Kind: "stderr", Output: "err"},
		{Kind: "exit", ExitCode: 7},
	})
	if resp.Output != "outerr" {
		t.Fatalf("Output = %q", resp.Output)
	}
	if resp.ExitCode != 7 {
		t.Fatalf("ExitCode = %d", resp.ExitCode)
	}
}

func TestCombineSidecarResources(t *testing.T) {
	var cleanupOrder []string
	resolver := &sidecarCommandResolver{workDir: "/work"}
	combined := combineSidecarResources(
		sidecarStartResources{
			env:      []string{"A=1"},
			close:    func() { cleanupOrder = append(cleanupOrder, "first") },
			resolver: resolver,
		},
		sidecarStartResources{
			env:         []string{"B=2"},
			close:       func() { cleanupOrder = append(cleanupOrder, "second") },
			remote:      true,
			networkIPv4: "10.42.0.2",
		},
		sidecarStartResources{
			env:         []string{"C=3"},
			resolver:    &sidecarCommandResolver{workDir: "/other"},
			networkIPv4: "10.42.0.3",
		},
	)
	if strings.Join(combined.env, ",") != "A=1,B=2,C=3" {
		t.Fatalf("env = %#v", combined.env)
	}
	if !combined.remote {
		t.Fatalf("remote = false")
	}
	if combined.resolver != resolver {
		t.Fatalf("resolver was not preserved")
	}
	if combined.networkIPv4 != "10.42.0.2" {
		t.Fatalf("networkIPv4 = %q", combined.networkIPv4)
	}
	combined.closeAll()
	if strings.Join(cleanupOrder, ",") != "first,second" {
		t.Fatalf("cleanup order = %#v", cleanupOrder)
	}
}

func TestPrepareSidecarUnixListener(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix sockets are not available on windows")
	}
	socketPath, ln, cleanup, err := prepareSidecarUnixListener(t.TempDir(), "test")
	if err != nil {
		t.Fatalf("prepareSidecarUnixListener: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err == nil {
			_ = conn.Close()
		}
		done <- err
	}()
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("dial listener: %v", err)
	}
	_ = conn.Close()
	if err := <-done; err != nil {
		t.Fatalf("accept: %v", err)
	}
	cleanup()
	if _, err := os.Stat(socketPath); !os.IsNotExist(err) {
		t.Fatalf("socket cleanup stat err = %v, want not exist", err)
	}
}

func TestServeSidecarUnixOnce(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix sockets are not available on windows")
	}
	socketPath, cleanup, err := serveSidecarUnixOnce(t.TempDir(), "test", func(conn net.Conn) error {
		_, err := conn.Write([]byte("hello"))
		return err
	})
	if err != nil {
		t.Fatalf("serveSidecarUnixOnce: %v", err)
	}
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("dial listener: %v", err)
	}
	buf := make([]byte, len("hello"))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	_ = conn.Close()
	if string(buf) != "hello" {
		t.Fatalf("payload = %q", string(buf))
	}
	cleanup()
	if _, err := os.Stat(socketPath); !os.IsNotExist(err) {
		t.Fatalf("socket cleanup stat err = %v, want not exist", err)
	}
}

func TestServeSidecarUnixOnceConnCanLeaveConnectionOpen(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix sockets are not available on windows")
	}
	dir, err := os.MkdirTemp("", "ccsc-")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	defer os.RemoveAll(dir)
	ready := make(chan struct{})
	release := make(chan struct{})
	socketPath, cleanup, err := serveSidecarUnixOnceConn(dir, "test", false, func(conn net.Conn) error {
		close(ready)
		<-release
		_, err := conn.Write([]byte("still-open"))
		return err
	})
	if err != nil {
		t.Fatalf("serveSidecarUnixOnceConn: %v", err)
	}
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("dial listener: %v", err)
	}
	<-ready
	close(release)
	buf := make([]byte, len("still-open"))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(buf) != "still-open" {
		t.Fatalf("payload = %q", string(buf))
	}
	_ = conn.Close()
	cleanup()
}

func TestSidecarLaunchCommand(t *testing.T) {
	t.Setenv(sidecarModeEnv, "")
	cmd := sidecarLaunchCommand("/tmp/ccvm", "/cache", "/tmp/control.sock", []string{"EXTRA=1"})
	if cmd.Path != "/tmp/ccvm" {
		t.Fatalf("path = %q", cmd.Path)
	}
	if got := strings.Join(cmd.Args, " "); got != "/tmp/ccvm -worker -cache-dir /cache" {
		t.Fatalf("args = %q", got)
	}
	if cmd.Stderr != os.Stderr {
		t.Fatalf("stderr was not inherited")
	}
	if !envHas(cmd.Env, sidecarDisableEnv+"=1") {
		t.Fatalf("env missing %s=1: %#v", sidecarDisableEnv, cmd.Env)
	}
	if !envHas(cmd.Env, sidecarControlEnv+"=/tmp/control.sock") {
		t.Fatalf("env missing control socket: %#v", cmd.Env)
	}
	if !envHas(cmd.Env, "EXTRA=1") {
		t.Fatalf("env missing extra entry: %#v", cmd.Env)
	}
}

func TestReadSidecarStartupHello(t *testing.T) {
	got, err := readSidecarStartupHello(bytes.NewBufferString(`{"addr":"127.0.0.1:1234"}`))
	if err != nil {
		t.Fatalf("readSidecarStartupHello: %v", err)
	}
	if got.Addr != "127.0.0.1:1234" {
		t.Fatalf("Addr = %q", got.Addr)
	}
}

func TestReadSidecarStartupHelloRejectsMalformedJSON(t *testing.T) {
	_, err := readSidecarStartupHello(bytes.NewBufferString(`{`))
	if err == nil || !strings.Contains(err.Error(), "read sidecar startup banner") {
		t.Fatalf("err = %v", err)
	}
}

func TestReadSidecarStartupHelloRejectsErrorBanner(t *testing.T) {
	_, err := readSidecarStartupHello(bytes.NewBufferString(`{"kind":"error","detail":"no host support"}`))
	if err == nil || !strings.Contains(err.Error(), "sidecar ccvm failed to start: no host support") {
		t.Fatalf("err = %v", err)
	}
}

func TestReadSidecarStartupHelloRejectsMissingAddress(t *testing.T) {
	_, err := readSidecarStartupHello(bytes.NewBufferString(`{"addr":"   "}`))
	if err == nil || !strings.Contains(err.Error(), "did not report an address") {
		t.Fatalf("err = %v", err)
	}
}

func TestSidecarWorkerDialTarget(t *testing.T) {
	network, address := sidecarWorkerDialTarget("tcp://127.0.0.1:1234")
	if network != "tcp" || address != "127.0.0.1:1234" {
		t.Fatalf("tcp target = %q %q", network, address)
	}
	network, address = sidecarWorkerDialTarget("/tmp/worker.sock")
	if network != "unix" || address != "/tmp/worker.sock" {
		t.Fatalf("unix target = %q %q", network, address)
	}
}

func TestDialSidecarWorkerReadsHello(t *testing.T) {
	addr, done := serveSidecarWorkerFrame(t, WorkerFrame{Type: WorkerFrameHello})
	worker, err := dialSidecarWorker(context.Background(), "tcp://"+addr)
	if err != nil {
		t.Fatalf("dialSidecarWorker: %v", err)
	}
	if worker == nil || worker.codec == nil {
		t.Fatalf("worker client was not initialized")
	}
	_ = worker.Close()
	if err := <-done; err != nil {
		t.Fatalf("server: %v", err)
	}
}

func TestDialSidecarWorkerRejectsNonHello(t *testing.T) {
	addr, done := serveSidecarWorkerFrame(t, WorkerFrame{Type: WorkerFrameError})
	worker, err := dialSidecarWorker(context.Background(), "tcp://"+addr)
	if worker != nil {
		_ = worker.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "before hello") {
		t.Fatalf("err = %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("server: %v", err)
	}
}

func serveSidecarWorkerFrame(t *testing.T, frame WorkerFrame) (string, <-chan error) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		defer ln.Close()
		conn, err := ln.Accept()
		if err != nil {
			done <- err
			return
		}
		codec := NewWorkerCodec(conn)
		if err := codec.Send(frame); err != nil {
			_ = codec.Close()
			done <- err
			return
		}
		done <- codec.Close()
	}()
	return ln.Addr().String(), done
}

func TestSidecarDaemonCloseRunsCleanupsOnceInReverseOrder(t *testing.T) {
	var order []string
	daemon := &sidecarDaemon{cleanups: []func(){
		func() { order = append(order, "first") },
		nil,
		func() { order = append(order, "second") },
	}}
	if err := daemon.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := daemon.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if got := strings.Join(order, ","); got != "second,first" {
		t.Fatalf("cleanup order = %q", got)
	}
}

func TestWaitSidecarCommand(t *testing.T) {
	cmd := exec.Command("true")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start true: %v", err)
	}
	if err := waitSidecarCommand(cmd, time.Second); err != nil {
		t.Fatalf("wait true: %v", err)
	}

	slow := exec.Command("sh", "-c", "sleep 10")
	if err := slow.Start(); err != nil {
		t.Fatalf("start sleep: %v", err)
	}
	start := time.Now()
	err := waitSidecarCommand(slow, 10*time.Millisecond)
	if err == nil {
		t.Fatalf("wait sleep unexpectedly succeeded")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("wait sleep took %s, want timeout kill", elapsed)
	}
}

func sidecarResolverRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "bin", "tool"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func envHas(env []string, want string) bool {
	for _, entry := range env {
		if entry == want {
			return true
		}
	}
	return false
}
