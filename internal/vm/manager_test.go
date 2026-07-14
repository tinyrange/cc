package vm

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/imagefs"
	"j5.nz/cc/internal/virtio"
)

func TestInstanceStatusPreservesSubsecondStartIdentity(t *testing.T) {
	manager := NewManager()
	manager.running = make(map[string]*Machine)
	manager.running[DefaultInstanceID] = &Machine{startedAt: time.Date(2026, 7, 14, 1, 2, 3, 456789, time.UTC)}
	state := manager.Status()
	if state.StartedAt != "2026-07-14T01:02:03.000456789Z" {
		t.Fatalf("started_at = %q, want nanosecond-stable VM identity", state.StartedAt)
	}
}

func TestManagerStartRoutesExistingInstanceOperations(t *testing.T) {
	ctx := context.Background()
	host := newFakeHost(VMHostCapabilities{Backend: "fake", MaxVMs: 2, SupportsL2: true})
	inst := newFakeInstance()
	inst.ipv4 = "10.0.2.15"
	host.queueInstance(inst)
	manager := testManager(host)
	defer manager.ShutdownAll(ctx)

	state, err := manager.Start(ctx, client.CreateInstanceRequest{
		ID:         "alpha",
		Image:      "alpine",
		MemoryMB:   256,
		BalloonMB:  64,
		CPUs:       2,
		NestedVirt: true,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if state.ID != "alpha" || state.Status != "running" || state.Image != "alpine" || state.MemoryMB != 256 || state.BalloonMB != 64 || state.CPUs != 2 || !state.NestedVirt || state.NetworkIPv4 != "10.0.2.15" {
		t.Fatalf("state = %+v", state)
	}

	resp, err := manager.RunIn(ctx, "alpha", client.RunRequest{Command: []string{"echo", "hi"}})
	if err != nil {
		t.Fatalf("run in instance: %v", err)
	}
	if resp.Output != "host run in alpine: echo hi" {
		t.Fatalf("run response = %+v", resp)
	}
	if got := host.runInCalls(); len(got) != 1 || got[0].runningImage != "alpine" {
		t.Fatalf("run-in calls = %+v", got)
	}

	var runEvents []client.ExecEvent
	share := client.ShareMount{Source: "/tmp/host", Mount: "/mnt/host", Writable: true}
	if err := manager.RunStreamIn(ctx, "alpha", client.RunRequest{
		Command: []string{"pwd"},
		Shares:  []client.ShareMount{share},
	}, nil, appendExecEvents(&runEvents)); err != nil {
		t.Fatalf("run stream in existing instance: %v", err)
	}
	if len(inst.shares) != 1 || inst.shares[0] != share {
		t.Fatalf("instance shares = %+v", inst.shares)
	}
	if got := host.runInStreamCalls(); len(got) != 0 {
		t.Fatalf("host run-in-stream calls = %+v", got)
	}
	if len(inst.execStreamReqs) != 1 {
		t.Fatalf("instance exec stream reqs = %+v", inst.execStreamReqs)
	}
	if inst.execStreamReqs[0].ID == "" || inst.execStreamReqs[0].ID == "alpha" {
		t.Fatalf("run stream exec id = %q, want unique guest exec id", inst.execStreamReqs[0].ID)
	}
	if len(runEvents) == 0 || runEvents[len(runEvents)-1].Kind != "exit" {
		t.Fatalf("run stream events = %+v", runEvents)
	}

	if err := manager.StreamIn(ctx, "alpha", client.ExecRequest{Command: []string{"id"}}, nil, nil); err != nil {
		t.Fatalf("exec stream in instance: %v", err)
	}
	if len(inst.execStreamReqs) != 2 {
		t.Fatalf("exec stream count = %d, want 2", len(inst.execStreamReqs))
	}
	if inst.execStreamReqs[1].ID == "" || inst.execStreamReqs[1].ID == "alpha" || inst.execStreamReqs[1].ID == inst.execStreamReqs[0].ID {
		t.Fatalf("exec stream ids = %q, %q; want unique guest exec ids", inst.execStreamReqs[0].ID, inst.execStreamReqs[1].ID)
	}
	if err := manager.StreamIn(ctx, "alpha", client.ExecRequest{Image: "other", Command: []string{"true"}}, nil, nil); err != nil {
		t.Fatalf("multi-image exec stream: %v", err)
	}
	if got := host.execInStreamCalls(); len(got) != 1 || got[0].runningImage != "alpine" || got[0].req.Image != "other" {
		t.Fatalf("host exec-in-stream calls = %+v", got)
	} else if got[0].req.ID == "" || got[0].req.ID == "alpha" || got[0].req.ID == inst.execStreamReqs[0].ID || got[0].req.ID == inst.execStreamReqs[1].ID {
		t.Fatalf("alternate exec stream id = %q, previous ids = %q/%q", got[0].req.ID, inst.execStreamReqs[0].ID, inst.execStreamReqs[1].ID)
	}
}

func TestManagerBlankStartRemembersImageForRunIn(t *testing.T) {
	ctx := context.Background()
	host := newFakeHost(VMHostCapabilities{Backend: "fake", MaxVMs: 2, SupportsL2: true})
	host.queueInstance(newFakeInstance())
	manager := testManager(host)
	defer manager.ShutdownAll(ctx)

	state, err := manager.StartBlankInstanceStream(ctx, "alpha", client.StartInstanceRequest{
		Image:      "ubuntu",
		InitSystem: "systemd",
		MemoryMB:   512,
		BalloonMB:  128,
	}, nil)
	if err != nil {
		t.Fatalf("start blank: %v", err)
	}
	if state.Image != "ubuntu" || state.InitSystem != "systemd" || state.BalloonMB != 128 {
		t.Fatalf("state = %+v, want image ubuntu and init systemd", state)
	}
	if _, err := manager.RunIn(ctx, "alpha", client.RunRequest{Image: "ubuntu", Command: []string{"systemctl"}}); err != nil {
		t.Fatalf("run in blank instance: %v", err)
	}
	if got := host.runInCalls(); len(got) != 1 || got[0].runningImage != "ubuntu" || got[0].req.Image != "ubuntu" {
		t.Fatalf("run-in calls = %+v", got)
	}
}

func TestManagerBlankStartPassesBuiltinGuestSharesToHost(t *testing.T) {
	ctx := context.Background()
	host := newFakeHost(VMHostCapabilities{Backend: "fake", MaxVMs: 2, SupportsL2: true})
	inst := newFakeInstance()
	host.queueInstance(inst)
	manager := testManager(host)
	defer manager.ShutdownAll(ctx)

	share := client.ShareMount{Source: "/tmp/host", Mount: "/host", Writable: true}
	if _, err := manager.StartBlankInstanceStream(ctx, "openbsd", client.StartInstanceRequest{
		Image:  "@openbsd",
		Shares: []client.ShareMount{share},
	}, nil); err != nil {
		t.Fatalf("start blank built-in guest: %v", err)
	}
	if len(host.blankStarts) != 1 {
		t.Fatalf("blank starts = %+v, want one start", host.blankStarts)
	}
	if got := host.blankStarts[0].Shares; len(got) != 1 || got[0] != share {
		t.Fatalf("host blank start shares = %+v, want startup share", got)
	}
	if len(inst.shares) != 0 {
		t.Fatalf("replayed shares = %+v, want none", inst.shares)
	}
}

func TestManagerRetainsCrashStatusUntilNextGeneration(t *testing.T) {
	host := newFakeHost(VMHostCapabilities{MaxVMs: 2})
	crashed := newFakeInstance()
	crashed.waitErr = errors.New("hypervisor exited")
	host.queueInstance(crashed)
	manager := testManager(host)
	if _, err := manager.Start(context.Background(), client.CreateInstanceRequest{ID: "alpha", Image: "alpine", MemoryMB: 256, CPUs: 1}); err != nil {
		t.Fatalf("start crashing instance: %v", err)
	}
	_ = crashed.Close()
	deadline := time.Now().Add(time.Second)
	var state client.InstanceState
	for time.Now().Before(deadline) {
		state = manager.StatusOf("alpha")
		if state.Status == "crashed" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if state.Status != "crashed" || state.ExitReason != "hypervisor exited" || state.ExitedAt == "" || state.StartedAt == "" {
		t.Fatalf("crash state = %+v", state)
	}
	host.queueInstance(newFakeInstance())
	if _, err := manager.Start(context.Background(), client.CreateInstanceRequest{ID: "alpha", Image: "alpine", MemoryMB: 256, CPUs: 1}); err != nil {
		t.Fatalf("start replacement instance: %v", err)
	}
	if state := manager.StatusOf("alpha"); state.Status != "running" || state.ExitReason != "" || state.ExitedAt != "" {
		t.Fatalf("replacement state = %+v", state)
	}
	_ = manager.ShutdownAll(context.Background())
}

func TestManagerRejectsInvalidResourcesBeforeHostAllocation(t *testing.T) {
	host := newFakeHost(VMHostCapabilities{MaxVMs: 4})
	manager := testManager(host)
	_, err := manager.Start(context.Background(), client.CreateInstanceRequest{Image: "alpine", MemoryMB: ^uint64(0), CPUs: 1})
	if err == nil {
		t.Fatal("overflowing memory request was accepted")
	}
	if len(host.starts) != 0 {
		t.Fatalf("host starts = %+v", host.starts)
	}
	_, err = manager.Start(context.Background(), client.CreateInstanceRequest{Image: "alpine", MemoryMB: 512, BalloonMB: 513, CPUs: 1})
	if err == nil {
		t.Fatal("balloon larger than guest memory was accepted")
	}
	if len(host.starts) != 0 {
		t.Fatalf("host starts after balloon rejection = %+v", host.starts)
	}
}

func TestManagerAdmissionIncludesInflightStarts(t *testing.T) {
	base := newFakeHost(VMHostCapabilities{MaxVMs: 4})
	base.queueInstance(newFakeInstance())
	host := &blockingStartHost{fakeHost: base, entered: make(chan struct{}), release: make(chan struct{})}
	manager := testManager(host)
	manager.maxMemoryMB = 512
	manager.maxCPUs = 2
	firstDone := make(chan error, 1)
	go func() {
		_, err := manager.Start(context.Background(), client.CreateInstanceRequest{ID: "one", Image: "alpine", MemoryMB: 512, CPUs: 2})
		firstDone <- err
	}()
	<-host.entered
	if _, err := manager.Start(context.Background(), client.CreateInstanceRequest{ID: "two", Image: "alpine", MemoryMB: 512, CPUs: 1}); err == nil {
		t.Fatal("concurrent start exceeded reserved budget")
	}
	if len(base.starts) != 0 {
		t.Fatalf("host starts before release = %d, want 0", len(base.starts))
	}
	close(host.release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first start: %v", err)
	}
	if len(base.starts) != 1 {
		t.Fatalf("host starts after release = %d, want 1", len(base.starts))
	}
}

func TestManagerBlankSnapshotStartKeepsSharesInStartRequest(t *testing.T) {
	ctx := context.Background()
	host := newFakeHost(VMHostCapabilities{Backend: "fake", MaxVMs: 2, SupportsL2: true})
	inst := newFakeInstance()
	host.queueInstance(inst)
	manager := testManager(host)
	defer manager.ShutdownAll(ctx)

	share := client.ShareMount{Source: "/tmp/host", Mount: "/host", Writable: true}
	if _, err := manager.StartBlankInstanceStream(ctx, "alpha", client.StartInstanceRequest{
		Image:       "alpine",
		SnapshotDir: "/tmp/snapshots",
		Shares:      []client.ShareMount{share},
	}, nil); err != nil {
		t.Fatalf("start blank: %v", err)
	}
	if len(host.blankStarts) != 1 {
		t.Fatalf("blank starts = %+v", host.blankStarts)
	}
	if got := host.blankStarts[0].Shares; len(got) != 1 || got[0] != share {
		t.Fatalf("start shares = %+v, want %+v", got, []client.ShareMount{share})
	}
	if len(inst.shares) != 0 {
		t.Fatalf("post-start shares = %+v, want none", inst.shares)
	}
}

func TestManagerRunWithoutInstanceRequiresOrUsesImage(t *testing.T) {
	ctx := context.Background()
	host := newFakeHost(VMHostCapabilities{Backend: "fake", MaxVMs: 2})
	manager := testManager(host)

	if _, err := manager.Run(ctx, client.RunRequest{Command: []string{"true"}}); err == nil {
		t.Fatalf("run without image error = %v", err)
	}

	resp, err := manager.Run(ctx, client.RunRequest{Image: "alpine", Command: []string{"echo", "ok"}})
	if err != nil {
		t.Fatalf("run with image: %v", err)
	}
	if resp.Output != "host run alpine: echo ok" {
		t.Fatalf("run output = %q", resp.Output)
	}
	if got := host.runCalls(); len(got) != 1 || got[0].Image != "alpine" {
		t.Fatalf("run calls = %+v", got)
	}

	var events []client.ExecEvent
	if err := manager.RunStream(ctx, client.RunRequest{Image: "alpine", Command: []string{"date"}}, nil, appendExecEvents(&events)); err != nil {
		t.Fatalf("run stream with image: %v", err)
	}
	if got := host.runStreamCalls(); len(got) != 1 || strings.Join(got[0].Command, " ") != "date" {
		t.Fatalf("run stream calls = %+v", got)
	}
	if len(events) == 0 || events[len(events)-1].Kind != "exit" {
		t.Fatalf("run stream events = %+v", events)
	}
}

func TestManagerAssignsDistinctNetworkLeasesBeforeStart(t *testing.T) {
	ctx := context.Background()
	host := newFakeHost(VMHostCapabilities{Backend: "fake", MaxVMs: 2})
	host.queueInstance(newFakeInstance())
	host.queueInstance(newFakeInstance())
	manager := testManager(host)
	defer manager.ShutdownAll(ctx)

	if _, err := manager.Start(ctx, client.CreateInstanceRequest{ID: "freebsd", Image: "@freebsd", Network: &client.NetworkConfig{Enabled: true}}); err != nil {
		t.Fatalf("start freebsd: %v", err)
	}
	if _, err := manager.Start(ctx, client.CreateInstanceRequest{ID: "openbsd", Image: "@openbsd", Network: &client.NetworkConfig{Enabled: true}}); err != nil {
		t.Fatalf("start openbsd: %v", err)
	}

	if len(host.starts) != 2 {
		t.Fatalf("starts = %+v", host.starts)
	}
	first := host.starts[0].Network
	second := host.starts[1].Network
	if first == nil || second == nil {
		t.Fatalf("network configs = %+v, %+v", first, second)
	}
	if first.GuestIPv4 != "10.42.0.2" || second.GuestIPv4 != "10.42.0.3" {
		t.Fatalf("guest IPv4 leases = %q, %q", first.GuestIPv4, second.GuestIPv4)
	}
	if first.GuestMAC != "02:42:0a:2a:00:02" || second.GuestMAC != "02:42:0a:2a:00:03" {
		t.Fatalf("guest MAC leases = %q, %q", first.GuestMAC, second.GuestMAC)
	}
}

func TestManagerShutdownAllDeadlinePreservesFailedAndPendingInstances(t *testing.T) {
	ctx := context.Background()
	alphaRelease := make(chan struct{})
	alphaStarted := make(chan struct{})
	alpha := &shutdownTestInstance{
		fakeInstance: newFakeInstance(),
		closeFunc: func(call int) error {
			if call == 1 {
				close(alphaStarted)
				<-alphaRelease
			}
			return nil
		},
	}
	betaErr := errors.New("beta cleanup failed")
	beta := &shutdownTestInstance{
		fakeInstance: newFakeInstance(),
		closeFunc: func(call int) error {
			if call == 1 {
				return betaErr
			}
			return nil
		},
	}
	gamma := &shutdownTestInstance{fakeInstance: newFakeInstance()}
	host := newFakeHost(VMHostCapabilities{Backend: "fake", MaxVMs: 3})
	for _, inst := range []Instance{alpha, beta, gamma} {
		host.queueInstance(inst)
	}
	manager := testManager(host)
	for _, id := range []string{"alpha", "beta", "gamma"} {
		if _, err := manager.Start(ctx, client.CreateInstanceRequest{
			ID:      id,
			Image:   "alpine",
			Network: &client.NetworkConfig{Enabled: true},
		}); err != nil {
			t.Fatal(err)
		}
	}

	deadline := newTestDeadlineContext()
	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- manager.ShutdownAll(deadline) }()
	<-alphaStarted
	waitForInstanceState(t, manager, "beta", "running", func(machine *Machine) bool { return !machine.stopping })
	waitForInstanceState(t, manager, "gamma", "stopped", nil)
	deadline.expire()
	if err := <-shutdownDone; !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, betaErr) {
		t.Fatalf("ShutdownAll error = %v, want deadline and beta cleanup errors", err)
	}

	if state := manager.StatusOf("alpha"); state.ID != "alpha" || state.Status != "running" {
		t.Fatalf("pending instance state = %+v", state)
	}
	if state := manager.StatusOf("beta"); state.ID != "beta" || state.Status != "running" {
		t.Fatalf("failed instance state = %+v", state)
	}
	if state := manager.StatusOf("gamma"); state.ID != "gamma" || state.Status != "stopped" {
		t.Fatalf("closed instance state = %+v", state)
	}
	assertManagerLease(t, manager, "alpha", true)
	assertManagerLease(t, manager, "beta", true)
	assertManagerLease(t, manager, "gamma", false)

	if err := manager.ShutdownInstance(ctx, "beta"); err != nil {
		t.Fatal(err)
	}
	assertManagerLease(t, manager, "beta", false)
	close(alphaRelease)
	waitForInstanceState(t, manager, "alpha", "stopped", nil)
	assertManagerLease(t, manager, "alpha", false)
	if got := alpha.closeCalls.Load(); got != 1 {
		t.Fatalf("alpha Close calls = %d, want 1", got)
	}
	if got := beta.closeCalls.Load(); got != 2 {
		t.Fatalf("beta Close calls = %d, want 2", got)
	}
}

func waitForInstanceState(t *testing.T, manager *Manager, id, status string, machineReady func(*Machine) bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		manager.mu.Lock()
		machine := manager.running[id]
		ready := status == "stopped" && machine == nil
		if status == "running" && machine != nil {
			ready = machineReady == nil || machineReady(machine)
		}
		manager.mu.Unlock()
		if ready {
			return
		}
		runtime.Gosched()
	}
	t.Fatalf("instance %q did not reach %s state", id, status)
}

