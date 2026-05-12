//go:build linux

package vm

import (
	"context"
	"encoding/binary"
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
	id        string
	ip        net.IP
	mac       net.HardwareAddr
	stack     *netstack.NetStack
	iface     *netstack.NetworkInterface
	dev       *virtio.Net
	switchNet *linuxVirtualSwitch
	mu        sync.Mutex
	listeners []net.Listener
	forwards  map[string]client.PortForward
	wg        sync.WaitGroup
}

type netstackVirtioBackend struct {
	runtime *linuxNetworkRuntime
}

func (b *netstackVirtioBackend) HandleTxPacket(packet []byte) error {
	if b == nil || b.runtime == nil || b.runtime.stack == nil {
		return fmt.Errorf("network runtime is not attached")
	}
	if b.runtime.switchNet != nil {
		b.runtime.switchNet.Forward(b.runtime, packet)
	}
	return b.runtime.ifaceDeliver(packet)
}

func newLinuxAMD64NetworkRuntime(id string, cfg *client.NetworkConfig) (*linuxNetworkRuntime, error) {
	if cfg == nil || !cfg.Enabled {
		return nil, nil
	}
	lease := defaultLinuxVirtualSwitch.Register(id)
	stack := netstack.New(slog.Default())
	stack.SetInternetAccessEnabled(cfg.AllowInternet)
	stack.SetHostDNSName(cfg.HostDNSName)

	if err := stack.SetGuestMAC(lease.mac); err != nil {
		defaultLinuxVirtualSwitch.Unregister(lease.id)
		_ = stack.Close()
		return nil, err
	}
	if err := stack.SetGuestIPv4(lease.ip); err != nil {
		defaultLinuxVirtualSwitch.Unregister(lease.id)
		_ = stack.Close()
		return nil, err
	}
	iface, err := stack.AttachNetworkInterface()
	if err != nil {
		defaultLinuxVirtualSwitch.Unregister(lease.id)
		_ = stack.Close()
		return nil, err
	}
	if err := stack.StartDNSServer(); err != nil {
		defaultLinuxVirtualSwitch.Unregister(lease.id)
		_ = stack.Close()
		return nil, fmt.Errorf("start guest dns server: %w", err)
	}

	runtime := &linuxNetworkRuntime{
		id:        lease.id,
		ip:        lease.ip,
		mac:       lease.mac,
		stack:     stack,
		iface:     iface,
		switchNet: defaultLinuxVirtualSwitch,
	}
	defaultLinuxVirtualSwitch.Attach(runtime)
	dev := virtio.NewNet(amd64vm.NetBase, amd64vm.NetSize, amd64vm.NetIRQ, lease.mac, &netstackVirtioBackend{runtime: runtime})
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

func (n *linuxNetworkRuntime) ifaceDeliver(packet []byte) error {
	if n == nil || n.iface == nil {
		return fmt.Errorf("network interface detached")
	}
	return n.iface.DeliverGuestPacket(packet, true)
}

func (n *linuxNetworkRuntime) Close() error {
	if n == nil {
		return nil
	}
	if n.switchNet != nil {
		n.switchNet.Unregister(n.id)
		n.switchNet = nil
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

func networkGuestAddress(n *linuxNetworkRuntime) string {
	if n == nil || n.ip == nil {
		return "10.42.0.2"
	}
	return n.ip.String()
}

func networkGuestCIDR(n *linuxNetworkRuntime) string {
	return networkGuestAddress(n) + "/24"
}

type linuxVirtualSwitch struct {
	mu        sync.Mutex
	nextHost  byte
	leases    map[string]linuxNetworkLease
	endpoints map[string]*linuxNetworkRuntime
}

type linuxNetworkLease struct {
	id  string
	ip  net.IP
	mac net.HardwareAddr
}

var defaultLinuxVirtualSwitch = &linuxVirtualSwitch{
	nextHost:  2,
	leases:    make(map[string]linuxNetworkLease),
	endpoints: make(map[string]*linuxNetworkRuntime),
}

func (s *linuxVirtualSwitch) Register(id string) linuxNetworkLease {
	if s == nil {
		return linuxNetworkLease{id: id, ip: net.IPv4(10, 42, 0, 2), mac: net.HardwareAddr{0x02, 0x42, 0x0a, 0x2a, 0x00, 0x02}}
	}
	id = strings.TrimSpace(id)
	if id == "" {
		id = DefaultInstanceID
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	used := map[byte]bool{1: true}
	for _, lease := range s.leases {
		if ip4 := lease.ip.To4(); ip4 != nil {
			used[ip4[3]] = true
		}
	}
	for _, endpoint := range s.endpoints {
		if len(endpoint.ip) >= net.IPv4len {
			ip4 := endpoint.ip.To4()
			if ip4 != nil {
				used[ip4[3]] = true
			}
		}
	}
	host := s.nextHost
	for i := 0; i < 253; i++ {
		if host < 2 || host > 254 {
			host = 2
		}
		if !used[host] {
			break
		}
		host++
	}
	s.nextHost = host + 1
	lease := linuxNetworkLease{
		id:  id,
		ip:  net.IPv4(10, 42, 0, host),
		mac: net.HardwareAddr{0x02, 0x42, 0x0a, 0x2a, 0x00, host},
	}
	if s.leases == nil {
		s.leases = make(map[string]linuxNetworkLease)
	}
	s.leases[id] = lease
	return lease
}

func (s *linuxVirtualSwitch) Attach(endpoint *linuxNetworkRuntime) {
	if s == nil || endpoint == nil {
		return
	}
	s.mu.Lock()
	if s.endpoints == nil {
		s.endpoints = make(map[string]*linuxNetworkRuntime)
	}
	delete(s.leases, endpoint.id)
	s.endpoints[endpoint.id] = endpoint
	s.mu.Unlock()
}

func (s *linuxVirtualSwitch) Unregister(id string) {
	if s == nil {
		return
	}
	id = strings.TrimSpace(id)
	if id == "" {
		id = DefaultInstanceID
	}
	s.mu.Lock()
	delete(s.leases, id)
	delete(s.endpoints, id)
	s.mu.Unlock()
}

func (s *linuxVirtualSwitch) Forward(source *linuxNetworkRuntime, frame []byte) {
	if s == nil || source == nil || len(frame) < 14 {
		return
	}
	dst := append(net.HardwareAddr(nil), frame[0:6]...)
	src := append(net.HardwareAddr(nil), frame[6:12]...)
	ethType := binary.BigEndian.Uint16(frame[12:14])
	if ethType == 0x0806 {
		s.forwardARP(source, frame)
		return
	}
	if isLinuxBroadcastMAC(dst) || isLinuxMulticastMAC(dst) {
		s.forwardToAll(source.id, frame)
		return
	}
	if bytesEqualMAC(dst, src) {
		return
	}
	s.forwardToMAC(source.id, dst, frame)
}

func (s *linuxVirtualSwitch) forwardARP(source *linuxNetworkRuntime, frame []byte) {
	if len(frame) < 42 {
		return
	}
	targetIP := net.IP(frame[38:42]).To4()
	if targetIP == nil {
		return
	}
	target := s.endpointByIP(source.id, targetIP)
	if target == nil {
		s.forwardToAll(source.id, frame)
		return
	}
	target.enqueueSwitchFrame(frame)
}

func (s *linuxVirtualSwitch) endpointByIP(sourceID string, ip net.IP) *linuxNetworkRuntime {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, endpoint := range s.endpoints {
		if id == sourceID || endpoint == nil {
			continue
		}
		if endpoint.ip.To4() != nil && endpoint.ip.Equal(ip) {
			return endpoint
		}
	}
	return nil
}

func (s *linuxVirtualSwitch) forwardToMAC(sourceID string, mac net.HardwareAddr, frame []byte) {
	s.mu.Lock()
	var target *linuxNetworkRuntime
	for id, endpoint := range s.endpoints {
		if id == sourceID || endpoint == nil {
			continue
		}
		if bytesEqualMAC(endpoint.mac, mac) {
			target = endpoint
			break
		}
	}
	s.mu.Unlock()
	if target != nil {
		target.enqueueSwitchFrame(frame)
	}
}

func (s *linuxVirtualSwitch) forwardToAll(sourceID string, frame []byte) {
	s.mu.Lock()
	targets := make([]*linuxNetworkRuntime, 0, len(s.endpoints))
	for id, endpoint := range s.endpoints {
		if id != sourceID && endpoint != nil {
			targets = append(targets, endpoint)
		}
	}
	s.mu.Unlock()
	for _, target := range targets {
		target.enqueueSwitchFrame(frame)
	}
}

func (n *linuxNetworkRuntime) enqueueSwitchFrame(frame []byte) {
	if n == nil || n.dev == nil {
		return
	}
	copied := append([]byte(nil), frame...)
	go func() {
		_ = n.dev.EnqueueRxPacketOwned(copied)
	}()
}

func isLinuxBroadcastMAC(mac net.HardwareAddr) bool {
	if len(mac) != 6 {
		return false
	}
	for _, b := range mac {
		if b != 0xff {
			return false
		}
	}
	return true
}

func isLinuxMulticastMAC(mac net.HardwareAddr) bool {
	return len(mac) == 6 && mac[0]&1 == 1
}

func bytesEqualMAC(a, b net.HardwareAddr) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
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
		guestAddr = networkGuestAddress(n)
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
