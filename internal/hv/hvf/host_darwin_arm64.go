//go:build darwin && arm64

package hvf

import (
	"context"
	"fmt"
	"net"
	"strings"

	"j5.nz/cc/client"
	managedhost "j5.nz/cc/internal/managed/host"
	"j5.nz/cc/internal/managed/machine"
	managedsession "j5.nz/cc/internal/managed/session"
	"j5.nz/cc/internal/netstack"
	"j5.nz/cc/internal/virtio"
	"j5.nz/cc/internal/vmruntime"
)

type Host struct{}

type LinuxManagedMachine struct {
	Spec       machine.Spec
	RunRequest vmruntime.RunRequest
}

type LinuxManagedAttachments struct {
	RunRequest vmruntime.RunRequest
}

type BSDManagedAttachments struct {
	GuestIPv4 net.IP
	GuestMAC  net.HardwareAddr
	NetDevice *virtio.Net
	NetStack  *netstack.NetStack
}

func (Host) Start(ctx context.Context, req managedhost.StartRequest, onEvent func(client.BootEvent) error) (managedsession.Session, error) {
	switch managedGuestKind(req.Spec) {
	case "linux":
		return Host{}.startLinux(ctx, req, onEvent)
	case "openbsd":
		return Host{}.startOpenBSD(ctx, req, onEvent)
	case "freebsd":
		return Host{}.startFreeBSD(ctx, req, onEvent)
	case "netbsd":
		return Host{}.startNetBSD(ctx, req, onEvent)
	default:
		return nil, fmt.Errorf("hvf host does not support managed guest %q boot %q", req.Spec.Guest, req.Spec.Boot.Kind)
	}
}

func (Host) startLinux(ctx context.Context, req managedhost.StartRequest, onEvent func(client.BootEvent) error) (managedsession.Session, error) {
	var attachments LinuxManagedAttachments
	switch value := req.Attachments.(type) {
	case LinuxManagedAttachments:
		attachments = value
	case *LinuxManagedAttachments:
		if value != nil {
			attachments = *value
		}
	default:
		return nil, fmt.Errorf("hvf linux managed attachments have type %T", req.Attachments)
	}
	return Host{}.StartLinuxManaged(ctx, LinuxManagedMachine{
		Spec:       req.Spec,
		RunRequest: attachments.RunRequest,
	}, onEvent)
}

func (Host) startOpenBSD(ctx context.Context, req managedhost.StartRequest, onEvent func(client.BootEvent) error) (managedsession.Session, error) {
	attachments, err := bsdManagedAttachments(req.Attachments)
	if err != nil {
		return nil, err
	}
	return StartOpenBSDManagedSession(ctx, OpenBSDManagedConfig{
		Kernel:    req.Artifact.Kernel,
		Root:      req.Artifact.RootBlock,
		MemoryMB:  req.Spec.MemoryMB,
		Dmesg:     req.Spec.Dmesg,
		GuestIPv4: attachments.GuestIPv4,
		GuestMAC:  attachments.GuestMAC,
		NetDevice: attachments.NetDevice,
		NetStack:  attachments.NetStack,
	}, onEvent)
}

func (Host) startFreeBSD(ctx context.Context, req managedhost.StartRequest, onEvent func(client.BootEvent) error) (managedsession.Session, error) {
	attachments, err := bsdManagedAttachments(req.Attachments)
	if err != nil {
		return nil, err
	}
	return StartFreeBSDManagedSession(ctx, FreeBSDManagedConfig{
		Kernel:    req.Artifact.Kernel,
		Root:      req.Artifact.RootBlock,
		MemoryMB:  req.Spec.MemoryMB,
		Dmesg:     req.Spec.Dmesg,
		GuestIPv4: attachments.GuestIPv4,
		GuestMAC:  attachments.GuestMAC,
		NetDevice: attachments.NetDevice,
		NetStack:  attachments.NetStack,
	}, onEvent)
}

func (Host) startNetBSD(ctx context.Context, req managedhost.StartRequest, onEvent func(client.BootEvent) error) (managedsession.Session, error) {
	attachments, err := bsdManagedAttachments(req.Attachments)
	if err != nil {
		return nil, err
	}
	return StartNetBSDManagedSession(ctx, NetBSDManagedConfig{
		Kernel:    req.Artifact.Kernel,
		Root:      req.Artifact.RootBlock,
		MemoryMB:  req.Spec.MemoryMB,
		Dmesg:     req.Spec.Dmesg,
		GuestIPv4: attachments.GuestIPv4,
		GuestMAC:  attachments.GuestMAC,
		NetDevice: attachments.NetDevice,
		NetStack:  attachments.NetStack,
	}, onEvent)
}

func bsdManagedAttachments(value any) (BSDManagedAttachments, error) {
	switch attachments := value.(type) {
	case nil:
		return BSDManagedAttachments{}, nil
	case BSDManagedAttachments:
		return attachments, nil
	case *BSDManagedAttachments:
		if attachments != nil {
			return *attachments, nil
		}
		return BSDManagedAttachments{}, nil
	default:
		return BSDManagedAttachments{}, fmt.Errorf("hvf bsd managed attachments have type %T", value)
	}
}

func (Host) StartLinuxManaged(ctx context.Context, machine LinuxManagedMachine, onEvent func(client.BootEvent) error) (*ContainerSession, error) {
	machine = normalizeLinuxManagedMachine(machine)
	req := machine.RunRequest
	req.Persistent = true
	if req.MemoryMB == 0 {
		req.MemoryMB = machine.Spec.MemoryMB
	}
	if req.CPUs == 0 {
		req.CPUs = machine.Spec.CPUs
	}
	if !req.Dmesg {
		req.Dmesg = machine.Spec.Dmesg
	}
	return StartContainerStream(ctx, req, onEvent)
}

func normalizeLinuxManagedMachine(machine LinuxManagedMachine) LinuxManagedMachine {
	if machine.Spec.Guest == "" {
		machine.Spec.Guest = "Linux"
	}
	if machine.Spec.Arch == "" {
		machine.Spec.Arch = "arm64"
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
	if guest == "openbsd" || boot == "openbsd" {
		return "openbsd"
	}
	if guest == "freebsd" || boot == "freebsd" {
		return "freebsd"
	}
	if guest == "netbsd" || boot == "netbsd" {
		return "netbsd"
	}
	return guest
}