func assertManagerLease(t *testing.T, manager *Manager, id string, want bool) {
	t.Helper()
	manager.mu.Lock()
	_, got := manager.networkLeases[id]
	manager.mu.Unlock()
	if got != want {
		t.Fatalf("lease %q exists = %v, want %v", id, got, want)
	}
}

type testDeadlineContext struct {
	context.Context
	done chan struct{}
}

func newTestDeadlineContext() *testDeadlineContext {
	return &testDeadlineContext{Context: context.Background(), done: make(chan struct{})}
}

func (c *testDeadlineContext) Done() <-chan struct{} { return c.done }

func (c *testDeadlineContext) Err() error {
	select {
	case <-c.done:
		return context.DeadlineExceeded
	default:
		return nil
	}
}

func (c *testDeadlineContext) expire() { close(c.done) }

func TestManagerRetriesFailedCloseAndSharesConcurrentResult(t *testing.T) {
	ctx := context.Background()
	transientErr := errors.New("transient close failure")
	inst := &flakyCloseInstance{
		fakeInstance: newFakeInstance(),
		firstStarted: make(chan struct{}),
		releaseFirst: make(chan struct{}),
		firstErr:     transientErr,
	}
	host := newFakeHost(VMHostCapabilities{Backend: "fake", MaxVMs: 1})
	host.queueInstance(inst)
	manager := testManager(host)
	if _, err := manager.Start(ctx, client.CreateInstanceRequest{
		ID:      "alpha",
		Image:   "alpine",
		Network: &client.NetworkConfig{Enabled: true},
	}); err != nil {
		t.Fatal(err)
	}

	firstDone := make(chan error, 1)
	secondDone := make(chan error, 1)
	go func() { firstDone <- manager.ShutdownInstance(ctx, "alpha") }()
	<-inst.firstStarted
	go func() { secondDone <- manager.ShutdownInstance(ctx, "alpha") }()
	waitForStopObservers(t, manager, "alpha", 2)
	close(inst.releaseFirst)
	for i, result := range []<-chan error{firstDone, secondDone} {
		if err := <-result; !errors.Is(err, transientErr) {
			t.Fatalf("concurrent shutdown %d error = %v, want transient failure", i+1, err)
		}
	}
	if got := inst.closeCalls.Load(); got != 1 {
		t.Fatalf("Close calls after shared failure = %d, want 1", got)
	}
	if state := manager.StatusOf("alpha"); state.ID != "alpha" || state.Status != "running" {
		t.Fatalf("state after failed close = %+v", state)
	}
	if _, err := manager.RunIn(ctx, "alpha", client.RunRequest{Command: []string{"true"}}); err != nil {
		t.Fatalf("instance remained unusable after failed close: %v", err)
	}

	if err := manager.ShutdownInstance(ctx, "alpha"); err != nil {
		t.Fatal(err)
	}
	if got := inst.closeCalls.Load(); got != 2 {
		t.Fatalf("Close calls after retry = %d, want 2", got)
	}
	if state := manager.StatusOf("alpha"); state.ID != "alpha" || state.Status != "stopped" {
		t.Fatalf("state after successful retry = %+v", state)
	}
	manager.mu.Lock()
	_, leaseExists := manager.networkLeases["alpha"]
	manager.mu.Unlock()
	if leaseExists {
		t.Fatal("network lease remained after successful close retry")
	}
}

