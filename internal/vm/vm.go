package vm

import (
	"context"
	"errors"
	"fmt"
	"net"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/hv"
	"j5.nz/cc/internal/imagefs"
	"j5.nz/cc/internal/virtio"
	"j5.nz/cc/internal/vm/builtin"
	vmhost "j5.nz/cc/internal/vm/host"
)

const DefaultInstanceID = "default"

type Backend = vmhost.Backend

type VMHost = vmhost.VMHost

type VMHandle interface {
	Instance
}

type Instance = vmhost.Instance

type virtioFSStatsProvider interface {
	VirtioFSStats() []virtio.FSStats
}

type networkIPv4Provider interface {
	NetworkIPv4() string
}

type serviceProxyPortAllower interface {
	AllowServiceProxyPort(context.Context, int) error
}

type instanceFlushProvider interface {
	Flush(context.Context) error
}

type consoleHistoryProvider interface {
	ConsoleHistory(context.Context) (string, error)
}

type rootSnapshotProvider interface {
	RootSnapshot() (imagefs.Directory, error)
}

type imageSnapshotProvider interface {
	SnapshotImage(string) (imagefs.Directory, error)
}

type Manager struct {
	mu            sync.Mutex
	host          VMHost
	supports      func() error
	capabilities  func() client.CapabilitiesResponse
	running       map[string]*Machine
	starting      map[string]struct{}
	networkLeases map[string]managerNetworkLease
}

type Machine struct {
	id           string
	image        string
	initSystem   string
	kernel       string
	memoryMB     uint64
	cpus         int
	nestedVirt   bool
	startedAt    time.Time
	instance     Instance
	lastErr      error
	exitedAt     time.Time
	shutdownCh   chan struct{}
	shutdownOnce sync.Once
	stopping     bool
}

type managerNetworkLease struct {
	ip  net.IP
	mac net.HardwareAddr
}

func NewManager() *Manager {
	return NewManagerWithBackend(vmhost.UnsupportedBackend{})
}

func NewManagerWithBackend(backend Backend) *Manager {
	m := &Manager{supports: Supports, capabilities: HostCapabilities}
	m.host = vmhost.NewInProcess(backend, func() client.CapabilitiesResponse {
		return m.Capabilities()
	})
	return m
}

func NewManagerWithHost(host VMHost) *Manager {
	if host == nil {
		host = vmhost.NewInProcess(vmhost.UnsupportedBackend{}, HostCapabilities)
	}
	return &Manager{host: host, supports: Supports, capabilities: HostCapabilities}
}

func NewManagerWithHosts(hosts ...VMHost) *Manager {
	return NewManagerWithHost(vmhost.NewPlacement(hosts...))
}

func Supports() error {
	return hv.Supports()
}

func HostCapabilities() client.CapabilitiesResponse {
	host := runtime.GOOS + "/" + runtime.GOARCH
	caps := client.CapabilitiesResponse{
		Host:                   host,
		Backend:                backendName(),
		MaxInstances:           64,
		SnapshotClasses:        []string{},
		NetworkModes:           networkModesForHost(runtime.GOOS, runtime.GOARCH),
		ShareConsistency:       []string{"host-backed"},
		ResourceLimits:         resourceLimitsForHost(runtime.GOOS, runtime.GOARCH),
		SupportsMultiImageExec: true,
		RequiresPrivilegedCCX3: false,
	}
	if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
		caps.MaxInstances = 1
		caps.Notes = append(caps.Notes, "macOS HVF currently limits ccx3 to one running instance")
	}
	if runtime.GOOS == "windows" && runtime.GOARCH == "amd64" {
		caps.MaxInstances = 1
		caps.Notes = append(caps.Notes, "Windows WHP currently supports one vCPU per instance")
	}
	if supported, err := hv.NestedVirtualizationSupported(); err == nil && supported {
		caps.SupportsNestedVirt = true
		caps.ResourceLimits = append(caps.ResourceLimits, "nested_virtualization")
	}
	if err := Supports(); err != nil {
		caps.VMSupported = false
		caps.VMError = err.Error()
	} else {
		caps.VMSupported = true
	}
	return caps
}

