//go:build linux

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
	"j5.nz/cc/internal/amd64vm"
	"j5.nz/cc/internal/netstack"
	"j5.nz/cc/internal/virtio"
)

type linuxNetworkRuntime struct {
	stack     *netstack.NetStack
	dev       *virtio.Net
	mu        sync.Mutex
	listeners []net.Listener
	forwards  map[string]client.PortForward
	wg        sync.WaitGroup
}

type netstackVirtioBackend struct {
	iface *netstack.NetworkInterface
}

func (b *netstackVirtioBackend) HandleTxPacket(packet []byte) error {
	return b.iface.DeliverGuestPacket(packet, true)
}

func newLinuxAMD64NetworkRuntime(cfg *client.NetworkConfig) (*linuxNetworkRuntime, error) {
	if cfg == nil || !cfg.Enabled {
		return nil, nil
	}
	stack := netstack.New(slog.Default())
	stack.SetInternetAccessEnabled(cfg.AllowInternet)
	stack.SetHostDNSName(cfg.HostDNSName)

	mac := net.HardwareAddr{0x02, 0x42, 0x0a, 0x2a, 0x00, 0x02}
	if err := stack.SetGuestMAC(mac); err != nil {
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

	dev := virtio.NewNet(amd64vm.NetBase, amd64vm.NetSize, amd64vm.NetIRQ, mac, &netstackVirtioBackend{iface: iface})
	iface.AttachVirtioBackend(func(frame []byte) error {
		copied := append([]byte(nil), frame...)
		go func() {
			_ = dev.EnqueueRxPacketOwned(copied)
		}()
		return nil
	})

	runtime := &linuxNetworkRuntime{stack: stack, dev: dev}
	for _, forward := range cfg.PortForwards {
		if err := runtime.AddPortForward(forward); err != nil {
			_ = runtime.Close()
			return nil, err
		}
	}

	return runtime, nil
}

func (n *linuxNetworkRuntime) Close() error {
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

func networkDevice(n *linuxNetworkRuntime) *virtio.Net {
	if n == nil {
		return nil
	}
	return n.dev
}

func (n *linuxNetworkRuntime) AddPortForward(forward client.PortForward) error {
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
		guestAddr = "10.42.0.2"
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

func (n *linuxNetworkRuntime) acceptPortForward(ln net.Listener, guestAddress string) {
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

func (n *linuxNetworkRuntime) handlePortForwardConn(hostConn net.Conn, guestAddress string) {
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
