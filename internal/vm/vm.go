package vm

import (
	"context"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/hv"
)

const DefaultInstanceID = "default"

type Backend interface {
	Start(context.Context, client.CreateInstanceRequest) (Instance, error)
	StartStream(context.Context, client.CreateInstanceRequest, func(client.BootEvent) error) (Instance, error)
	StartBlank(context.Context, client.StartInstanceRequest) (Instance, error)
	StartBlankStream(context.Context, client.StartInstanceRequest, func(client.BootEvent) error) (Instance, error)
	Run(context.Context, client.RunRequest) (client.ExecResponse, error)
	RunInInstance(context.Context, Instance, string, client.RunRequest) (client.ExecResponse, error)
}

type Instance interface {
	AddShare(context.Context, client.ShareMount) error
	Exec(context.Context, client.ExecRequest) (client.ExecResponse, error)
	ExecStream(context.Context, client.ExecRequest, <-chan client.ExecInput, func(client.ExecEvent) error) error
	Wait() error
	Close() error
}

type Manager struct {
	mu           sync.Mutex
	backend      Backend
	supports     func() error
	capabilities func() client.CapabilitiesResponse
	running      map[string]*Machine
}

type Machine struct {
	id         string
	image      string
	memoryMB   uint64
	cpus       int
	startedAt  time.Time
	instance   Instance
	lastErr    error
	exitedAt   time.Time
	shutdownCh chan struct{}
}

func NewManager() *Manager {
	return &Manager{backend: unsupportedBackend{}, supports: Supports, capabilities: HostCapabilities}
}

func NewManagerWithBackend(backend Backend) *Manager {
	return &Manager{backend: backend, supports: Supports, capabilities: HostCapabilities}
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
		NetworkModes:           []string{},
		ShareConsistency:       []string{"host-backed"},
		ResourceLimits:         []string{"memory_mb", "cpus"},
		SupportsMultiImageExec: true,
		RequiresPrivilegedCCX3: false,
	}
	if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
		caps.MaxInstances = 1
		caps.Notes = append(caps.Notes, "macOS HVF currently limits ccx3 to one running instance")
	}
	if err := Supports(); err != nil {
		caps.VMSupported = false
		caps.VMError = err.Error()
	} else {
		caps.VMSupported = true
	}
	return caps
}

func backendName() string {
	switch {
	case runtime.GOOS == "linux" && (runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64"):
		return "kvm"
	case runtime.GOOS == "darwin" && runtime.GOARCH == "arm64":
		return "hvf"
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
	if m.running[id] != nil {
		state := m.statusLocked(id)
		m.mu.Unlock()
		return state, fmt.Errorf("VM %q is already running", id)
	}
	if err := m.checkCapacityLocked(); err != nil {
		m.mu.Unlock()
		return client.InstanceState{}, err
	}
	m.mu.Unlock()

	inst, err := m.backend.StartStream(ctx, req, onEvent)
	if err != nil {
		return client.InstanceState{}, err
	}

	machine := &Machine{
		id:         id,
		image:      req.Image,
		memoryMB:   req.MemoryMB,
		cpus:       req.CPUs,
		startedAt:  time.Now().UTC(),
		instance:   inst,
		shutdownCh: make(chan struct{}),
	}

	m.mu.Lock()
	if m.running == nil {
		m.running = make(map[string]*Machine)
	}
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
	if err := m.supports(); err != nil {
		return client.InstanceState{}, err
	}

	m.mu.Lock()
	if m.running == nil {
		m.running = make(map[string]*Machine)
	}
	if m.running[id] != nil {
		state := m.statusLocked(id)
		m.mu.Unlock()
		return state, fmt.Errorf("VM %q is already running", id)
	}
	if err := m.checkCapacityLocked(); err != nil {
		m.mu.Unlock()
		return client.InstanceState{}, err
	}
	m.mu.Unlock()

	inst, err := m.backend.StartBlankStream(ctx, req, onEvent)
	if err != nil {
		return client.InstanceState{}, err
	}

	machine := &Machine{
		id:         id,
		image:      "",
		memoryMB:   req.MemoryMB,
		cpus:       req.CPUs,
		startedAt:  time.Now().UTC(),
		instance:   inst,
		shutdownCh: make(chan struct{}),
	}

	m.mu.Lock()
	if m.running == nil {
		m.running = make(map[string]*Machine)
	}
	m.running[id] = machine
	m.mu.Unlock()

	go m.watch(machine)

	return m.StatusOf(id), nil
}

func (m *Manager) Shutdown(ctx context.Context) error {
	return m.ShutdownInstance(ctx, DefaultInstanceID)
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
	delete(m.running, id)
	close(machine.shutdownCh)
	m.mu.Unlock()

	return machine.instance.Close()
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
		return m.backend.RunInInstance(ctx, machine.instance, machine.image, req)
	}
	if req.Image == "" {
		return client.ExecResponse{}, fmt.Errorf("image is required")
	}
	if err := m.supports(); err != nil {
		return client.ExecResponse{}, err
	}
	return m.backend.Run(ctx, req)
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
	m.mu.Unlock()
	if machine == nil {
		return fmt.Errorf("no VM %q is running", id)
	}
	targetImage := strings.TrimSpace(req.Image)
	if targetImage != "" && targetImage != machine.image {
		return fmt.Errorf("streaming /vm/run only supports the running image %q, got %q", machine.image, targetImage)
	}
	for _, share := range req.Shares {
		if err := machine.instance.AddShare(ctx, share); err != nil {
			return err
		}
	}
	return machine.instance.ExecStream(ctx, client.ExecRequest{
		Command:    append([]string(nil), req.Command...),
		Env:        append([]string(nil), req.Env...),
		RootDir:    req.RootDir,
		ReplaceEnv: req.ReplaceEnv,
		WorkDir:    req.WorkDir,
		User:       req.User,
		Stdin:      append([]byte(nil), req.Stdin...),
		TTY:        req.TTY,
		Cols:       req.Cols,
		Rows:       req.Rows,
	}, inputs, onEvent)
}

func (m *Manager) StreamIn(ctx context.Context, id string, req client.ExecRequest, inputs <-chan client.ExecInput, onEvent func(client.ExecEvent) error) error {
	id = instanceID(id)
	m.mu.Lock()
	machine := m.running[id]
	m.mu.Unlock()
	if machine == nil {
		return fmt.Errorf("no VM %q is running", id)
	}
	return machine.instance.ExecStream(ctx, req, inputs, onEvent)
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
		return client.InstanceState{ID: id, Status: "stopped"}
	}
	machine := m.running[id]
	return client.InstanceState{
		ID:        id,
		Status:    "running",
		Image:     machine.image,
		MemoryMB:  machine.memoryMB,
		CPUs:      machine.cpus,
		StartedAt: machine.startedAt.Format(time.RFC3339),
	}
}

