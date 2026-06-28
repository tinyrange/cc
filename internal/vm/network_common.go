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
	"j5.nz/cc/internal/netstack"
	"j5.nz/cc/internal/virtio"
	"j5.nz/cc/internal/vmruntime"
)

const defaultGatewayMAC = "02:42:0a:2a:00:01"

type networkRuntime struct {
	id        string
	ip        net.IP
	mac       net.HardwareAddr
	stack     *netstack.NetStack
	iface     *netstack.NetworkInterface
	dev       *virtio.Net
	txHook    func([]byte)
	mu        sync.Mutex
	listeners []net.Listener
	forwards  map[string]client.PortForward
	wg        sync.WaitGroup
}

type netstackVirtioBackend struct {
	runtime *networkRuntime
}

type networkDeviceConfig struct {
	ID      string
	Config  *client.NetworkConfig
	IP      net.IP
	MAC     net.HardwareAddr
	Base    uint64
	Size    uint64
	IRQ     uint32
	TXHook  func([]byte)
	RXHook  func([]byte) error
	Cleanup func()
}

func newNetworkRuntime(cfg networkDeviceConfig) (_ *networkRuntime, retErr error) {
	if cfg.Config == nil || !cfg.Config.Enabled {
		return nil, nil
	}
	if cfg.IP == nil {
		if ip := net.ParseIP(strings.TrimSpace(cfg.Config.GuestIPv4)).To4(); ip != nil {
			cfg.IP = ip
		}
	}
	if cfg.IP == nil {
		cfg.IP = net.IPv4(10, 42, 0, 2)
	}
	if cfg.MAC == nil {
		if macText := strings.TrimSpace(cfg.Config.GuestMAC); macText != "" {
			mac, err := net.ParseMAC(macText)
			if err != nil {
				return nil, fmt.Errorf("parse guest network MAC %q: %w", macText, err)
			}
			cfg.MAC = mac
		}
	}
	if cfg.MAC == nil {
		cfg.MAC = net.HardwareAddr{0x02, 0x42, 0x0a, 0x2a, 0x00, 0x02}
	}
	cleanup := func() {
		if cfg.Cleanup != nil {
			cfg.Cleanup()
		}
	}
	defer func() {
		if retErr != nil {
			cleanup()
		}
	}()

	stack := netstack.New(slog.Default())
	stack.SetInternetAccessEnabled(cfg.Config.AllowInternet)
	stack.SetHostAccessEnabled(!cfg.Config.BlockHostAccess)
	stack.SetAllowedServiceProxyPorts(cfg.Config.AllowedServiceProxyPorts)
	stack.SetHostDNSName(cfg.Config.HostDNSName)
	if err := stack.SetGuestMAC(cfg.MAC); err != nil {
		_ = stack.Close()
		return nil, err
	}
	if err := stack.SetGuestIPv4(cfg.IP); err != nil {
		_ = stack.Close()
		return nil, err
	}
	gatewayMAC, err := net.ParseMAC(defaultGatewayMAC)
	if err != nil {
		_ = stack.Close()
		return nil, err
	}
	if err := stack.SetHostMAC(gatewayMAC); err != nil {
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

	runtime := &networkRuntime{
		id:     cfg.ID,
		ip:     cfg.IP,
		mac:    cfg.MAC,
		stack:  stack,
		iface:  iface,
		txHook: cfg.TXHook,
	}
	dev := virtio.NewNet(cfg.Base, cfg.Size, cfg.IRQ, cfg.MAC, &netstackVirtioBackend{runtime: runtime})
	runtime.dev = dev
	rxHook := cfg.RXHook
	if rxHook == nil {
		rxHook = func(frame []byte) error {
			copied := append([]byte(nil), frame...)
			go func() {
				_ = dev.EnqueueRxPacketOwned(copied)
			}()
			return nil
		}
	}
	iface.AttachVirtioBackend(func(frame []byte) error {
		return rxHook(frame)
	})

	for _, forward := range cfg.Config.PortForwards {
		if err := runtime.AddPortForward(forward); err != nil {
			_ = runtime.Close()
			return nil, err
		}
	}
	return runtime, nil
}

func (b *netstackVirtioBackend) HandleTxPacket(packet []byte) error {
	if b == nil || b.runtime == nil || b.runtime.stack == nil {
		return fmt.Errorf("network runtime is not attached")
	}
	if b.runtime.txHook != nil {
		b.runtime.txHook(packet)
	}
	return b.runtime.ifaceDeliver(packet)
}

func (n *networkRuntime) ifaceDeliver(packet []byte) error {
	if n == nil || n.iface == nil {
		return fmt.Errorf("network interface detached")
	}
	return n.iface.DeliverGuestPacket(packet, true)
}

func (n *networkRuntime) Close() error {
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

func (n *networkRuntime) Device() *virtio.Net {
	if n == nil {
		return nil
	}
	return n.dev
}

func (n *networkRuntime) GuestAddress() string {
	if n == nil || n.ip == nil {
		return "10.42.0.2"
	}
	return n.ip.String()
}

func (n *networkRuntime) GuestCIDR() string {
	return n.GuestAddress() + "/24"
}

func (n *networkRuntime) GuestInitConfig() *vmruntime.GuestNetworkConfig {
	if n == nil {
		return nil
	}
	return &vmruntime.GuestNetworkConfig{
		Interface: "eth0",
		Address:   n.GuestCIDR(),
		Gateway:   "10.42.0.1",
		DNS:       "10.42.0.1",
	}
}

func (n *networkRuntime) AddPortForward(forward client.PortForward) error {
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
		guestAddr = n.GuestAddress()
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

func (n *networkRuntime) AllowServiceProxyPort(port int) error {
	if n == nil || n.stack == nil {
		return fmt.Errorf("network is not enabled")
	}
	if port <= 0 || port > 65535 {
		return fmt.Errorf("service proxy port %d out of range", port)
	}
	n.stack.AllowServiceProxyPort(port)
	return nil
}

func (n *networkRuntime) acceptPortForward(ln net.Listener, guestAddress string) {
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

func (n *networkRuntime) handlePortForwardConn(hostConn net.Conn, guestAddress string) {
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
