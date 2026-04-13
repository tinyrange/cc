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
	Start(context.Context, client.StartVMRequest) (Instance, error)
	Run(context.Context, client.StartVMRequest) (client.RunVMResponse, error)
}

type Instance interface {
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
	command    []string
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

func (m *Manager) Start(ctx context.Context, req client.StartVMRequest) (client.VMState, error) {
	if req.Image == "" {
		return client.VMState{}, fmt.Errorf("image is required")
	}
	if err := m.supports(); err != nil {
		return client.VMState{}, err
	}

	m.mu.Lock()
	if m.running != nil {
		state := m.statusLocked()
		m.mu.Unlock()
		return state, fmt.Errorf("a VM is already running")
	}
	m.mu.Unlock()

	inst, err := m.backend.Start(ctx, req)
	if err != nil {
		return client.VMState{}, err
	}

	machine := &Machine{
		image:      req.Image,
		command:    append([]string(nil), req.Command...),
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

func (m *Manager) Run(ctx context.Context, req client.StartVMRequest) (client.RunVMResponse, error) {
	if req.Image == "" {
		return client.RunVMResponse{}, fmt.Errorf("image is required")
	}
	if err := m.supports(); err != nil {
		return client.RunVMResponse{}, err
	}
	return m.backend.Run(ctx, req)
}

func (m *Manager) Status() client.VMState {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.statusLocked()
}

func (m *Manager) statusLocked() client.VMState {
	if m.running == nil {
		return client.VMState{Status: "stopped"}
	}
	return client.VMState{
		Status:    "running",
		Image:     m.running.image,
		Command:   append([]string(nil), m.running.command...),
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

func (unsupportedBackend) Start(ctx context.Context, req client.StartVMRequest) (Instance, error) {
	_ = ctx
	_ = req
	return nil, fmt.Errorf("VM backend is not configured")
}

func (unsupportedBackend) Run(ctx context.Context, req client.StartVMRequest) (client.RunVMResponse, error) {
	_ = ctx
	_ = req
	return client.RunVMResponse{}, fmt.Errorf("VM backend is not configured")
}