func (m *Manager) watch(machine *Machine) {
	err := machine.instance.Wait()

	m.mu.Lock()
	defer m.mu.Unlock()

	select {
	case <-machine.shutdownCh:
		return
	default:
	}

	if m.running != nil && m.running[machine.id] == machine {
		delete(m.running, machine.id)
	}
	machine.lastErr = err
	machine.exitedAt = time.Now().UTC()
}

func (m *Manager) checkCapacityLocked() error {
	caps := m.Capabilities()
	if caps.MaxInstances > 0 && len(m.running) >= caps.MaxInstances {
		return fmt.Errorf("maximum running VM instances reached: %d", caps.MaxInstances)
	}
	return nil
}

func instanceID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return DefaultInstanceID
	}
	return id
}

type unsupportedBackend struct{}

func (unsupportedBackend) Start(ctx context.Context, req client.CreateInstanceRequest) (Instance, error) {
	return unsupportedBackend{}.StartStream(ctx, req, nil)
}

func (unsupportedBackend) StartStream(ctx context.Context, req client.CreateInstanceRequest, onEvent func(client.BootEvent) error) (Instance, error) {
	_ = ctx
	_ = req
	_ = onEvent
	return nil, fmt.Errorf("VM backend is not configured")
}

func (unsupportedBackend) StartBlank(ctx context.Context, req client.StartInstanceRequest) (Instance, error) {
	return unsupportedBackend{}.StartBlankStream(ctx, req, nil)
}

func (unsupportedBackend) StartBlankStream(ctx context.Context, req client.StartInstanceRequest, onEvent func(client.BootEvent) error) (Instance, error) {
	_ = ctx
	_ = req
	_ = onEvent
	return nil, fmt.Errorf("VM backend is not configured")
}

func (unsupportedBackend) Run(ctx context.Context, req client.RunRequest) (client.ExecResponse, error) {
	_ = ctx
	_ = req
	return client.ExecResponse{}, fmt.Errorf("VM backend is not configured")
}

func (unsupportedBackend) RunInInstance(ctx context.Context, inst Instance, runningImage string, req client.RunRequest) (client.ExecResponse, error) {
	_ = ctx
	_ = inst
	_ = runningImage
	_ = req
	return client.ExecResponse{}, fmt.Errorf("VM backend is not configured")
}
