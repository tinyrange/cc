//go:build windows && amd64

package whp

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
)

type Host struct{}

type LinuxManagedMachine struct {
	Spec            machine.Spec
	Kernel          []byte
	Initrd          []byte
	FSDevices       []*virtio.FS
	NetDevice       *virtio.Net
	SnapshotDir     string
	RestoreSnapshot string
}

type LinuxManagedAttachments struct {
	FSDevices       []*virtio.FS
	NetDevice       *virtio.Net
	SnapshotDir     string
	RestoreSnapshot string
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
	case "freebsd":
		attachments, err := bsdManagedAttachments(req.Attachments)
		if err != nil {
			return nil, err
		}
		return StartFreeBSDManagedSession(ctx, FreeBSDManagedConfig{
			Kernel:      req.Artifact.Kernel,
			Root:        req.Artifact.RootBlock,
			ExtraBlocks: req.Artifact.ExtraBlocks,
			MemoryMB:    req.Spec.MemoryMB,
			Dmesg:       req.Spec.Dmesg,
			GuestIPv4:   attachments.GuestIPv4,
			GuestMAC:    attachments.GuestMAC,
			NetDevice:   attachments.NetDevice,
			NetStack:    attachments.NetStack,
		}, onEvent)
	case "netbsd":
		attachments, err := bsdManagedAttachments(req.Attachments)
		if err != nil {
			return nil, err
		}
		return StartNetBSDManagedSession(ctx, NetBSDManagedConfig{
			Kernel:      req.Artifact.Kernel,
			Root:        req.Artifact.RootBlock,
			ExtraBlocks: req.Artifact.ExtraBlocks,
			MemoryMB:    req.Spec.MemoryMB,
			Dmesg:       req.Spec.Dmesg,
			GuestIPv4:   attachments.GuestIPv4,
			GuestMAC:    attachments.GuestMAC,
			NetDevice:   attachments.NetDevice,
			NetStack:    attachments.NetStack,
		}, onEvent)
	default:
		return nil, fmt.Errorf("whp host does not support managed guest %q boot %q", req.Spec.Guest, req.Spec.Boot.Kind)
	}
}

func (Host) startLinux(ctx context.Context, req managedhost.StartRequest, onEvent func(client.BootEvent) error) (managedsession.Session, error) {
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
		Spec:            req.Spec,
		Kernel:          req.Artifact.Kernel,
		Initrd:          req.Artifact.Initrd,
		FSDevices:       attachments.FSDevices,
		NetDevice:       attachments.NetDevice,
		SnapshotDir:     attachments.SnapshotDir,
		RestoreSnapshot: attachments.RestoreSnapshot,
	}, onEvent)
}

func (Host) StartLinuxManaged(ctx context.Context, machine LinuxManagedMachine, onEvent func(client.BootEvent) error) (*ManagedSession, error) {
	machine = normalizeLinuxManagedMachine(machine)
	opts := ManagedSessionOptions{
		SnapshotDir:     strings.TrimSpace(machine.SnapshotDir),
		RestoreSnapshot: strings.TrimSpace(machine.RestoreSnapshot),
	}
	return StartManagedSessionWithNetOptions(ctx, machine.Kernel, machine.Initrd, machine.Spec.MemoryMB, machine.Spec.Dmesg, machine.FSDevices, machine.NetDevice, opts, onEvent)
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

func bsdManagedAttachments(value any) (BSDManagedAttachments, error) {
	switch attachments := value.(type) {
	case BSDManagedAttachments:
		return attachments, nil
	case *BSDManagedAttachments:
		if attachments == nil {
			return BSDManagedAttachments{}, fmt.Errorf("whp bsd managed attachments are nil")
		}
		return *attachments, nil
	default:
		return BSDManagedAttachments{}, fmt.Errorf("whp bsd managed attachments have type %T", value)
	}
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
	for _, bsd := range []string{"openbsd", "freebsd", "netbsd"} {
		if guest == bsd || boot == bsd {
			return bsd
		}
	}
	return guest
}
