package netstack

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/tinyrange/cc/internal/pcap"
)

var (
	testGuestMAC = net.HardwareAddr{0x02, 0x00, 0x00, 0x00, 0x00, 0x02}
	testHostMAC  = net.HardwareAddr{0x0a, 0x42, 0x00, 0x00, 0x00, 0x01}
)

func newTestNetStack(tb testing.TB, frameBufferSize int) (*NetStack, *NetworkInterface, chan []byte) {
	tb.Helper()

	stack := New(slog.Default())
	if err := stack.SetGuestMAC(testGuestMAC); err != nil {
		tb.Fatalf("set guest mac: %v", err)
	}

	iface, err := stack.AttachNetworkInterface()
	if err != nil {
		tb.Fatalf("attach network interface: %v", err)
	}

	nic := iface
	if val, ok := macToUint64(testHostMAC); ok {
		stack.hostMAC.Store(uint64(val))
	} else {
		tb.Fatalf("invalid test host mac: %v", testHostMAC)
	}

	frames := make(chan []byte, frameBufferSize)
	nic.AttachVirtioBackend(func(frame []byte) error {
		out := append([]byte(nil), frame...)
		select {
		case frames <- out:
		default:
			tb.Fatalf("virtio backend channel full")
		}
		return nil
	})

	tb.Cleanup(func() {
		close(frames)
		_ = stack.Close()
	})

	return stack, nic, frames
}

func awaitFrame(t testing.TB, frames <-chan []byte) []byte {
	t.Helper()
	select {
	case frame, ok := <-frames:
		if !ok {
			t.Fatalf("frame channel closed")
		}
		return frame
	case <-time.After(time.Second):
		t.Fatalf("timeout waiting for frame")
		return nil
	}
}

func parseEthernet(frame []byte) (net.HardwareAddr, net.HardwareAddr, uint16, []byte) {
	dst := net.HardwareAddr(frame[0:6])
	src := net.HardwareAddr(frame[6:12])
	etherType := binary.BigEndian.Uint16(frame[12:14])
	return dst, src, etherType, frame[14:]
}

func buildARPRequest(guestIP, targetIP net.IP) []byte {
	frame := make([]byte, 14+28)
	copy(frame[0:6], []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff})
	copy(frame[6:12], testGuestMAC)
	binary.BigEndian.PutUint16(frame[12:14], uint16(etherTypeARP))

	payload := frame[14:]
	binary.BigEndian.PutUint16(payload[0:2], arpHardwareEthernet)
	binary.BigEndian.PutUint16(payload[2:4], arpProtoIPv4)
	payload[4] = 6
	payload[5] = 4
	binary.BigEndian.PutUint16(payload[6:8], 1)
	copy(payload[8:14], testGuestMAC)
	copy(payload[14:18], guestIP.To4())
	copy(payload[18:24], []byte{0, 0, 0, 0, 0, 0})
	copy(payload[24:28], targetIP.To4())
	return frame
}

func buildICMPEchoRequest(hostIP, guestIP net.IP) []byte {
	payload := []byte("payload")
	icmp := make([]byte, 8+len(payload))
	icmp[0] = 8 // echo request
	icmp[1] = 0
	copy(icmp[8:], payload)
	binary.BigEndian.PutUint16(icmp[2:4], 0)
	binary.BigEndian.PutUint16(icmp[4:6], 0x1234)
	binary.BigEndian.PutUint16(icmp[6:8], 1)
	sum := checksum(icmp)
	binary.BigEndian.PutUint16(icmp[2:4], sum)

	ip := buildIPv4Packet(guestIP, hostIP, icmpProtocol, icmp)
	frame := make([]byte, 14+len(ip))
	copy(frame[0:6], testHostMAC)
	copy(frame[6:12], testGuestMAC)
	binary.BigEndian.PutUint16(frame[12:14], uint16(etherTypeIPv4))
	copy(frame[14:], ip)
	return frame
}

func buildUDPFrame(hostIP, guestIP net.IP, hostPort, guestPort uint16, payload []byte) []byte {
	udp := make([]byte, 8+len(payload))
	binary.BigEndian.PutUint16(udp[0:2], guestPort)
	binary.BigEndian.PutUint16(udp[2:4], hostPort)
	binary.BigEndian.PutUint16(udp[4:6], uint16(len(udp)))
	copy(udp[8:], payload)
	// No checksum (optional for IPv4).

	ip := buildIPv4Packet(guestIP, hostIP, udpProtocolNumber, udp)
	frame := make([]byte, 14+len(ip))
	copy(frame[0:6], testHostMAC)
	copy(frame[6:12], testGuestMAC)
	binary.BigEndian.PutUint16(frame[12:14], uint16(etherTypeIPv4))
	copy(frame[14:], ip)
	return frame
}

