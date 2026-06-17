package vm

import (
	"context"
	"strings"
	"sync"
	"testing"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/imagefs"
	"j5.nz/cc/internal/oci"
	"j5.nz/cc/internal/virtio"
	"j5.nz/cc/internal/vmruntime"
)

func TestMountAlternateImageWithShares(t *testing.T) {
	inst := newFakeInstance()
	mounter := &recordingImageMounter{}
	image := &oci.Image{Name: "alt"}

	err := mountAlternateImageWithShares(context.Background(), inst, mounter, "/run/images/alt", image, []client.ShareMount{
		{Source: "/host/data", Mount: "/data", Writable: true},
	})
	if err != nil {
		t.Fatalf("mountAlternateImageWithShares: %v", err)
	}
	if mounter.mountPath != "/run/images/alt" || mounter.image != image {
		t.Fatalf("image mount = (%q, %p), want (%q, %p)", mounter.mountPath, mounter.image, "/run/images/alt", image)
	}
	if len(inst.shares) != 1 {
		t.Fatalf("shares = %d, want 1", len(inst.shares))
	}
	if inst.shares[0].Mount != "/run/images/alt/data" {
		t.Fatalf("rebased mount = %q", inst.shares[0].Mount)
	}
	if !inst.shares[0].Writable {
		t.Fatalf("rebased share lost writable flag")
	}
}

func TestMountAlternateImageWithSharesRequiresMounter(t *testing.T) {
	err := mountAlternateImageWithShares(context.Background(), newFakeInstance(), nil, "/run/images/alt", &oci.Image{}, nil)
	if err == nil || !strings.Contains(err.Error(), "image mounts") {
		t.Fatalf("nil mounter error = %v", err)
	}
}

func TestDelegatedRuntimeResources(t *testing.T) {
	ctx := context.Background()
	shareDelegate := &recordingShareDelegate{}
	share := client.ShareMount{Source: "/host", Mount: "/guest", Writable: true}
	if err := addDelegatedRuntimeShare(ctx, shareDelegate, share, "runtime shares"); err != nil {
		t.Fatalf("addDelegatedRuntimeShare: %v", err)
	}
	if len(shareDelegate.shares) != 1 || shareDelegate.shares[0] != share {
		t.Fatalf("delegated shares = %#v", shareDelegate.shares)
	}
	if err := addDelegatedRuntimeShare(ctx, nil, share, "runtime shares"); err == nil || !strings.Contains(err.Error(), "runtime shares") {
		t.Fatalf("nil delegated share error = %v", err)
	}

	imageDelegate := &recordingImageMounter{}
	image := &oci.Image{Name: "alt"}
	if err := addDelegatedRuntimeImage(ctx, imageDelegate, "/run/images/alt", image); err != nil {
		t.Fatalf("addDelegatedRuntimeImage: %v", err)
	}
	if imageDelegate.mountPath != "/run/images/alt" || imageDelegate.image != image {
		t.Fatalf("delegated image = (%q, %p)", imageDelegate.mountPath, imageDelegate.image)
	}
	if err := addDelegatedRuntimeImage(ctx, nil, "/run/images/alt", image); err == nil || !strings.Contains(err.Error(), "image mounts") {
		t.Fatalf("nil delegated image error = %v", err)
	}
}

func TestConvertShareMounts(t *testing.T) {
	if got := convertShareMounts(nil); got != nil {
		t.Fatalf("nil shares = %#v, want nil", got)
	}
	got := convertShareMounts([]client.ShareMount{{
		Source:   "/host",
		Mount:    "/guest",
		Writable: true,
		MapOwner: true,
		OwnerUID: 1000,
		OwnerGID: 1001,
		Cache:    "auto",
	}})
	if len(got) != 1 {
		t.Fatalf("shares = %d, want 1", len(got))
	}
	if got[0].Source != "/host" || got[0].Mount != "/guest" || !got[0].Writable || !got[0].MapOwner || got[0].OwnerUID != 1000 || got[0].OwnerGID != 1001 || got[0].Cache != "auto" {
		t.Fatalf("converted share = %+v", got[0])
	}
}

func TestShareMountToDirectoryShare(t *testing.T) {
	got := shareMountToDirectoryShare(client.ShareMount{
		Source:   "/host",
		Mount:    "/guest",
		Writable: true,
		MapOwner: true,
		OwnerUID: 1000,
		OwnerGID: 1001,
		Cache:    "always",
	})
	if got.Source != "/host" || got.Mount != "/guest" || !got.Writable || !got.MapOwner || got.OwnerUID != 1000 || got.OwnerGID != 1001 || got.Cache != "always" {
		t.Fatalf("directory share = %+v", got)
	}
}

