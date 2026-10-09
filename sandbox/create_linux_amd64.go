//go:build linux && amd64

package sandbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"j5.nz/cc/client"
	"j5.nz/cc/internal/kernel/alpine"
	"j5.nz/cc/internal/oci"
	"j5.nz/cc/internal/vm"
	"path"
	"path/filepath"
	"strings"
)

// Runtime shares only immutable image/kernel preparation caches across VMs.
// Each VM gets a separate backend and network switch; guest root writes use
// cc's per-instance copy-on-write storage, not the image cache or live shares.
type Runtime struct {
	root    string
	prepare chan struct{}
	images  *oci.Store
	kernel  *alpine.Manager
}

func NewRuntime(cacheDirectory string) (*Runtime, error) {
	if !filepath.IsAbs(cacheDirectory) {
		return nil, errors.New("sandbox cache directory must be absolute")
	}
	if err := vm.Supports(); err != nil {
		return nil, err
	}
	return &Runtime{root: cacheDirectory, prepare: make(chan struct{}, 1), images: oci.NewStore(filepath.Join(cacheDirectory, "images")), kernel: alpine.NewManager(filepath.Join(cacheDirectory, "kernel"))}, nil
}
func validateConfig(cfg Config) error {
	if err := validateInit(cfg.Init, cfg.SandboxProtocol); err != nil {
		return err
	}
	parts := strings.Split(cfg.Image, "@sha256:")
	if len(parts) != 2 || parts[0] == "" || len(parts[1]) != 64 {
		return errors.New("sandbox requires an immutable OCI image reference")
	}
	if _, err := hex.DecodeString(parts[1]); err != nil {
		return errors.New("invalid OCI digest")
	}
	source, err := oci.ParseSource(cfg.Image)
	if err != nil || source.Kind != oci.SourceKindOCI {
		return errors.New("sandbox accepts only OCI registry references")
	}
	if cfg.Platform != "linux/amd64" {
		return errors.New("embedded sandbox currently requires linux/amd64")
	}
	if cfg.ID == "" || len(cfg.ID) > 128 || cfg.MemoryMB < 128 || cfg.MemoryMB > 1048576 || cfg.CPUs < 1 || cfg.CPUs > 256 {
		return errors.New("invalid sandbox identity or resources")
	}
	for n, m := range cfg.Mounts {
		if m.Backend == nil || !path.IsAbs(m.GuestPath) || path.Clean(m.GuestPath) != m.GuestPath || m.GuestPath == "/" || strings.ContainsRune(m.GuestPath, 0) {
			return errors.New("invalid sandbox filesystem mount")
		}
		for _, other := range cfg.Mounts[:n] {
			if m.GuestPath == other.GuestPath || strings.HasPrefix(m.GuestPath, other.GuestPath+"/") || strings.HasPrefix(other.GuestPath, m.GuestPath+"/") {
				return errors.New("overlapping sandbox mounts")
			}
		}
	}
	return nil
}
func (r *Runtime) Create(ctx context.Context, cfg Config, progress func(Progress)) (*Sandbox, error) {
	if err := validateConfig(cfg); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	report := func(p Progress) {
		if progress != nil {
			progress(p)
		}
	}
	name := fmt.Sprintf("sandbox-%x", sha256.Sum256([]byte(cfg.Platform+"\n"+cfg.Image)))
	// cc image activation is serialized, so simultaneous boots of one image do
	// not get a transient "download already in progress" error.
	select {
	case r.prepare <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	state, err := r.images.Pull(ctx, name, cfg.Image, oci.PullOptions{Architecture: "amd64", Report: func(event client.ProgressEvent) { report(Progress{Stage: "image", Image: &event}) }})
	if err == nil {
		report(Progress{Stage: "kernel", Message: "Preparing Linux kernel"})
		err = r.kernel.Ensure(ctx)
	}
	<-r.prepare
	if err != nil {
		return nil, err
	}
	image, err := r.images.Open(name)
	if err != nil {
		return nil, err
	}
	if image.Architecture != "amd64" {
		return nil, errors.New("resolved image architecture mismatch")
	}
	parts := strings.Split(state.ResolvedSource, "@sha256:")
	if len(parts) != 2 || len(parts[1]) != 64 {
		return nil, errors.New("OCI store did not record a resolved manifest digest")
	}
	if _, err := hex.DecodeString(parts[1]); err != nil {
		return nil, errors.New("OCI store returned invalid manifest digest")
	}
	identity := Identity{Reference: state.ResolvedSource, Digest: "sha256:" + parts[1], Platform: "linux/amd64"}
	report(Progress{Stage: "boot", Message: "Starting Linux VM"})
	backend := vm.NewRuntimeBackend(r.kernel, r.images, filepath.Join(r.root, "guestinit"))
	ctx = vm.WithSandboxInit(ctx, cfg.Init, cfg.SandboxProtocol)
	inst, err := vm.StartSandbox(ctx, backend, client.CreateInstanceRequest{ID: cfg.ID, Image: name, DefaultUser: "root", MemoryMB: cfg.MemoryMB, CPUs: cfg.CPUs, Network: &client.NetworkConfig{Enabled: true, AllowInternet: cfg.Internet, BlockHostAccess: true}}, cfg.Mounts, func(event client.BootEvent) error {
		report(Progress{Stage: "boot", Message: event.Message})
		return nil
	})
	if err != nil {
		return nil, err
	}
	s := &Sandbox{instance: inst, identity: identity}
	if cfg.SandboxProtocol != 0 {
		if err := verifyProtocol(ctx, s, cfg.SandboxProtocol); err != nil {
			closeErr := s.Close()
			if closeErr != nil {
				return s, errors.Join(err, closeErr)
			}
			return nil, err
		}
	}
	return s, nil
}
