package vm

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"j5.nz/cc/client"
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

func TestManagerRunDelegatesCrossImageExecToBackend(t *testing.T) {
	inst := &fakeInstance{waitCh: make(chan error, 1)}
	var seen struct {
		running string
		image   string
	}
	mgr := NewManagerWithBackend(fakeBackend{
		instance: inst,
		runInInstanceFn: func(ctx context.Context, inst Instance, runningImage string, req client.RunRequest) (client.ExecResponse, error) {
			_ = ctx
			_ = inst
			seen.running = runningImage
			seen.image = req.Image
			return client.ExecResponse{ExitCode: 0, Output: "ok"}, nil
		},
	})
	mgr.supports = func() error { return nil }

	if _, err := mgr.Start(context.Background(), client.CreateInstanceRequest{Image: "alpine"}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	resp, err := mgr.Run(context.Background(), client.RunRequest{
		Image:   "niimath",
		Command: []string{"niimath"},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if resp.Output != "ok" {
		t.Fatalf("Run().Output = %q, want %q", resp.Output, "ok")
	}
	if seen.running != "alpine" || seen.image != "niimath" {
		t.Fatalf("backend saw running=%q image=%q", seen.running, seen.image)
	}
}

func TestManagerRunSupportsMultipleImagesOnBlankRunningVM(t *testing.T) {
	inst := &fakeInstance{waitCh: make(chan error, 1)}
	var seen []struct {
		running string
		image   string
	}
	mgr := NewManagerWithBackend(fakeBackend{
		instance: inst,
		runInInstanceFn: func(ctx context.Context, inst Instance, runningImage string, req client.RunRequest) (client.ExecResponse, error) {
			_ = ctx
			_ = inst
			seen = append(seen, struct {
				running string
				image   string
			}{
				running: runningImage,
				image:   req.Image,
			})
			return client.ExecResponse{ExitCode: 0, Output: req.Image}, nil
		},
	})
	mgr.supports = func() error { return nil }

	if _, err := mgr.StartBlank(context.Background(), client.StartInstanceRequest{}); err != nil {
		t.Fatalf("StartBlank() error = %v", err)
	}

	for _, image := range []string{"fsl", "niimath"} {
		resp, err := mgr.Run(context.Background(), client.RunRequest{
			Image:   image,
			Command: []string{image},
		})
		if err != nil {
			t.Fatalf("Run(%q) error = %v", image, err)
		}
		if resp.Output != image {
			t.Fatalf("Run(%q).Output = %q, want %q", image, resp.Output, image)
		}
	}

	if len(seen) != 2 {
		t.Fatalf("backend call count = %d, want 2", len(seen))
	}
	if seen[0].running != "" || seen[0].image != "fsl" {
		t.Fatalf("first backend call = %#v", seen[0])
	}
	if seen[1].running != "" || seen[1].image != "niimath" {
		t.Fatalf("second backend call = %#v", seen[1])
	}
	if got := mgr.Status().Status; got != "running" {
		t.Fatalf("Status().Status = %q, want running", got)
	}
}

func TestManagerSupportsNamedInstances(t *testing.T) {
	first := &fakeInstance{waitCh: make(chan error, 1)}
	second := &fakeInstance{waitCh: make(chan error, 1)}
	var startCount int
	mgr := NewManagerWithBackend(fakeBackend{
		startFn: func(req client.CreateInstanceRequest) (Instance, error) {
			startCount++
			if startCount == 1 {
				return first, nil
			}
			return second, nil
		},
	})
	mgr.supports = func() error { return nil }
	mgr.capabilities = func() client.CapabilitiesResponse {
		return client.CapabilitiesResponse{MaxInstances: 2}
	}

	if _, err := mgr.Start(context.Background(), client.CreateInstanceRequest{ID: "one", Image: "alpine"}); err != nil {
		t.Fatalf("Start(one) error = %v", err)
	}
	if _, err := mgr.Start(context.Background(), client.CreateInstanceRequest{ID: "two", Image: "busybox"}); err != nil {
		t.Fatalf("Start(two) error = %v", err)
	}

	if got := mgr.StatusOf("one").Image; got != "alpine" {
		t.Fatalf("StatusOf(one).Image = %q, want alpine", got)
	}
	if got := mgr.StatusOf("two").Image; got != "busybox" {
		t.Fatalf("StatusOf(two).Image = %q, want busybox", got)
	}
	statuses := mgr.Statuses()
	if len(statuses) != 2 {
		t.Fatalf("Statuses() len = %d, want 2", len(statuses))
	}
	if statuses[0].ID != "one" || statuses[1].ID != "two" {
		t.Fatalf("Statuses() = %#v, want sorted named instances", statuses)
	}
}

func TestManagerEnforcesInstanceCapacity(t *testing.T) {
	inst := &fakeInstance{waitCh: make(chan error, 1)}
	mgr := NewManagerWithBackend(fakeBackend{instance: inst})
	mgr.supports = func() error { return nil }
	mgr.capabilities = func() client.CapabilitiesResponse {
		return client.CapabilitiesResponse{MaxInstances: 1}
	}

	if _, err := mgr.Start(context.Background(), client.CreateInstanceRequest{ID: "one", Image: "alpine"}); err != nil {
		t.Fatalf("Start(one) error = %v", err)
	}
	if _, err := mgr.Start(context.Background(), client.CreateInstanceRequest{ID: "two", Image: "busybox"}); err == nil {
		t.Fatal("Start(two) error = nil, want capacity error")
	}
}

func TestManagerReservesCapacityWhileStartIsInFlight(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	inst := &fakeInstance{waitCh: make(chan error, 1)}
	mgr := NewManagerWithBackend(fakeBackend{
		startFn: func(req client.CreateInstanceRequest) (Instance, error) {
			close(started)
			<-release
			return inst, nil
		},
	})
	mgr.supports = func() error { return nil }
	mgr.capabilities = func() client.CapabilitiesResponse {
		return client.CapabilitiesResponse{MaxInstances: 1}
	}

	firstDone := make(chan error, 1)
	go func() {
		_, err := mgr.Start(context.Background(), client.CreateInstanceRequest{ID: "one", Image: "alpine"})
		firstDone <- err
	}()
	<-started

	if got := mgr.StatusOf("one").Status; got != "starting" {
		t.Fatalf("StatusOf(one).Status = %q, want starting", got)
	}
	if _, err := mgr.Start(context.Background(), client.CreateInstanceRequest{ID: "two", Image: "busybox"}); err == nil {
		t.Fatal("Start(two) error = nil, want capacity error while first start is in flight")
	}

	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first Start() error = %v", err)
	}
	if got := mgr.StatusOf("one").Status; got != "running" {
		t.Fatalf("StatusOf(one).Status = %q, want running", got)
	}
}

func TestManagerClearsStartReservationAfterBackendError(t *testing.T) {
	attempts := 0
	inst := &fakeInstance{waitCh: make(chan error, 1)}
	mgr := NewManagerWithBackend(fakeBackend{
		startFn: func(req client.CreateInstanceRequest) (Instance, error) {
			attempts++
			if attempts == 1 {
				return nil, fmt.Errorf("boom")
			}
			return inst, nil
		},
	})
	mgr.supports = func() error { return nil }
	mgr.capabilities = func() client.CapabilitiesResponse {
		return client.CapabilitiesResponse{MaxInstances: 1}
	}

	if _, err := mgr.Start(context.Background(), client.CreateInstanceRequest{Image: "alpine"}); err == nil {
		t.Fatal("first Start() error = nil, want backend error")
	}
	state, err := mgr.Start(context.Background(), client.CreateInstanceRequest{Image: "alpine"})
	if err != nil {
		t.Fatalf("second Start() error = %v", err)
	}
	if state.Status != "running" {
		t.Fatalf("second Start().Status = %q, want running", state.Status)
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

func TestManagerRunMountsRuntimeSharesBeforeExec(t *testing.T) {
	inst := &fakeInstance{waitCh: make(chan error, 1)}
	var mounted []client.ShareMount
	var got client.ExecRequest
	inst.addShareFn = func(share client.ShareMount) error {
		mounted = append(mounted, share)
		return nil
	}
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
		Image: "alpine",
		Shares: []client.ShareMount{{
			Source: "/host/share",
			Mount:  "/.share/demo",
		}},
		Command: []string{"cat", "/.share/demo/hello.txt"},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(mounted) != 1 {
		t.Fatalf("mounted shares = %d, want 1", len(mounted))
	}
	if mounted[0].Mount != "/.share/demo" {
		t.Fatalf("mounted share path = %q, want %q", mounted[0].Mount, "/.share/demo")
	}
	if len(got.Command) != 2 || got.Command[1] != "/.share/demo/hello.txt" {
		t.Fatalf("exec command = %q, want guest share path", got.Command)
	}
}

func TestManagerAddPortForwardUpdatesRunningInstance(t *testing.T) {
	forward := client.PortForward{HostPort: 8080, GuestPort: 8080}
	inst := &fakeInstance{}
	var seen client.PortForward
	inst.forwardFn = func(f client.PortForward) error {
		seen = f
		return nil
	}
	mgr := NewManagerWithBackend(fakeBackend{
		startFn: func(req client.CreateInstanceRequest) (Instance, error) {
			return inst, nil
		},
	})
	mgr.supports = func() error { return nil }

	if _, err := mgr.Start(context.Background(), client.CreateInstanceRequest{Image: "alpine"}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := mgr.AddPortForward(context.Background(), forward); err != nil {
		t.Fatalf("AddPortForward() error = %v", err)
	}
	if seen != forward {
		t.Fatalf("forward = %#v, want %#v", seen, forward)
	}
}

func TestManagerRunStreamFallsBackToOneShotRunWhenNoInstance(t *testing.T) {
	var seen client.RunRequest
	mgr := NewManagerWithBackend(fakeBackend{
		runStreamFn: func(ctx context.Context, req client.RunRequest, inputs <-chan client.ExecInput, onEvent func(client.ExecEvent) error) error {
			_ = ctx
			_ = inputs
			seen = req
			if err := onEvent(client.ExecEvent{Kind: "stdout", Stream: "stdout", Output: "hello", Data: []byte("hello")}); err != nil {
				return err
			}
			return onEvent(client.ExecEvent{Kind: "exit", ExitCode: 7})
		},
	})
	mgr.supports = func() error { return nil }

	var events []client.ExecEvent
	err := mgr.RunStream(context.Background(), client.RunRequest{
		Image:   "alpine",
		Command: []string{"echo", "hello"},
	}, nil, func(event client.ExecEvent) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatalf("RunStream() error = %v", err)
	}
	if seen.Image != "alpine" || len(seen.Command) != 2 {
		t.Fatalf("backend Run saw %#v", seen)
	}
	if len(events) != 2 || events[0].Kind != "stdout" || events[0].Output != "hello" || events[1].Kind != "exit" || events[1].ExitCode != 7 {
		t.Fatalf("events = %#v", events)
	}
}

func TestManagerRunStreamDelegatesCrossImageExecToBackend(t *testing.T) {
	inst := &fakeInstance{waitCh: make(chan error, 1)}
	var seen struct {
		running string
		image   string
	}
	mgr := NewManagerWithBackend(fakeBackend{
		instance: inst,
		runInStreamFn: func(ctx context.Context, inst Instance, runningImage string, req client.RunRequest, inputs <-chan client.ExecInput, onEvent func(client.ExecEvent) error) error {
			_ = ctx
			_ = inst
			_ = inputs
			seen.running = runningImage
			seen.image = req.Image
			if err := onEvent(client.ExecEvent{Kind: "stdout", Stream: "stdout", Output: "ok", Data: []byte("ok")}); err != nil {
				return err
			}
			return onEvent(client.ExecEvent{Kind: "exit"})
		},
	})
	mgr.supports = func() error { return nil }

	if _, err := mgr.Start(context.Background(), client.CreateInstanceRequest{Image: "alpine"}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	var events []client.ExecEvent
	err := mgr.RunStream(context.Background(), client.RunRequest{
		Image:   "niimath",
		Command: []string{"niimath"},
	}, nil, func(event client.ExecEvent) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatalf("RunStream() error = %v", err)
	}
	if seen.running != "alpine" || seen.image != "niimath" {
		t.Fatalf("backend saw running=%q image=%q", seen.running, seen.image)
	}
	if len(events) != 2 || events[0].Output != "ok" || events[1].Kind != "exit" {
		t.Fatalf("events = %#v", events)
	}
}

func TestLoadAMD64EmulatorReadsQEMU(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("amd64 emulation helper is only enabled on arm64 hosts")
	}
	image := &oci.Image{
		Architecture: "amd64",
	}

	qemu, err := PrepareAMD64Emulator(context.Background(), image, func(ctx context.Context, repo, packageName, innerPath string) (string, error) {
		_ = ctx
		if repo != "community" || packageName != "qemu-x86_64" || innerPath != "usr/bin/qemu-x86_64" {
			t.Fatalf("unexpected package lookup %q %q %q", repo, packageName, innerPath)
		}
		return "/tmp/qemu-static", nil
	})
	if err != nil {
		t.Fatalf("PrepareAMD64Emulator() error = %v", err)
	}
	if qemu != "/tmp/qemu-static" {
		t.Fatalf("qemu path = %q, want %q", qemu, "/tmp/qemu-static")
	}
}

func TestNeedsAMD64EmulationDependsOnHostArchitecture(t *testing.T) {
	got := NeedsAMD64Emulation(&oci.Image{Architecture: "arm64"})
	want := runtime.GOARCH == "arm64"
	if got != want {
		t.Fatalf("NeedsAMD64Emulation(arm64 image) = %v, want %v", got, want)
	}
}

type fakeBackend struct {
	instance        Instance
	err             error
	runResp         client.ExecResponse
	runFn           func(client.RunRequest) (client.ExecResponse, error)
	runStreamFn     func(context.Context, client.RunRequest, <-chan client.ExecInput, func(client.ExecEvent) error) error
	startFn         func(client.CreateInstanceRequest) (Instance, error)
	runInInstanceFn func(context.Context, Instance, string, client.RunRequest) (client.ExecResponse, error)
	runInStreamFn   func(context.Context, Instance, string, client.RunRequest, <-chan client.ExecInput, func(client.ExecEvent) error) error
}

func (f fakeBackend) Start(ctx context.Context, req client.CreateInstanceRequest) (Instance, error) {
	return f.StartStream(ctx, req, nil)
}

func (f fakeBackend) StartStream(ctx context.Context, req client.CreateInstanceRequest, onEvent func(client.BootEvent) error) (Instance, error) {
	_ = ctx
	_ = onEvent
	if f.startFn != nil {
		return f.startFn(req)
	}
	_ = req
	return f.instance, f.err
}

func (f fakeBackend) StartBlank(ctx context.Context, req client.StartInstanceRequest) (Instance, error) {
	return f.StartBlankStream(ctx, req, nil)
}

func (f fakeBackend) StartBlankStream(ctx context.Context, req client.StartInstanceRequest, onEvent func(client.BootEvent) error) (Instance, error) {
	_ = ctx
	_ = req
	_ = onEvent
	return f.instance, f.err
}

func (f fakeBackend) Run(ctx context.Context, req client.RunRequest) (client.ExecResponse, error) {
	_ = ctx
	if f.runFn != nil {
		return f.runFn(req)
	}
	_ = req
	return f.runResp, f.err
}

func (f fakeBackend) RunStream(ctx context.Context, req client.RunRequest, inputs <-chan client.ExecInput, onEvent func(client.ExecEvent) error) error {
	if f.runStreamFn != nil {
		return f.runStreamFn(ctx, req, inputs, onEvent)
	}
	resp, err := f.Run(ctx, req)
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

func (f fakeBackend) RunInInstance(ctx context.Context, inst Instance, runningImage string, req client.RunRequest) (client.ExecResponse, error) {
	if f.runInInstanceFn != nil {
		return f.runInInstanceFn(ctx, inst, runningImage, req)
	}
	_ = ctx
	_ = runningImage
	if inst == nil {
		return client.ExecResponse{}, f.err
	}
	for _, share := range req.Shares {
		if err := inst.AddShare(ctx, share); err != nil {
			return client.ExecResponse{}, err
		}
	}
	return inst.Exec(ctx, client.ExecRequest{
		Command: append([]string(nil), req.Command...),
		Env:     append([]string(nil), req.Env...),
		WorkDir: req.WorkDir,
		User:    req.User,
		Stdin:   append([]byte(nil), req.Stdin...),
		TTY:     req.TTY,
		Cols:    req.Cols,
		Rows:    req.Rows,
	})
}

func (f fakeBackend) RunInInstanceStream(ctx context.Context, inst Instance, runningImage string, req client.RunRequest, inputs <-chan client.ExecInput, onEvent func(client.ExecEvent) error) error {
	if f.runInStreamFn != nil {
		return f.runInStreamFn(ctx, inst, runningImage, req, inputs, onEvent)
	}
	if inst == nil {
		return f.err
	}
	for _, share := range req.Shares {
		if err := inst.AddShare(ctx, share); err != nil {
			return err
		}
	}
	return inst.ExecStream(ctx, runExecRequest(req), inputs, onEvent)
}

type fakeInstance struct {
	waitCh     chan error
	closed     int
	execResp   client.ExecResponse
	execErr    error
	addShareFn func(client.ShareMount) error
	forwardFn  func(client.PortForward) error
	execFn     func(client.ExecRequest) (client.ExecResponse, error)
	streamFn   func(client.ExecRequest, <-chan client.ExecInput, func(client.ExecEvent) error) error
}

func (f *fakeInstance) AddShare(ctx context.Context, share client.ShareMount) error {
	_ = ctx
	if f.addShareFn != nil {
		return f.addShareFn(share)
	}
	return nil
}

func (f *fakeInstance) AddPortForward(ctx context.Context, forward client.PortForward) error {
	_ = ctx
	if f.forwardFn != nil {
		return f.forwardFn(forward)
	}
	return nil
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