func waitForStopObservers(t *testing.T, manager *Manager, id string, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		manager.mu.Lock()
		machine := manager.running[id]
		got := 0
		if machine != nil && machine.stop != nil {
			got = machine.stop.observers
		}
		manager.mu.Unlock()
		if got >= want {
			return
		}
		runtime.Gosched()
	}
	t.Fatalf("shutdown observers did not reach %d", want)
}

func TestManagerAssignsNetworkLeasesForBuiltinDefaultNetwork(t *testing.T) {
	ctx := context.Background()
	host := newFakeHost(VMHostCapabilities{Backend: "fake", MaxVMs: 2})
	host.queueInstance(newFakeInstance())
	host.queueInstance(newFakeInstance())
	manager := testManager(host)
	defer manager.ShutdownAll(ctx)

	if _, err := manager.Start(ctx, client.CreateInstanceRequest{ID: "freebsd", Image: "@freebsd"}); err != nil {
		t.Fatalf("start freebsd: %v", err)
	}
	if _, err := manager.Start(ctx, client.CreateInstanceRequest{ID: "openbsd", Image: "@openbsd"}); err != nil {
		t.Fatalf("start openbsd: %v", err)
	}

	if len(host.starts) != 2 {
		t.Fatalf("starts = %+v", host.starts)
	}
	first := host.starts[0].Network
	second := host.starts[1].Network
	if first == nil || second == nil {
		t.Fatalf("network configs = %+v, %+v", first, second)
	}
	if !first.Enabled || !second.Enabled {
		t.Fatalf("network enabled = %v, %v", first.Enabled, second.Enabled)
	}
	if first.GuestIPv4 != "10.42.0.2" || second.GuestIPv4 != "10.42.0.3" {
		t.Fatalf("guest IPv4 leases = %q, %q", first.GuestIPv4, second.GuestIPv4)
	}
	if first.GuestMAC != "02:42:0a:2a:00:02" || second.GuestMAC != "02:42:0a:2a:00:03" {
		t.Fatalf("guest MAC leases = %q, %q", first.GuestMAC, second.GuestMAC)
	}
}