func resourceLimitsForHost(goos, goarch string) []string {
	limits := []string{"memory_mb"}
	switch {
	case goos == "linux" && goarch == "amd64":
		limits = append(limits, "cpus")
	case goos == "darwin" && goarch == "arm64":
		limits = append(limits, "cpus")
	}
	return limits
}

func networkModesForHost(goos, goarch string) []string {
	switch {
	case goos == "linux" && goarch == "amd64":
		return []string{"user"}
	case goos == "darwin" && goarch == "arm64":
		return []string{"user"}
	case goos == "windows" && goarch == "amd64":
		return []string{"user"}
	default:
		return []string{}
	}
}

func backendName() string {
	switch {
	case runtime.GOOS == "linux" && (runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64"):
		return "kvm"
	case runtime.GOOS == "darwin" && runtime.GOARCH == "arm64":
		return "hvf"
	case runtime.GOOS == "windows" && runtime.GOARCH == "amd64":
		return "whp"
	default:
		return "unsupported"
	}
}

func (m *Manager) Start(ctx context.Context, req client.CreateInstanceRequest) (client.InstanceState, error) {
	return m.StartStream(ctx, req, nil)
}

func (m *Manager) StartStream(ctx context.Context, req client.CreateInstanceRequest, onEvent func(client.BootEvent) error) (client.InstanceState, error) {
	id := instanceID(req.ID)
	return m.StartInstanceStream(ctx, id, req, onEvent)
}

func (m *Manager) StartInstanceStream(ctx context.Context, id string, req client.CreateInstanceRequest, onEvent func(client.BootEvent) error) (client.InstanceState, error) {
	id = instanceID(id)
	req.ID = id
	if req.Image == "" {
		return client.InstanceState{}, fmt.Errorf("image is required")
	}
	if err := m.supports(); err != nil {
		return client.InstanceState{}, err
	}

	m.mu.Lock()
	if m.running == nil {
		m.running = make(map[string]*Machine)
	}
	if m.starting == nil {
		m.starting = make(map[string]struct{})
	}
	if m.running[id] != nil {
		state := m.statusLocked(id)
		m.mu.Unlock()
		return state, fmt.Errorf("VM %q is already running", id)
	}
	if _, ok := m.starting[id]; ok {
		state := m.statusLocked(id)
		m.mu.Unlock()
		return state, fmt.Errorf("VM %q is already starting", id)
	}
	if err := m.checkCapacityLocked(); err != nil {
		m.mu.Unlock()
		return client.InstanceState{}, err
	}
	m.starting[id] = struct{}{}
	req.Network = m.ensureNetworkLeaseLocked(id, req.Image, req.Network)
	m.mu.Unlock()

	inst, err := m.host.StartStream(ctx, req, onEvent)
	if err != nil {
		m.clearStarting(id)
		m.releaseNetworkLease(id)
		return client.InstanceState{}, err
	}

	machine := &Machine{
		id:         id,
		image:      req.Image,
		initSystem: req.InitSystem,
		kernel:     req.Kernel,
		memoryMB:   req.MemoryMB,
		cpus:       req.CPUs,
		nestedVirt: req.NestedVirt,
		startedAt:  time.Now().UTC(),
		instance:   inst,
		shutdownCh: make(chan struct{}),
	}

	m.mu.Lock()
	if m.running == nil {
		m.running = make(map[string]*Machine)
	}
	delete(m.starting, id)
	m.running[id] = machine
	m.mu.Unlock()

	go m.watch(machine)

	return m.StatusOf(id), nil
}

func (m *Manager) StartBlank(ctx context.Context, req client.StartInstanceRequest) (client.InstanceState, error) {
	return m.StartBlankStream(ctx, req, nil)
}

func (m *Manager) StartBlankStream(
	ctx context.Context,
	req client.StartInstanceRequest,
	onEvent func(client.BootEvent) error,
) (client.InstanceState, error) {
	id := instanceID(req.ID)
	return m.StartBlankInstanceStream(ctx, id, req, onEvent)
}

func (m *Manager) StartBlankInstanceStream(
	ctx context.Context,
	id string,
	req client.StartInstanceRequest,
	onEvent func(client.BootEvent) error,
) (client.InstanceState, error) {
	id = instanceID(id)
	req.ID = id
	if err := m.supports(); err != nil {
		return client.InstanceState{}, err
	}

	m.mu.Lock()
	if m.running == nil {
		m.running = make(map[string]*Machine)
	}
	if m.starting == nil {
		m.starting = make(map[string]struct{})
	}
	if m.running[id] != nil {
		state := m.statusLocked(id)
		m.mu.Unlock()
		return state, fmt.Errorf("VM %q is already running", id)
	}
	if _, ok := m.starting[id]; ok {
		state := m.statusLocked(id)
		m.mu.Unlock()
		return state, fmt.Errorf("VM %q is already starting", id)
	}
	if err := m.checkCapacityLocked(); err != nil {
		m.mu.Unlock()
		return client.InstanceState{}, err
	}
	m.starting[id] = struct{}{}
	req.Network = m.ensureNetworkLeaseLocked(id, req.Image, req.Network)
	m.mu.Unlock()

	shares := append([]client.ShareMount(nil), req.Shares...)
	req.Shares = nil
	inst, err := m.host.StartBlankStream(ctx, req, onEvent)
	if err != nil {
		m.clearStarting(id)
		m.releaseNetworkLease(id)
		return client.InstanceState{}, err
	}
	for _, share := range shares {
		if err := inst.AddShare(ctx, share); err != nil {
			_ = inst.Close()
			m.clearStarting(id)
			m.releaseNetworkLease(id)
			return client.InstanceState{}, err
		}
	}

	machine := &Machine{
		id:         id,
		image:      req.Image,
		initSystem: req.InitSystem,
		kernel:     req.Kernel,
		memoryMB:   req.MemoryMB,
		cpus:       req.CPUs,
		nestedVirt: req.NestedVirt,
		startedAt:  time.Now().UTC(),
		instance:   inst,
		shutdownCh: make(chan struct{}),
	}

	m.mu.Lock()
	if m.running == nil {
		m.running = make(map[string]*Machine)
	}
	delete(m.starting, id)
	m.running[id] = machine
	m.mu.Unlock()

	go m.watch(machine)

	return m.StatusOf(id), nil
}

func (m *Manager) Shutdown(ctx context.Context) error {
	return m.ShutdownInstance(ctx, DefaultInstanceID)
}

func (m *Manager) ShutdownAll(ctx context.Context) error {
	_ = ctx
	m.mu.Lock()
	running := m.running
	m.running = nil
	m.starting = nil
	m.mu.Unlock()

	var errs []error
	for id, machine := range running {
		machine.shutdownOnce.Do(func() {
			close(machine.shutdownCh)
		})
		if err := machine.instance.Close(); err != nil {
			errs = append(errs, fmt.Errorf("shutdown VM %q: %w", id, err))
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

func (m *Manager) ShutdownInstance(ctx context.Context, id string) error {
	_ = ctx
	id = instanceID(id)

	m.mu.Lock()
	machine := m.running[id]
	if machine == nil {
		m.mu.Unlock()
		return fmt.Errorf("no VM %q is running", id)
	}
	if machine.stopping {
		m.mu.Unlock()
		return fmt.Errorf("VM %q is already shutting down", id)
	}
	machine.stopping = true
	machine.shutdownOnce.Do(func() {
		close(machine.shutdownCh)
	})
	m.mu.Unlock()

	if err := machine.instance.Close(); err != nil {
		return err
	}

	m.mu.Lock()
	if m.running != nil && m.running[id] == machine {
		delete(m.running, id)
	}
	delete(m.networkLeases, id)
	m.mu.Unlock()
	return nil
}

func (m *Manager) Run(ctx context.Context, req client.RunRequest) (client.ExecResponse, error) {
	return m.RunIn(ctx, req.ID, req)
}

func (m *Manager) RunIn(ctx context.Context, id string, req client.RunRequest) (client.ExecResponse, error) {
	id = instanceID(id)
	m.mu.Lock()
	machine := m.running[id]
	m.mu.Unlock()
	if machine != nil {
		return m.host.RunInInstance(ctx, machine.instance, machine.image, req)
	}
	if req.Image == "" {
		return client.ExecResponse{}, fmt.Errorf("image is required")
	}
	if err := m.supports(); err != nil {
		return client.ExecResponse{}, err
	}
	return m.host.Run(ctx, req)
}

func (m *Manager) Stream(ctx context.Context, req client.ExecRequest, inputs <-chan client.ExecInput, onEvent func(client.ExecEvent) error) error {
	return m.StreamIn(ctx, req.ID, req, inputs, onEvent)
}

func (m *Manager) RunStream(ctx context.Context, req client.RunRequest, inputs <-chan client.ExecInput, onEvent func(client.ExecEvent) error) error {
	return m.RunStreamIn(ctx, req.ID, req, inputs, onEvent)
}

func (m *Manager) RunStreamIn(ctx context.Context, id string, req client.RunRequest, inputs <-chan client.ExecInput, onEvent func(client.ExecEvent) error) error {
	id = instanceID(id)
	m.mu.Lock()
	machine := m.running[id]
	stopping := machine != nil && machine.stopping
	m.mu.Unlock()
	if machine == nil {
		if req.Image == "" {
			return fmt.Errorf("image is required")
		}
		if err := m.supports(); err != nil {
			return err
		}
		return m.host.RunStream(ctx, req, inputs, onEvent)
	}
	if stopping {
		return stoppedVMError(id)
	}
	req.ID = guestExecID()
	targetImage := strings.TrimSpace(req.Image)
	if !sameRuntimeImage(targetImage, machine.image) {
		err := m.host.RunInInstanceStream(ctx, machine.instance, machine.image, req, inputs, onEvent)
		if err != nil && m.instanceIsStopping(id, machine) {
			return stoppedVMError(id)
		}
		return err
	}
	for _, share := range req.Shares {
		if err := machine.instance.AddShare(ctx, share); err != nil {
			if m.instanceIsStopping(id, machine) {
				return stoppedVMError(id)
			}
			return err
		}
	}
	err := machine.instance.ExecStream(ctx, runExecRequest(req), inputs, onEvent)
	if err != nil && m.instanceIsStopping(id, machine) {
		return stoppedVMError(id)
	}
	return err
}

func (m *Manager) StreamIn(ctx context.Context, id string, req client.ExecRequest, inputs <-chan client.ExecInput, onEvent func(client.ExecEvent) error) error {
	id = instanceID(id)
	m.mu.Lock()
	machine := m.running[id]
	stopping := machine != nil && machine.stopping
	m.mu.Unlock()
	if machine == nil {
		return fmt.Errorf("no VM %q is running", id)
	}
	if stopping {
		return stoppedVMError(id)
	}
	req.ID = guestExecID()
	targetImage := strings.TrimSpace(req.Image)
	if vmhost.IsHostedInstance(machine.instance) || targetImage != "" {
		err := m.host.ExecInInstanceStream(ctx, machine.instance, machine.image, req, inputs, onEvent)
		if err != nil && m.instanceIsStopping(id, machine) {
			return stoppedVMError(id)
		}
		return err
	}
	err := machine.instance.ExecStream(ctx, req, inputs, onEvent)
	if err != nil && m.instanceIsStopping(id, machine) {
		return stoppedVMError(id)
	}
	return err
}

func (m *Manager) instanceIsStopping(id string, machine *Machine) bool {
	if machine == nil {
		return false
	}
	select {
	case <-machine.shutdownCh:
		return true
	default:
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return machine.stopping || m.running[id] != machine
}

func stoppedVMError(id string) error {
	return fmt.Errorf("VM %q stopped", id)
}

func (m *Manager) AddPortForward(ctx context.Context, forward client.PortForward) error {
	return m.AddPortForwardTo(ctx, DefaultInstanceID, forward)
}

func (m *Manager) AddShareTo(ctx context.Context, id string, share client.ShareMount) error {
	id = instanceID(id)
	m.mu.Lock()
	machine := m.running[id]
	m.mu.Unlock()
	if machine == nil {
		return fmt.Errorf("no VM %q is running", id)
	}
	return machine.instance.AddShare(ctx, share)
}

func (m *Manager) AddPortForwardTo(ctx context.Context, id string, forward client.PortForward) error {
	id = instanceID(id)
	m.mu.Lock()
	machine := m.running[id]
	m.mu.Unlock()
	if machine == nil {
		return fmt.Errorf("no VM %q is running", id)
	}
	return machine.instance.AddPortForward(ctx, forward)
}

func (m *Manager) AllowServiceProxyPortTo(ctx context.Context, id string, port int) error {
	id = instanceID(id)
	m.mu.Lock()
	machine := m.running[id]
	m.mu.Unlock()
	if machine == nil {
		return fmt.Errorf("no VM %q is running", id)
	}
	allower, ok := machine.instance.(serviceProxyPortAllower)
	if !ok {
		return fmt.Errorf("VM %q network does not support service proxy port updates", id)
	}
	return allower.AllowServiceProxyPort(ctx, port)
}

func (m *Manager) FlushInstance(ctx context.Context, id string) error {
	id = instanceID(id)
	m.mu.Lock()
	machine := m.running[id]
	m.mu.Unlock()
	if machine == nil {
		return fmt.Errorf("no VM %q is running", id)
	}
	flusher, ok := machine.instance.(instanceFlushProvider)
	if !ok {
		return fmt.Errorf("VM %q root filesystem cannot be flushed", id)
	}
	return flusher.Flush(ctx)
}

func (m *Manager) ConsoleHistory(ctx context.Context, id string) (string, error) {
	id = instanceID(id)
	m.mu.Lock()
	machine := m.running[id]
	m.mu.Unlock()
	if machine == nil {
		return "", fmt.Errorf("no VM %q is running", id)
	}
	provider, ok := machine.instance.(consoleHistoryProvider)
	if !ok {
		return "", fmt.Errorf("VM %q console history is not available", id)
	}
	return provider.ConsoleHistory(ctx)
}

func (m *Manager) SnapshotRootFS(ctx context.Context, id string, imageName string) (imagefs.Directory, string, error) {
	id = instanceID(id)
	imageName = strings.TrimSpace(imageName)
	m.mu.Lock()
	machine := m.running[id]
	m.mu.Unlock()
	if machine == nil {
		return nil, "", fmt.Errorf("no VM %q is running", id)
	}
	if err := m.FlushInstance(ctx, id); err != nil {
		return nil, "", err
	}
	if imageName != "" {
		if snapshotter, ok := machine.instance.(imageSnapshotProvider); ok {
			root, err := snapshotter.SnapshotImage(imageName)
			if err != nil {
				return nil, "", err
			}
			return root, imageName, nil
		}
		return nil, "", fmt.Errorf("VM %q image %q cannot be snapshotted", id, imageName)
	}
	snapshotter, ok := machine.instance.(rootSnapshotProvider)
	if !ok {
		return nil, "", fmt.Errorf("VM %q root filesystem cannot be snapshotted", id)
	}
	root, err := snapshotter.RootSnapshot()
	if err != nil {
		return nil, "", err
	}
	return root, machine.image, nil
}

func (m *Manager) Status() client.InstanceState {
	return m.StatusOf(DefaultInstanceID)
}

func (m *Manager) StatusOf(id string) client.InstanceState {
	id = instanceID(id)
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.statusLocked(id)
}

func (m *Manager) VirtioFSStats(id string) []virtio.FSStats {
	id = instanceID(id)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.running == nil || m.running[id] == nil || m.running[id].instance == nil {
		return nil
	}
	provider, ok := m.running[id].instance.(virtioFSStatsProvider)
	if !ok {
		return nil
	}
	return provider.VirtioFSStats()
}

func (m *Manager) Statuses() []client.InstanceState {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.running) == 0 {
		return nil
	}
	ids := make([]string, 0, len(m.running))
	for id := range m.running {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]client.InstanceState, 0, len(ids))
	for _, id := range ids {
		out = append(out, m.statusLocked(id))
	}
	return out
}

func (m *Manager) Capabilities() client.CapabilitiesResponse {
	if m.capabilities == nil {
		return HostCapabilities()
	}
	return m.capabilities()
}

func (m *Manager) statusLocked(id string) client.InstanceState {
	id = instanceID(id)
	if m.running == nil || m.running[id] == nil {
		if m.starting != nil {
			if _, ok := m.starting[id]; ok {
				return client.InstanceState{ID: id, Status: "starting"}
			}
		}
		return client.InstanceState{ID: id, Status: "stopped"}
	}
	machine := m.running[id]
	state := client.InstanceState{
		ID:         id,
		Status:     "running",
		Image:      machine.image,
		InitSystem: machine.initSystem,
		Kernel:     machine.kernel,
		MemoryMB:   machine.memoryMB,
		CPUs:       machine.cpus,
		NestedVirt: machine.nestedVirt,
		StartedAt:  machine.startedAt.Format(time.RFC3339),
	}
	if provider, ok := machine.instance.(networkIPv4Provider); ok {
		state.NetworkIPv4 = provider.NetworkIPv4()
	}
	return state
}

func (m *Manager) watch(machine *Machine) {
	err := machine.instance.Wait()

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running != nil && m.running[machine.id] == machine {
		delete(m.running, machine.id)
	}
	delete(m.networkLeases, machine.id)
	machine.lastErr = err
	machine.exitedAt = time.Now().UTC()
}

func (m *Manager) checkCapacityLocked() error {
	caps := m.host.HostCapabilities(context.Background())
	if caps.MaxVMs > 0 && len(m.running)+len(m.starting) >= caps.MaxVMs {
		return fmt.Errorf("maximum running VM instances reached: %d", caps.MaxVMs)
	}
	return nil
}

func (m *Manager) clearStarting(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.starting, id)
}

func (m *Manager) ensureNetworkLeaseLocked(id, image string, cfg *client.NetworkConfig) *client.NetworkConfig {
	if cfg == nil {
		if !builtin.IsGuestImage(image) {
			return nil
		}
		cfg = &client.NetworkConfig{Enabled: true}
	}
	if !cfg.Enabled {
		return cfg
	}
	if strings.TrimSpace(cfg.GuestIPv4) != "" && strings.TrimSpace(cfg.GuestMAC) != "" {
		return cfg
	}
	id = instanceID(id)
	if m.networkLeases == nil {
		m.networkLeases = make(map[string]managerNetworkLease)
	}
	lease, ok := m.networkLeases[id]
	if !ok {
		used := map[byte]bool{1: true}
		for _, existing := range m.networkLeases {
			if ip4 := existing.ip.To4(); ip4 != nil {
				used[ip4[3]] = true
			}
		}
		host := byte(2)
		for ; host <= 254; host++ {
			if !used[host] {
				break
			}
		}
		lease = managerNetworkLease{
			ip:  net.IPv4(10, 42, 0, host),
			mac: net.HardwareAddr{0x02, 0x42, 0x0a, 0x2a, 0x00, host},
		}
		m.networkLeases[id] = lease
	}
	next := *cfg
	if strings.TrimSpace(next.GuestIPv4) == "" {
		next.GuestIPv4 = lease.ip.String()
	}
	if strings.TrimSpace(next.GuestMAC) == "" {
		next.GuestMAC = lease.mac.String()
	}
	return &next
}

func (m *Manager) releaseNetworkLease(id string) {
	id = instanceID(id)
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.networkLeases, id)
}

func instanceID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return DefaultInstanceID
	}
	return id
}
