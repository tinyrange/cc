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

func (b *testNetBackend) HandleTxPacket(packet []byte, release func()) error {
	b.packets = append(b.packets, append([]byte(nil), packet...))
	if release != nil {
		release()
	}
	return nil
}

func TestNetDeviceFeaturesAndConfig(t *testing.T) {
	mac := net.HardwareAddr{0x02, 0x42, 0x0a, 0x2a, 0x00, 0x02}
	n := NewNet(0x1000, 0x1000, 41, mac, nil)

	id, err := n.Read(0x1000+regDeviceID, 4)
	if err != nil {
		t.Fatalf("Read(device_id) error = %v", err)
	}
	if id != mmioDeviceIDNet {
		t.Fatalf("device id = %d, want %d", id, mmioDeviceIDNet)
	}

	low, err := n.Read(0x1000+regDeviceFeatures, 4)
	if err != nil {
		t.Fatalf("Read(device_features low) error = %v", err)
	}
	wantLow := netFeatureMAC | netFeatureStatus | netFeatureMergeRX
	if low != wantLow {
		t.Fatalf("features low = %#x, want %#x", low, wantLow)
	}

	if err := n.Write(0x1000+regDeviceFeatSel, 4, 1); err != nil {
		t.Fatalf("Write(device_feature_sel high) error = %v", err)
	}
	high, err := n.Read(0x1000+regDeviceFeatures, 4)
	if err != nil {
		t.Fatalf("Read(device_features high) error = %v", err)
	}
	if high != 1 {
		t.Fatalf("features high = %#x, want 1", high)
	}

	cfg0, err := n.Read(0x1000+regConfig, 4)
	if err != nil {
		t.Fatalf("Read(config mac low) error = %v", err)
	}
	if cfg0 != 0x2a0a4202 {
		t.Fatalf("config mac low = %#x", cfg0)
	}
	cfg1, err := n.Read(0x1000+regConfig+4, 4)
	if err != nil {
		t.Fatalf("Read(config mac high/status) error = %v", err)
	}
	if cfg1 != 0x00010200 {
		t.Fatalf("config mac high/status = %#x", cfg1)
	}
}

func TestNetTransmitQueueDeliversPacketAndRaisesIRQ(t *testing.T) {
	mem := &testGuestMemory{data: make([]byte, 0x8000)}
	irq := &testIRQController{}
	backend := &testNetBackend{}
	n := NewNet(0x1000, 0x1000, 41, nil, backend)
	n.Attach(mem, irq)

	const (
		descAddr   = 0x2000
		availAddr  = 0x2100
		usedAddr   = 0x2200
		headerAddr = 0x2300
		packetAddr = 0x2400
	)

	packet := []byte{
		0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
		0x02, 0x42, 0x0a, 0x2a, 0x00, 0x02,
		0x08, 0x06,
		1, 2, 3, 4,
	}
	copy(mem.data[packetAddr:], packet)

	binary.LittleEndian.PutUint64(mem.data[descAddr:descAddr+8], headerAddr)
	binary.LittleEndian.PutUint32(mem.data[descAddr+8:descAddr+12], netHeaderLen)
	binary.LittleEndian.PutUint16(mem.data[descAddr+12:descAddr+14], descFNext)
	binary.LittleEndian.PutUint16(mem.data[descAddr+14:descAddr+16], 1)
	binary.LittleEndian.PutUint64(mem.data[descAddr+16:descAddr+24], packetAddr)
	binary.LittleEndian.PutUint32(mem.data[descAddr+24:descAddr+28], uint32(len(packet)))
	binary.LittleEndian.PutUint16(mem.data[descAddr+28:descAddr+30], 0)

	binary.LittleEndian.PutUint16(mem.data[availAddr+2:availAddr+4], 1)
	binary.LittleEndian.PutUint16(mem.data[availAddr+4:availAddr+6], 0)

	configureNetQueue(t, n, netQueueTX, descAddr, availAddr, usedAddr)
	if err := n.Write(0x1000+regQueueNotify, 4, netQueueTX); err != nil {
		t.Fatalf("notify tx: %v", err)
	}

	if len(backend.packets) != 1 {
		t.Fatalf("backend packets = %d, want 1", len(backend.packets))
	}
	if !bytes.Equal(backend.packets[0], packet) {
		t.Fatalf("packet = %x, want %x", backend.packets[0], packet)
	}
	if irq.calls == 0 || !irq.level || irq.irq != 41 {
		t.Fatalf("irq state = irq=%d level=%v calls=%d, want asserted", irq.irq, irq.level, irq.calls)
	}
	if usedIdx := binary.LittleEndian.Uint16(mem.data[usedAddr+2 : usedAddr+4]); usedIdx != 1 {
		t.Fatalf("used idx = %d, want 1", usedIdx)
	}
	if usedLen := binary.LittleEndian.Uint32(mem.data[usedAddr+8 : usedAddr+12]); usedLen != 0 {
		t.Fatalf("used len = %d, want 0", usedLen)
	}
}

