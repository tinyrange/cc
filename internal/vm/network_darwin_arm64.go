//go:build darwin && arm64

package vm

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/arm64vm"
	"j5.nz/cc/internal/virtio"
	"j5.nz/cc/internal/vmruntime"
)

type darwinNetworkRuntime struct {
	*networkRuntime
}

func newDarwinARM64NetworkRuntime(cfg *client.NetworkConfig) (*darwinNetworkRuntime, error) {
	if remote, err := newDarwinRemoteNetworkRuntime(cfg); err != nil || remote != nil {
		return remote, err
	}
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

func newDarwinRemoteNetworkRuntime(cfg *client.NetworkConfig) (*darwinNetworkRuntime, error) {
	if cfg == nil || !cfg.Enabled {
		return nil, nil
	}
	socketPath := strings.TrimSpace(os.Getenv(sidecarNetSocketEnv))
	if socketPath == "" {
		return nil, nil
	}
	ip := net.ParseIP(strings.TrimSpace(os.Getenv(sidecarNetIPv4Env))).To4()
	if ip == nil {
		return nil, fmt.Errorf("sidecar network IPv4 is not configured")
	}
	mac, err := net.ParseMAC(strings.TrimSpace(os.Getenv(sidecarNetMACEnv)))
	if err != nil {
		return nil, fmt.Errorf("sidecar network MAC is not configured: %w", err)
	}
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("dial coordinator net backend: %w", err)
	}
	codec := virtio.NewNetPacketCodec(conn)
	dev := virtio.NewNet(arm64vm.NetBase, arm64vm.NetSize, arm64vm.NetIRQ, mac, virtio.NewNetRemoteBackend(codec, DefaultInstanceID, "eth0"))
	go func() {
		_ = virtio.ReceiveNetRXPackets(context.Background(), codec, dev)
	}()
	return &darwinNetworkRuntime{networkRuntime: &networkRuntime{
		ip:  ip,
		mac: mac,
		dev: dev,
	}}, nil
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