func TestManagerConcurrentStreamsUseDistinctGuestExecIDs(t *testing.T) {
	ctx := context.Background()
	host := newFakeHost(VMHostCapabilities{Backend: "fake", MaxVMs: 2})
	inst := newFakeInstance()
	host.queueInstance(inst)
	manager := testManager(host)
	defer manager.ShutdownAll(ctx)

	if _, err := manager.Start(ctx, client.CreateInstanceRequest{ID: "alpha", Image: "alpine"}); err != nil {
		t.Fatalf("start: %v", err)
	}

	started := make(chan client.ExecRequest, 2)
	release := make(chan struct{})
	inst.execStream = func(req client.ExecRequest, onEvent func(client.ExecEvent) error) error {
		started <- req
		<-release
		return emitFakeExecEvents(onEvent, strings.Join(req.Command, " "))
	}

	errs := make(chan error, 2)
	go func() {
		errs <- manager.RunStreamIn(ctx, "alpha", client.RunRequest{Command: []string{"one"}}, nil, nil)
	}()
	go func() {
		errs <- manager.RunStreamIn(ctx, "alpha", client.RunRequest{Command: []string{"two"}}, nil, nil)
	}()

	first := <-started
	second := <-started
	if first.ID == "" || second.ID == "" || first.ID == second.ID || first.ID == "alpha" || second.ID == "alpha" {
		close(release)
		t.Fatalf("concurrent exec ids = %q, %q; want unique guest exec ids", first.ID, second.ID)
	}
	close(release)
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("stream %d: %v", i, err)
		}
	}
}

