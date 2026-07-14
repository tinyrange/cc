//go:build darwin && arm64

package vm

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/imagefs"
	"j5.nz/cc/internal/virtio"
)

func TestBuiltinBSDSidecarResourcesUseGuestProfile(t *testing.T) {
	profile, ok := builtinGuestForImage("@openbsd")
	if !ok {
		t.Fatal("OpenBSD built-in profile was not resolved")
	}
	resources := builtinSidecarResources(profile)
	if resources.osName != "OpenBSD" {
		t.Fatalf("sidecar built-in OS name = %q, want OpenBSD", resources.osName)
	}
	if !reflect.DeepEqual(resources.capabilities, profile.Caps) {
		t.Fatalf("sidecar built-in capabilities = %+v, want %+v", resources.capabilities, profile.Caps)
	}
	if resources.execEnv == nil {
		t.Fatal("sidecar built-in exec env was not configured")
	}
}

func TestSidecarResourcePrepRejectsBuiltinBSDBeforeImageStore(t *testing.T) {
	host := &sidecarVMHost{}
	_, err := prepareSidecarCreateResources(host, context.Background(), client.CreateInstanceRequest{Image: "@netbsd"})
	if err == nil {
		t.Fatal("prepareSidecarCreateResources unexpectedly succeeded")
	}

	_, err = prepareSidecarBlankResources(host, context.Background(), client.StartInstanceRequest{Image: "@openbsd"})
	if err == nil {
		t.Fatal("prepareSidecarBlankResources unexpectedly succeeded")
	}
}

func TestSidecarBlankRestoreSkipsBootBundle(t *testing.T) {
	cacheDir, err := os.MkdirTemp("/tmp", "ccsc-*")
	if err != nil {
		t.Fatalf("create short cache dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(cacheDir) })
	host := &sidecarVMHost{cacheDir: cacheDir}
	resources, err := prepareSidecarBlankResources(host, context.Background(), client.StartInstanceRequest{
		RestoreSnapshot: filepath.Join(t.TempDir(), "snapshot"),
	})
	if err != nil {
		t.Fatalf("prepareSidecarBlankResources: %v", err)
	}
	defer resources.closeAll()

	var hasFS bool
	for _, env := range resources.env {
		switch {
		case strings.HasPrefix(env, sidecarFSSocketEnv+"="):
			hasFS = true
		case strings.HasPrefix(env, sidecarBootSocketEnv+"="):
			t.Fatalf("restore resources included boot socket env %q", env)
		}
	}
	if !hasFS {
		t.Fatalf("restore resources env = %q, want fs socket", resources.env)
	}
}

func TestSidecarAlternateImageRejectsBuiltinBSDBeforeImageStore(t *testing.T) {
	host := &sidecarVMHost{}
	rootFS, ok := virtio.NewMountedFS(virtio.NewImageFS(imagefs.NewHostFS(t.TempDir(), nil), ""), nil).(sidecarRootFS)
	if !ok {
		t.Fatal("test rootfs does not implement sidecarRootFS")
	}
	inst := &sidecarInstance{rootFS: rootFS}

	_, err := host.prepareRunInInstanceExec(context.Background(), inst, "alpine", client.RunRequest{
		Image:   "@openbsd",
		Command: []string{"uname"},
	})
	if err == nil {
		t.Fatal("prepareRunInInstanceExec unexpectedly succeeded")
	}

	_, err = host.prepareExecInInstance(context.Background(), inst, "alpine", client.ExecRequest{
		Image:   "@netbsd",
		Command: []string{"uname"},
	})
	if err == nil {
		t.Fatal("prepareExecInInstance unexpectedly succeeded")
	}
}
