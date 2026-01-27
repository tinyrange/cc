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
func NewOCIClient() (OCIClient, error) {
	client, err := oci.NewClient("")
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
		image: image,
		cfs:   cfs,
		arch:  cfg.arch,
	}, nil
}