func buildTCPFrame(hostIP, guestIP net.IP, hostPort, guestPort uint16, seq, ack uint32, flags uint16, payload []byte) []byte {
	headerLen := 20
	packet := make([]byte, headerLen+len(payload))
	binary.BigEndian.PutUint16(packet[0:2], guestPort)
	binary.BigEndian.PutUint16(packet[2:4], hostPort)
	binary.BigEndian.PutUint32(packet[4:8], seq)
	binary.BigEndian.PutUint32(packet[8:12], ack)
	packet[12] = (uint8(headerLen/4) << 4)
	packet[13] = uint8(flags)
	binary.BigEndian.PutUint16(packet[14:16], 0x6000)
	copy(packet[20:], payload)

	binary.BigEndian.PutUint16(packet[16:18], 0)
	check := tcpChecksum(guestIP, hostIP, packet)
	binary.BigEndian.PutUint16(packet[16:18], check)

	ip := buildIPv4Packet(guestIP, hostIP, tcpProtocolNumber, packet)
	frame := make([]byte, 14+len(ip))
	copy(frame[0:6], testHostMAC)
	copy(frame[6:12], testGuestMAC)
	binary.BigEndian.PutUint16(frame[12:14], uint16(etherTypeIPv4))
	copy(frame[14:], ip)
	return frame
}

func hostIP4(ns *NetStack) net.IP {
	ip := ns.hostIPv4
	return net.IPv4(ip[0], ip[1], ip[2], ip[3])
}

func guestIP4(ns *NetStack) net.IP {
	ip := ns.guestIPv4
	return net.IPv4(ip[0], ip[1], ip[2], ip[3])
}

func TestARPReply(t *testing.T) {
	stack, nic, frames := newTestNetStack(t, 1024)

	hostIP := hostIP4(stack)
	guestIP := guestIP4(stack)

	req := buildARPRequest(guestIP, hostIP)
	if err := nic.DeliverGuestPacket(req, nil); err != nil {
		t.Fatalf("deliver arp request: %v", err)
	}

	frame := awaitFrame(t, frames)
	dst, src, ethType, payload := parseEthernet(frame)

	if !bytes.Equal(dst, testGuestMAC) {
		t.Fatalf("unexpected dst mac %s", dst)
	}
	if !bytes.Equal(src, testHostMAC) {
		t.Fatalf("unexpected src mac %s", src)
	}
	if etherType(ethType) != etherTypeARP {
		t.Fatalf("unexpected ethertype %#04x", ethType)
	}
	op := binary.BigEndian.Uint16(payload[6:8])
	if op != 2 {
		t.Fatalf("expected arp reply opcode 2, got %d", op)
	}
	if got := net.IP(payload[14:18]); !got.Equal(hostIP) {
		t.Fatalf("unexpected sender ip %s", got)
	}
}

func TestICMPEchoReply(t *testing.T) {
	stack, nic, frames := newTestNetStack(t, 1024)

	hostIP := hostIP4(stack)
	guestIP := guestIP4(stack)

	req := buildICMPEchoRequest(hostIP, guestIP)
	if err := nic.DeliverGuestPacket(req, nil); err != nil {
		t.Fatalf("deliver icmp request: %v", err)
	}

	frame := awaitFrame(t, frames)
	_, src, ethType, payload := parseEthernet(frame)
	if !bytes.Equal(src, testHostMAC) {
		t.Fatalf("unexpected src mac %s", src)
	}
	if etherType(ethType) != etherTypeIPv4 {
		t.Fatalf("unexpected ethertype %#04x", ethType)
	}
	ipHdr, err := parseIPv4Header(payload)
	if err != nil {
		t.Fatalf("parse ipv4: %v", err)
	}
	if ipHdr.protocol != icmpProtocol {
		t.Fatalf("unexpected protocol %d", ipHdr.protocol)
	}
	resp := ipHdr.payload
	if resp[0] != 0 {
		t.Fatalf("expected icmp echo reply, got type %d", resp[0])
	}
	if binary.BigEndian.Uint16(resp[4:6]) != 0x1234 {
		t.Fatalf("unexpected identifier %x", binary.BigEndian.Uint16(resp[4:6]))
	}
}

