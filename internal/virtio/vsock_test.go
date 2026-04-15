package virtio

import (
	"encoding/binary"
	"fmt"
	"io"
	"testing"
	"time"
)

func TestVsockConfigReportsGuestCID(t *testing.T) {
	v := NewVsock(0x1000, 0x1000, 42, 3, nil)

	if err := v.Write(0x1000+regDeviceFeatSel, 4, 1); err != nil {
		t.Fatalf("Write(device_feature_sel high) error = %v", err)
	}
	high, err := v.Read(0x1000+regDeviceFeatures, 4)
	if err != nil {
		t.Fatalf("Read(device_features high) error = %v", err)
	}
	if high != 1 {
		t.Fatalf("device features high = %#x, want %#x", high, uint64(1))
	}

	lowCID, err := v.Read(0x1000+regConfig, 4)
	if err != nil {
		t.Fatalf("Read(config low) error = %v", err)
	}
	if lowCID != 3 {
		t.Fatalf("config low CID = %d, want 3", lowCID)
	}
}

func TestVsockConnectGuestToHostAndExchangeData(t *testing.T) {
	mem := &testGuestMemory{data: make([]byte, 0x10000)}
	irq := &testIRQController{}
	backend := NewSimpleVsockBackend()
	listener, err := backend.Listen(1024)
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer listener.Close()

	v := NewVsock(0x1000, 0x1000, 42, 3, backend)
	defer v.Close()
	v.Attach(mem, irq)

	setupVsockQueue(t, v, vsockQueueTX, 8, 0x2000, 0x2100, 0x2200)
	setupVsockQueue(t, v, vsockQueueRX, 8, 0x3000, 0x3100, 0x3200)

	const txDataAddr = 0x2300
	connect := encodeVsockHeader(vsockHeader{
		SrcCID:   3,
		DstCID:   VSockCIDHost,
		SrcPort:  5555,
		DstPort:  1024,
		Type:     vsockTypeStream,
		Op:       vsockOpRequest,
		BufAlloc: vsockDefaultBufSize,
	})
	writeReadableDescriptor(mem, 0x2000, txDataAddr, connect)
	writeWritableDescriptor(mem, 0x3000, 0x3300, 4096)
	setAvail(mem, 0x2100, 0, 0)
	setAvail(mem, 0x3100, 0, 0)

	if err := v.Write(0x1000+regQueueNotify, 4, vsockQueueTX); err != nil {
		t.Fatalf("Write(queue_notify tx connect) error = %v", err)
	}

	serverConn, err := listener.Accept()
	if err != nil {
		t.Fatalf("Accept() error = %v", err)
	}
	defer serverConn.Close()

	resp := readUsedRXPacketAt(t, mem, 0x3200, 0x3300, 1)
	respHdr, err := parseVsockHeader(resp)
	if err != nil {
		t.Fatalf("parseVsockHeader(response) error = %v", err)
	}
	if respHdr.Op != vsockOpResponse {
		t.Fatalf("response op = %d, want %d", respHdr.Op, vsockOpResponse)
	}

	writeWritableDescriptor(mem, 0x3010, 0x4300, 4096)
	writeWritableDescriptor(mem, 0x3020, 0x5300, 4096)

	dataPacket := append(encodeVsockHeader(vsockHeader{
		SrcCID:   3,
		DstCID:   VSockCIDHost,
		SrcPort:  5555,
		DstPort:  1024,
		Len:      5,
		Type:     vsockTypeStream,
		Op:       vsockOpRW,
		BufAlloc: vsockDefaultBufSize,
	}), []byte("hello")...)
	writeReadableDescriptor(mem, 0x2010, txDataAddr+0x100, dataPacket)
	setAvail(mem, 0x2100, 1, 1)

	if err := v.Write(0x1000+regQueueNotify, 4, vsockQueueTX); err != nil {
		t.Fatalf("Write(queue_notify tx data) error = %v", err)
	}

	buf := make([]byte, 5)
	if _, err := ioReadFullWithTimeout(serverConn, buf, time.Second); err != nil {
		t.Fatalf("read guest payload: %v", err)
	}
	if string(buf) != "hello" {
		t.Fatalf("server received %q, want %q", string(buf), "hello")
	}

	setAvail(mem, 0x3100, 1, 1)
	setAvail(mem, 0x3100, 2, 2)
	if _, err := serverConn.Write([]byte("world")); err != nil {
		t.Fatalf("server Write() error = %v", err)
	}
	packet := waitForVsockOp(t, mem, 0x3200, map[uint16]uint64{2: 0x4300, 3: 0x5300}, 2, vsockOpRW)
	hdr, err := parseVsockHeader(packet)
	if err != nil {
		t.Fatalf("parseVsockHeader(data) error = %v", err)
	}
	if got := string(packet[vsockHeaderSize : vsockHeaderSize+hdr.Len]); got != "world" {
		t.Fatalf("payload = %q, want %q", got, "world")
	}
}

