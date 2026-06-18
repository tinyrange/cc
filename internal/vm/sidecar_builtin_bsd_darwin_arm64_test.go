//go:build darwin && arm64

package vm

import (
	"context"
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
	if strings.Contains(err.Error(), "image store") || strings.Contains(err.Error(), "image.json") {
		t.Fatalf("built-in NetBSD create prep went through OCI image store path: %v", err)
	}
	if !strings.Contains(err.Error(), "managed guest runtime") {
		t.Fatalf("prepareSidecarCreateResources error = %v, want managed guest runtime guard", err)
	}

	_, err = prepareSidecarBlankResources(host, context.Background(), client.StartInstanceRequest{Image: "@openbsd"})
	if err == nil {
		t.Fatal("prepareSidecarBlankResources unexpectedly succeeded")
	}
	if strings.Contains(err.Error(), "image store") || strings.Contains(err.Error(), "image.json") {
		t.Fatalf("built-in OpenBSD blank prep went through OCI image store path: %v", err)
	}
	if !strings.Contains(err.Error(), "managed guest runtime") {
		t.Fatalf("prepareSidecarBlankResources error = %v, want managed guest runtime guard", err)
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
	if strings.Contains(err.Error(), "image store") || strings.Contains(err.Error(), "image.json") {
		t.Fatalf("built-in OpenBSD run-in-instance went through OCI image store path: %v", err)
	}
	if !strings.Contains(err.Error(), "cannot be mounted as an alternate Linux root") {
		t.Fatalf("prepareRunInInstanceExec error = %v, want alternate root guard", err)
	}

	_, err = host.prepareExecInInstance(context.Background(), inst, "alpine", client.ExecRequest{
		Image:   "@netbsd",
		Command: []string{"uname"},
	})
	if err == nil {
		t.Fatal("prepareExecInInstance unexpectedly succeeded")
	}
	if strings.Contains(err.Error(), "image store") || strings.Contains(err.Error(), "image.json") {
		t.Fatalf("built-in NetBSD exec-in-instance went through OCI image store path: %v", err)
	}
	if !strings.Contains(err.Error(), "cannot be mounted as an alternate Linux root") {
		t.Fatalf("prepareExecInInstance error = %v, want alternate root guard", err)
	}
}
