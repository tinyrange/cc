//go:build linux && amd64

package kvm

import (
	"context"
	"strings"
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

func TestKVMHostRejectsUnsupportedManagedGuest(t *testing.T) {
	_, err := (Host{}).Start(context.Background(), managedhost.StartRequest{
		Spec: machine.Spec{Guest: "NetBSD", Boot: machine.BootSpec{Kind: "netbsd"}},
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "does not support") {
		t.Fatalf("Start unsupported guest error = %v", err)
	}
}

func TestKVMHostRejectsUnexpectedLinuxAttachments(t *testing.T) {
	_, err := (Host{}).Start(context.Background(), managedhost.StartRequest{
		Spec:        machine.Spec{Guest: "Linux", Boot: machine.BootSpec{Kind: "linux"}},
		Artifact:    rootartifact.Artifact{Kernel: []byte("kernel"), Initrd: []byte("initrd")},
		Attachments: "bad",
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "attachments") {
		t.Fatalf("Start unexpected attachments error = %v", err)
	}
}

func TestKVMHostRejectsUnexpectedBSDAttachments(t *testing.T) {
	_, err := (Host{}).Start(context.Background(), managedhost.StartRequest{
		Spec:        machine.Spec{Guest: "OpenBSD", Boot: machine.BootSpec{Kind: "openbsd"}},
		Artifact:    rootartifact.Artifact{Kernel: []byte("kernel")},
		Attachments: "bad",
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "attachments") {
		t.Fatalf("Start unexpected BSD attachments error = %v", err)
	}
}
