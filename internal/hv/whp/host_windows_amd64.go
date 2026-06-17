//go:build windows && amd64

package whp

import (
	"context"
	"fmt"
	"strings"

	"j5.nz/cc/client"
	managedhost "j5.nz/cc/internal/managed/host"
	"j5.nz/cc/internal/managed/machine"
	managedsession "j5.nz/cc/internal/managed/session"
	"j5.nz/cc/internal/virtio"
)

type Host struct{}

type LinuxManagedMachine struct {
	Spec      machine.Spec
	Kernel    []byte
	Initrd    []byte
	FSDevices []*virtio.FS
	NetDevice *virtio.Net
}

type LinuxManagedAttachments struct {
	FSDevices []*virtio.FS
	NetDevice *virtio.Net
}

func (Host) Start(ctx context.Context, req managedhost.StartRequest, onEvent func(client.BootEvent) error) (managedsession.Session, error) {
	if managedGuestKind(req.Spec) != "linux" {
		return nil, fmt.Errorf("whp host does not support managed guest %q boot %q", req.Spec.Guest, req.Spec.Boot.Kind)
	}
	var attachments LinuxManagedAttachments
	switch value := req.Attachments.(type) {
	case nil:
	case LinuxManagedAttachments:
		attachments = value
	case *LinuxManagedAttachments:
		if value != nil {
			attachments = *value
		}
	default:
		return nil, fmt.Errorf("whp linux managed attachments have type %T", req.Attachments)
	}
	return Host{}.StartLinuxManaged(ctx, LinuxManagedMachine{
		Spec:      req.Spec,
		Kernel:    req.Artifact.Kernel,
		Initrd:    req.Artifact.Initrd,
		FSDevices: attachments.FSDevices,
		NetDevice: attachments.NetDevice,
	}, onEvent)
}

func (Host) StartLinuxManaged(ctx context.Context, machine LinuxManagedMachine, onEvent func(client.BootEvent) error) (*ManagedSession, error) {
	machine = normalizeLinuxManagedMachine(machine)
	return StartManagedSessionWithNet(
		ctx,
		machine.Kernel,
		machine.Initrd,
		machine.Spec.MemoryMB,
		machine.Spec.Dmesg,
		machine.FSDevices,
		machine.NetDevice,
		onEvent,
	)
}

func normalizeLinuxManagedMachine(machine LinuxManagedMachine) LinuxManagedMachine {
	if machine.Spec.Guest == "" {
		machine.Spec.Guest = "Linux"
	}
	if machine.Spec.Arch == "" {
		machine.Spec.Arch = "amd64"
	}
	if machine.Spec.Boot.Kind == "" {
		machine.Spec.Boot.Kind = "linux"
	}
	if machine.Spec.Control.Kind == "" {
		machine.Spec.Control.Kind = "vsock"
	}
	return machine
}

func managedGuestKind(spec machine.Spec) string {
	guest := strings.ToLower(strings.TrimSpace(spec.Guest))
	boot := strings.ToLower(strings.TrimSpace(spec.Boot.Kind))
	if guest == "" && boot == "" {
		return "linux"
	}
	if guest == "" {
		guest = boot
	}
	if boot == "" {
		return guest
	}
	if guest == "linux" && boot == "linux" {
		return "linux"
	}
	return guest
}
