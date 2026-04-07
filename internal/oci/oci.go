package oci

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"j5.nz/cc/client"
)

type Store struct {
	root string

	mu          sync.Mutex
	downloading map[string]bool
	lastErr     map[string]error
}

type metadata struct {
	Name   string `json:"name"`
	Source string `json:"source"`
}

func NewStore(root string) *Store {
	return &Store{
		root:        root,
		downloading: map[string]bool{},
		lastErr:     map[string]error{},
	}
}

func (s *Store) List() ([]client.ImageState, error) {
	if err := os.MkdirAll(s.root, 0o755); err != nil {
		return nil, fmt.Errorf("create image store: %w", err)
	}

	entries, err := os.ReadDir(s.root)
	if err != nil {
		return nil, fmt.Errorf("read image store: %w", err)
	}

	ret := make([]client.ImageState, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		state, err := s.Get(entry.Name())
		if err != nil {
			continue
		}
		ret = append(ret, state)
	}

	sort.Slice(ret, func(i, j int) bool {
		return ret[i].Name < ret[j].Name
	})

	return ret, nil
}

func (s *Store) Get(name string) (client.ImageState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getLocked(name)
}

func (s *Store) Pull(ctx context.Context, name, source string) (client.ImageState, error) {
	_ = ctx

	if name == "" {
		return client.ImageState{}, fmt.Errorf("image name is required")
	}
	if source == "" {
		return client.ImageState{}, fmt.Errorf("image source is required")
	}

	s.mu.Lock()
	if s.downloading[name] {
		s.mu.Unlock()
		return client.ImageState{}, fmt.Errorf("image %q download already in progress", name)
	}
	s.downloading[name] = true
	delete(s.lastErr, name)
	s.mu.Unlock()

	err := s.writeMetadata(name, metadata{
		Name:   name,
		Source: source,
	})

	s.mu.Lock()
	delete(s.downloading, name)
	s.lastErr[name] = err
	state, stateErr := s.getLocked(name)
	s.mu.Unlock()

	if err != nil {
		return client.ImageState{}, err
	}
	return state, stateErr
}

func (s *Store) getLocked(name string) (client.ImageState, error) {
	if s.downloading[name] {
		meta, err := s.readMetadata(name)
		if err == nil {
			return client.ImageState{Name: name, Source: meta.Source, Status: "downloading"}, nil
		}
		return client.ImageState{Name: name, Status: "downloading"}, nil
	}

	meta, err := s.readMetadata(name)
	if err == nil {
		return client.ImageState{
			Name:   meta.Name,
			Source: meta.Source,
			Status: "downloaded",
		}, nil
	}
	if lastErr := s.lastErr[name]; lastErr != nil {
		return client.ImageState{
			Name:   name,
			Status: "error",
			Error:  lastErr.Error(),
		}, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return client.ImageState{}, fmt.Errorf("image %q not found", name)
	}
	return client.ImageState{}, err
}

func (s *Store) writeMetadata(name string, meta metadata) error {
	dir := s.imageDir(name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create image dir: %w", err)
	}
	buf, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal image metadata: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "image.json"), buf, 0o644); err != nil {
		return fmt.Errorf("write image metadata: %w", err)
	}
	return nil
}

func (s *Store) readMetadata(name string) (metadata, error) {
	var ret metadata
	buf, err := os.ReadFile(filepath.Join(s.imageDir(name), "image.json"))
	if err != nil {
		return ret, err
	}
	if err := json.Unmarshal(buf, &ret); err != nil {
		return ret, fmt.Errorf("decode image metadata: %w", err)
	}
	if ret.Name == "" {
		ret.Name = name
	}
	if ret.Source == "" {
		return ret, errors.New("image metadata missing source")
	}
	return ret, nil
}

func (s *Store) imageDir(name string) string {
	return filepath.Join(s.root, name)
}
