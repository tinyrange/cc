package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// VMSourceType identifies how the VM was loaded
type VMSourceType string

const (
	VMSourceBundle    VMSourceType = "bundle" // Bundle directory with ccbundle.yaml
	VMSourceTarball   VMSourceType = "tarball" // OCI tarball (.tar file)
	VMSourceImageName VMSourceType = "image"   // Container image name to pull
)

// RecentVM stores metadata about a recently launched VM
type RecentVM struct {
	Name           string       `json:"name"`
	SourceType     VMSourceType `json:"source_type"`
	SourcePath     string       `json:"source_path"`
	NetworkEnabled bool         `json:"network_enabled"`
	LastUsed       time.Time    `json:"last_used"`
}

// RecentVMsStore manages persistent storage of recent VMs
type RecentVMsStore struct {
	path    string
	entries []RecentVM
}

const maxRecentVMs = 5

// NewRecentVMsStore creates or loads the recent VMs store
func NewRecentVMsStore() (*RecentVMsStore, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}

	storePath := filepath.Join(configDir, "ccapp", "recent_vms.json")
	store := &RecentVMsStore{path: storePath}

	if err := store.load(); err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	return store, nil
}

func (s *RecentVMsStore) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &s.entries)
}

func (s *RecentVMsStore) save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o644)
}

// AddOrUpdate adds a new entry or updates an existing one.
// Note: in-memory state is updated before save. If save fails, memory and disk
// will diverge until the next successful save. This is acceptable for an MRU cache.
func (s *RecentVMsStore) AddOrUpdate(vm RecentVM) error {
	vm.LastUsed = time.Now()

	// Check if already exists (by source path and type)
	for i, existing := range s.entries {
		if existing.SourceType == vm.SourceType && existing.SourcePath == vm.SourcePath {
			s.entries[i] = vm
			s.sortAndTruncate()
			return s.save()
		}
	}

	// Add new entry
	s.entries = append(s.entries, vm)
	s.sortAndTruncate()
	return s.save()
}

func (s *RecentVMsStore) sortAndTruncate() {
	// Sort by LastUsed descending (most recent first)
	sort.Slice(s.entries, func(i, j int) bool {
		return s.entries[i].LastUsed.After(s.entries[j].LastUsed)
	})

	// Keep only top N
	if len(s.entries) > maxRecentVMs {
		s.entries = s.entries[:maxRecentVMs]
	}
}

// GetRecent returns the recent VMs (most recent first)
func (s *RecentVMsStore) GetRecent() []RecentVM {
	return s.entries
}
