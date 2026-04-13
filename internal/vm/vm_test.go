package vm

import (
	"context"
	"testing"
	"time"

	"j5.nz/cc/client"
)

func TestManagerStartShutdownLifecycle(t *testing.T) {
	inst := &fakeInstance{waitCh: make(chan error, 1)}
	mgr := NewManagerWithBackend(fakeBackend{instance: inst})
	mgr.supports = func() error { return nil }

	state, err := mgr.Start(context.Background(), client.StartVMRequest{Image: "alpine"})
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

	if _, err := mgr.Start(context.Background(), client.StartVMRequest{Image: "alpine"}); err != nil {
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

type fakeBackend struct {
	instance Instance
	err      error
	runResp  client.RunVMResponse
}

func (f fakeBackend) Start(ctx context.Context, req client.StartVMRequest) (Instance, error) {
	_ = ctx
	_ = req
	return f.instance, f.err
}

func (f fakeBackend) Run(ctx context.Context, req client.StartVMRequest) (client.RunVMResponse, error) {
	_ = ctx
	_ = req
	return f.runResp, f.err
}

type fakeInstance struct {
	waitCh chan error
	closed int
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
