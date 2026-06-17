package virtio

import (
	"bytes"
	"encoding/binary"
	"net"
	"testing"
)

type testNetBackend struct {
	packets [][]byte
}

func (b *testNetBackend) HandleTxPacket(packet []byte) error {
	b.packets = append(b.packets, append([]byte(nil), packet...))
	return nil
}

func TestNetLegacyFeaturesAdvertiseMergeRXByDefault(t *testing.T) {
	dev := NewNet(0, 0x1000, 11, nil, nil)

	features, err := dev.ReadLegacy(0, 4)
	if err != nil {
		t.Fatal(err)
	}
	if features&netFeatureMergeRX == 0 {
		t.Fatalf("legacy features are missing mergeable RX: %#x", features)
	}
	if features&netFeatureMAC == 0 || features&netFeatureStatus == 0 {
		t.Fatalf("legacy features missing base net features: %#x", features)
	}
}

func TestNetLegacyFeaturesCanDisableMergeRX(t *testing.T) {
	dev := NewNet(0, 0x1000, 11, nil, nil)
	dev.DisableMergeRX = true

	features, err := dev.ReadLegacy(0, 4)
	if err != nil {
		t.Fatal(err)
	}
	if features&netFeatureMergeRX != 0 {
		t.Fatalf("legacy features include mergeable RX after disabling it: %#x", features)
	}
	if features&netFeatureMAC == 0 || features&netFeatureStatus == 0 {
		t.Fatalf("legacy features missing base net features: %#x", features)
	}
}

func TestNetLegacyTXStripsTenByteHeader(t *testing.T) {
	mem := make(testGuestMemory, 0x20000)
	backend := &testNetBackend{}
	irq := &testIRQ{}
	dev := NewNet(0, 0x1000, 11, net.HardwareAddr{0x02, 0x42, 0x0a, 0x2a, 0x00, 0x02}, backend)
	dev.DisableMergeRX = true
	dev.Attach(mem, irq)

	if err := dev.WriteLegacy(14, 2, netQueueTX); err != nil {
		t.Fatal(err)
	}
	if err := dev.WriteLegacy(8, 4, 0x10); err != nil {
		t.Fatal(err)
	}
	packet := []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x02, 0x42, 0x0a, 0x2a, 0x00, 0x02, 0x08, 0x06}
	copy(mem[0x2000+netHeaderLen:], packet)
	writeDesc(mem, 0x10000, 0x2000, uint32(netHeaderLen+len(packet)), 0, 0)
	binary.LittleEndian.PutUint16(mem[0x10000+16*netQueueSize+2:], 1)
	binary.LittleEndian.PutUint16(mem[0x10000+16*netQueueSize+4:], 0)

	if err := dev.WriteLegacy(16, 2, netQueueTX); err != nil {
		t.Fatal(err)
	}
	if len(backend.packets) != 1 {
		t.Fatalf("packets = %d", len(backend.packets))
	}
	if !bytes.Equal(backend.packets[0], packet) {
		t.Fatalf("packet = %x, want %x", backend.packets[0], packet)
	}
}

func TestNetLegacyRXWritesTenByteHeader(t *testing.T) {
	mem := make(testGuestMemory, 0x20000)
	irq := &testIRQ{}
	dev := NewNet(0, 0x1000, 11, nil, nil)
	dev.DisableMergeRX = true
	dev.Attach(mem, irq)

	if err := dev.WriteLegacy(14, 2, netQueueRX); err != nil {
		t.Fatal(err)
	}
	if err := dev.WriteLegacy(8, 4, 0x10); err != nil {
		t.Fatal(err)
	}
	writeDesc(mem, 0x10000, 0x3000, 2048, descFWrite, 0)
	binary.LittleEndian.PutUint16(mem[0x10000+16*netQueueSize+2:], 1)
	binary.LittleEndian.PutUint16(mem[0x10000+16*netQueueSize+4:], 0)

	packet := []byte{0x02, 0x42, 0x0a, 0x2a, 0x00, 0x02, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x08, 0x00}
	if err := dev.EnqueueRxPacket(packet); err != nil {
		t.Fatal(err)
	}

	if got := binary.LittleEndian.Uint16(mem[0x10000+16*netQueueSize+4096+4:]); got != 0 {
		t.Fatalf("used element id = %d", got)
	}
	usedLen := binary.LittleEndian.Uint32(mem[0x10000+16*netQueueSize+4096+8:])
	if usedLen != uint32(netHeaderLen+len(packet)) {
		t.Fatalf("used len = %d, want %d", usedLen, netHeaderLen+len(packet))
	}
	if !bytes.Equal(mem[0x3000+netHeaderLen:0x3000+netHeaderLen+uint64(len(packet))], packet) {
		t.Fatalf("packet payload was not written after ten-byte header")
	}
}

func TestNetLegacyRXDrainsPendingPacketsWhenQueueIsConfigured(t *testing.T) {
	mem := make(testGuestMemory, 0x20000)
	irq := &testIRQ{}
	dev := NewNet(0, 0x1000, 11, nil, nil)
	dev.DisableMergeRX = true
	dev.Attach(mem, irq)

	packet := []byte{0x02, 0x42, 0x0a, 0x2a, 0x00, 0x02, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x08, 0x00}
	if err := dev.EnqueueRxPacket(packet); err != nil {
		t.Fatal(err)
	}

	if err := dev.WriteLegacy(14, 2, netQueueRX); err != nil {
		t.Fatal(err)
	}
	writeDesc(mem, 0x10000, 0x3000, 2048, descFWrite, 0)
	binary.LittleEndian.PutUint16(mem[0x10000+16*netQueueSize+2:], 1)
	binary.LittleEndian.PutUint16(mem[0x10000+16*netQueueSize+4:], 0)

	if err := dev.WriteLegacy(8, 4, 0x10); err != nil {
		t.Fatal(err)
	}

	usedLen := binary.LittleEndian.Uint32(mem[0x10000+16*netQueueSize+4096+8:])
	if usedLen != uint32(netHeaderLen+len(packet)) {
		t.Fatalf("used len = %d, want %d", usedLen, netHeaderLen+len(packet))
	}
	if !bytes.Equal(mem[0x3000+netHeaderLen:0x3000+netHeaderLen+uint64(len(packet))], packet) {
		t.Fatalf("pending packet payload was not written when queue was configured")
	}
}
