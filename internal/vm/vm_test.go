package vm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/imagefs"
	"j5.nz/cc/internal/oci"
)

func TestManagerStartShutdownLifecycle(t *testing.T) {
	inst := &fakeInstance{waitCh: make(chan error, 1)}
	mgr := NewManagerWithBackend(fakeBackend{instance: inst})
	mgr.supports = func() error { return nil }

	state, err := mgr.Start(context.Background(), client.CreateInstanceRequest{Image: "alpine"})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if state.Status != "running" {
		t.Fatalf("Start().Status = %q, want running", state.Status)
	}

	if err := mgr.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}

	if got := mgr.Status().Status; got != "stopped" {
		t.Fatalf("Status().Status = %q, want stopped", got)
	}
	if inst.closed != 1 {
		t.Fatalf("instance Close() count = %d, want 1", inst.closed)
	}
}

func TestManagerClearsRunningStateWhenInstanceExits(t *testing.T) {
	inst := &fakeInstance{waitCh: make(chan error, 1)}
	mgr := NewManagerWithBackend(fakeBackend{instance: inst})
	mgr.supports = func() error { return nil }

	if _, err := mgr.Start(context.Background(), client.CreateInstanceRequest{Image: "alpine"}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	inst.waitCh <- nil
	close(inst.waitCh)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if mgr.Status().Status == "stopped" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("manager never transitioned to stopped after instance exit")
}

func TestManagerRunDelegatesToRunningInstanceExec(t *testing.T) {
	inst := &fakeInstance{
		waitCh:   make(chan error, 1),
		execResp: client.ExecResponse{ExitCode: 0, Output: "ok"},
	}
	mgr := NewManagerWithBackend(fakeBackend{instance: inst})
	mgr.supports = func() error { return nil }

	if _, err := mgr.Start(context.Background(), client.CreateInstanceRequest{Image: "alpine"}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	resp, err := mgr.Run(context.Background(), client.RunRequest{
		Image:   "alpine",
		Command: []string{"echo", "hello"},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if resp.Output != "ok" {
		t.Fatalf("Run().Output = %q, want %q", resp.Output, "ok")
	}
}

func TestManagerRunAllowsConcurrentExecsOnRunningInstance(t *testing.T) {
	inst := &fakeInstance{waitCh: make(chan error, 1)}
	inst.execFn = func(req client.ExecRequest) (client.ExecResponse, error) {
		time.Sleep(20 * time.Millisecond)
		return client.ExecResponse{ExitCode: 0, Output: fmt.Sprintf("ran:%s", req.Command[0])}, nil
	}
	mgr := NewManagerWithBackend(fakeBackend{instance: inst})
	mgr.supports = func() error { return nil }

	if _, err := mgr.Start(context.Background(), client.CreateInstanceRequest{Image: "alpine"}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	var wg sync.WaitGroup
	results := make(chan string, 2)
	for _, cmd := range []string{"one", "two"} {
		wg.Add(1)
		go func(cmd string) {
			defer wg.Done()
			resp, err := mgr.Run(context.Background(), client.RunRequest{
				Image:   "alpine",
				Command: []string{cmd},
			})
			if err != nil {
				results <- "err:" + err.Error()
				return
			}
			results <- resp.Output
		}(cmd)
	}
	wg.Wait()
	close(results)

	got := map[string]bool{}
	for result := range results {
		got[result] = true
	}
	for _, want := range []string{"ran:one", "ran:two"} {
		if !got[want] {
			t.Fatalf("missing concurrent exec result %q in %v", want, got)
		}
	}
}

func TestManagerRunForwardsStdinToRunningInstance(t *testing.T) {
	inst := &fakeInstance{waitCh: make(chan error, 1)}
	var got client.ExecRequest
	inst.execFn = func(req client.ExecRequest) (client.ExecResponse, error) {
		got = req
		return client.ExecResponse{ExitCode: 0}, nil
	}
	mgr := NewManagerWithBackend(fakeBackend{instance: inst})
	mgr.supports = func() error { return nil }

	if _, err := mgr.Start(context.Background(), client.CreateInstanceRequest{Image: "alpine"}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	_, err := mgr.Run(context.Background(), client.RunRequest{
		Image:   "alpine",
		Command: []string{"cat"},
		Stdin:   []byte("hello\n"),
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if string(got.Stdin) != "hello\n" {
		t.Fatalf("forwarded stdin = %q, want %q", string(got.Stdin), "hello\n")
	}
}

func TestPrepareImageForAMD64EmulationOverlaysQEMU(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("amd64 emulation overlay is only enabled on arm64 hosts")
	}

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
		t.Fatalf("MkdirAll(bin) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "bin", "sh"), []byte("busybox"), 0o755); err != nil {
		t.Fatalf("WriteFile(bin/sh) error = %v", err)
	}
	image := &oci.Image{
		Architecture: "amd64",
		RootFS:       imagefs.NewHostFS(root, nil),
	}

	prepared, err := prepareImageForAMD64Emulation(context.Background(), image, func(ctx context.Context, repo, packageName, innerPath string) ([]byte, error) {
		_ = ctx
		if repo != "community" || packageName != "qemu-x86_64" || innerPath != "usr/bin/qemu-x86_64" {
			t.Fatalf("unexpected package lookup %q %q %q", repo, packageName, innerPath)
		}
		return []byte("qemu-static"), nil
	})
	if err != nil {
		t.Fatalf("prepareImageForAMD64Emulation() error = %v", err)
	}
	entry, err := imagefs.LookupPath(prepared.RootFS, "/usr/bin/qemu-x86_64-static")
	if err != nil {
		t.Fatalf("LookupPath(/usr/bin/qemu-x86_64-static) error = %v", err)
	}
	data, err := entry.File.ReadAt(0, 32)
	if err != nil {
		t.Fatalf("ReadAt(qemu) error = %v", err)
	}
	if string(data) != "qemu-static" {
		t.Fatalf("qemu data = %q, want %q", string(data), "qemu-static")
	}

	baseEntry, err := imagefs.LookupPath(image.RootFS, "/usr/bin/qemu-x86_64-static")
	if err == nil && baseEntry.File != nil {
		t.Fatal("base image unexpectedly contains /usr/bin/qemu-x86_64-static")
	}
}

type fakeBackend struct {
	instance Instance
	err      error
	runResp  client.ExecResponse
}

func (f fakeBackend) Start(ctx context.Context, req client.CreateInstanceRequest) (Instance, error) {
	_ = ctx
	_ = req
	return f.instance, f.err
}

func (f fakeBackend) Run(ctx context.Context, req client.RunRequest) (client.ExecResponse, error) {
	_ = ctx
	_ = req
	return f.runResp, f.err
}

type fakeInstance struct {
	waitCh   chan error
	closed   int
	execResp client.ExecResponse
	execErr  error
	execFn   func(client.ExecRequest) (client.ExecResponse, error)
	streamFn func(client.ExecRequest, <-chan client.ExecInput, func(client.ExecEvent) error) error
}

func (f *fakeInstance) Exec(ctx context.Context, req client.ExecRequest) (client.ExecResponse, error) {
	_ = ctx
	if f.execFn != nil {
		return f.execFn(req)
	}
	return f.execResp, f.execErr
}

func (f *fakeInstance) ExecStream(ctx context.Context, req client.ExecRequest, inputs <-chan client.ExecInput, onEvent func(client.ExecEvent) error) error {
	_ = ctx
	if f.streamFn != nil {
		return f.streamFn(req, inputs, onEvent)
	}
	resp, err := f.Exec(ctx, req)
	if err != nil {
		return err
	}
	if resp.Output != "" && onEvent != nil {
		if err := onEvent(client.ExecEvent{Kind: "stdout", Stream: "stdout", Output: resp.Output, Data: []byte(resp.Output)}); err != nil {
			return err
		}
	}
	if onEvent != nil {
		return onEvent(client.ExecEvent{Kind: "exit", ExitCode: resp.ExitCode})
	}
	return nil
}

func (f *fakeInstance) Wait() error {
	err, ok := <-f.waitCh
	if !ok {
		return nil
	}
	return err
}

func (f *fakeInstance) Close() error {
	f.closed++
	select {
	case f.waitCh <- nil:
	default:
	}
	close(f.waitCh)
	return nil
}
