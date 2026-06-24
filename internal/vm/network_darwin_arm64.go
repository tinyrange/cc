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
	switchID string
	closeFn  func() error
}

func newDarwinARM64NetworkRuntime(id string, cfg *client.NetworkConfig) (*darwinNetworkRuntime, error) {
	if remote, err := newDarwinRemoteNetworkRuntime(cfg); err != nil || remote != nil {
		return remote, err
	}
	lease, explicit := darwinSidecarLeaseFromConfig(id, cfg)
	if !explicit {
		lease = defaultDarwinSidecarSwitch.Register(id)
	}
	common, err := newNetworkRuntime(networkDeviceConfig{
		ID:     lease.id,
		Config: cfg,
		IP:     lease.ip,
		MAC:    lease.mac,
		Base:   arm64vm.NetBase,
		Size:   arm64vm.NetSize,
		IRQ:    arm64vm.NetIRQ,
		TXHook: func(packet []byte) {
			defaultDarwinSidecarSwitch.Forward(lease.id, packet)
		},
	})
	if err != nil || common == nil {
		if !explicit {
			defaultDarwinSidecarSwitch.Unregister(lease.id)
		}
		return nil, err
	}
	runtime := &darwinNetworkRuntime{networkRuntime: common, switchID: lease.id}
	defaultDarwinSidecarSwitch.Attach(darwinSidecarEndpoint{
		id:  lease.id,
		ip:  lease.ip,
		mac: lease.mac,
		rx: func(frame []byte) {
			copied := append([]byte(nil), frame...)
			go func() {
				_ = common.dev.EnqueueRxPacketOwned(copied)
			}()
		},
	})
	return runtime, nil
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
	if strings.TrimSpace(os.Getenv(sidecarNetModeEnv)) == "bridge" {
		common, err := newNetworkRuntime(networkDeviceConfig{
			ID:     DefaultInstanceID,
			Config: cfg,
			IP:     ip,
			MAC:    mac,
			Base:   arm64vm.NetBase,
			Size:   arm64vm.NetSize,
			IRQ:    arm64vm.NetIRQ,
			TXHook: func(packet []byte) {
				_ = codec.Send(virtio.NetPacket{
					Kind:     virtio.NetPacketTX,
					VMID:     DefaultInstanceID,
					DeviceID: "eth0",
					Frame:    append([]byte(nil), packet...),
				})
			},
		})
		if err != nil {
			_ = codec.Close()
			return nil, err
		}
		if common == nil {
			_ = codec.Close()
			return nil, nil
		}
		go func() {
			_ = virtio.ReceiveNetRXPackets(context.Background(), codec, common.dev)
		}()
		return &darwinNetworkRuntime{
			networkRuntime: common,
			closeFn:        codec.Close,
		}, nil
	}
	dev := virtio.NewNet(arm64vm.NetBase, arm64vm.NetSize, arm64vm.NetIRQ, mac, virtio.NewNetRemoteBackend(codec, DefaultInstanceID, "eth0"))
	go func() {
		_ = virtio.ReceiveNetRXPackets(context.Background(), codec, dev)
	}()
	return &darwinNetworkRuntime{networkRuntime: &networkRuntime{
		ip:  ip,
		mac: mac,
		dev: dev,
	}, closeFn: codec.Close}, nil
}

func (n *darwinNetworkRuntime) Close() error {
	if n == nil {
		return nil
	}
	if n.switchID != "" {
		defaultDarwinSidecarSwitch.Unregister(n.switchID)
		n.switchID = ""
	}
	if n.networkRuntime == nil {
		if n.closeFn != nil {
			return n.closeFn()
		}
		return nil
	}
	err := n.networkRuntime.Close()
	if n.closeFn != nil {
		if closeErr := n.closeFn(); closeErr != nil && err == nil {
			err = closeErr
		}
	}
	return err
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