func TestManagerCapacityAndDuplicateStart(t *testing.T) {
	ctx := context.Background()
	host := newFakeHost(VMHostCapabilities{Backend: "fake", MaxVMs: 1})
	manager := testManager(host)
	defer manager.ShutdownAll(ctx)

	if _, err := manager.Start(ctx, client.CreateInstanceRequest{ID: "alpha", Image: "alpine"}); err != nil {
		t.Fatalf("start alpha: %v", err)
	}
	if _, err := manager.Start(ctx, client.CreateInstanceRequest{ID: "alpha", Image: "alpine"}); err == nil {
		t.Fatalf("duplicate start error = %v", err)
	}
	if _, err := manager.Start(ctx, client.CreateInstanceRequest{ID: "beta", Image: "alpine"}); err == nil {
		t.Fatalf("capacity error = %v", err)
	}
}

func TestManagerSnapshotConsoleForwardAndStats(t *testing.T) {
	ctx := context.Background()
	host := newFakeHost(VMHostCapabilities{Backend: "fake", MaxVMs: 2})
	inst := newFakeInstance()
	inst.root = vmTestRoot(t, map[string]string{"/root.txt": "root"})
	inst.snapshots["base"] = vmTestRoot(t, map[string]string{"/image.txt": "image"})
	inst.history = "console text"
	inst.stats = []virtio.FSStats{{Tag: "root", CacheMode: "strict"}}
	host.queueInstance(inst)
	manager := testManager(host)
	defer manager.ShutdownAll(ctx)

	if _, err := manager.Start(ctx, client.CreateInstanceRequest{ID: "alpha", Image: "alpine"}); err != nil {
		t.Fatalf("start: %v", err)
	}

	forward := client.PortForward{Protocol: "tcp", HostPort: 8080, GuestPort: 80}
	if err := manager.AddPortForwardTo(ctx, "alpha", forward); err != nil {
		t.Fatalf("add port forward: %v", err)
	}
	if len(inst.forwards) != 1 || inst.forwards[0] != forward {
		t.Fatalf("forwards = %+v", inst.forwards)
	}

	if err := manager.FlushInstance(ctx, "alpha"); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if history, err := manager.ConsoleHistory(ctx, "alpha"); err != nil || history != "console text" {
		t.Fatalf("console history = %q, %v", history, err)
	}

	root, sourceImage, err := manager.SnapshotRootFS(ctx, "alpha", "")
	if err != nil {
		t.Fatalf("root snapshot: %v", err)
	}
	if sourceImage != "alpine" || vmTestReadFile(t, root, "/root.txt") != "root" {
		t.Fatalf("root snapshot source=%q", sourceImage)
	}
	imageRoot, sourceImage, err := manager.SnapshotRootFS(ctx, "alpha", "base")
	if err != nil {
		t.Fatalf("image snapshot: %v", err)
	}
	if sourceImage != "base" || vmTestReadFile(t, imageRoot, "/image.txt") != "image" {
		t.Fatalf("image snapshot source=%q", sourceImage)
	}
	if inst.flushes < 3 {
		t.Fatalf("flushes = %d, want at least explicit flush plus snapshots", inst.flushes)
	}

	stats := manager.VirtioFSStats("alpha")
	if len(stats) != 1 || stats[0].Tag != "root" {
		t.Fatalf("virtiofs stats = %+v", stats)
	}

	if err := manager.ShutdownInstance(ctx, "alpha"); err != nil {
		t.Fatalf("shutdown instance: %v", err)
	}
	if state := manager.StatusOf("alpha"); state.Status != "stopped" {
		t.Fatalf("post-shutdown state = %+v", state)
	}
}

