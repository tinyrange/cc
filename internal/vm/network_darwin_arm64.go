//go:build darwin && arm64

package vm

import (
	"net"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/arm64vm"
	"j5.nz/cc/internal/vmruntime"
)

type darwinNetworkRuntime struct {
	*networkRuntime
}

func newDarwinARM64NetworkRuntime(cfg *client.NetworkConfig) (*darwinNetworkRuntime, error) {
	common, err := newNetworkRuntime(networkDeviceConfig{
		Config: cfg,
		IP:     net.IPv4(10, 42, 0, 2),
		MAC:    net.HardwareAddr{0x02, 0x42, 0x0a, 0x2a, 0x00, 0x02},
		Base:   arm64vm.NetBase,
		Size:   arm64vm.NetSize,
		IRQ:    arm64vm.NetIRQ,
	})
	if err != nil || common == nil {
		return nil, err
	}
	return &darwinNetworkRuntime{networkRuntime: common}, nil
}

func (n *darwinNetworkRuntime) guestInitConfig() *vmruntime.GuestNetworkConfig {
	if n == nil {
		return nil
	}
	return n.GuestInitConfig()
}

func darwinNetworkGuestAddress(n *darwinNetworkRuntime) string {
	if n == nil {
		return (&networkRuntime{}).GuestAddress()
	}
	return n.GuestAddress()
}

func darwinNetworkGuestCIDR(n *darwinNetworkRuntime) string {
	if n == nil {
		return (&networkRuntime{}).GuestCIDR()
	}
	return n.GuestCIDR()
}
