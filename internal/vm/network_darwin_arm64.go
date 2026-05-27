//go:build darwin && arm64

package vm

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/arm64vm"
	"j5.nz/cc/internal/netstack"
	"j5.nz/cc/internal/virtio"
	"j5.nz/cc/internal/vmruntime"
)

type darwinNetworkRuntime struct {
	ip        net.IP
	mac       net.HardwareAddr
	stack     *netstack.NetStack
	iface     *netstack.NetworkInterface
	dev       *virtio.Net
	mu        sync.Mutex
	listeners []net.Listener
	forwards  map[string]client.PortForward
	wg        sync.WaitGroup
}

type darwinNetstackVirtioBackend struct {
	runtime *darwinNetworkRuntime
}

func (b *darwinNetstackVirtioBackend) HandleTxPacket(packet []byte) error {
	if b == nil || b.runtime == nil || b.runtime.stack == nil {
		return fmt.Errorf("network runtime is not attached")
	}
	return b.runtime.ifaceDeliver(packet)
}

func newDarwinARM64NetworkRuntime(cfg *client.NetworkConfig) (*darwinNetworkRuntime, error) {
	if cfg == nil || !cfg.Enabled {
		return nil, nil
	}
	ip := net.IPv4(10, 42, 0, 2)
	mac := net.HardwareAddr{0x02, 0x42, 0x0a, 0x2a, 0x00, 0x02}
	stack := netstack.New(slog.Default())
	stack.SetInternetAccessEnabled(cfg.AllowInternet)
	stack.SetHostDNSName(cfg.HostDNSName)
	if err := stack.SetGuestMAC(mac); err != nil {
		_ = stack.Close()
		return nil, err
	}
	if err := stack.SetGuestIPv4(ip); err != nil {
		_ = stack.Close()
		return nil, err
	}
	iface, err := stack.AttachNetworkInterface()
	if err != nil {
		_ = stack.Close()
		return nil, err
	}
	if err := stack.StartDNSServer(); err != nil {
		_ = stack.Close()
		return nil, fmt.Errorf("start guest dns server: %w", err)
	}
	runtime := &darwinNetworkRuntime{
		ip:    ip,
		mac:   mac,
		stack: stack,
		iface: iface,
	}
	dev := virtio.NewNet(arm64vm.NetBase, arm64vm.NetSize, arm64vm.NetIRQ, mac, &darwinNetstackVirtioBackend{runtime: runtime})
	runtime.dev = dev
	iface.AttachVirtioBackend(func(frame []byte) error {
		copied := append([]byte(nil), frame...)
		go func() {
			_ = dev.EnqueueRxPacketOwned(copied)
		}()
		return nil
	})
	for _, forward := range cfg.PortForwards {
		if err := runtime.AddPortForward(forward); err != nil {
			_ = runtime.Close()
			return nil, err
		}
	}
	return runtime, nil
}

func (n *darwinNetworkRuntime) guestInitConfig() *vmruntime.GuestNetworkConfig {
	if n == nil {
		return nil
	}
	return &vmruntime.GuestNetworkConfig{
		Interface: "eth0",
		Address:   darwinNetworkGuestCIDR(n),
		Gateway:   "10.42.0.1",
		DNS:       "10.42.0.1",
	}
}

func (n *darwinNetworkRuntime) ifaceDeliver(packet []byte) error {
	if n == nil || n.iface == nil {
		return fmt.Errorf("network interface detached")
	}
	return n.iface.DeliverGuestPacket(packet, true)
}

func (n *darwinNetworkRuntime) Close() error {
	if n == nil {
		return nil
	}
	var err error
	n.mu.Lock()
	listeners := append([]net.Listener(nil), n.listeners...)
	n.listeners = nil
	n.mu.Unlock()
	for _, ln := range listeners {
		if closeErr := ln.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}
	n.wg.Wait()
	if n.stack != nil {
		if stackErr := n.stack.Close(); stackErr != nil && err == nil {
			err = stackErr
		}
	}
	return err
}

func darwinNetworkGuestAddress(n *darwinNetworkRuntime) string {
	if n == nil || n.ip == nil {
		return "10.42.0.2"
	}
	return n.ip.String()
}

func darwinNetworkGuestCIDR(n *darwinNetworkRuntime) string {
	return darwinNetworkGuestAddress(n) + "/24"
}

func (n *darwinNetworkRuntime) AddPortForward(forward client.PortForward) error {
	if n == nil || n.stack == nil {
		return fmt.Errorf("network is not enabled")
	}
	protocol := strings.ToLower(strings.TrimSpace(forward.Protocol))
	if protocol == "" {
		protocol = "tcp"
	}
	if protocol != "tcp" && protocol != "tcp4" {
		return fmt.Errorf("port forward protocol %q is not supported", forward.Protocol)
	}
	if forward.HostPort <= 0 || forward.HostPort > 65535 {
		return fmt.Errorf("host port %d out of range", forward.HostPort)
	}
	if forward.GuestPort <= 0 || forward.GuestPort > 65535 {
		return fmt.Errorf("guest port %d out of range", forward.GuestPort)
	}
	hostAddr := strings.TrimSpace(forward.HostAddr)
	if hostAddr == "" {
		hostAddr = "127.0.0.1"
	}
	guestAddr := strings.TrimSpace(forward.GuestAddr)
	if guestAddr == "" {
		guestAddr = darwinNetworkGuestAddress(n)
	}
	forward.Protocol = protocol
	forward.HostAddr = hostAddr
	forward.GuestAddr = guestAddr
	key := strings.Join([]string{protocol, hostAddr, strconv.Itoa(forward.HostPort), guestAddr, strconv.Itoa(forward.GuestPort)}, "\x00")

	n.mu.Lock()
	if existing, ok := n.forwards[key]; ok {
		n.mu.Unlock()
		if existing == forward {
			return nil
		}
		return fmt.Errorf("port forward already exists")
	}
	n.mu.Unlock()

	ln, err := net.Listen("tcp", net.JoinHostPort(hostAddr, strconv.Itoa(forward.HostPort)))
	if err != nil {
		return fmt.Errorf("listen port forward %s:%d: %w", hostAddr, forward.HostPort, err)
	}
	n.mu.Lock()
	if n.forwards == nil {
		n.forwards = make(map[string]client.PortForward)
	}
	n.forwards[key] = forward
	n.listeners = append(n.listeners, ln)
	n.wg.Add(1)
	n.mu.Unlock()
	go n.acceptPortForward(ln, net.JoinHostPort(guestAddr, strconv.Itoa(forward.GuestPort)))
	return nil
}

func (n *darwinNetworkRuntime) acceptPortForward(ln net.Listener, guestAddress string) {
	defer n.wg.Done()
	for {
		hostConn, err := ln.Accept()
		if err != nil {
			return
		}
		n.wg.Add(1)
		go n.handlePortForwardConn(hostConn, guestAddress)
	}
}

func (n *darwinNetworkRuntime) handlePortForwardConn(hostConn net.Conn, guestAddress string) {
	defer n.wg.Done()
	defer hostConn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	guestConn, err := n.stack.DialInternalContext(ctx, "tcp", guestAddress)
	if err != nil {
		return
	}
	defer guestConn.Close()

	errCh := make(chan error, 2)
	go func() {
		_, err := io.Copy(guestConn, hostConn)
		errCh <- err
	}()
	go func() {
		_, err := io.Copy(hostConn, guestConn)
		errCh <- err
	}()
	<-errCh
}
