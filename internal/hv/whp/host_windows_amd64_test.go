//go:build windows && amd64

package whp

import (
	"context"
	"testing"

	managedhost "j5.nz/cc/internal/managed/host"
	"j5.nz/cc/internal/managed/machine"
	"j5.nz/cc/internal/managed/rootartifact"
)

func TestNormalizeLinuxManagedMachineDefaultsSpec(t *testing.T) {
	machine := normalizeLinuxManagedMachine(LinuxManagedMachine{})
	if machine.Spec.Guest != "Linux" {
		t.Fatalf("guest = %q", machine.Spec.Guest)
	}
	if machine.Spec.Arch != "amd64" {
		t.Fatalf("arch = %q", machine.Spec.Arch)
	}
	if machine.Spec.Boot.Kind != "linux" {
		t.Fatalf("boot kind = %q", machine.Spec.Boot.Kind)
	}
	if machine.Spec.Control.Kind != "vsock" {
		t.Fatalf("control kind = %q", machine.Spec.Control.Kind)
	}
}

func TestWHPHostDispatchesBSDToAttachmentValidation(t *testing.T) {
	_, err := (Host{}).Start(context.Background(), managedhost.StartRequest{
		Spec: machine.Spec{Guest: "FreeBSD", Boot: machine.BootSpec{Kind: "freebsd"}},
	}, nil)
	if err == nil {
		t.Fatalf("Start unsupported guest error = %v", err)
	}
}

func TestWHPHostRejectsUnexpectedLinuxAttachments(t *testing.T) {
	_, err := (Host{}).Start(context.Background(), managedhost.StartRequest{
		Spec:        machine.Spec{Guest: "Linux", Boot: machine.BootSpec{Kind: "linux"}},
		Artifact:    rootartifact.Artifact{Kernel: []byte("kernel"), Initrd: []byte("initrd")},
		Attachments: "bad",
	}, nil)
	if err == nil {
		t.Fatalf("Start unexpected attachments error = %v", err)
	}
}
