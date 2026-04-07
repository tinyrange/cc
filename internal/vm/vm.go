package vm

import (
	"context"
	"fmt"
	"sync"
	"time"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/hv"
)

type Manager struct {
	mu      sync.Mutex
	running *Machine
}

type Machine struct {
	image     string
	command   []string
	startedAt time.Time
}

func NewManager() *Manager {
	return &Manager{}
}

func Supports() error {
	return hv.Supports()
}

func (m *Manager) Start(ctx context.Context, req client.StartVMRequest) (client.VMState, error) {
	_ = ctx

	if req.Image == "" {
		return client.VMState{}, fmt.Errorf("image is required")
	}
	if err := Supports(); err != nil {
		return client.VMState{}, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running != nil {
		return m.statusLocked(), fmt.Errorf("a VM is already running")
	}

	m.running = &Machine{
		image:     req.Image,
		command:   append([]string(nil), req.Command...),
		startedAt: time.Now().UTC(),
	}
	return m.statusLocked(), nil
}

func (m *Manager) Shutdown(ctx context.Context) error {
	_ = ctx

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running == nil {
		return fmt.Errorf("no VM is running")
	}
	m.running = nil
	return nil
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
