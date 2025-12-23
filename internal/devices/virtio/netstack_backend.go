package virtio

import (
	"fmt"
	"io"
	"net"

	"github.com/tinyrange/cc/internal/debug"
	"github.com/tinyrange/cc/internal/netstack"
)

var netstackBackendDbg = debug.WithSource("virtio-netstack-backend")

type NetstackBackend struct {
	ns  *netstack.NetStack
	nic *netstack.NetworkInterface
}

func NewNetstackBackend(ns *netstack.NetStack, mac net.HardwareAddr) (*NetstackBackend, error) {
	if ns == nil {
		return nil, fmt.Errorf("netstack backend requires a netstack instance")
	}
	if len(mac) != 6 {
		return nil, fmt.Errorf("netstack backend requires 6-byte MAC address, got %d", len(mac))
	}
	netstackBackendDbg.Writef("NewNetstackBackend mac=%s", mac.String())

	if err := ns.SetGuestMAC(mac); err != nil {
		return nil, fmt.Errorf("set guest mac: %w", err)
	}

	nic, err := ns.AttachNetworkInterface()
	if err != nil {
		return nil, fmt.Errorf("attach network interface: %w", err)
	}

	return &NetstackBackend{
		ns:  ns,
		nic: nic,
	}, nil
}

func (b *NetstackBackend) HandleTx(packet []byte, release func()) error {
	if b.nic == nil {
		if release != nil {
			release()
		}
		netstackBackendDbg.Writef("HandleTx drop nic=nil len=%d", len(packet))
		return fmt.Errorf("netstack backend is not attached")
	}

	netstackBackendDbg.Writef("HandleTx deliverGuestPacket len=%d", len(packet))
	if err := b.nic.DeliverGuestPacket(packet, release); err != nil {
		if release != nil {
			release()
		}
		netstackBackendDbg.Writef("HandleTx deliverGuestPacket err=%v", err)
		return err
	}
	return nil
}

func (b *NetstackBackend) BindNetDevice(netdev *Net) {
	if b.nic == nil || netdev == nil {
		return
	}
	netstackBackendDbg.Writef("BindNetDevice attach virtio->netstack callback")

	b.nic.AttachVirtioBackend(func(frame []byte) error {
		netstackBackendDbg.Writef("AttachVirtioBackend callback frameLen=%d", len(frame))
		// Avoid synchronous re-entry into virtio-net (netstack can emit frames
		// while we're still processing a guest TX packet). Make this best-effort
		// async to prevent deadlocks.
		copied := append([]byte(nil), frame...)
		go func() { _ = netdev.EnqueueRxPacket(copied) }()
		return nil
	})
}

func (b *NetstackBackend) NetworkInterface() *netstack.NetworkInterface {
	return b.nic
}

func (b *NetstackBackend) OpenPacketCapture(w io.Writer) error {
	if b == nil || b.ns == nil {
		return fmt.Errorf("netstack backend has no netstack")
	}
	return b.ns.OpenPacketCapture(w)
}

var (
	_ NetBackend      = (*NetstackBackend)(nil)
	_ netDeviceBinder = (*NetstackBackend)(nil)
)
