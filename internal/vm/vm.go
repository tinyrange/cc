package vm

import (
	"context"
	"fmt"
	"sync"
	"time"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/hv"
)

type Backend interface {
	Start(context.Context, client.CreateInstanceRequest) (Instance, error)
	StartStream(context.Context, client.CreateInstanceRequest, func(client.BootEvent) error) (Instance, error)
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
	mu       sync.Mutex
	backend  Backend
	supports func() error
	running  *Machine
}

type Machine struct {
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
	return &Manager{backend: unsupportedBackend{}, supports: Supports}
}

func NewManagerWithBackend(backend Backend) *Manager {
	return &Manager{backend: backend, supports: Supports}
}

func Supports() error {
	return hv.Supports()
}

func (m *Manager) Start(ctx context.Context, req client.CreateInstanceRequest) (client.InstanceState, error) {
	return m.StartStream(ctx, req, nil)
}

func (m *Manager) StartStream(ctx context.Context, req client.CreateInstanceRequest, onEvent func(client.BootEvent) error) (client.InstanceState, error) {
	if req.Image == "" {
		return client.InstanceState{}, fmt.Errorf("image is required")
	}
	if err := m.supports(); err != nil {
		return client.InstanceState{}, err
	}

	m.mu.Lock()
	if m.running != nil {
		state := m.statusLocked()
		m.mu.Unlock()
		return state, fmt.Errorf("a VM is already running")
	}
	m.mu.Unlock()

	inst, err := m.backend.StartStream(ctx, req, onEvent)
	if err != nil {
		return client.InstanceState{}, err
	}

	machine := &Machine{
		image:      req.Image,
		memoryMB:   req.MemoryMB,
		cpus:       req.CPUs,
		startedAt:  time.Now().UTC(),
		instance:   inst,
		shutdownCh: make(chan struct{}),
	}

	m.mu.Lock()
	m.running = machine
	m.mu.Unlock()

	go m.watch(machine)

	return m.Status(), nil
}

func (m *Manager) Shutdown(ctx context.Context) error {
	_ = ctx

	m.mu.Lock()
	machine := m.running
	if machine == nil {
		m.mu.Unlock()
		return fmt.Errorf("no VM is running")
	}
	m.running = nil
	close(machine.shutdownCh)
	m.mu.Unlock()

	return machine.instance.Close()
}

func (m *Manager) Run(ctx context.Context, req client.RunRequest) (client.ExecResponse, error) {
	m.mu.Lock()
	machine := m.running
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
	m.mu.Lock()
	machine := m.running
	m.mu.Unlock()
	if machine == nil {
		return fmt.Errorf("no VM is running")
	}
	return machine.instance.ExecStream(ctx, req, inputs, onEvent)
}

func (m *Manager) Status() client.InstanceState {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.statusLocked()
}

func (m *Manager) statusLocked() client.InstanceState {
	if m.running == nil {
		return client.InstanceState{Status: "stopped"}
	}
	return client.InstanceState{
		Status:    "running",
		Image:     m.running.image,
		MemoryMB:  m.running.memoryMB,
		CPUs:      m.running.cpus,
		StartedAt: m.running.startedAt.Format(time.RFC3339),
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

	if m.running == machine {
		m.running = nil
	}
	machine.lastErr = err
	machine.exitedAt = time.Now().UTC()
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
