package mounts

import (
	"context"
	"fmt"
	"path"
	"strings"
	"sync"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/oci"
	"j5.nz/cc/internal/virtio"
	"j5.nz/cc/internal/vmruntime"
)

type AlternateImageMounter interface {
	AddImage(context.Context, string, *oci.Image) error
}

type DelegatedShareMounter interface {
	AddShare(context.Context, client.ShareMount) error
}

type RuntimeShareAdder interface {
	AddShare(context.Context, client.ShareMount) error
}

type State struct {
	mu          sync.Mutex
	shares      map[string]client.ShareMount
	imageMounts map[string]string
}

func NewState(shares []client.ShareMount) State {
	tracked := make(map[string]client.ShareMount, len(shares))
	for _, share := range shares {
		if key := strings.TrimSpace(share.Mount); key != "" {
			tracked[key] = share
		}
	}
	return State{shares: tracked}
}

func (s *State) AddShare(rootFS virtio.ShareMounter, share client.ShareMount, unsupportedFeature string, build func(client.ShareMount) (virtio.ShareMount, error)) error {
	if s == nil {
		return AddRuntimeShareMount(rootFS, nil, nil, share, unsupportedFeature, build)
	}
	return AddRuntimeShareMount(rootFS, &s.mu, &s.shares, share, unsupportedFeature, build)
}

func (s *State) AddImage(rootFS virtio.ShareMounter, mountPath string, image *oci.Image, backend virtio.FSBackend) error {
	if s == nil {
		return AddImageMount(rootFS, nil, nil, mountPath, image, backend)
	}
	return AddImageMount(rootFS, &s.mu, &s.imageMounts, mountPath, image, backend)
}

func AddRuntimeShares(ctx context.Context, inst RuntimeShareAdder, shares []client.ShareMount) error {
	for _, share := range shares {
		if err := inst.AddShare(ctx, share); err != nil {
			return err
		}
	}
	return nil
}

func AddDelegatedRuntimeShare(ctx context.Context, delegate DelegatedShareMounter, share client.ShareMount, unsupportedFeature string) error {
	if delegate == nil {
		return AddRuntimeShareMount(nil, nil, nil, share, unsupportedFeature, nil)
	}
	return delegate.AddShare(ctx, share)
}

func AddDelegatedRuntimeImage(ctx context.Context, delegate AlternateImageMounter, mountPath string, image *oci.Image) error {
	if delegate == nil {
		return AddImageMount(nil, nil, nil, mountPath, image, nil)
	}
	return delegate.AddImage(ctx, mountPath, image)
}

func RebaseRuntimeShares(rootDir string, shares []client.ShareMount) []client.ShareMount {
	if rootDir == "" || len(shares) == 0 {
		return append([]client.ShareMount(nil), shares...)
	}
	out := make([]client.ShareMount, 0, len(shares))
	for _, share := range shares {
		rebased := share
		rebased.Mount = path.Join(rootDir, share.Mount)
		out = append(out, rebased)
	}
	return out
}

func ConvertShareMounts(shares []client.ShareMount) []vmruntime.DirectoryShare {
	if len(shares) == 0 {
		return nil
	}
	out := make([]vmruntime.DirectoryShare, 0, len(shares))
	for _, share := range shares {
		out = append(out, ShareMountToDirectoryShare(share))
	}
	return out
}

func ShareMountToDirectoryShare(share client.ShareMount) vmruntime.DirectoryShare {
	return vmruntime.DirectoryShare{
		Source:   share.Source,
		Mount:    share.Mount,
		Writable: share.Writable,
		MapOwner: share.MapOwner,
		OwnerUID: share.OwnerUID,
		OwnerGID: share.OwnerGID,
		Cache:    share.Cache,
	}
}

func BuildRuntimeDirectoryShare(share client.ShareMount, build func(int, vmruntime.DirectoryShare) (virtio.ShareMount, error)) (virtio.ShareMount, error) {
	if build == nil {
		return virtio.ShareMount{}, fmt.Errorf("share mount builder is not configured")
	}
	return build(0, ShareMountToDirectoryShare(share))
}

func MountAlternateImageWithShares(ctx context.Context, inst RuntimeShareAdder, mounter AlternateImageMounter, mountPath string, image *oci.Image, shares []client.ShareMount) error {
	if mounter == nil {
		return fmt.Errorf("running instance does not support image mounts")
	}
	if err := mounter.AddImage(ctx, mountPath, image); err != nil {
		return err
	}
	return AddRuntimeShares(ctx, inst, RebaseRuntimeShares(mountPath, shares))
}

func ImageFSBackend(image *oci.Image) virtio.FSBackend {
	if image == nil {
		return nil
	}
	return virtio.NewImageFS(image.RootFS, image.RootFSDir)
}

func AddRuntimeShareMount(rootFS virtio.ShareMounter, mu *sync.Mutex, shares *map[string]client.ShareMount, share client.ShareMount, unsupportedFeature string, build func(client.ShareMount) (virtio.ShareMount, error)) error {
	if rootFS == nil {
		if strings.TrimSpace(unsupportedFeature) == "" {
			unsupportedFeature = "shares"
		}
		return fmt.Errorf("instance rootfs does not support %s", unsupportedFeature)
	}
	key := strings.TrimSpace(share.Mount)
	if key == "" {
		return fmt.Errorf("share mount path is required")
	}
	if mu == nil {
		mu = &sync.Mutex{}
	}
	if shares == nil {
		local := map[string]client.ShareMount{}
		shares = &local
	}
	mu.Lock()
	if existing, ok := (*shares)[key]; ok {
		mu.Unlock()
		if existing.Source == share.Source && existing.Writable == share.Writable && existing.Cache == share.Cache {
			return nil
		}
		return fmt.Errorf("share mount %q already exists", key)
	}
	mu.Unlock()
	mount, err := build(share)
	if err != nil {
		return err
	}
	if err := rootFS.AddShare(mount); err != nil {
		return err
	}
	mu.Lock()
	if *shares == nil {
		*shares = make(map[string]client.ShareMount)
	}
	(*shares)[key] = share
	mu.Unlock()
	return nil
}

func AddImageMount(rootFS virtio.ShareMounter, mu *sync.Mutex, mounts *map[string]string, mountPath string, image *oci.Image, backend virtio.FSBackend) error {
	if rootFS == nil {
		return fmt.Errorf("instance rootfs does not support image mounts")
	}
	if strings.TrimSpace(mountPath) == "" || !strings.HasPrefix(mountPath, "/") {
		return fmt.Errorf("image mount path must be absolute")
	}
	if image == nil || image.RootFS == nil || backend == nil {
		return fmt.Errorf("image root filesystem is not available")
	}
	if mu == nil {
		mu = &sync.Mutex{}
	}
	if mounts == nil {
		local := map[string]string{}
		mounts = &local
	}
	mu.Lock()
	if existing, ok := (*mounts)[mountPath]; ok {
		mu.Unlock()
		if existing == image.Name {
			return nil
		}
		return fmt.Errorf("image mount %q already exists", mountPath)
	}
	mu.Unlock()
	if err := rootFS.AddShare(virtio.ShareMount{
		GuestPath: mountPath,
		Backend:   backend,
		Writable:  true,
		CacheMode: "aggressive",
	}); err != nil {
		return err
	}
	mu.Lock()
	if *mounts == nil {
		*mounts = make(map[string]string)
	}
	(*mounts)[mountPath] = image.Name
	mu.Unlock()
	return nil
}
