package virtio

import (
	"bytes"
	"context"
	"encoding/binary"
	"net"
	"testing"

	"github.com/tinyrange/cc/internal/hv"
)

const (
	testNetBase = NetDefaultMMIOBase
	testNetSize = 0x200
)

type netBackendStub struct {
	packets [][]byte
}

func (n *netBackendStub) HandleTx(packet []byte, release func()) error {
	n.packets = append(n.packets, append([]byte(nil), packet...))
	if release != nil {
		release()
	}
	return nil
}

// mockVM implements hv.VirtualMachine for testing
type mockVM struct {
	mem  []byte
	base uint64
}

// SetIRQ implements [hv.VirtualMachine].
func (m *mockVM) SetIRQ(irqLine uint32, level bool) error {
	panic("unimplemented")
}

func newMockVM() *mockVM {
	return &mockVM{
		mem:  make([]byte, 0x1000000), // 16MB
		base: 0,
	}
}

func (m *mockVM) ReadAt(p []byte, off int64) (int, error) {
	idx := int(off - int64(m.base))
	if idx < 0 || idx >= len(m.mem) {
		return 0, nil
	}
	if idx+len(p) > len(m.mem) {
		p = p[:len(m.mem)-idx]
	}
	return copy(p, m.mem[idx:]), nil
}

func (m *mockVM) WriteAt(p []byte, off int64) (int, error) {
	idx := int(off - int64(m.base))
	if idx < 0 {
		return 0, nil
	}
	if idx >= len(m.mem) {
		return 0, nil
	}
	if idx+len(p) > len(m.mem) {
		p = p[:len(m.mem)-idx]
	}
	return copy(m.mem[idx:], p), nil
}

func (m *mockVM) Close() error {
	return nil
}

func (m *mockVM) Hypervisor() hv.Hypervisor {
	return nil
}

func (m *mockVM) MemorySize() uint64 {
	return uint64(len(m.mem))
}

func (m *mockVM) MemoryBase() uint64 {
	return m.base
}

func (m *mockVM) Run(ctx context.Context, cfg hv.RunConfig) error {
	return nil
}

func (m *mockVM) VirtualCPUCall(id int, f func(vcpu hv.VirtualCPU) error) error {
	return nil
}

func (m *mockVM) AddDevice(dev hv.Device) error {
	return nil
}

func (m *mockVM) AddDeviceFromTemplate(template hv.DeviceTemplate) error {
	return nil
}

func (m *mockVM) AllocateMemory(physAddr, size uint64) (hv.MemoryRegion, error) {
	return nil, nil
}

func (m *mockVM) CaptureSnapshot() (hv.Snapshot, error) {
	return nil, nil
}

func (m *mockVM) RestoreSnapshot(snap hv.Snapshot) error {
	return nil
}

// Helper function to read 32-bit value from MMIO
func mmioRead32(t *testing.T, dev *Net, base uint64, offset uint64) uint32 {
	var data [4]byte
	err := dev.ReadMMIO(base+offset, data[:])
	if err != nil {
		t.Fatalf("MMIO read failed: %v", err)
	}
	return binary.LittleEndian.Uint32(data[:])
}

// Helper function to write 32-bit value to MMIO
func mmioWrite32(t *testing.T, dev *Net, base uint64, offset uint64, value uint32) {
	var data [4]byte
	binary.LittleEndian.PutUint32(data[:], value)
	err := dev.WriteMMIO(base+offset, data[:])
	if err != nil {
		t.Fatalf("MMIO write failed: %v", err)
	}
}

func TestNetIdentification(t *testing.T) {
	vm := newMockVM()
	mac, _ := net.ParseMAC("02:00:00:00:00:01")
	netdev := NewNet(vm, testNetBase, testNetSize, NetDefaultIRQLine, mac, &netBackendStub{})

	if got := mmioRead32(t, netdev, testNetBase, VIRTIO_MMIO_MAGIC_VALUE); got != 0x74726976 {
		t.Fatalf("magic value = %#x, want %#x", got, 0x74726976)
	}
	if got := mmioRead32(t, netdev, testNetBase, VIRTIO_MMIO_VERSION); got != netVersion {
		t.Fatalf("version = %#x, want %#x", got, netVersion)
	}
	if got := mmioRead32(t, netdev, testNetBase, VIRTIO_MMIO_DEVICE_ID); got != netDeviceID {
		t.Fatalf("device id = %#x, want %#x", got, netDeviceID)
	}
	if got := mmioRead32(t, netdev, testNetBase, VIRTIO_MMIO_VENDOR_ID); got == 0 {
		t.Fatalf("vendor id = %#x, want non-zero", got)
	}
}

func TestNetBackend(t *testing.T) {
	backend := &netBackendStub{}
	mac, _ := net.ParseMAC("02:00:00:00:00:02")
	vm := newMockVM()
	netdev := NewNet(vm, testNetBase, testNetSize, NetDefaultIRQLine, mac, backend)

	// Test that backend is properly set
	if netdev.backend != backend {
		t.Fatalf("backend not properly set")
	}

	// Test MAC address
	if !bytes.Equal(netdev.mac, mac) {
		t.Fatalf("MAC address mismatch")
	}
}
