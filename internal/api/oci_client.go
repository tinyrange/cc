package api

import (
	"context"
	"fmt"

	"github.com/tinyrange/cc/internal/oci"
)

// ociClient implements OCIClient.
type ociClient struct {
	client *oci.Client
}

// NewOCIClient creates a new OCI client for pulling images.
// Uses the default cache directory (platform-specific user config directory).
func NewOCIClient() (OCIClient, error) {
	cache, err := NewCacheDir("")
	if err != nil {
		return nil, err
	}
	return NewOCIClientWithCache(cache)
}

// NewOCIClientWithCache creates a new OCI client using the provided CacheDir.
// This ensures the OCI client uses the same cache location as other components.
func NewOCIClientWithCache(cache CacheDir) (OCIClient, error) {
	client, err := oci.NewClient(cache.OCIPath())
	if err != nil {
		return nil, fmt.Errorf("create OCI client: %w", err)
	}
	return &ociClient{client: client}, nil
}

// Pull fetches an image and prepares it for use with New.
func (c *ociClient) Pull(ctx context.Context, imageRef string, opts ...OCIPullOption) (InstanceSource, error) {
	cfg := parsePullOptions(opts)

	// TODO: Handle authentication (cfg.username, cfg.password)
	// The internal oci.Client doesn't currently support authentication directly,
	// but we can extend it in the future.

	// TODO: Handle pull policy (cfg.policy)
	// For now, we always pull if not in cache (the default behavior of PullForArch).

	image, err := c.client.PullForArch(imageRef, cfg.arch)
	if err != nil {
		return nil, fmt.Errorf("pull image %q: %w", imageRef, err)
	}

	cfs, err := oci.NewContainerFS(image)
	if err != nil {
		return nil, fmt.Errorf("create container filesystem: %w", err)
	}

	return &ociSource{
		image:    image,
		cfs:      cfs,
		arch:     cfg.arch,
		imageRef: imageRef,
	}, nil
}

// LoadFromDir loads a prebaked image from a directory.
func (c *ociClient) LoadFromDir(dir string, opts ...OCIPullOption) (InstanceSource, error) {
	cfg := parsePullOptions(opts)

	image, err := oci.LoadFromDir(dir)
	if err != nil {
		return nil, fmt.Errorf("load image from %q: %w", dir, err)
	}

	cfs, err := oci.NewContainerFS(image)
	if err != nil {
		return nil, fmt.Errorf("create container filesystem: %w", err)
	}

	return &ociSource{
		image:    image,
		cfs:      cfs,
		arch:     cfg.arch,
		imageRef: dir,
	}, nil
}

// ExportToDir exports an InstanceSource to a directory.
func (c *ociClient) ExportToDir(source InstanceSource, dir string) error {
	src, ok := source.(*ociSource)
	if !ok {
		return fmt.Errorf("source type %T does not support export", source)
	}
	return oci.ExportToDir(src.image, dir)
}

// CacheDir returns the cache directory used by this client.
func (c *ociClient) CacheDir() string {
	return c.client.CacheDir()
}