func TestUDPInboundAndOutbound(t *testing.T) {
	stack, nic, frames := newTestNetStack(t, 1024)

	hostIP := hostIP4(stack)
	guestIP := guestIP4(stack)

	pc, err := stack.ListenPacketInternal("udp", ":1053")
	if err != nil {
		t.Fatalf("listen udp: %v", err)
	}
	defer pc.Close()

	readDone := make(chan struct{})
	var recvPayload []byte
	var recvAddr net.Addr
	go func() {
		buf := make([]byte, 64)
		n, addr, err := pc.ReadFrom(buf)
		if err != nil {
			t.Errorf("read from udp: %v", err)
			close(readDone)
			return
		}
		recvPayload = append([]byte(nil), buf[:n]...)
		recvAddr = addr
		close(readDone)
	}()

	payload := []byte("dns?")
	frame := buildUDPFrame(hostIP, guestIP, 1053, 5353, payload)
	if err := nic.DeliverGuestPacket(frame, nil); err != nil {
		t.Fatalf("deliver udp packet: %v", err)
	}

	select {
	case <-readDone:
	case <-time.After(time.Second):
		t.Fatalf("timeout waiting for udp read")
	}
	if string(recvPayload) != "dns?" {
		t.Fatalf("unexpected udp payload %q", string(recvPayload))
	}
	uaddr, ok := recvAddr.(*net.UDPAddr)
	if !ok {
		t.Fatalf("unexpected addr type %T", recvAddr)
	}
	if !uaddr.IP.Equal(guestIP) {
		t.Fatalf("unexpected udp source ip %s", uaddr.IP)
	}
	if uaddr.Port != 5353 {
		t.Fatalf("unexpected udp source port %d", uaddr.Port)
	}

	response := []byte("ok!")
	if _, err := pc.WriteTo(response, &net.UDPAddr{IP: guestIP, Port: 5353}); err != nil {
		t.Fatalf("write udp: %v", err)
	}

	respFrame := awaitFrame(t, frames)
	dst, src, ethType, payloadBytes := parseEthernet(respFrame)
	if !bytes.Equal(dst, testGuestMAC) || !bytes.Equal(src, testHostMAC) {
		t.Fatalf("unexpected macs dst=%s src=%s", dst, src)
	}
	if etherType(ethType) != etherTypeIPv4 {
		t.Fatalf("unexpected ethertype %#04x", ethType)
	}

	ipHdr, err := parseIPv4Header(payloadBytes)
	if err != nil {
		t.Fatalf("parse ipv4: %v", err)
	}
	if ipHdr.protocol != udpProtocolNumber {
		t.Fatalf("unexpected protocol %d", ipHdr.protocol)
	}
	udpPayload := ipHdr.payload[8:]
	if string(udpPayload) != "ok!" {
		t.Fatalf("unexpected udp response %q", string(udpPayload))
	}
}

