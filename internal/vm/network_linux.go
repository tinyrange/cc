//go:build linux

package vm

import (
	"encoding/binary"
	"net"
	"strings"
	"sync"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/amd64vm"
	"j5.nz/cc/internal/virtio"
)

type linuxNetworkRuntime struct {
	*networkRuntime
	switchNet *linuxVirtualSwitch
}

func newLinuxAMD64NetworkRuntime(id string, cfg *client.NetworkConfig) (*linuxNetworkRuntime, error) {
	if cfg == nil || !cfg.Enabled {
		return nil, nil
	}
	lease := defaultLinuxVirtualSwitch.Register(id)
	runtime := &linuxNetworkRuntime{switchNet: defaultLinuxVirtualSwitch}
	common, err := newNetworkRuntime(networkDeviceConfig{
		ID:     lease.id,
		Config: cfg,
		IP:     lease.ip,
		MAC:    lease.mac,
		Base:   amd64vm.NetBase,
		Size:   amd64vm.NetSize,
		IRQ:    amd64vm.NetIRQ,
		TXHook: func(packet []byte) {
			defaultLinuxVirtualSwitch.Forward(runtime, packet)
		},
		Cleanup: func() {
			defaultLinuxVirtualSwitch.Unregister(lease.id)
		},
	})
	if err != nil {
		return nil, err
	}
	runtime.networkRuntime = common
	defaultLinuxVirtualSwitch.Attach(runtime)
	return runtime, nil
}

func (n *linuxNetworkRuntime) Close() error {
	if n == nil {
		return nil
	}
	if n.switchNet != nil {
		n.switchNet.Unregister(n.id)
		n.switchNet = nil
	}
	if n.networkRuntime == nil {
		return nil
	}
	return n.networkRuntime.Close()
}

func networkDevice(n *linuxNetworkRuntime) *virtio.Net {
	if n == nil {
		return nil
	}
	return n.Device()
}

func networkGuestAddress(n *linuxNetworkRuntime) string {
	if n == nil {
		return (&networkRuntime{}).GuestAddress()
	}
	return n.GuestAddress()
}

func networkGuestCIDR(n *linuxNetworkRuntime) string {
	if n == nil {
		return (&networkRuntime{}).GuestCIDR()
	}
	return n.GuestCIDR()
}

type linuxVirtualSwitch struct {
	mu        sync.Mutex
	leases    map[string]linuxNetworkLease
	endpoints map[string]*linuxNetworkRuntime
}

type linuxNetworkLease struct {
	id  string
	ip  net.IP
	mac net.HardwareAddr
}

var defaultLinuxVirtualSwitch = &linuxVirtualSwitch{
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
		if endpoint != nil && endpoint.ip != nil {
			if ip4 := endpoint.ip.To4(); ip4 != nil {
				used[ip4[3]] = true
			}
		}
	}
	host := byte(2)
	for ; host <= 254; host++ {
		if !used[host] {
			break
		}
	}
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