type fakeHost struct {
	mu                sync.Mutex
	caps              VMHostCapabilities
	next              []Instance
	starts            []client.CreateInstanceRequest
	blankStarts       []client.StartInstanceRequest
	runs              []client.RunRequest
	runStreams        []client.RunRequest
	runIns            []fakeRunInCall
	runInStreams      []fakeRunInCall
	execInStreams     []fakeExecInCall
	closeCalled       bool
	hostCapabilitiesN int
}

type blockingStartHost struct {
	*fakeHost
	entered chan struct{}
	release chan struct{}
}

func (h *blockingStartHost) StartStream(ctx context.Context, req client.CreateInstanceRequest, onEvent func(client.BootEvent) error) (Instance, error) {
	close(h.entered)
	select {
	case <-h.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return h.fakeHost.StartStream(ctx, req, onEvent)
}

type fakeRunInCall struct {
	inst         Instance
	runningImage string
	req          client.RunRequest
}

type fakeExecInCall struct {
	inst         Instance
	runningImage string
	req          client.ExecRequest
}

func newFakeHost(caps VMHostCapabilities) *fakeHost {
	if caps.Backend == "" {
		caps.Backend = "fake"
	}
	return &fakeHost{caps: caps}
}

func (h *fakeHost) queueInstance(inst Instance) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.next = append(h.next, inst)
}