func TestTCPHandshakeDataAndClose(t *testing.T) {
	stack, nic, frames := newTestNetStack(t, 1024)

	hostIP := hostIP4(stack)
	guestIP := guestIP4(stack)

	ln, err := stack.ListenInternal("tcp", ":8080")
	if err != nil {
		t.Fatalf("listen tcp: %v", err)
	}
	defer ln.Close()

	type acceptResult struct {
		conn net.Conn
		err  error
	}
	acceptCh := make(chan acceptResult, 1)
	go func() {
		conn, err := ln.Accept()
		acceptCh <- acceptResult{conn: conn, err: err}
	}()

	const (
		guestSeq  = 100
		guestPort = 40000
		hostPort  = 8080
	)

	syn := buildTCPFrame(hostIP, guestIP, hostPort, guestPort, guestSeq, 0, tcpFlagSYN, nil)
	if err := nic.DeliverGuestPacket(syn, nil); err != nil {
		t.Fatalf("deliver syn: %v", err)
	}

	synAckFrame := awaitFrame(t, frames)
	_, _, _, payload := parseEthernet(synAckFrame)
	ipHdr, err := parseIPv4Header(payload)
	if err != nil {
		t.Fatalf("parse ipv4 synack: %v", err)
	}
	tcpHdr, err := parseTCPHeader(ipHdr.payload)
	if err != nil {
		t.Fatalf("parse tcp synack: %v", err)
	}
	if tcpHdr.flags&(tcpFlagSYN|tcpFlagACK) != (tcpFlagSYN | tcpFlagACK) {
		t.Fatalf("expected syn+ack, got flags %#x", tcpHdr.flags)
	}
	if tcpHdr.ack != guestSeq+1 {
		t.Fatalf("unexpected ack number %d", tcpHdr.ack)
	}

	hostSeq := tcpHdr.seq + 1

	ack := buildTCPFrame(hostIP, guestIP, hostPort, guestPort, guestSeq+1, hostSeq, tcpFlagACK, nil)
	if err := nic.DeliverGuestPacket(ack, nil); err != nil {
		t.Fatalf("deliver ack: %v", err)
	}

	var serverConn net.Conn
	select {
	case res := <-acceptCh:
		if res.err != nil {
			t.Fatalf("accept failed: %v", res.err)
		}
		serverConn = res.conn
	case <-time.After(time.Second):
		t.Fatalf("timeout waiting for accept")
	}
	defer serverConn.Close()

	data := []byte("hello")
	dataFrame := buildTCPFrame(hostIP, guestIP, hostPort, guestPort, guestSeq+1, hostSeq, tcpFlagACK|tcpFlagPSH, data)
	if err := nic.DeliverGuestPacket(dataFrame, nil); err != nil {
		t.Fatalf("deliver data: %v", err)
	}

	dataAckFrame := awaitFrame(t, frames)
	_, _, _, payload = parseEthernet(dataAckFrame)
	ipHdr, err = parseIPv4Header(payload)
	if err != nil {
		t.Fatalf("parse ipv4 data ack: %v", err)
	}
	tcpHdr, err = parseTCPHeader(ipHdr.payload)
	if err != nil {
		t.Fatalf("parse tcp data ack: %v", err)
	}
	if tcpHdr.flags&tcpFlagACK == 0 {
		t.Fatalf("expected ack flag, got %#x", tcpHdr.flags)
	}
	if tcpHdr.ack != guestSeq+1+uint32(len(data)) {
		t.Fatalf("unexpected ack number %d", tcpHdr.ack)
	}

	readBuf := make([]byte, 16)
	n, err := serverConn.Read(readBuf)
	if err != nil {
		t.Fatalf("server read: %v", err)
	}
	if string(readBuf[:n]) != "hello" {
		t.Fatalf("unexpected server payload %q", string(readBuf[:n]))
	}

	if _, err := serverConn.Write([]byte("ok")); err != nil {
		t.Fatalf("server write: %v", err)
	}

	respFrame := awaitFrame(t, frames)
	_, _, _, payload = parseEthernet(respFrame)
	ipHdr, err = parseIPv4Header(payload)
	if err != nil {
		t.Fatalf("parse ipv4 resp: %v", err)
	}
	tcpHdr, err = parseTCPHeader(ipHdr.payload)
	if err != nil {
		t.Fatalf("parse tcp resp: %v", err)
	}
	if tcpHdr.flags&(tcpFlagACK|tcpFlagPSH) != (tcpFlagACK | tcpFlagPSH) {
		t.Fatalf("unexpected response flags %#x", tcpHdr.flags)
	}
	if string(tcpHdr.payload) != "ok" {
		t.Fatalf("unexpected response payload %q", string(tcpHdr.payload))
	}
	hostSeq = tcpHdr.seq + uint32(len(tcpHdr.payload))

	finSeq := guestSeq + 1 + uint32(len(data))
	fin := buildTCPFrame(hostIP, guestIP, hostPort, guestPort, finSeq, hostSeq, tcpFlagFIN|tcpFlagACK, nil)
	if err := nic.DeliverGuestPacket(fin, nil); err != nil {
		t.Fatalf("deliver fin: %v", err)
	}

	finAckFrame := awaitFrame(t, frames)
	_, _, _, payload = parseEthernet(finAckFrame)
	ipHdr, err = parseIPv4Header(payload)
	if err != nil {
		t.Fatalf("parse ipv4 fin ack: %v", err)
	}
	tcpHdr, err = parseTCPHeader(ipHdr.payload)
	if err != nil {
		t.Fatalf("parse tcp fin ack: %v", err)
	}
	if tcpHdr.ack != finSeq+1 {
		t.Fatalf("unexpected fin ack number %d", tcpHdr.ack)
	}

	hostFinFrame := awaitFrame(t, frames)
	_, _, _, payload = parseEthernet(hostFinFrame)
	ipHdr, err = parseIPv4Header(payload)
	if err != nil {
		t.Fatalf("parse ipv4 host fin: %v", err)
	}
	tcpHdr, err = parseTCPHeader(ipHdr.payload)
	if err != nil {
		t.Fatalf("parse tcp host fin: %v", err)
	}
	if tcpHdr.flags&(tcpFlagFIN|tcpFlagACK) != (tcpFlagFIN | tcpFlagACK) {
		t.Fatalf("unexpected host fin flags %#x", tcpHdr.flags)
	}

	finalAck := buildTCPFrame(hostIP, guestIP, hostPort, guestPort, finSeq+1, tcpHdr.seq+1, tcpFlagACK, nil)
	if err := nic.DeliverGuestPacket(finalAck, nil); err != nil {
		t.Fatalf("deliver final ack: %v", err)
	}

	_ = serverConn.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	buf := make([]byte, 1)
	_, err = serverConn.Read(buf)
	if err != io.EOF && !isTimeout(err) {
		t.Fatalf("expected eof or timeout, got %v", err)
	}
}

