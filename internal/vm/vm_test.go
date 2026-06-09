package vm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/fsmeta"
	"j5.nz/cc/internal/imagefs"
	"j5.nz/cc/internal/linuxabi"
	"j5.nz/cc/internal/oci"
	"j5.nz/cc/internal/virtio"
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

func TestManagerConsoleHistoryUsesRunningInstance(t *testing.T) {
	inst := &fakeInstance{waitCh: make(chan error, 1), console: "serial boot ok\n"}
	mgr := NewManagerWithBackend(fakeBackend{instance: inst})
	mgr.supports = func() error { return nil }

	if _, err := mgr.Start(context.Background(), client.CreateInstanceRequest{Image: "alpine"}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	history, err := mgr.ConsoleHistory(context.Background(), DefaultInstanceID)
	if err != nil {
		t.Fatalf("ConsoleHistory() error = %v", err)
	}
	if history != "serial boot ok\n" {
		t.Fatalf("ConsoleHistory() = %q, want serial boot ok", history)
	}
}

func TestHostCapabilityHelpersMatchBackendLimits(t *testing.T) {
	tests := []struct {
		name         string
		goos         string
		goarch       string
		wantLimits   []string
		wantNetworks []string
	}{
		{name: "linux amd64", goos: "linux", goarch: "amd64", wantLimits: []string{"memory_mb", "cpus"}, wantNetworks: []string{"user"}},
		{name: "linux arm64", goos: "linux", goarch: "arm64", wantLimits: []string{"memory_mb"}, wantNetworks: []string{}},
		{name: "darwin arm64", goos: "darwin", goarch: "arm64", wantLimits: []string{"memory_mb", "cpus"}, wantNetworks: []string{"user"}},
		{name: "windows amd64", goos: "windows", goarch: "amd64", wantLimits: []string{"memory_mb"}, wantNetworks: []string{"user"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fmt.Sprint(resourceLimitsForHost(tt.goos, tt.goarch)); got != fmt.Sprint(tt.wantLimits) {
				t.Fatalf("resourceLimitsForHost() = %v, want %v", got, tt.wantLimits)
			}
			if got := fmt.Sprint(networkModesForHost(tt.goos, tt.goarch)); got != fmt.Sprint(tt.wantNetworks) {
				t.Fatalf("networkModesForHost() = %v, want %v", got, tt.wantNetworks)
			}
		})
	}
}

func TestInProcessVMHostReportsDynamicCapabilities(t *testing.T) {
	maxInstances := 2
	host := newInProcessVMHost(fakeBackend{}, func() client.CapabilitiesResponse {
		return client.CapabilitiesResponse{
			Backend:      "kvm",
			MaxInstances: maxInstances,
			NetworkModes: []string{"user"},
		}
	})

	caps := host.HostCapabilities(context.Background())
	if caps.Backend != "kvm" || caps.MaxVMs != 2 || caps.Locality != "in-process" || !caps.SupportsL2 {
		t.Fatalf("HostCapabilities() = %#v, want in-process kvm with L2 and max 2", caps)
	}
	maxInstances = 4
	if got := host.HostCapabilities(context.Background()).MaxVMs; got != 4 {
		t.Fatalf("HostCapabilities().MaxVMs after update = %d, want 4", got)
	}
}

func TestManagerCanUseInjectedVMHost(t *testing.T) {
	inst := &fakeInstance{waitCh: make(chan error, 1)}
	host := &fakeVMHost{
		Backend: fakeBackend{instance: inst},
		caps:    VMHostCapabilities{Backend: "test", MaxVMs: 1, Locality: "sidecar"},
	}
	mgr := NewManagerWithHost(host)
	mgr.supports = func() error { return nil }

	if _, err := mgr.Start(context.Background(), client.CreateInstanceRequest{Image: "alpine"}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if _, err := mgr.Start(context.Background(), client.CreateInstanceRequest{ID: "two", Image: "busybox"}); err == nil {
		t.Fatal("Start(two) error = nil, want injected host capacity error")
	}
}

func TestPlacementVMHostReportsCombinedCapabilities(t *testing.T) {
	host := newPlacementVMHost(
		&fakeVMHost{caps: VMHostCapabilities{Backend: "one", MaxVMs: 1, SupportsL2: true}},
		&fakeVMHost{caps: VMHostCapabilities{Backend: "two", MaxVMs: 2, SupportsFSRPC: true}},
	)

	caps := host.HostCapabilities(context.Background())
	if caps.Backend != "placement" || caps.Locality != "mixed" || caps.MaxVMs != 3 || !caps.SupportsL2 || !caps.SupportsFSRPC {
		t.Fatalf("HostCapabilities() = %#v, want combined placement capabilities", caps)
	}

	unlimited := newPlacementVMHost(
		&fakeVMHost{caps: VMHostCapabilities{Backend: "one", MaxVMs: 1}},
		&fakeVMHost{caps: VMHostCapabilities{Backend: "two", MaxVMs: 0}},
	)
	if got := unlimited.HostCapabilities(context.Background()).MaxVMs; got != 0 {
		t.Fatalf("unlimited HostCapabilities().MaxVMs = %d, want 0", got)
	}
}

func TestPlacementVMHostStartsAcrossHostCapacity(t *testing.T) {
	firstInst := &fakeInstance{waitCh: make(chan error, 1)}
	secondInst := &fakeInstance{waitCh: make(chan error, 1)}
	var firstStarts, secondStarts int
	first := &fakeVMHost{
		Backend: fakeBackend{startFn: func(req client.CreateInstanceRequest) (Instance, error) {
			firstStarts++
			return firstInst, nil
		}},
		caps: VMHostCapabilities{Backend: "first", MaxVMs: 1},
	}
	second := &fakeVMHost{
		Backend: fakeBackend{startFn: func(req client.CreateInstanceRequest) (Instance, error) {
			secondStarts++
			return secondInst, nil
		}},
		caps: VMHostCapabilities{Backend: "second", MaxVMs: 1},
	}
	mgr := NewManagerWithHosts(first, second)
	mgr.supports = func() error { return nil }

	if _, err := mgr.Start(context.Background(), client.CreateInstanceRequest{ID: "one", Image: "alpine"}); err != nil {
		t.Fatalf("Start(one) error = %v", err)
	}
	if _, err := mgr.Start(context.Background(), client.CreateInstanceRequest{ID: "two", Image: "busybox"}); err != nil {
		t.Fatalf("Start(two) error = %v", err)
	}
	if firstStarts != 1 || secondStarts != 1 {
		t.Fatalf("start counts first=%d second=%d, want 1 each", firstStarts, secondStarts)
	}
	if _, err := mgr.Start(context.Background(), client.CreateInstanceRequest{ID: "three", Image: "ubuntu"}); err == nil {
		t.Fatal("Start(three) error = nil, want capacity error")
	}
}

func TestPlacementVMHostRoutesNetworkedStartsToL2Host(t *testing.T) {
	firstInst := &fakeInstance{waitCh: make(chan error, 1)}
	secondInst := &fakeInstance{waitCh: make(chan error, 1)}
	var firstStarts, secondStarts int
	first := &fakeVMHost{
		Backend: fakeBackend{startFn: func(req client.CreateInstanceRequest) (Instance, error) {
			firstStarts++
			return firstInst, nil
		}},
		caps: VMHostCapabilities{Backend: "first", MaxVMs: 1, Locality: "in-process", SupportsL2: false},
	}
	second := &fakeVMHost{
		Backend: fakeBackend{startFn: func(req client.CreateInstanceRequest) (Instance, error) {
			secondStarts++
			return secondInst, nil
		}},
		caps: VMHostCapabilities{Backend: "second", MaxVMs: 1, Locality: "sidecar", SupportsL2: true},
	}
	mgr := NewManagerWithHosts(first, second)
	mgr.supports = func() error { return nil }

	if _, err := mgr.Start(context.Background(), client.CreateInstanceRequest{
		ID:      "net",
		Image:   "alpine",
		Network: &client.NetworkConfig{Enabled: true},
	}); err != nil {
		t.Fatalf("Start(networked) error = %v", err)
	}
	if firstStarts != 0 || secondStarts != 1 {
		t.Fatalf("start counts first=%d second=%d, want 0/1", firstStarts, secondStarts)
	}
}

func TestPlacementVMHostRoutesNetworkedBlankStartsToL2Host(t *testing.T) {
	firstInst := &fakeInstance{waitCh: make(chan error, 1)}
	secondInst := &fakeInstance{waitCh: make(chan error, 1)}
	var firstStarts, secondStarts int
	first := &fakeVMHost{
		Backend: fakeBackend{startBlankFn: func(req client.StartInstanceRequest) (Instance, error) {
			firstStarts++
			return firstInst, nil
		}},
		caps: VMHostCapabilities{Backend: "first", MaxVMs: 1, Locality: "in-process", SupportsL2: false},
	}
	second := &fakeVMHost{
		Backend: fakeBackend{startBlankFn: func(req client.StartInstanceRequest) (Instance, error) {
			secondStarts++
			return secondInst, nil
		}},
		caps: VMHostCapabilities{Backend: "second", MaxVMs: 1, Locality: "sidecar", SupportsL2: true},
	}
	mgr := NewManagerWithHosts(first, second)
	mgr.supports = func() error { return nil }

	if _, err := mgr.StartBlank(context.Background(), client.StartInstanceRequest{
		ID:      "net",
		Image:   "alpine",
		Network: &client.NetworkConfig{Enabled: true},
	}); err != nil {
		t.Fatalf("StartBlank(networked) error = %v", err)
	}
	if firstStarts != 0 || secondStarts != 1 {
		t.Fatalf("blank start counts first=%d second=%d, want 0/1", firstStarts, secondStarts)
	}
}

func TestPlacementVMHostFailsNetworkedStartWithoutL2Host(t *testing.T) {
	host := &fakeVMHost{
		Backend: fakeBackend{instance: &fakeInstance{waitCh: make(chan error, 1)}},
		caps:    VMHostCapabilities{Backend: "first", MaxVMs: 1, SupportsL2: false},
	}
	mgr := NewManagerWithHosts(host)
	mgr.supports = func() error { return nil }

	_, err := mgr.Start(context.Background(), client.CreateInstanceRequest{
		ID:      "net",
		Image:   "alpine",
		Network: &client.NetworkConfig{Enabled: true},
	})
	if err == nil || !strings.Contains(err.Error(), "coordinator L2") {
		t.Fatalf("Start(networked) error = %v, want coordinator L2 capacity error", err)
	}
}

func TestPlacementVMHostReleasesCapacityOnClose(t *testing.T) {
	firstInst := &fakeInstance{waitCh: make(chan error, 1)}
	secondInst := &fakeInstance{waitCh: make(chan error, 1)}
	starts := 0
	host := &fakeVMHost{
		Backend: fakeBackend{startFn: func(req client.CreateInstanceRequest) (Instance, error) {
			starts++
			if starts == 1 {
				return firstInst, nil
			}
			return secondInst, nil
		}},
		caps: VMHostCapabilities{Backend: "single", MaxVMs: 1},
	}
	mgr := NewManagerWithHosts(host)
	mgr.supports = func() error { return nil }

	if _, err := mgr.Start(context.Background(), client.CreateInstanceRequest{ID: "one", Image: "alpine"}); err != nil {
		t.Fatalf("Start(one) error = %v", err)
	}
	if err := mgr.ShutdownInstance(context.Background(), "one"); err != nil {
		t.Fatalf("ShutdownInstance(one) error = %v", err)
	}
	if _, err := mgr.Start(context.Background(), client.CreateInstanceRequest{ID: "two", Image: "busybox"}); err != nil {
		t.Fatalf("Start(two) after shutdown error = %v", err)
	}
	if starts != 2 {
		t.Fatalf("start count = %d, want 2", starts)
	}
}

func TestPlacementVMHostRoutesExecToOwningHost(t *testing.T) {
	firstInst := &fakeInstance{waitCh: make(chan error, 1)}
	secondInst := &fakeInstance{waitCh: make(chan error, 1)}
	var firstExecs, secondExecs int
	first := &fakeVMHost{
		Backend: fakeBackend{
			instance: firstInst,
			runInInstanceFn: func(ctx context.Context, inst Instance, runningImage string, req client.RunRequest) (client.ExecResponse, error) {
				_ = ctx
				_ = inst
				_ = runningImage
				_ = req
				firstExecs++
				return client.ExecResponse{ExitCode: 0, Output: "first"}, nil
			},
		},
		caps: VMHostCapabilities{Backend: "first", MaxVMs: 1},
	}
	second := &fakeVMHost{
		Backend: fakeBackend{
			instance: secondInst,
			runInInstanceFn: func(ctx context.Context, inst Instance, runningImage string, req client.RunRequest) (client.ExecResponse, error) {
				_ = ctx
				_ = inst
				_ = runningImage
				_ = req
				secondExecs++
				return client.ExecResponse{ExitCode: 0, Output: "second"}, nil
			},
		},
		caps: VMHostCapabilities{Backend: "second", MaxVMs: 1},
	}
	mgr := NewManagerWithHosts(first, second)
	mgr.supports = func() error { return nil }

	if _, err := mgr.Start(context.Background(), client.CreateInstanceRequest{ID: "one", Image: "alpine"}); err != nil {
		t.Fatalf("Start(one) error = %v", err)
	}
	if _, err := mgr.Start(context.Background(), client.CreateInstanceRequest{ID: "two", Image: "busybox"}); err != nil {
		t.Fatalf("Start(two) error = %v", err)
	}

	resp, err := mgr.Run(context.Background(), client.RunRequest{ID: "two", Image: "ubuntu", Command: []string{"true"}})
	if err != nil {
		t.Fatalf("Run(two) error = %v", err)
	}
	if resp.Output != "second" || firstExecs != 0 || secondExecs != 1 {
		t.Fatalf("exec routed output=%q first=%d second=%d, want second host", resp.Output, firstExecs, secondExecs)
	}
}

func TestManagerRunStreamRoutesHostedInstanceToHost(t *testing.T) {
	inst := &fakeInstance{waitCh: make(chan error, 1)}
	var routed bool
	var host *fakeVMHost
	host = &fakeVMHost{
		Backend: fakeBackend{
			startFn: func(req client.CreateInstanceRequest) (Instance, error) {
				return &hostedInstance{Instance: inst, host: host, release: func() {}}, nil
			},
			runInStreamFn: func(ctx context.Context, inst Instance, runningImage string, req client.RunRequest, inputs <-chan client.ExecInput, onEvent func(client.ExecEvent) error) error {
				_ = ctx
				_ = inst
				_ = runningImage
				_ = req
				_ = inputs
				routed = true
				if onEvent != nil {
					if err := onEvent(client.ExecEvent{Kind: "stdout", Output: "host"}); err != nil {
						return err
					}
					return onEvent(client.ExecEvent{Kind: "exit"})
				}
				return nil
			},
		},
		caps: VMHostCapabilities{Backend: "hosted", MaxVMs: 1},
	}
	mgr := NewManagerWithHost(host)
	mgr.supports = func() error { return nil }

	if _, err := mgr.Start(context.Background(), client.CreateInstanceRequest{Image: "alpine"}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	var events []client.ExecEvent
	if err := mgr.RunStream(context.Background(), client.RunRequest{Image: "alpine", Command: []string{"true"}}, nil, func(event client.ExecEvent) error {
		events = append(events, event)
		return nil
	}); err != nil {
		t.Fatalf("RunStream() error = %v", err)
	}
	if !routed {
		t.Fatal("RunStream() was not routed through the owning host")
	}
	if len(events) != 2 || events[0].Output != "host" || events[1].Kind != "exit" {
		t.Fatalf("events = %#v, want host output then exit", events)
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

func TestManagerStreamDelegatesImageScopedExecToBackend(t *testing.T) {
	inst := &fakeInstance{waitCh: make(chan error, 1)}
	var seen struct {
		running string
		image   string
		kind    string
		path    string
	}
	mgr := NewManagerWithBackend(fakeBackend{
		instance: inst,
		execInStreamFn: func(ctx context.Context, inst Instance, runningImage string, req client.ExecRequest, inputs <-chan client.ExecInput, onEvent func(client.ExecEvent) error) error {
			_ = ctx
			_ = inst
			_ = inputs
			seen.running = runningImage
			seen.image = req.Image
			seen.kind = req.Kind
			seen.path = req.Path
			if onEvent != nil {
				return onEvent(client.ExecEvent{Kind: "exit", ExitCode: 0})
			}
			return nil
		},
	})
	mgr.supports = func() error { return nil }

	if _, err := mgr.Start(context.Background(), client.CreateInstanceRequest{Image: "alpine"}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := mgr.Stream(context.Background(), client.ExecRequest{
		Image: "alpine",
		Kind:  "fs_write",
		Path:  "/home/cc/go.mod",
	}, nil, nil); err != nil {
		t.Fatalf("Stream() error = %v", err)
	}
	if seen.running != "alpine" || seen.image != "alpine" || seen.kind != "fs_write" || seen.path != "/home/cc/go.mod" {
		t.Fatalf("backend saw %#v", seen)
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

func TestManagerRoutesActionsToNamedInstances(t *testing.T) {
	first := &fakeInstance{
		waitCh:   make(chan error, 1),
		execResp: client.ExecResponse{ExitCode: 0, Output: "one"},
	}
	second := &fakeInstance{
		waitCh:   make(chan error, 1),
		execResp: client.ExecResponse{ExitCode: 0, Output: "two"},
	}
	var started int
	mgr := NewManagerWithBackend(fakeBackend{
		startFn: func(req client.CreateInstanceRequest) (Instance, error) {
			started++
			if started == 1 {
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

	resp, err := mgr.Run(context.Background(), client.RunRequest{ID: "two", Command: []string{"hostname"}})
	if err != nil {
		t.Fatalf("Run(two) error = %v", err)
	}
	if resp.Output != "two" {
		t.Fatalf("Run(two).Output = %q, want two", resp.Output)
	}

	var streamEvents []client.ExecEvent
	if err := mgr.Stream(context.Background(), client.ExecRequest{ID: "one", Command: []string{"hostname"}}, nil, func(event client.ExecEvent) error {
		streamEvents = append(streamEvents, event)
		return nil
	}); err != nil {
		t.Fatalf("Stream(one) error = %v", err)
	}
	if len(streamEvents) != 2 || streamEvents[0].Output != "one" || streamEvents[1].Kind != "exit" {
		t.Fatalf("Stream(one) events = %#v, want output one then exit", streamEvents)
	}

	var forwarded client.PortForward
	second.forwardFn = func(forward client.PortForward) error {
		forwarded = forward
		return nil
	}
	wantForward := client.PortForward{HostPort: 8080, GuestPort: 80}
	if err := mgr.AddPortForwardTo(context.Background(), "two", wantForward); err != nil {
		t.Fatalf("AddPortForwardTo(two) error = %v", err)
	}
	if forwarded != wantForward {
		t.Fatalf("forwarded = %#v, want %#v", forwarded, wantForward)
	}

	if err := mgr.ShutdownInstance(context.Background(), "one"); err != nil {
		t.Fatalf("ShutdownInstance(one) error = %v", err)
	}
	resp, err = mgr.Run(context.Background(), client.RunRequest{ID: "two", Command: []string{"hostname"}})
	if err != nil {
		t.Fatalf("Run(two after one shutdown) error = %v", err)
	}
	if resp.Output != "two" {
		t.Fatalf("Run(two after one shutdown).Output = %q, want two", resp.Output)
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

func TestManagerSnapshotRootFSFlushesBeforeSnapshot(t *testing.T) {
	overlay := imagefs.NewOverlay(nil)
	if err := overlay.AddFile("/etc/motd", 0o644, []byte("saved")); err != nil {
		t.Fatalf("AddFile() error = %v", err)
	}
	var calls []string
	inst := &fakeInstance{
		waitCh: make(chan error, 1),
		flushFn: func(context.Context) error {
			calls = append(calls, "flush")
			return nil
		},
		snapshotFn: func() (imagefs.Directory, error) {
			calls = append(calls, "snapshot")
			return overlay.Root(), nil
		},
	}
	mgr := NewManagerWithBackend(fakeBackend{instance: inst})
	mgr.supports = func() error { return nil }

	if _, err := mgr.Start(context.Background(), client.CreateInstanceRequest{Image: "alpine"}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	root, source, err := mgr.SnapshotRootFS(context.Background(), "default", "")
	if err != nil {
		t.Fatalf("SnapshotRootFS() error = %v", err)
	}
	if root == nil || source != "alpine" {
		t.Fatalf("SnapshotRootFS() = root %v source %q, want root and alpine", root, source)
	}
	if got := fmt.Sprint(calls); got != "[flush snapshot]" {
		t.Fatalf("call order = %s, want [flush snapshot]", got)
	}
}

func TestManagerSnapshotRootFSReportsFlushAndSnapshotProgress(t *testing.T) {
	inst := &fakeInstance{waitCh: make(chan error, 1)}
	mgr := NewManagerWithBackend(fakeBackend{instance: inst})
	mgr.supports = func() error { return nil }

	if _, err := mgr.Start(context.Background(), client.CreateInstanceRequest{Image: "alpine"}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	var statuses []string
	if _, _, err := mgr.SnapshotRootFSWithProgress(context.Background(), "default", "", func(event client.ProgressEvent) {
		statuses = append(statuses, event.Status+":"+event.Blob)
	}); err != nil {
		t.Fatalf("SnapshotRootFSWithProgress() error = %v", err)
	}
	if got := fmt.Sprint(statuses); got != "[flushing:default snapshotting:default]" {
		t.Fatalf("progress statuses = %s, want flush then snapshot", got)
	}
}

func TestSidecarCommandResolverResolvesBeforeWorkerExec(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "opt", "bin"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "opt", "bin", "tool"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	resolver := newSidecarCommandResolver(&oci.Image{
		RootFS: imagefs.NewHostFS(root, map[string]fsmeta.Entry{
			"/opt/bin/tool": {Mode: linuxabi.SIFREG | 0o755},
		}),
		Config: oci.RuntimeConfig{
			Env: []string{"PATH=/opt/bin"},
		},
	})

	got, err := resolver.resolve(client.ExecRequest{
		Command: []string{"tool", "arg"},
	})
	if err != nil {
		t.Fatalf("resolve() error = %v", err)
	}
	if !got.SkipResolve {
		t.Fatal("resolve() SkipResolve = false, want true")
	}
	if want := "/opt/bin/tool"; got.Command[0] != want {
		t.Fatalf("resolve() command[0] = %q, want %q", got.Command[0], want)
	}
	if got.Command[1] != "arg" {
		t.Fatalf("resolve() command args = %#v", got.Command)
	}
}

func TestCleanupStaleSidecarSocketsOnlyRemovesSocketFiles(t *testing.T) {
	root := t.TempDir()
	socketDir := filepath.Join(root, "_worker-sockets")
	if err := os.MkdirAll(socketDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	stale := filepath.Join(socketDir, "fs-stale.sock")
	keep := filepath.Join(socketDir, "state.json")
	if err := os.WriteFile(stale, []byte("stale"), 0o644); err != nil {
		t.Fatalf("WriteFile(stale) error = %v", err)
	}
	if err := os.WriteFile(keep, []byte("keep"), 0o644); err != nil {
		t.Fatalf("WriteFile(keep) error = %v", err)
	}

	cleanupStaleSidecarSockets(root)

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale socket stat error = %v, want not exist", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("keep stat error = %v", err)
	}
}

func TestSidecarInstanceAddShareIsIdempotent(t *testing.T) {
	requireSidecarRuntimeShares(t)

	root := t.TempDir()
	rootFS, ok := virtio.NewMountedFS(virtio.NewPassthroughFS(root, nil), nil).(sidecarRootFS)
	if !ok {
		t.Fatalf("mounted FS does not implement sidecarRootFS")
	}
	inst := &sidecarInstance{rootFS: rootFS}
	share := client.ShareMount{Source: t.TempDir(), Mount: "/host", Writable: true, Cache: "strict"}

	if err := inst.AddShare(context.Background(), share); err != nil {
		t.Fatalf("AddShare() error = %v", err)
	}
	if err := inst.AddShare(context.Background(), share); err != nil {
		t.Fatalf("AddShare() repeat error = %v", err)
	}
}

func TestSidecarInstanceAddShareRejectsConflictingMount(t *testing.T) {
	requireSidecarRuntimeShares(t)

	root := t.TempDir()
	rootFS, ok := virtio.NewMountedFS(virtio.NewPassthroughFS(root, nil), nil).(sidecarRootFS)
	if !ok {
		t.Fatalf("mounted FS does not implement sidecarRootFS")
	}
	inst := &sidecarInstance{rootFS: rootFS}
	first := client.ShareMount{Source: t.TempDir(), Mount: "/host", Writable: true, Cache: "strict"}
	second := client.ShareMount{Source: t.TempDir(), Mount: "/host", Writable: true, Cache: "strict"}

	if err := inst.AddShare(context.Background(), first); err != nil {
		t.Fatalf("AddShare() error = %v", err)
	}
	err := inst.AddShare(context.Background(), second)
	if err == nil {
		t.Fatal("AddShare() conflicting error = nil, want error")
	}
	if !strings.Contains(err.Error(), `share mount "/host" already exists`) {
		t.Fatalf("AddShare() conflicting error = %q", err)
	}
}

func TestSidecarCommandResolverSkipsFSRequests(t *testing.T) {
	root := t.TempDir()
	resolver := &sidecarCommandResolver{root: imagefs.NewHostFS(root, nil)}
	req := client.ExecRequest{
		Kind:      "fs_extract",
		Path:      "/home/cc",
		Directory: true,
	}
	got, err := resolver.resolve(req)
	if err != nil {
		t.Fatalf("resolve(fs_extract) error = %v", err)
	}
	if got.Kind != req.Kind || got.Path != req.Path || got.Directory != req.Directory || got.RootDir != "" || len(got.Command) != 0 {
		t.Fatalf("resolved request = %#v, want fs request passed through unchanged", got)
	}
}

func requireSidecarRuntimeShares(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("sidecar runtime shares are only supported on darwin/arm64")
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
	tests := []struct {
		name  string
		image *oci.Image
		want  bool
	}{
		{name: "amd64", image: &oci.Image{Architecture: "amd64"}, want: runtime.GOARCH == "arm64"},
		{name: "arm64", image: &oci.Image{Architecture: "arm64"}, want: false},
		{name: "nil", image: nil, want: false},
	}
	for _, tt := range tests {
		if got := NeedsAMD64Emulation(tt.image); got != tt.want {
			t.Fatalf("NeedsAMD64Emulation(%s) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

type fakeBackend struct {
	instance        Instance
	err             error
	runResp         client.ExecResponse
	runFn           func(client.RunRequest) (client.ExecResponse, error)
	runStreamFn     func(context.Context, client.RunRequest, <-chan client.ExecInput, func(client.ExecEvent) error) error
	startFn         func(client.CreateInstanceRequest) (Instance, error)
	startBlankFn    func(client.StartInstanceRequest) (Instance, error)
	runInInstanceFn func(context.Context, Instance, string, client.RunRequest) (client.ExecResponse, error)
	runInStreamFn   func(context.Context, Instance, string, client.RunRequest, <-chan client.ExecInput, func(client.ExecEvent) error) error
	execInStreamFn  func(context.Context, Instance, string, client.ExecRequest, <-chan client.ExecInput, func(client.ExecEvent) error) error
}

type fakeVMHost struct {
	Backend
	caps VMHostCapabilities
}

func (f *fakeVMHost) HostCapabilities(context.Context) VMHostCapabilities {
	return f.caps
}

func (f *fakeVMHost) Close() error {
	return nil
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
	_ = onEvent
	if f.startBlankFn != nil {
		return f.startBlankFn(req)
	}
	_ = req
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
		Command:     append([]string(nil), req.Command...),
		Env:         append([]string(nil), req.Env...),
		WorkDir:     req.WorkDir,
		User:        req.User,
		Stdin:       append([]byte(nil), req.Stdin...),
		StdinClosed: req.StdinClosed,
		TTY:         req.TTY,
		Cols:        req.Cols,
		Rows:        req.Rows,
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

func (f fakeBackend) ExecInInstanceStream(ctx context.Context, inst Instance, runningImage string, req client.ExecRequest, inputs <-chan client.ExecInput, onEvent func(client.ExecEvent) error) error {
	if f.execInStreamFn != nil {
		return f.execInStreamFn(ctx, inst, runningImage, req, inputs, onEvent)
	}
	if inst == nil {
		return f.err
	}
	return inst.ExecStream(ctx, req, inputs, onEvent)
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
	flushFn    func(context.Context) error
	snapshotFn func() (imagefs.Directory, error)
	imageFn    func(string) (imagefs.Directory, error)
	console    string
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

func (f *fakeInstance) Flush(ctx context.Context) error {
	if f.flushFn != nil {
		return f.flushFn(ctx)
	}
	return nil
}

func (f *fakeInstance) ConsoleHistory(context.Context) (string, error) {
	return f.console, nil
}

func (f *fakeInstance) RootSnapshot() (imagefs.Directory, error) {
	if f.snapshotFn != nil {
		return f.snapshotFn()
	}
	return imagefs.NewOverlay(nil).Root(), nil
}

func (f *fakeInstance) SnapshotImage(imageName string) (imagefs.Directory, error) {
	if f.imageFn != nil {
		return f.imageFn(imageName)
	}
	return f.RootSnapshot()
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
