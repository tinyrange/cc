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
	"j5.nz/cc/internal/vm/mounts"
)

const DefaultInstanceID = "default"
const maxExitTombstones = 64
const minimumGuestMemoryMB = 128
const maxLifecycleDiagnosticBytes = 4096

var ErrManagerClosing = errors.New("VM manager is shutting down")

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

type rootSnapshotContextProvider interface {
	RootSnapshotContext(context.Context) (imagefs.Directory, error)
}

type imageSnapshotProvider interface {
	SnapshotImage(string) (imagefs.Directory, error)
}

type imageSnapshotContextProvider interface {
	SnapshotImageContext(context.Context, string) (imagefs.Directory, error)
}

type instanceBalloonController interface {
	SetBalloonMB(uint64) error
}

type instanceBalloonStateProvider interface {
	BalloonState() (targetMB, actualMB uint64, driverReady, supported bool)
}

type instanceBackingUsageProvider interface {
	BackingUsage() (uint64, uint64, uint64, error)
}

type instanceBackingMetadataUsageProvider interface {
	BackingMetadataUsage() (uint64, uint64)
}

type instanceBackingCombinedUsageProvider interface {
	BackingCombinedUsage() (uint64, uint64)
}

type Manager struct {
	mu            sync.Mutex
	host          VMHost
	supports      func() error
	capabilities  func() client.CapabilitiesResponse
	running       map[string]*Machine
	starting      map[string]*managerStart
	networkLeases map[string]managerNetworkLease
	exited        map[string]client.InstanceState
	reservations  map[string]resourceReservation
	maxMemoryMB   uint64
	maxCPUs       int
	closing       bool
}

type resourceReservation struct {
	memoryMB uint64
	cpus     int
}

type managerStart struct {
	cancel     context.CancelFunc
	done       chan struct{}
	cleanupErr error
}

type Machine struct {
	lifecycleMu              sync.Mutex
	balloonMu                sync.Mutex
	backingMu                sync.Mutex
	snapshotMu               sync.Mutex
	snapshotCancel           context.CancelFunc
	backingHighWater         uint64
	backingDataHighWater     uint64
	backingMetadataHighWater uint64
	id                       string
	image                    string
	initSystem               string
	kernel                   string
	memoryMB                 uint64
	balloonMB                uint64
	cpus                     int
	nestedVirt               bool
	startedAt                time.Time
	instance                 Instance
	lastErr                  error
	exitedAt                 time.Time
	stopping                 bool
	stop                     *machineStopOperation
}

type machineStopOperation struct {
	done      chan struct{}
	err       error
	observers int
}

type managerNetworkLease struct {
	ip  net.IP
	mac net.HardwareAddr
}

func NewManager() *Manager {
	return NewManagerWithBackend(vmhost.UnsupportedBackend{})
}

func NewManagerWithBackend(backend Backend) *Manager {
	m := newManagerBudgets(&Manager{supports: Supports, capabilities: HostCapabilities})
	m.host = vmhost.NewInProcess(backend, HostCapabilities)
	return m
}

func NewManagerWithHost(host VMHost) *Manager {
	if host == nil {
		host = vmhost.NewInProcess(vmhost.UnsupportedBackend{}, HostCapabilities)
	}
	return newManagerBudgets(&Manager{host: host, supports: Supports, capabilities: HostCapabilities})
}

