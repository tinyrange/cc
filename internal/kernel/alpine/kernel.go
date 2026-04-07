package alpine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"j5.nz/cc/client"
)

const (
	defaultVersion = "alpine-edge"
	defaultSource  = "alpine:edge"
)

type Manager struct {
	root string

	mu          sync.Mutex
	downloading bool
	lastErr     error
}

type metadata struct {
	Version string `json:"version"`
	Source  string `json:"source"`
}

func NewManager(root string) *Manager {
	return &Manager{root: root}
}

func (m *Manager) Status() client.KernelState {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.statusLocked()
}

func (m *Manager) Ensure(ctx context.Context) error {
	_ = ctx

	m.mu.Lock()
	if m.downloading {
		m.mu.Unlock()
		return fmt.Errorf("kernel download already in progress")
	}
	m.downloading = true
	m.lastErr = nil
	m.mu.Unlock()

	err := m.ensureMetadata()

	m.mu.Lock()
	m.downloading = false
	m.lastErr = err
	m.mu.Unlock()

	return err
}

func (m *Manager) statusLocked() client.KernelState {
	if m.downloading {
		return client.KernelState{
			Status: "downloading",
			Source: defaultSource,
		}
	}

	meta, err := m.readMetadata()
	if err == nil {
		return client.KernelState{
			Status:  "downloaded",
			Version: meta.Version,
			Source:  meta.Source,
		}
	}
	if m.lastErr != nil {
		return client.KernelState{
			Status: "error",
			Error:  m.lastErr.Error(),
			Source: defaultSource,
		}
	}
	return client.KernelState{
		Status: "missing",
		Source: defaultSource,
	}
}

func (m *Manager) ensureMetadata() error {
	if err := os.MkdirAll(m.root, 0o755); err != nil {
		return fmt.Errorf("create kernel cache dir: %w", err)
	}

	meta := metadata{
		Version: defaultVersion + "-" + runtime.GOARCH,
		Source:  defaultSource,
	}

	buf, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal kernel metadata: %w", err)
	}
	if err := os.WriteFile(m.metadataPath(), buf, 0o644); err != nil {
		return fmt.Errorf("write kernel metadata: %w", err)
	}
	return nil
}

func (m *Manager) readMetadata() (metadata, error) {
	var ret metadata
	buf, err := os.ReadFile(m.metadataPath())
	if err != nil {
		return ret, err
	}
	if err := json.Unmarshal(buf, &ret); err != nil {
		return ret, fmt.Errorf("decode kernel metadata: %w", err)
	}
	if ret.Version == "" || ret.Source == "" {
		return ret, errors.New("kernel metadata is incomplete")
	}
	return ret, nil
}

func (m *Manager) metadataPath() string {
	return filepath.Join(m.root, "kernel.json")
}