func (h *fakeHost) HostCapabilities(context.Context) VMHostCapabilities {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.hostCapabilitiesN++
	return h.caps
}

func (h *fakeHost) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closeCalled = true
	return nil
}

func (h *fakeHost) Start(ctx context.Context, req client.CreateInstanceRequest) (Instance, error) {
	return h.StartStream(ctx, req, nil)
}

func (h *fakeHost) StartStream(_ context.Context, req client.CreateInstanceRequest, onEvent func(client.BootEvent) error) (Instance, error) {
	h.mu.Lock()
	h.starts = append(h.starts, req)
	inst := h.popInstanceLocked()
	h.mu.Unlock()
	if onEvent != nil {
		if err := onEvent(client.BootEvent{Kind: "status", Message: "fake start"}); err != nil {
			return nil, err
		}
	}
	return inst, nil
}

func (h *fakeHost) StartBlank(ctx context.Context, req client.StartInstanceRequest) (Instance, error) {
	return h.StartBlankStream(ctx, req, nil)
}

func (h *fakeHost) StartBlankStream(_ context.Context, req client.StartInstanceRequest, onEvent func(client.BootEvent) error) (Instance, error) {
	h.mu.Lock()
	h.blankStarts = append(h.blankStarts, req)
	inst := h.popInstanceLocked()
	h.mu.Unlock()
	if onEvent != nil {
		if err := onEvent(client.BootEvent{Kind: "status", Message: "fake blank start"}); err != nil {
			return nil, err
		}
	}
	return inst, nil
}

func (h *fakeHost) Run(_ context.Context, req client.RunRequest) (client.ExecResponse, error) {
	h.mu.Lock()
	h.runs = append(h.runs, req)
	h.mu.Unlock()
	return client.ExecResponse{Output: fmt.Sprintf("host run %s: %s", req.Image, strings.Join(req.Command, " "))}, nil
}

func (h *fakeHost) RunStream(_ context.Context, req client.RunRequest, _ <-chan client.ExecInput, onEvent func(client.ExecEvent) error) error {
	h.mu.Lock()
	h.runStreams = append(h.runStreams, req)
	h.mu.Unlock()
	return emitFakeExecEvents(onEvent, "host stream")
}

func (h *fakeHost) RunInInstance(_ context.Context, inst Instance, runningImage string, req client.RunRequest) (client.ExecResponse, error) {
	h.mu.Lock()
	h.runIns = append(h.runIns, fakeRunInCall{inst: inst, runningImage: runningImage, req: req})
	h.mu.Unlock()
	return client.ExecResponse{Output: fmt.Sprintf("host run in %s: %s", runningImage, strings.Join(req.Command, " "))}, nil
}

func (h *fakeHost) RunInInstanceStream(_ context.Context, inst Instance, runningImage string, req client.RunRequest, _ <-chan client.ExecInput, onEvent func(client.ExecEvent) error) error {
	h.mu.Lock()
	h.runInStreams = append(h.runInStreams, fakeRunInCall{inst: inst, runningImage: runningImage, req: req})
	h.mu.Unlock()
	return emitFakeExecEvents(onEvent, "host run in stream")
}

func (h *fakeHost) ExecInInstanceStream(_ context.Context, inst Instance, runningImage string, req client.ExecRequest, _ <-chan client.ExecInput, onEvent func(client.ExecEvent) error) error {
	h.mu.Lock()
	h.execInStreams = append(h.execInStreams, fakeExecInCall{inst: inst, runningImage: runningImage, req: req})
	h.mu.Unlock()
	return emitFakeExecEvents(onEvent, "host exec in stream")
}

func (h *fakeHost) popInstanceLocked() Instance {
	if len(h.next) == 0 {
		return newFakeInstance()
	}
	inst := h.next[0]
	h.next = h.next[1:]
	return inst
}

func (h *fakeHost) runCalls() []client.RunRequest {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]client.RunRequest(nil), h.runs...)
}

func (h *fakeHost) runStreamCalls() []client.RunRequest {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]client.RunRequest(nil), h.runStreams...)
}

func (h *fakeHost) runInCalls() []fakeRunInCall {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]fakeRunInCall(nil), h.runIns...)
}

func (h *fakeHost) runInStreamCalls() []fakeRunInCall {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]fakeRunInCall(nil), h.runInStreams...)
}

func (h *fakeHost) execInStreamCalls() []fakeExecInCall {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]fakeExecInCall(nil), h.execInStreams...)
}

type fakeInstance struct {
	mu             sync.Mutex
	done           chan struct{}
	closeOnce      sync.Once
	shares         []client.ShareMount
	forwards       []client.PortForward
	execReqs       []client.ExecRequest
	execStreamReqs []client.ExecRequest
	flushes        int
	history        string
	root           imagefs.Directory
	snapshots      map[string]imagefs.Directory
	stats          []virtio.FSStats
	ipv4           string
	execStream     func(client.ExecRequest, func(client.ExecEvent) error) error
	waitErr        error
}