func newManagerBudgets(m *Manager) *Manager {
	// Guest memory is sparsely committed and vCPUs are scheduler work, so
	// configured totals are not useful host-capacity ceilings. The vmsh host
	// observes real memory pressure and dynamically balloons guests instead.
	// Explicit test/embedding budgets can still set these fields when desired.
	return m
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
	if runtime.GOOS == "windows" && (runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64") {
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
	case goos == "windows" && (goarch == "amd64" || goarch == "arm64"):
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
	case runtime.GOOS == "windows" && (runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64"):
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
	if err := normalizeResources(&req.MemoryMB, &req.BalloonMB, &req.CPUs); err != nil {
		return client.InstanceState{}, err
	}
	canonicalShares, err := mounts.CanonicalRuntimeShares(req.Shares)
	if err != nil {
		return client.InstanceState{}, err
	}
	req.Shares = canonicalShares
	if err := m.supports(); err != nil {
		return client.InstanceState{}, err
	}
	maxVMs := m.host.HostCapabilities(ctx).MaxVMs

	m.mu.Lock()
	if m.closing {
		m.mu.Unlock()
		return client.InstanceState{}, ErrManagerClosing
	}
	if m.running == nil {
		m.running = make(map[string]*Machine)
	}
	if m.starting == nil {
		m.starting = make(map[string]*managerStart)
	}
	if m.running[id] != nil {
		snapshot := m.statusSnapshotLocked(id)
		m.mu.Unlock()
		return m.resolveStatusSnapshot(snapshot), fmt.Errorf("VM %q is already running", id)
	}
	if _, ok := m.starting[id]; ok {
		snapshot := m.statusSnapshotLocked(id)
		m.mu.Unlock()
		return m.resolveStatusSnapshot(snapshot), fmt.Errorf("VM %q is already starting", id)
	}
	if err := m.checkCapacityLocked(maxVMs, req.MemoryMB, req.CPUs); err != nil {
		m.mu.Unlock()
		return client.InstanceState{}, err
	}
	delete(m.exited, id)
	if m.reservations == nil {
		m.reservations = make(map[string]resourceReservation)
	}
	m.reservations[id] = resourceReservation{memoryMB: req.MemoryMB, cpus: req.CPUs}
	startCtx, cancelStart := context.WithCancel(ctx)
	start := &managerStart{cancel: cancelStart, done: make(chan struct{})}
	m.starting[id] = start
	req.Network = m.ensureNetworkLeaseLocked(id, req.Image, req.Network)
	m.mu.Unlock()

	inst, err := m.host.StartStream(startCtx, req, onEvent)
	if err != nil {
		m.finishStart(id, start)
		return client.InstanceState{}, err
	}

	machine := &Machine{
		id:         id,
		image:      req.Image,
		initSystem: req.InitSystem,
		kernel:     req.Kernel,
		memoryMB:   req.MemoryMB,
		balloonMB:  req.BalloonMB,
		cpus:       req.CPUs,
		nestedVirt: req.NestedVirt,
		startedAt:  time.Now().UTC(),
		instance:   inst,
	}

	m.mu.Lock()
	if m.closing || m.starting[id] != start {
		if m.starting[id] == start {
			delete(m.starting, id)
		}
		delete(m.reservations, id)
		delete(m.networkLeases, id)
		m.mu.Unlock()
		cancelStart()
		start.cleanupErr = inst.Close()
		close(start.done)
		return client.InstanceState{}, errors.Join(ErrManagerClosing, start.cleanupErr)
	}
	if m.running == nil {
		m.running = make(map[string]*Machine)
	}
	delete(m.starting, id)
	delete(m.reservations, id)
	m.running[id] = machine
	m.mu.Unlock()
	cancelStart()
	close(start.done)

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
	if err := normalizeResources(&req.MemoryMB, &req.BalloonMB, &req.CPUs); err != nil {
		return client.InstanceState{}, err
	}
	canonicalShares, err := mounts.CanonicalRuntimeShares(req.Shares)
	if err != nil {
		return client.InstanceState{}, err
	}
	req.Shares = canonicalShares
	if err := m.supports(); err != nil {
		return client.InstanceState{}, err
	}
	maxVMs := m.host.HostCapabilities(ctx).MaxVMs

	m.mu.Lock()
	if m.closing {
		m.mu.Unlock()
		return client.InstanceState{}, ErrManagerClosing
	}
	if m.running == nil {
		m.running = make(map[string]*Machine)
	}
	if m.starting == nil {
		m.starting = make(map[string]*managerStart)
	}
	if m.running[id] != nil {
		snapshot := m.statusSnapshotLocked(id)
		m.mu.Unlock()
		return m.resolveStatusSnapshot(snapshot), fmt.Errorf("VM %q is already running", id)
	}
	if _, ok := m.starting[id]; ok {
		snapshot := m.statusSnapshotLocked(id)
		m.mu.Unlock()
		return m.resolveStatusSnapshot(snapshot), fmt.Errorf("VM %q is already starting", id)
	}
	if err := m.checkCapacityLocked(maxVMs, req.MemoryMB, req.CPUs); err != nil {
		m.mu.Unlock()
		return client.InstanceState{}, err
	}
	delete(m.exited, id)
	if m.reservations == nil {
		m.reservations = make(map[string]resourceReservation)
	}
	m.reservations[id] = resourceReservation{memoryMB: req.MemoryMB, cpus: req.CPUs}
	startCtx, cancelStart := context.WithCancel(ctx)
	start := &managerStart{cancel: cancelStart, done: make(chan struct{})}
	m.starting[id] = start
	req.Network = m.ensureNetworkLeaseLocked(id, req.Image, req.Network)
	m.mu.Unlock()

	shares := append([]client.ShareMount(nil), req.Shares...)
	snapshotStartup := strings.TrimSpace(req.SnapshotDir) != "" || strings.TrimSpace(req.RestoreSnapshot) != ""
	startupShares := builtin.IsGuestImage(req.Image) || snapshotStartup
	if !startupShares {
		req.Shares = nil
	}
	inst, err := m.host.StartBlankStream(startCtx, req, onEvent)
	if err != nil {
		m.finishStart(id, start)
		return client.InstanceState{}, err
	}
	if !startupShares {
		if err := addInstanceShares(startCtx, inst, shares); err != nil {
			start.cleanupErr = inst.Close()
			m.finishStart(id, start)
			return client.InstanceState{}, errors.Join(err, start.cleanupErr)
		}
	}

	machine := &Machine{
		id:         id,
		image:      req.Image,
		initSystem: req.InitSystem,
		kernel:     req.Kernel,
		memoryMB:   req.MemoryMB,
		balloonMB:  req.BalloonMB,
		cpus:       req.CPUs,
		nestedVirt: req.NestedVirt,
		startedAt:  time.Now().UTC(),
		instance:   inst,
	}

	m.mu.Lock()
	if m.closing || m.starting[id] != start {
		if m.starting[id] == start {
			delete(m.starting, id)
		}
		delete(m.reservations, id)
		delete(m.networkLeases, id)
		m.mu.Unlock()
		cancelStart()
		start.cleanupErr = inst.Close()
		close(start.done)
		return client.InstanceState{}, errors.Join(ErrManagerClosing, start.cleanupErr)
	}
	if m.running == nil {
		m.running = make(map[string]*Machine)
	}
	delete(m.starting, id)
	delete(m.reservations, id)
	m.running[id] = machine
	m.mu.Unlock()
	cancelStart()
	close(start.done)

	go m.watch(machine)

	return m.StatusOf(id), nil
}

func (m *Manager) Shutdown(ctx context.Context) error {
	return m.ShutdownInstance(ctx, DefaultInstanceID)
}

func (m *Manager) ShutdownAll(ctx context.Context) error {
	m.mu.Lock()
	m.closing = true
	results := make(chan managerShutdownResult, len(m.running))
	pending := 0
	pendingStops := make(map[string]*machineStopOperation, len(m.running))
	var errs []error
	for id, machine := range m.running {
		stop := m.beginMachineStopLocked(machine)
		pendingStops[id] = stop
		pending++
		go func() {
			results <- managerShutdownResult{id: id, err: waitMachineStop(context.Background(), stop)}
		}()
	}
	starts := make([]*managerStart, 0, len(m.starting))
	for _, start := range m.starting {
		starts = append(starts, start)
	}
	m.mu.Unlock()
	for _, start := range starts {
		start.cancel()
	}

	for pending > 0 {
		if ctx == nil {
			result := <-results
			pending--
			delete(pendingStops, result.id)
			if result.err != nil {
				errs = append(errs, fmt.Errorf("shutdown VM %q: %w", result.id, result.err))
			}
			continue
		}
		select {
		case result := <-results:
			pending--
			delete(pendingStops, result.id)
			if result.err != nil {
				errs = append(errs, fmt.Errorf("shutdown VM %q: %w", result.id, result.err))
			}
		case <-ctx.Done():
			errs = append(errs, ctx.Err())
			for id, stop := range pendingStops {
				select {
				case <-stop.done:
					if stop.err != nil {
						errs = append(errs, fmt.Errorf("shutdown VM %q: %w", id, stop.err))
					}
				default:
				}
			}
			return errors.Join(errs...)
		}
	}
	for _, start := range starts {
		if ctx == nil {
			<-start.done
		} else {
			select {
			case <-start.done:
			case <-ctx.Done():
				errs = append(errs, ctx.Err())
				return errors.Join(errs...)
			}
		}
		if start.cleanupErr != nil {
			errs = append(errs, fmt.Errorf("clean up canceled VM start: %w", start.cleanupErr))
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

type managerShutdownResult struct {
	id  string
	err error
}

func (m *Manager) ShutdownInstance(ctx context.Context, id string) error {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	id = instanceID(id)

	m.mu.Lock()
	machine := m.running[id]
	if machine == nil {
		if _, exited := m.exited[id]; exited {
			delete(m.exited, id)
			m.mu.Unlock()
			return nil
		}
		m.mu.Unlock()
		return fmt.Errorf("no VM %q is running", id)
	}
	stop := m.beginMachineStopLocked(machine)
	m.mu.Unlock()
	return waitMachineStop(ctx, stop)
}

func (m *Manager) beginMachineStopLocked(machine *Machine) *machineStopOperation {
	if machine.stop != nil {
		select {
		case <-machine.stop.done:
			if machine.stop.err == nil {
				machine.stop.observers++
				return machine.stop
			}
		default:
			machine.stop.observers++
			return machine.stop
		}
	}
	stop := &machineStopOperation{done: make(chan struct{}), observers: 1}
	machine.stop = stop
	machine.stopping = true
	machine.snapshotMu.Lock()
	if machine.snapshotCancel != nil {
		machine.snapshotCancel()
	}
	machine.snapshotMu.Unlock()
	go m.runMachineStop(machine, stop)
	return stop
}

func (m *Manager) runMachineStop(machine *Machine, stop *machineStopOperation) {
	machine.lifecycleMu.Lock()
	// Closing the instance is the recovery mechanism for a backend balloon
	// request which has stopped making progress. Do not wait behind balloonMu:
	// Close must be allowed to tear down the device and make that request return.
	err := machine.instance.Close()
	machine.lifecycleMu.Unlock()
	m.mu.Lock()
	stop.err = err
	if err == nil {
		if m.running != nil && m.running[machine.id] == machine {
			delete(m.running, machine.id)
		}
		delete(m.networkLeases, machine.id)
		m.recordExitLocked(machine, nil)
	} else if machine.stop == stop {
		machine.stopping = false
	}
	close(stop.done)
	m.mu.Unlock()
}

func waitMachineStop(ctx context.Context, stop *machineStopOperation) error {
	if ctx == nil {
		<-stop.done
		return stop.err
	}
	select {
	case <-stop.done:
		return stop.err
	default:
	}
	select {
	case <-stop.done:
		return stop.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *Manager) Run(ctx context.Context, req client.RunRequest) (client.ExecResponse, error) {
	return m.RunIn(ctx, req.ID, req)
}

func (m *Manager) RunIn(ctx context.Context, id string, req client.RunRequest) (client.ExecResponse, error) {
	id = instanceID(id)
	shares, err := mounts.CanonicalRuntimeShares(req.Shares)
	if err != nil {
		return client.ExecResponse{}, err
	}
	req.Shares = shares
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
	shares, err := mounts.CanonicalRuntimeShares(req.Shares)
	if err != nil {
		return err
	}
	req.Shares = shares
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
	if err := validateRuntimeShares(req.Shares); err != nil {
		return err
	}
	locked, unlock, lockErr := m.lockMachineOperation(id)
	if lockErr != nil {
		return lockErr
	}
	if locked != machine {
		unlock()
		return stoppedVMError(id)
	}
	if err := addInstanceShares(ctx, machine.instance, req.Shares); err != nil {
		unlock()
		if m.instanceIsStopping(id, machine) {
			return stoppedVMError(id)
		}
		return err
	}
	unlock()
	err = machine.instance.ExecStream(ctx, runningVMExecRequest(req), inputs, onEvent)
	if err != nil && m.instanceIsStopping(id, machine) {
		return stoppedVMError(id)
	}
	return err
}

func addInstanceShares(ctx context.Context, instance vmhost.Instance, shares []client.ShareMount) error {
	if len(shares) > 1 {
		batch, ok := instance.(interface {
			AddShares(context.Context, []client.ShareMount) error
		})
		if !ok {
			return fmt.Errorf("instance does not support atomic multi-share mutation")
		}
		return batch.AddShares(ctx, shares)
	}
	for _, share := range shares {
		if err := instance.AddShare(ctx, share); err != nil {
			return err
		}
	}
	return nil
}

func validateRuntimeShares(shares []client.ShareMount) error {
	seen := make(map[string]client.ShareMount, len(shares))
	for _, share := range shares {
		canonical, err := mounts.CanonicalRuntimeShare(share)
		if err != nil {
			return err
		}
		mount := canonical.Mount
		if existing, ok := seen[mount]; ok {
			if existing != canonical {
				return fmt.Errorf("share mount %q is specified more than once with different settings", mount)
			}
			continue
		}
		seen[mount] = canonical
	}
	return nil
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
	machine, unlock, err := m.lockMachineOperation(id)
	if err != nil {
		return err
	}
	defer unlock()
	return machine.instance.AddShare(ctx, share)
}

func (m *Manager) AddPortForwardTo(ctx context.Context, id string, forward client.PortForward) error {
	machine, unlock, err := m.lockMachineOperation(id)
	if err != nil {
		return err
	}
	defer unlock()
	return machine.instance.AddPortForward(ctx, forward)
}

func (m *Manager) AllowServiceProxyPortTo(ctx context.Context, id string, port int) error {
	machine, unlock, err := m.lockMachineOperation(id)
	if err != nil {
		return err
	}
	defer unlock()
	allower, ok := machine.instance.(serviceProxyPortAllower)
	if !ok {
		return fmt.Errorf("VM %q network does not support service proxy port updates", machine.id)
	}
	return allower.AllowServiceProxyPort(ctx, port)
}

func (m *Manager) FlushInstance(ctx context.Context, id string) error {
	machine, unlock, err := m.lockMachineOperation(id)
	if err != nil {
		return err
	}
	defer unlock()
	flusher, ok := machine.instance.(instanceFlushProvider)
	if !ok {
		return fmt.Errorf("VM %q root filesystem cannot be flushed", machine.id)
	}
	return flusher.Flush(ctx)
}

func (m *Manager) ConsoleHistory(ctx context.Context, id string) (string, error) {
	machine, unlock, err := m.lockMachineOperation(id)
	if err != nil {
		return "", err
	}
	defer unlock()
	provider, ok := machine.instance.(consoleHistoryProvider)
	if !ok {
		return "", fmt.Errorf("VM %q console history is not available", machine.id)
	}
	return provider.ConsoleHistory(ctx)
}

func (m *Manager) SnapshotRootFS(ctx context.Context, id string, imageName string) (imagefs.Directory, string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	imageName = strings.TrimSpace(imageName)
	machine, unlock, err := m.lockMachineOperation(id)
	if err != nil {
		return nil, "", err
	}
	defer unlock()
	snapshotCtx, snapshotCancel := context.WithCancel(ctx)
	m.mu.Lock()
	if m.running[machine.id] != machine || machine.stopping {
		m.mu.Unlock()
		snapshotCancel()
		return nil, "", stoppedVMError(machine.id)
	}
	machine.snapshotMu.Lock()
	machine.snapshotCancel = snapshotCancel
	machine.snapshotMu.Unlock()
	m.mu.Unlock()
	defer func() {
		snapshotCancel()
		machine.snapshotMu.Lock()
		machine.snapshotCancel = nil
		machine.snapshotMu.Unlock()
	}()
	flusher, ok := machine.instance.(instanceFlushProvider)
	if !ok {
		return nil, "", fmt.Errorf("VM %q root filesystem cannot be flushed", machine.id)
	}
	if err := flusher.Flush(snapshotCtx); err != nil {
		return nil, "", err
	}
	if imageName != "" {
		if snapshotter, ok := machine.instance.(imageSnapshotContextProvider); ok {
			root, err := snapshotter.SnapshotImageContext(snapshotCtx, imageName)
			if err != nil {
				return nil, "", err
			}
			return root, imageName, nil
		}
		return nil, "", fmt.Errorf("VM %q image %q cannot be snapshotted", machine.id, imageName)
	}
	contextSnapshotter, ok := machine.instance.(rootSnapshotContextProvider)
	if !ok {
		return nil, "", fmt.Errorf("VM %q root filesystem does not support cancelable snapshots", machine.id)
	}
	root, err := contextSnapshotter.RootSnapshotContext(snapshotCtx)
	if err != nil {
		return nil, "", err
	}
	return root, machine.image, nil
}

func (m *Manager) lockMachineOperation(id string) (*Machine, func(), error) {
	id = instanceID(id)
	m.mu.Lock()
	machine := m.running[id]
	stopping := machine != nil && machine.stopping
	m.mu.Unlock()
	if machine == nil {
		return nil, nil, fmt.Errorf("no VM %q is running", id)
	}
	if stopping {
		return nil, nil, stoppedVMError(id)
	}
	machine.lifecycleMu.Lock()
	m.mu.Lock()
	valid := m.running[id] == machine && !machine.stopping
	m.mu.Unlock()
	if !valid {
		machine.lifecycleMu.Unlock()
		return nil, nil, stoppedVMError(id)
	}
	return machine, machine.lifecycleMu.Unlock, nil
}

func (m *Manager) Status() client.InstanceState {
	return m.StatusOf(DefaultInstanceID)
}

func (m *Manager) StatusOf(id string) client.InstanceState {
	id = instanceID(id)
	m.mu.Lock()
	snapshot := m.statusSnapshotLocked(id)
	m.mu.Unlock()
	return m.resolveStatusSnapshot(snapshot)
}

func (m *Manager) VirtioFSStats(id string) []virtio.FSStats {
	id = instanceID(id)
	m.mu.Lock()
	if m.running == nil || m.running[id] == nil || m.running[id].instance == nil {
		m.mu.Unlock()
		return nil
	}
	provider, ok := m.running[id].instance.(virtioFSStatsProvider)
	m.mu.Unlock()
	if !ok {
		return nil
	}
	return provider.VirtioFSStats()
}

func (m *Manager) Statuses() []client.InstanceState {
	m.mu.Lock()
	if len(m.running) == 0 && len(m.starting) == 0 && len(m.exited) == 0 {
		m.mu.Unlock()
		return nil
	}
	ids := make([]string, 0, len(m.running)+len(m.starting)+len(m.exited))
	for id := range m.running {
		ids = append(ids, id)
	}
	for id := range m.starting {
		if m.running[id] == nil {
			ids = append(ids, id)
		}
	}
	for id := range m.exited {
		if m.running[id] == nil {
			if _, starting := m.starting[id]; !starting {
				ids = append(ids, id)
			}
		}
	}
	sort.Strings(ids)
	snapshots := make([]managerStatusSnapshot, 0, len(ids))
	for _, id := range ids {
		snapshots = append(snapshots, m.statusSnapshotLocked(id))
	}
	m.mu.Unlock()
	out := make([]client.InstanceState, 0, len(snapshots))
	for _, snapshot := range snapshots {
		out = append(out, m.resolveStatusSnapshot(snapshot))
	}
	return out
}

func (m *Manager) Capabilities() client.CapabilitiesResponse {
	var caps client.CapabilitiesResponse
	if m.capabilities == nil {
		caps = HostCapabilities()
	} else {
		caps = m.capabilities()
	}
	m.mu.Lock()
	memory, cpus := m.resourceUsageLocked()
	m.mu.Unlock()
	if m.maxCPUs > 0 && (caps.MaxInstances <= 0 || caps.MaxInstances > m.maxCPUs) {
		caps.MaxInstances = m.maxCPUs
	}
	caps.MemoryCapacityMB, caps.MemoryReservedMB = m.maxMemoryMB, memory
	caps.CPUCapacity, caps.CPUReserved = m.maxCPUs, cpus
	return caps
}

type managerStatusSnapshot struct {
	id                      string
	machine                 *Machine
	state                   client.InstanceState
	provider                networkIPv4Provider
	backingProvider         instanceBackingUsageProvider
	backingMetadataProvider instanceBackingMetadataUsageProvider
	backingCombinedProvider instanceBackingCombinedUsageProvider
	balloonProvider         instanceBalloonStateProvider
}

func (m *Manager) statusSnapshotLocked(id string) managerStatusSnapshot {
	id = instanceID(id)
	if m.running == nil || m.running[id] == nil {
		if m.closing {
			return managerStatusSnapshot{id: id, state: client.InstanceState{ID: id, Status: "stopped"}}
		}
		if m.starting != nil {
			if _, ok := m.starting[id]; ok {
				return managerStatusSnapshot{id: id, state: client.InstanceState{ID: id, Status: "starting"}}
			}
		}
		if state, ok := m.exited[id]; ok {
			return managerStatusSnapshot{id: id, state: state}
		}
		return managerStatusSnapshot{id: id, state: client.InstanceState{ID: id, Status: "stopped"}}
	}
	machine := m.running[id]
	state := client.InstanceState{
		ID:         id,
		Status:     "running",
		Image:      machine.image,
		InitSystem: machine.initSystem,
		Kernel:     machine.kernel,
		MemoryMB:   machine.memoryMB,
		BalloonMB:  machine.balloonMB,
		CPUs:       machine.cpus,
		NestedVirt: machine.nestedVirt,
		StartedAt:  machine.startedAt.Format(time.RFC3339Nano),
	}
	snapshot := managerStatusSnapshot{id: id, machine: machine, state: state}
	if machine.stopping {
		snapshot.state.Status = "stopping"
		return snapshot
	}
	if provider, ok := machine.instance.(networkIPv4Provider); ok {
		snapshot.provider = provider
	}
	if provider, ok := machine.instance.(instanceBackingUsageProvider); ok {
		snapshot.backingProvider = provider
	}
	if provider, ok := machine.instance.(instanceBackingMetadataUsageProvider); ok {
		snapshot.backingMetadataProvider = provider
	}
	if provider, ok := machine.instance.(instanceBackingCombinedUsageProvider); ok {
		snapshot.backingCombinedProvider = provider
	}
	if provider, ok := machine.instance.(instanceBalloonStateProvider); ok {
		snapshot.balloonProvider = provider
	} else {
		snapshot.state.BalloonMB = 0
		snapshot.state.BalloonStatus = "unsupported"
	}
	return snapshot
}

func (m *Manager) resolveStatusSnapshot(snapshot managerStatusSnapshot) client.InstanceState {
	if snapshot.provider == nil && snapshot.backingProvider == nil && snapshot.backingMetadataProvider == nil && snapshot.backingCombinedProvider == nil && snapshot.balloonProvider == nil {
		return snapshot.state
	}
	if snapshot.provider != nil {
		snapshot.state.NetworkIPv4 = snapshot.provider.NetworkIPv4()
	}
	if snapshot.backingProvider != nil {
		current, highWater, physical, err := snapshot.backingProvider.BackingUsage()
		snapshot.state.BackingDataBytes = current
		snapshot.machine.backingMu.Lock()
		snapshot.machine.backingDataHighWater = max(snapshot.machine.backingDataHighWater, current, highWater)
		snapshot.state.BackingDataHighWaterBytes = snapshot.machine.backingDataHighWater
		snapshot.machine.backingMu.Unlock()
		snapshot.state.BackingPhysicalBytes = physical
		if err != nil {
			snapshot.state.BackingReclaimError = err.Error()
		}
	}
	if snapshot.backingMetadataProvider != nil {
		current, highWater := snapshot.backingMetadataProvider.BackingMetadataUsage()
		snapshot.state.BackingMetadataBytes = current
		snapshot.machine.backingMu.Lock()
		snapshot.machine.backingMetadataHighWater = max(snapshot.machine.backingMetadataHighWater, current, highWater)
		snapshot.state.BackingMetadataHighWaterBytes = snapshot.machine.backingMetadataHighWater
		snapshot.machine.backingMu.Unlock()
	}
	if snapshot.backingProvider != nil || snapshot.backingMetadataProvider != nil {
		snapshot.state.BackingBytes = saturatingUint64Add(snapshot.state.BackingDataBytes, snapshot.state.BackingMetadataBytes)
		// Either component peak is a lower bound for the combined usage at
		// that instant. Taking their maximum is safe; adding them would invent
		// a state whose peaks may have occurred at different times.
		observedHighWater := max(snapshot.state.BackingBytes, snapshot.state.BackingDataHighWaterBytes, snapshot.state.BackingMetadataHighWaterBytes)
		snapshot.machine.backingMu.Lock()
		if observedHighWater > snapshot.machine.backingHighWater {
			snapshot.machine.backingHighWater = observedHighWater
		}
		snapshot.state.BackingHighWaterBytes = snapshot.machine.backingHighWater
		snapshot.machine.backingMu.Unlock()
	}
	if snapshot.backingCombinedProvider != nil {
		current, highWater := snapshot.backingCombinedProvider.BackingCombinedUsage()
		snapshot.state.BackingBytes = current
		snapshot.machine.backingMu.Lock()
		snapshot.machine.backingHighWater = max(snapshot.machine.backingHighWater, current, highWater)
		snapshot.state.BackingHighWaterBytes = snapshot.machine.backingHighWater
		snapshot.machine.backingMu.Unlock()
	}
	if snapshot.balloonProvider != nil {
		target, actual, ready, supported := snapshot.balloonProvider.BalloonState()
		if !supported {
			snapshot.state.BalloonMB = 0
			snapshot.state.BalloonStatus = "unsupported"
		} else {
			snapshot.state.BalloonMB = target
			snapshot.state.BalloonActualMB = actual
			switch {
			case !ready:
				snapshot.state.BalloonStatus = "driver_unavailable"
			case target == actual:
				snapshot.state.BalloonStatus = "converged"
			case actual < target:
				snapshot.state.BalloonStatus = "inflating"
			default:
				snapshot.state.BalloonStatus = "deflating"
			}
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.running == nil || m.running[snapshot.id] != snapshot.machine {
		return m.statusSnapshotLocked(snapshot.id).state
	}
	return snapshot.state
}

func saturatingUint64Add(a, b uint64) uint64 {
	if ^uint64(0)-a < b {
		return ^uint64(0)
	}
	return a + b
}

func (m *Manager) watch(machine *Machine) {
	err := machine.instance.Wait()

	m.mu.Lock()
	defer m.mu.Unlock()

	// An explicit stop owns exit publication. Close implementations commonly
	// cancel their run context, so Wait may report context.Canceled even though
	// the requested shutdown completed successfully. A failed Close keeps the
	// machine registered so cleanup can be retried.
	if machine.stopping || machine.stop != nil && machine.stop.err != nil {
		return
	}
	if m.running != nil && m.running[machine.id] == machine {
		delete(m.running, machine.id)
	}
	delete(m.networkLeases, machine.id)
	m.recordExitLocked(machine, err)
}

func (m *Manager) recordExitLocked(machine *Machine, err error) {
	machine.lastErr = err
	machine.exitedAt = time.Now().UTC()
	if m.exited == nil {
		m.exited = make(map[string]client.InstanceState)
	}
	state := client.InstanceState{ID: machine.id, Status: "stopped", Image: machine.image, InitSystem: machine.initSystem, Kernel: machine.kernel, MemoryMB: machine.memoryMB, BalloonMB: machine.balloonMB, CPUs: machine.cpus, NestedVirt: machine.nestedVirt, StartedAt: machine.startedAt.Format(time.RFC3339), ExitedAt: machine.exitedAt.Format(time.RFC3339)}
	if err != nil {
		state.Status = "crashed"
		state.Error = boundedLifecycleDiagnostic(err.Error())
		state.ExitReason = "VM backend exited unexpectedly"
	} else {
		state.ExitReason = "clean shutdown"
	}
	m.exited[machine.id] = state
	for len(m.exited) > maxExitTombstones {
		var oldestID string
		var oldest time.Time
		for id, candidate := range m.exited {
			at, _ := time.Parse(time.RFC3339, candidate.ExitedAt)
			if oldestID == "" || at.Before(oldest) {
				oldestID, oldest = id, at
			}
		}
		delete(m.exited, oldestID)
	}
}

func boundedLifecycleDiagnostic(diagnostic string) string {
	diagnostic = strings.TrimSpace(diagnostic)
	if len(diagnostic) <= maxLifecycleDiagnosticBytes {
		return diagnostic
	}
	const headBytes = 512
	omitted := len(diagnostic) - maxLifecycleDiagnosticBytes
	marker := fmt.Sprintf("\n[... %d diagnostic bytes omitted ...]\n", omitted)
	tailBytes := maxLifecycleDiagnosticBytes - headBytes - len(marker)
	if tailBytes < 0 {
		tailBytes = 0
	}
	return diagnostic[:headBytes] + marker + diagnostic[len(diagnostic)-tailBytes:]
}

func (m *Manager) checkCapacityLocked(maxVMs int, memoryMB uint64, cpus int) error {
	if maxVMs > 0 && len(m.running)+len(m.starting) >= maxVMs {
		return fmt.Errorf("maximum running VM instances reached: %d", maxVMs)
	}
	usedMemory, usedCPUs := m.resourceUsageLocked()
	if m.maxMemoryMB > 0 && (memoryMB > m.maxMemoryMB || usedMemory > m.maxMemoryMB-memoryMB) {
		return fmt.Errorf("VM memory admission rejected: requested=%d MiB reserved=%d MiB capacity=%d MiB", memoryMB, usedMemory, m.maxMemoryMB)
	}
	if m.maxCPUs > 0 && (cpus > m.maxCPUs || usedCPUs > m.maxCPUs-cpus) {
		return fmt.Errorf("VM CPU admission rejected: requested=%d reserved=%d capacity=%d", cpus, usedCPUs, m.maxCPUs)
	}
	return nil
}

func (m *Manager) resourceUsageLocked() (uint64, int) {
	var memory uint64
	var cpus int
	for _, machine := range m.running {
		memory += machine.memoryMB
		cpus += machine.cpus
	}
	for _, reservation := range m.reservations {
		memory += reservation.memoryMB
		cpus += reservation.cpus
	}
	return memory, cpus
}

func (m *Manager) SetInstanceBalloon(id string, targetMB uint64) error {
	id = strings.TrimSpace(id)
	m.mu.Lock()
	machine := m.running[id]
	if machine == nil {
		m.mu.Unlock()
		return fmt.Errorf("VM %q is not running", id)
	}
	if targetMB > machine.memoryMB {
		m.mu.Unlock()
		return fmt.Errorf("balloon target %d MiB exceeds VM memory %d MiB", targetMB, machine.memoryMB)
	}
	if machine.stopping {
		m.mu.Unlock()
		return fmt.Errorf("VM %q is stopping", id)
	}
	controller, ok := machine.instance.(instanceBalloonController)
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("VM %q does not support dynamic ballooning", id)
	}
	machine.balloonMu.Lock()
	m.mu.Lock()
	if m.running[id] != machine || machine.stopping {
		m.mu.Unlock()
		machine.balloonMu.Unlock()
		return fmt.Errorf("VM %q is stopping", id)
	}
	m.mu.Unlock()
	err := controller.SetBalloonMB(targetMB)
	machine.balloonMu.Unlock()
	if err != nil {
		return fmt.Errorf("set VM %q balloon target: %w", id, err)
	}
	m.mu.Lock()
	if current := m.running[id]; current == machine {
		current.balloonMB = targetMB
	}
	m.mu.Unlock()
	return nil
}

func normalizeResources(memoryMB, balloonMB *uint64, cpus *int) error {
	if *memoryMB == 0 {
		*memoryMB = 512
	}
	if *cpus == 0 {
		*cpus = 1
	}
	if *cpus < 0 {
		return fmt.Errorf("cpus must be positive")
	}
	if *memoryMB < minimumGuestMemoryMB {
		return fmt.Errorf("memory_mb must be at least %d MiB", minimumGuestMemoryMB)
	}
	maxAllocationMB := uint64(^uint(0)>>1) >> 20
	if *memoryMB > maxAllocationMB {
		return fmt.Errorf("memory_mb %d overflows host allocation size", *memoryMB)
	}
	if *balloonMB > *memoryMB {
		return fmt.Errorf("balloon_mb %d exceeds memory_mb %d", *balloonMB, *memoryMB)
	}
	return nil
}

func (m *Manager) finishStart(id string, start *managerStart) {
	m.mu.Lock()
	if m.starting[id] == start {
		delete(m.starting, id)
	}
	delete(m.reservations, id)
	delete(m.networkLeases, id)
	m.mu.Unlock()
	start.cancel()
	close(start.done)
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