func TestDebugHTTP(t *testing.T) {
	stack, _, _ := newTestNetStack(t, 1024)

	if err := stack.EnableDebugHTTP("127.0.0.1:0"); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "operation not permitted") {
			t.Skip("debug http listener requires network permissions")
		}
		t.Fatalf("enable debug http: %v", err)
	}

	addr := stack.DebugHTTPAddr()
	if addr == "" {
		t.Fatalf("debug addr not set")
	}

	var (
		resp *http.Response
		err  error
	)
	for i := 0; i < 20; i++ {
		resp, err = http.Get("http://" + addr + "/status")
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("GET /status: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status %d", resp.StatusCode)
	}

	var payload debugStatus
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}

	if payload.HostIPv4 != "10.42.0.1" {
		t.Fatalf("unexpected host ipv4 %q", payload.HostIPv4)
	}
	if payload.Interfaces != 1 {
		t.Fatalf("expected one interface, got %d", payload.Interfaces)
	}
	if payload.DebugAddr != addr {
		t.Fatalf("unexpected debug addr %q", payload.DebugAddr)
	}
}

func TestOpenPacketCaptureEmitsRecords(t *testing.T) {
	stack, _, _ := newTestNetStack(t, 0)
	var buf bytes.Buffer

	if err := stack.OpenPacketCapture(&buf); err != nil {
		t.Fatalf("open capture: %v", err)
	}

	frame1 := []byte{
		0, 1, 2, 3, 4, 5,
		6, 7, 8, 9, 10, 11,
		0x08, 0x00,
		1, 2, 3, 4,
	}
	frame2 := []byte{
		1, 1, 1, 1, 1, 1,
		2, 2, 2, 2, 2, 2,
		0x08, 0x06,
		9, 8, 7, 6, 5,
	}
	stack.writePacketCapture(frame1)
	stack.writePacketCapture(frame2)

	raw := buf.Bytes()
	wantLen := 24 + (16 + len(frame1)) + (16 + len(frame2))
	if len(raw) != wantLen {
		t.Fatalf("expected %d bytes, got %d", wantLen, len(raw))
	}

	global := raw[:24]
	if magic := binary.LittleEndian.Uint32(global[0:4]); magic != 0xa1b2c3d4 {
		t.Fatalf("unexpected magic %#x", magic)
	}
	if snap := binary.LittleEndian.Uint32(global[16:20]); snap != 8192 {
		t.Fatalf("unexpected snaplen %d", snap)
	}
	if link := binary.LittleEndian.Uint32(global[20:24]); link != pcap.LinkTypeEthernet {
		t.Fatalf("unexpected link type %d", link)
	}

	off := 24
	record := raw[off : off+16]
	if capLen := binary.LittleEndian.Uint32(record[8:12]); capLen != uint32(len(frame1)) {
		t.Fatalf("unexpected caplen %d", capLen)
	}
	if origLen := binary.LittleEndian.Uint32(record[12:16]); origLen != uint32(len(frame1)) {
		t.Fatalf("unexpected origlen %d", origLen)
	}

	payload := raw[off+16 : off+16+len(frame1)]
	if !bytes.Equal(payload, frame1) {
		t.Fatalf("frame1 payload mismatch")
	}
	off += 16 + len(frame1)

	record = raw[off : off+16]
	if capLen := binary.LittleEndian.Uint32(record[8:12]); capLen != uint32(len(frame2)) {
		t.Fatalf("unexpected caplen %d", capLen)
	}
	if origLen := binary.LittleEndian.Uint32(record[12:16]); origLen != uint32(len(frame2)) {
		t.Fatalf("unexpected origlen %d", origLen)
	}

	payload = raw[off+16 : off+16+len(frame2)]
	if !bytes.Equal(payload, frame2) {
		t.Fatalf("frame2 payload mismatch")
	}
}

func isTimeout(err error) bool {
	if err == nil {
		return false
	}
	nErr, ok := err.(net.Error)
	return ok && nErr.Timeout()
}
