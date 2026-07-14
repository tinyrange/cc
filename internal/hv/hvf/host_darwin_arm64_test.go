//go:build darwin && arm64

package hvf

import (
	"context"
	"testing"

	managedhost "j5.nz/cc/internal/managed/host"
	"j5.nz/cc/internal/managed/machine"
)

func TestNormalizeLinuxManagedMachineDefaultsSpec(t *testing.T) {
	machine := normalizeLinuxManagedMachine(LinuxManagedMachine{})
	if machine.Spec.Guest != "Linux" {
		t.Fatalf("guest = %q", machine.Spec.Guest)
	}
	if machine.Spec.Arch != "arm64" {
		t.Fatalf("arch = %q", machine.Spec.Arch)
	}
	if machine.Spec.Boot.Kind != "linux" {
		t.Fatalf("boot kind = %q", machine.Spec.Boot.Kind)
	}
	if machine.Spec.Control.Kind != "vsock" {
		t.Fatalf("control kind = %q", machine.Spec.Control.Kind)
	}
}

func TestHVFHostRejectsUnsupportedManagedGuest(t *testing.T) {
	_, err := (Host{}).Start(context.Background(), managedhost.StartRequest{
		Spec: machine.Spec{Guest: "Plan9", Boot: machine.BootSpec{Kind: "plan9"}},
	}, nil)
	if err == nil {
		t.Fatalf("Start unsupported guest error = %v", err)
	}
}

func TestHVFManagedGuestKindRecognizesBSD(t *testing.T) {
	for _, tc := range []struct {
		name string
		spec machine.Spec
		want string
	}{
		{
			name: "openbsd guest",
			spec: machine.Spec{Guest: "OpenBSD"},
			want: "openbsd",
		},
		{
			name: "freebsd boot",
			spec: machine.Spec{Boot: machine.BootSpec{Kind: "freebsd"}},
			want: "freebsd",
		},
		{
			name: "netbsd guest with boot",
			spec: machine.Spec{Guest: "NetBSD", Boot: machine.BootSpec{Kind: "netbsd"}},
			want: "netbsd",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := managedGuestKind(tc.spec); got != tc.want {
				t.Fatalf("managedGuestKind = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestHVFHostRejectsUnexpectedLinuxAttachments(t *testing.T) {
	_, err := (Host{}).Start(context.Background(), managedhost.StartRequest{
		Spec:        machine.Spec{Guest: "Linux", Boot: machine.BootSpec{Kind: "linux"}},
		Attachments: "bad",
	}, nil)
	if err == nil {
		t.Fatalf("Start unexpected attachments error = %v", err)
	}
}

func TestHVFHostRejectsUnexpectedBSDAttachments(t *testing.T) {
	_, err := (Host{}).Start(context.Background(), managedhost.StartRequest{
		Spec:        machine.Spec{Guest: "FreeBSD", Boot: machine.BootSpec{Kind: "freebsd"}},
		Attachments: "bad",
	}, nil)
	if err == nil {
		t.Fatalf("Start unexpected attachments error = %v", err)
	}
}
