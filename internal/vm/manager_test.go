package vm

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/imagefs"
	"j5.nz/cc/internal/virtio"
)

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
		CPUs:       2,
		NestedVirt: true,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if state.ID != "alpha" || state.Status != "running" || state.Image != "alpine" || state.MemoryMB != 256 || state.CPUs != 2 || !state.NestedVirt || state.NetworkIPv4 != "10.0.2.15" {
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
	}, nil)
	if err != nil {
		t.Fatalf("start blank: %v", err)
	}
	if state.Image != "ubuntu" || state.InitSystem != "systemd" {
		t.Fatalf("state = %+v, want image ubuntu and init systemd", state)
	}
	if _, err := manager.RunIn(ctx, "alpha", client.RunRequest{Image: "ubuntu", Command: []string{"systemctl"}}); err != nil {
		t.Fatalf("run in blank instance: %v", err)
	}
	if got := host.runInCalls(); len(got) != 1 || got[0].runningImage != "ubuntu" || got[0].req.Image != "ubuntu" {
		t.Fatalf("run-in calls = %+v", got)
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
	next              []*fakeInstance
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

func (h *fakeHost) queueInstance(inst *fakeInstance) {
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

func (h *fakeHost) popInstanceLocked() *fakeInstance {
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
	return nil
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
