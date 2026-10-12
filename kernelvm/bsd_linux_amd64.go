//go:build linux && amd64

package kernelvm

import (
	"context"
	"j5.nz/cc/internal/hv/kvm"
	"j5.nz/cc/internal/nvme"
	"strings"
)

func runBSD(ctx context.Context, kernel []byte, opts BSDOptions) (string, error) {
	var root *nvme.Controller
	if opts.Root != nil {
		root = nvme.NewController(opts.Root)
	}
	next, consumed := 0, 0
	input := func(serial string) []byte {
		if next == len(opts.Console) {
			return nil
		}
		index := strings.Index(serial[consumed:], opts.Console[next].Wait)
		if index < 0 {
			return nil
		}
		consumed += index + len(opts.Console[next].Wait)
		data := []byte(opts.Console[next].Send)
		next++
		return data
	}
	switch opts.OS {
	case "openbsd":
		return kvm.BootOpenBSDKernelToMarkerWithNVMENetConsole(ctx, kernel, opts.MemoryMB, opts.Marker, root, nil, input)
	case "freebsd":
		return kvm.BootFreeBSDKernelToMarkerWithNVMENetConsole(ctx, kernel, opts.MemoryMB, opts.Marker, root, nil, input)
	default:
		return kvm.BootNetBSDKernelToMarkerWithNVMeConsole(ctx, kernel, opts.MemoryMB, opts.Marker, root, input)
	}
}