type shutdownTestInstance struct {
	*fakeInstance
	closeFunc  func(int) error
	closeCalls atomic.Int32
}

func (i *shutdownTestInstance) Close() error {
	call := int(i.closeCalls.Add(1))
	if i.closeFunc != nil {
		if err := i.closeFunc(call); err != nil {
			return err
		}
	}
	return i.fakeInstance.Close()
}

type flakyCloseInstance struct {
	*fakeInstance
	firstStarted chan struct{}
	releaseFirst chan struct{}
	firstErr     error
	closeCalls   atomic.Int32
}

func (i *flakyCloseInstance) Close() error {
	if i.closeCalls.Add(1) == 1 {
		close(i.firstStarted)
		<-i.releaseFirst
		return i.firstErr
	}
	return i.fakeInstance.Close()
}

func newFakeInstance() *fakeInstance {
	return &fakeInstance{
		done:      make(chan struct{}),
		root:      vmTestRoot(nil, nil),
		snapshots: map[string]imagefs.Directory{},
	}
}

func (i *fakeInstance) AddShare(_ context.Context, share client.ShareMount) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.shares = append(i.shares, share)
	return nil
}

func (i *fakeInstance) AddPortForward(_ context.Context, forward client.PortForward) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.forwards = append(i.forwards, forward)
	return nil
}

func (i *fakeInstance) Exec(_ context.Context, req client.ExecRequest) (client.ExecResponse, error) {
	i.mu.Lock()
	i.execReqs = append(i.execReqs, req)
	i.mu.Unlock()
	return client.ExecResponse{Output: strings.Join(req.Command, " ")}, nil
}

func (i *fakeInstance) ExecStream(_ context.Context, req client.ExecRequest, _ <-chan client.ExecInput, onEvent func(client.ExecEvent) error) error {
	i.mu.Lock()
	i.execStreamReqs = append(i.execStreamReqs, req)
	i.mu.Unlock()
	if i.execStream != nil {
		return i.execStream(req, onEvent)
	}
	return emitFakeExecEvents(onEvent, strings.Join(req.Command, " "))
}

func (i *fakeInstance) Wait() error {
	<-i.done
	return i.waitErr
}

func (i *fakeInstance) Close() error {
	i.closeOnce.Do(func() { close(i.done) })
	return nil
}

func (i *fakeInstance) Flush(context.Context) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.flushes++
	return nil
}

func (i *fakeInstance) ConsoleHistory(context.Context) (string, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.history, nil
}

func (i *fakeInstance) RootSnapshot() (imagefs.Directory, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.root, nil
}

func (i *fakeInstance) SnapshotImage(imageName string) (imagefs.Directory, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	root, ok := i.snapshots[imageName]
	if !ok {
		return nil, fmt.Errorf("snapshot %q missing", imageName)
	}
	return root, nil
}

func (i *fakeInstance) NetworkIPv4() string {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.ipv4
}

func (i *fakeInstance) VirtioFSStats() []virtio.FSStats {
	i.mu.Lock()
	defer i.mu.Unlock()
	return append([]virtio.FSStats(nil), i.stats...)
}

func testManager(host VMHost) *Manager {
	manager := NewManagerWithHost(host)
	manager.maxMemoryMB = 1 << 40
	manager.maxCPUs = 1 << 20
	manager.supports = func() error { return nil }
	manager.capabilities = func() client.CapabilitiesResponse {
		caps := host.HostCapabilities(context.Background())
		return client.CapabilitiesResponse{
			Backend:      caps.Backend,
			MaxInstances: caps.MaxVMs,
			VMSupported:  true,
		}
	}
	return manager
}

func appendExecEvents(events *[]client.ExecEvent) func(client.ExecEvent) error {
	return func(event client.ExecEvent) error {
		*events = append(*events, event)
		return nil
	}
}

func emitFakeExecEvents(onEvent func(client.ExecEvent) error, output string) error {
	if onEvent == nil {
		return nil
	}
	if output != "" {
		if err := onEvent(client.ExecEvent{Kind: "stdout", Stream: "stdout", Output: output}); err != nil {
			return err
		}
	}
	return onEvent(client.ExecEvent{Kind: "exit"})
}

func vmTestRoot(t *testing.T, files map[string]string) imagefs.Directory {
	if t != nil {
		t.Helper()
	}
	overlay := imagefs.NewOverlay(nil)
	for guestPath, contents := range files {
		if err := overlay.AddFile(guestPath, 0o644, []byte(contents)); err != nil {
			if t == nil {
				panic(err)
			}
			t.Fatalf("add %s: %v", guestPath, err)
		}
	}
	return overlay.Root()
}

func vmTestReadFile(t *testing.T, root imagefs.Directory, guestPath string) string {
	t.Helper()
	entry, err := imagefs.LookupPath(root, guestPath)
	if err != nil {
		t.Fatalf("lookup %s: %v", guestPath, err)
	}
	if entry.File == nil {
		t.Fatalf("%s is not a file", guestPath)
	}
	size, _ := entry.File.Stat()
	data, err := entry.File.ReadAt(0, uint32(size))
	if err != nil {
		t.Fatalf("read %s: %v", guestPath, err)
	}
	return string(data)
}