func TestBuildRuntimeDirectoryShare(t *testing.T) {
	var gotIndex int
	var gotShare vmruntime.DirectoryShare
	mount, err := buildRuntimeDirectoryShare(client.ShareMount{
		Source:   "/host",
		Mount:    "/guest",
		Writable: true,
		Cache:    "auto",
	}, func(index int, share vmruntime.DirectoryShare) (virtio.ShareMount, error) {
		gotIndex = index
		gotShare = share
		return virtio.ShareMount{GuestPath: share.Mount, Writable: share.Writable, CacheMode: share.Cache}, nil
	})
	if err != nil {
		t.Fatalf("buildRuntimeDirectoryShare: %v", err)
	}
	if gotIndex != 0 || gotShare.Source != "/host" || gotShare.Mount != "/guest" || !gotShare.Writable || gotShare.Cache != "auto" {
		t.Fatalf("builder received index=%d share=%+v", gotIndex, gotShare)
	}
	if mount.GuestPath != "/guest" || !mount.Writable || mount.CacheMode != "auto" {
		t.Fatalf("mount = %+v", mount)
	}

	if _, err := buildRuntimeDirectoryShare(client.ShareMount{Mount: "/guest"}, nil); err == nil || !strings.Contains(err.Error(), "builder is not configured") {
		t.Fatalf("nil builder error = %v", err)
	}
}

func TestAddImageMountTracksDuplicates(t *testing.T) {
	root := &recordingShareMounter{}
	var mu sync.Mutex
	mounts := map[string]string{}
	image := &oci.Image{
		Name:   "alt",
		RootFS: imagefs.NewHostFS(t.TempDir(), nil),
	}
	backend := imageFSBackend(image)
	if err := addImageMount(root, &mu, &mounts, "/run/images/alt", image, backend); err != nil {
		t.Fatalf("addImageMount: %v", err)
	}
	if len(root.shares) != 1 {
		t.Fatalf("shares = %d, want 1", len(root.shares))
	}
	if root.shares[0].GuestPath != "/run/images/alt" || root.shares[0].Backend != backend || !root.shares[0].Writable || root.shares[0].CacheMode != "aggressive" {
		t.Fatalf("share mount = %+v", root.shares[0])
	}
	if mounts["/run/images/alt"] != "alt" {
		t.Fatalf("mounts = %#v", mounts)
	}

	if err := addImageMount(root, &mu, &mounts, "/run/images/alt", image, backend); err != nil {
		t.Fatalf("duplicate same image: %v", err)
	}
	if len(root.shares) != 1 {
		t.Fatalf("duplicate added share, count = %d", len(root.shares))
	}
	other := &oci.Image{Name: "other", RootFS: image.RootFS}
	err := addImageMount(root, &mu, &mounts, "/run/images/alt", other, imageFSBackend(other))
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("duplicate other image error = %v", err)
	}
}

func TestManagedMountStateAddImageTracksDuplicates(t *testing.T) {
	root := &recordingShareMounter{}
	state := managedMountState{}
	image := &oci.Image{
		Name:   "alt",
		RootFS: imagefs.NewHostFS(t.TempDir(), nil),
	}
	backend := imageFSBackend(image)
	if err := state.AddImage(root, "/run/images/alt", image, backend); err != nil {
		t.Fatalf("AddImage: %v", err)
	}
	if len(root.shares) != 1 {
		t.Fatalf("shares = %d, want 1", len(root.shares))
	}
	if root.shares[0].GuestPath != "/run/images/alt" {
		t.Fatalf("guest path = %q", root.shares[0].GuestPath)
	}
	if err := state.AddImage(root, "/run/images/alt", image, backend); err != nil {
		t.Fatalf("duplicate same image: %v", err)
	}
	if len(root.shares) != 1 {
		t.Fatalf("duplicate added share, count = %d", len(root.shares))
	}
	other := &oci.Image{Name: "other", RootFS: image.RootFS}
	err := state.AddImage(root, "/run/images/alt", other, imageFSBackend(other))
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("duplicate other image error = %v", err)
	}
}