func TestNetReceiveQueueWritesHeaderPacketAndRaisesIRQ(t *testing.T) {
	mem := &testGuestMemory{data: make([]byte, 0x8000)}
	irq := &testIRQController{}
	n := NewNet(0x1000, 0x1000, 41, nil, nil)
	n.Attach(mem, irq)

	const (
		descAddr  = 0x3000
		availAddr = 0x3100
		usedAddr  = 0x3200
		dataAddr  = 0x3300
	)
	binary.LittleEndian.PutUint64(mem.data[descAddr:descAddr+8], dataAddr)
	binary.LittleEndian.PutUint32(mem.data[descAddr+8:descAddr+12], 256)
	binary.LittleEndian.PutUint16(mem.data[descAddr+12:descAddr+14], descFWrite)
	binary.LittleEndian.PutUint16(mem.data[availAddr+2:availAddr+4], 1)
	binary.LittleEndian.PutUint16(mem.data[availAddr+4:availAddr+6], 0)

	configureNetQueue(t, n, netQueueRX, descAddr, availAddr, usedAddr)

	packet := []byte{
		0x02, 0x42, 0x0a, 0x2a, 0x00, 0x02,
		0x0a, 0x42, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x00,
		5, 6, 7, 8,
	}
	if err := n.EnqueueRxPacket(packet); err != nil {
		t.Fatalf("enqueue rx: %v", err)
	}

	if gotNumBuffers := binary.LittleEndian.Uint16(mem.data[dataAddr+10 : dataAddr+12]); gotNumBuffers != 1 {
		t.Fatalf("num_buffers = %d, want 1", gotNumBuffers)
	}
	if got := mem.data[dataAddr+netHeaderLen : dataAddr+netHeaderLen+uint64(len(packet))]; !bytes.Equal(got, packet) {
		t.Fatalf("rx packet = %x, want %x", got, packet)
	}
	if usedLen := binary.LittleEndian.Uint32(mem.data[usedAddr+8 : usedAddr+12]); usedLen != uint32(netHeaderLen+len(packet)) {
		t.Fatalf("used len = %d, want %d", usedLen, netHeaderLen+len(packet))
	}
	if irq.calls == 0 || !irq.level || irq.irq != 41 {
		t.Fatalf("irq state = irq=%d level=%v calls=%d, want asserted", irq.irq, irq.level, irq.calls)
	}
}

func configureNetQueue(t testing.TB, n *Net, queue uint64, descAddr, availAddr, usedAddr uint64) {
	t.Helper()
	for _, write := range []struct {
		reg   uint64
		value uint64
	}{
		{regQueueSel, queue},
		{regQueueNum, 8},
		{regQueueDescLow, descAddr},
		{regQueueAvailLow, availAddr},
		{regQueueUsedLow, usedAddr},
		{regQueueReady, 1},
	} {
		if err := n.Write(0x1000+write.reg, 4, write.value); err != nil {
			t.Fatalf("Write(reg=%#x) error = %v", write.reg, err)
		}
	}
}