func setupVsockQueue(t *testing.T, v *Vsock, queueID uint64, size uint16, descAddr, availAddr, usedAddr uint64) {
	t.Helper()
	for _, write := range []struct {
		reg   uint64
		value uint64
	}{
		{regQueueSel, queueID},
		{regQueueNum, uint64(size)},
		{regQueueDescLow, descAddr},
		{regQueueAvailLow, availAddr},
		{regQueueUsedLow, usedAddr},
		{regQueueReady, 1},
	} {
		if err := v.Write(v.Base+write.reg, 4, write.value); err != nil {
			t.Fatalf("setup queue %d reg=%#x error = %v", queueID, write.reg, err)
		}
	}
}

func writeReadableDescriptor(mem *testGuestMemory, descAddr, dataAddr uint64, payload []byte) {
	copy(mem.data[dataAddr:], payload)
	binary.LittleEndian.PutUint64(mem.data[descAddr:descAddr+8], dataAddr)
	binary.LittleEndian.PutUint32(mem.data[descAddr+8:descAddr+12], uint32(len(payload)))
	binary.LittleEndian.PutUint16(mem.data[descAddr+12:descAddr+14], 0)
}

func writeWritableDescriptor(mem *testGuestMemory, descAddr, dataAddr uint64, length uint32) {
	binary.LittleEndian.PutUint64(mem.data[descAddr:descAddr+8], dataAddr)
	binary.LittleEndian.PutUint32(mem.data[descAddr+8:descAddr+12], length)
	binary.LittleEndian.PutUint16(mem.data[descAddr+12:descAddr+14], descFWrite)
}

func setAvail(mem *testGuestMemory, availAddr uint64, slot int, head uint16) {
	binary.LittleEndian.PutUint16(mem.data[availAddr+2:availAddr+4], uint16(slot+1))
	binary.LittleEndian.PutUint16(mem.data[availAddr+4+uint64(slot)*2:availAddr+6+uint64(slot)*2], head)
}

func readUsedRXPacketAt(t *testing.T, mem *testGuestMemory, usedAddr, dataAddr uint64, wantIdx uint16) []byte {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		usedIdx := binary.LittleEndian.Uint16(mem.data[usedAddr+2 : usedAddr+4])
		if usedIdx >= wantIdx {
			elemOff := usedAddr + 4 + uint64(wantIdx-1)*8
			usedLen := binary.LittleEndian.Uint32(mem.data[elemOff+4 : elemOff+8])
			return append([]byte(nil), mem.data[dataAddr:dataAddr+uint64(usedLen)]...)
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("used ring did not reach index %d", wantIdx)
	return nil
}

func waitForVsockOp(t *testing.T, mem *testGuestMemory, usedAddr uint64, dataAddrs map[uint16]uint64, startIdx uint16, op uint16) []byte {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		usedIdx := binary.LittleEndian.Uint16(mem.data[usedAddr+2 : usedAddr+4])
		for idx := startIdx; idx <= usedIdx; idx++ {
			dataAddr, ok := dataAddrs[idx]
			if !ok {
				continue
			}
			packet := readUsedRXPacketAt(t, mem, usedAddr, dataAddr, idx)
			hdr, err := parseVsockHeader(packet)
			if err == nil && hdr.Op == op {
				return packet
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("did not observe vsock op %d", op)
	return nil
}

func ioReadFullWithTimeout(conn VsockConn, buf []byte, timeout time.Duration) (int, error) {
	type readResult struct {
		n   int
		err error
	}
	ch := make(chan readResult, 1)
	go func() {
		n, err := io.ReadFull(conn, buf)
		ch <- readResult{n: n, err: err}
	}()
	select {
	case res := <-ch:
		return res.n, res.err
	case <-time.After(timeout):
		return 0, fmt.Errorf("timed out reading vsock payload")
	}
}