func TestAddImageMountValidatesInputs(t *testing.T) {
	var mu sync.Mutex
	mounts := map[string]string{}
	root := &recordingShareMounter{}
	image := &oci.Image{Name: "alt", RootFS: imagefs.NewHostFS(t.TempDir(), nil)}
	for _, tc := range []struct {
		name      string
		root      virtio.ShareMounter
		mountPath string
		image     *oci.Image
		backend   virtio.FSBackend
		want      string
	}{
		{name: "missing root", root: nil, mountPath: "/run/images/alt", image: image, backend: imageFSBackend(image), want: "image mounts"},
		{name: "relative mount", root: root, mountPath: "relative", image: image, backend: imageFSBackend(image), want: "absolute"},
		{name: "missing image", root: root, mountPath: "/run/images/alt", image: nil, backend: nil, want: "root filesystem"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := addImageMount(tc.root, &mu, &mounts, tc.mountPath, tc.image, tc.backend)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestAddRuntimeShareMountTracksDuplicates(t *testing.T) {
	root := &recordingShareMounter{}
	var mu sync.Mutex
	shares := map[string]client.ShareMount{}
	share := client.ShareMount{Source: "/host/data", Mount: "/data", Writable: true, Cache: "auto"}
	builds := 0
	build := func(share client.ShareMount) (virtio.ShareMount, error) {
		builds++
		return virtio.ShareMount{GuestPath: share.Mount, Writable: share.Writable, CacheMode: share.Cache}, nil
	}
	if err := addRuntimeShareMount(root, &mu, &shares, share, "shares", build); err != nil {
		t.Fatalf("addRuntimeShareMount: %v", err)
	}
	if builds != 1 || len(root.shares) != 1 {
		t.Fatalf("builds/shares = %d/%d, want 1/1", builds, len(root.shares))
	}
	if root.shares[0].GuestPath != "/data" || !root.shares[0].Writable || root.shares[0].CacheMode != "auto" {
		t.Fatalf("share mount = %+v", root.shares[0])
	}
	if shares["/data"] != share {
		t.Fatalf("tracked shares = %#v", shares)
	}
	if err := addRuntimeShareMount(root, &mu, &shares, share, "shares", build); err != nil {
		t.Fatalf("duplicate same share: %v", err)
	}
	if builds != 1 || len(root.shares) != 1 {
		t.Fatalf("duplicate builds/shares = %d/%d, want 1/1", builds, len(root.shares))
	}
	err := addRuntimeShareMount(root, &mu, &shares, client.ShareMount{Source: "/other", Mount: "/data"}, "shares", build)
	if err == nil || !strings.Contains(err.Error(), `share mount "/data" already exists`) {
		t.Fatalf("duplicate conflicting share error = %v", err)
	}
}

func TestManagedMountStateAddShareTracksDuplicates(t *testing.T) {
	root := &recordingShareMounter{}
	state := managedMountState{}
	share := client.ShareMount{Source: "/host/data", Mount: "/data", Writable: true, Cache: "auto"}
	builds := 0
	build := func(share client.ShareMount) (virtio.ShareMount, error) {
		builds++
		return virtio.ShareMount{GuestPath: share.Mount, Writable: share.Writable, CacheMode: share.Cache}, nil
	}
	if err := state.AddShare(root, share, "shares", build); err != nil {
		t.Fatalf("AddShare: %v", err)
	}
	if builds != 1 || len(root.shares) != 1 {
		t.Fatalf("builds/shares = %d/%d, want 1/1", builds, len(root.shares))
	}
	if err := state.AddShare(root, share, "shares", build); err != nil {
		t.Fatalf("duplicate same share: %v", err)
	}
	if builds != 1 || len(root.shares) != 1 {
		t.Fatalf("duplicate builds/shares = %d/%d, want 1/1", builds, len(root.shares))
	}
	err := state.AddShare(root, client.ShareMount{Source: "/other", Mount: "/data"}, "shares", build)
	if err == nil || !strings.Contains(err.Error(), `share mount "/data" already exists`) {
		t.Fatalf("duplicate conflicting share error = %v", err)
	}
}

func TestAddRuntimeShareMountValidatesInputs(t *testing.T) {
	var mu sync.Mutex
	shares := map[string]client.ShareMount{}
	build := func(share client.ShareMount) (virtio.ShareMount, error) {
		return virtio.ShareMount{GuestPath: share.Mount}, nil
	}
	err := addRuntimeShareMount(nil, &mu, &shares, client.ShareMount{Mount: "/data"}, "runtime shares", build)
	if err == nil || !strings.Contains(err.Error(), "runtime shares") {
		t.Fatalf("nil root error = %v", err)
	}
	err = addRuntimeShareMount(&recordingShareMounter{}, &mu, &shares, client.ShareMount{}, "shares", build)
	if err == nil || !strings.Contains(err.Error(), "share mount path is required") {
		t.Fatalf("missing mount error = %v", err)
	}
}

type recordingImageMounter struct {
	mountPath string
	image     *oci.Image
}

func (m *recordingImageMounter) AddImage(_ context.Context, mountPath string, image *oci.Image) error {
	m.mountPath = mountPath
	m.image = image
	return nil
}

type recordingShareMounter struct {
	shares []virtio.ShareMount
}

func (m *recordingShareMounter) AddShare(share virtio.ShareMount) error {
	m.shares = append(m.shares, share)
	return nil
}

type recordingShareDelegate struct {
	shares []client.ShareMount
}

func (d *recordingShareDelegate) AddShare(_ context.Context, share client.ShareMount) error {
	d.shares = append(d.shares, share)
	return nil
}
