//go:build guest

package main

import (
	"bytes"
	"net"
	"os"
	"syscall"
	"testing"
	"time"
	"unsafe"

	amd64defs "github.com/tinyrange/cc/internal/linux/defs/amd64"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/sys/unix"
)

func TestHello(t *testing.T) {
	// This is a placeholder test.
}

func TestInterruptMapping(t *testing.T) {
	// mount /proc if not already mounted
	if _, err := os.Stat("/proc/interrupts"); os.IsNotExist(err) {
		if err := os.MkdirAll("/proc", 0755); err != nil {
			t.Fatalf("failed to create /proc directory: %v", err)
		}

		err = syscall.Mount("proc", "/proc", "proc", 0, "")
		if err != nil {
			t.Fatalf("failed to mount /proc: %v", err)
		}

		defer syscall.Unmount("/proc", 0)
	}

	data, err := os.ReadFile("/proc/interrupts")
	if err != nil {
		t.Fatalf("failed to read /proc/interrupts: %v", err)
	}

	t.Logf("guest /proc/interrupts:\n%s", data)
}

func TestKernelLog(t *testing.T) {
	// use syscalls to read kernel log
	const klogSize = 1024 * 1024
	buf := make([]byte, klogSize)
	n, err := unix.Klogctl(unix.SYSLOG_ACTION_READ_ALL, buf)
	if err != nil {
		t.Fatalf("failed to read kernel log: %v", err)
	}

	logData := buf[:n]
	t.Logf("kernel log:\n%s", logData)
}

func TestVirtioFs(t *testing.T) {
	// mount the virtio-fs filesystem and verify it works
	tmpDir := "/mnt/virtiofs"
	err := os.MkdirAll(tmpDir, 0755)
	if err != nil {
		t.Fatalf("failed to create mount point: %v", err)
	}

	err = syscall.Mount("bringup", tmpDir, "virtiofs", 0, "")
	if err != nil {
		t.Fatalf("failed to mount virtio-fs: %v", err)
	}
	defer syscall.Unmount(tmpDir, 0)

	t.Logf("virtio-fs mounted at %s", tmpDir)

	testFS(t, tmpDir)
}

func TestNetwork(t *testing.T) {
	// Ensure that eth0 shows up
	iface, err := net.InterfaceByName("eth0")
	if err != nil {
		t.Fatalf("failed to get eth0 interface: %v", err)
	}
	t.Logf("eth0 interface: %+v", iface)
}

// configureInterfaceIP configures a network interface with an IP address and netmask using syscalls
func configureInterfaceIP(ifName string, ip net.IP, mask net.IPMask) error {
	// Create socket for ioctl
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_DGRAM, 0)
	if err != nil {
		return err
	}
	defer syscall.Close(fd)

	// ifreq structure: 40 bytes total
	// Offset 0-15: interface name (IFNAMSIZ = 16)
	// Offset 16-31: union containing sockaddr_in for address/netmask
	// For SIOCSIFFLAGS, offset 16 contains flags (uint16)
	type ifreqFlags struct {
		Name  [16]byte
		Flags uint16
		_     [22]byte // padding
	}

	// ifreqAddr structure for SIOCSIFADDR/SIOCSIFNETMASK
	// Contains sockaddr_in: sa_family (uint16), sin_port (uint16), sin_addr (uint32)
	type ifreqAddr struct {
		Name   [16]byte // interface name
		Family uint16   // AF_INET
		Port   uint16   // 0
		Addr   [4]byte  // IPv4 address in network byte order
		_      [8]byte  // sin_zero padding
	}

	// Bring interface up first - read current flags, then set IFF_UP
	var reqFlags ifreqFlags
	copy(reqFlags.Name[:], ifName)
	// Read current flags
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), amd64defs.SIOCGIFFLAGS, uintptr(unsafe.Pointer(&reqFlags)))
	if errno != 0 {
		return errno
	}
	// Preserve existing flags and set IFF_UP
	reqFlags.Flags |= amd64defs.IFF_UP
	// Write flags back
	_, _, errno = unix.Syscall(unix.SYS_IOCTL, uintptr(fd), amd64defs.SIOCSIFFLAGS, uintptr(unsafe.Pointer(&reqFlags)))
	if errno != 0 {
		return errno
	}

	// Set IP address
	ipBytes := ip.To4()
	if ipBytes == nil {
		return syscall.EINVAL
	}
	var reqAddr ifreqAddr
	copy(reqAddr.Name[:], ifName)
	reqAddr.Family = unix.AF_INET
	reqAddr.Port = 0
	// IP address in network byte order (big-endian)
	copy(reqAddr.Addr[:], ipBytes)
	_, _, errno = unix.Syscall(unix.SYS_IOCTL, uintptr(fd), amd64defs.SIOCSIFADDR, uintptr(unsafe.Pointer(&reqAddr)))
	if errno != 0 {
		return errno
	}

	// Set netmask
	maskBytes := mask
	if len(maskBytes) != 4 {
		return syscall.EINVAL
	}
	var reqMask ifreqAddr
	copy(reqMask.Name[:], ifName)
	reqMask.Family = unix.AF_INET
	reqMask.Port = 0
	copy(reqMask.Addr[:], maskBytes)
	_, _, errno = unix.Syscall(unix.SYS_IOCTL, uintptr(fd), amd64defs.SIOCSIFNETMASK, uintptr(unsafe.Pointer(&reqMask)))
	if errno != 0 {
		// Netmask failure prevents proper routing - fail the configuration
		return errno
	}

	return nil
}

func htons(port uint16) uint16 {
	return (port << 8) | (port >> 8)
}

func TestNetworkRaw(t *testing.T) {
	if err := configureInterfaceIP("eth0", net.ParseIP("10.42.0.2"), net.CIDRMask(24, 32)); err != nil {
		t.Fatalf("failed to configure IP address: %v", err)
	}

	iface, err := net.InterfaceByName("eth0")
	if err != nil {
		t.Fatalf("failed to get eth0 interface: %v", err)
	}
	ifaceIndex := iface.Index

	// Send a raw Ethernet frame and verify it's received
	// The frame should target a custom ethertype designed to be responded to by a custom protocol handler
	frame := []byte{
		// Ethernet header (14 bytes)
		0x02, 0x00, 0x00, 0x00, 0x00, 0x01, // Destination MAC
		0x02, 0x00, 0x00, 0x00, 0x00, 0x01, // Source MAC (eth0's MAC)
		0x12, 0x34,
		0x12, 0x34, 0x12, 0x34, // Custom payload
	}

	fd, err := syscall.Socket(syscall.AF_PACKET, syscall.SOCK_RAW, int(htons(0x1234)))
	if err != nil {
		t.Fatalf("failed to create raw socket: %v", err)
	}
	defer syscall.Close(fd)

	// Bind to the interface to receive packets
	if err := syscall.Bind(fd, &syscall.SockaddrLinklayer{
		Protocol: htons(0x1234),
		Ifindex:  ifaceIndex,
	}); err != nil {
		t.Fatalf("failed to bind raw socket: %v", err)
	}

	addr := &syscall.SockaddrLinklayer{
		Protocol: htons(0x1234),
		Ifindex:  ifaceIndex, // eth0's interface index
		Halen:    6,
		Addr:     [8]byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x01}, // destination MAC
	}

	// Send the frame to the custom ethertype
	if err := syscall.Sendto(fd, frame, 0, addr); err != nil {
		t.Fatalf("failed to write to custom ethertype socket: %v", err)
	}

	t.Logf("sent frame payload: %x", frame[14:])

	rb := make([]byte, 1500)
	n, _, err := syscall.Recvfrom(fd, rb, 0)
	if err != nil {
		t.Fatalf("failed to read from custom ethertype socket: %v", err)
	}
	t.Logf("received frame payload: %x", rb[14:n])

	// Verify the payload is the same as the sent frame
	if !bytes.Equal(rb[14:n], frame[14:]) {
		t.Fatalf("received frame does not match sent frame")
	}
}

func TestNetworkPing(t *testing.T) {
	// Ensure that eth0 shows up
	iface, err := net.InterfaceByName("eth0")
	if err != nil {
		t.Fatalf("failed to get eth0 interface: %v", err)
	}
	t.Logf("eth0 interface: %+v", iface)

	// Validate MAC address matches what's configured in quest/main.go
	expectedMAC := net.HardwareAddr{0x02, 0x00, 0x00, 0x00, 0x00, 0x01}
	if len(iface.HardwareAddr) != 6 {
		t.Fatalf("invalid MAC address length: got %d, want 6", len(iface.HardwareAddr))
	}
	if !bytes.Equal(iface.HardwareAddr, expectedMAC) {
		t.Fatalf("unexpected MAC address: got %s, want %s", iface.HardwareAddr.String(), expectedMAC.String())
	}
	t.Logf("MAC address validated: %s", iface.HardwareAddr.String())

	// Configure eth0 with IP address 10.42.0.2/24 (matching netstack default guest IP)
	guestIP := net.ParseIP("10.42.0.2")
	if guestIP == nil {
		t.Fatalf("failed to parse guest IP")
	}
	mask := net.CIDRMask(24, 32)

	if err := configureInterfaceIP("eth0", guestIP, mask); err != nil {
		t.Fatalf("failed to configure IP address: %v", err)
	}

	// Verify interface configuration
	ifaceAfter, err := net.InterfaceByName("eth0")
	if err != nil {
		t.Fatalf("failed to get eth0 interface after configuration: %v", err)
	}
	addrs, err := ifaceAfter.Addrs()
	if err != nil {
		t.Fatalf("failed to get interface addresses: %v", err)
	}
	t.Logf("eth0 addresses after configuration: %v", addrs)
	t.Logf("eth0 flags after configuration: %v", ifaceAfter.Flags)

	// Default host IP is 10.42.0.1 (configured in netstack)
	hostIP := net.ParseIP("10.42.0.1")
	if hostIP == nil {
		t.Fatalf("failed to parse host IP")
	}

	c, err := icmp.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		t.Fatalf("failed to listen on ICMP socket: %v", err)
	}
	defer c.Close()

	wm := icmp.Message{
		Type: ipv4.ICMPTypeEcho, Code: 0,
		Body: &icmp.Echo{
			ID: os.Getpid() & 0xffff, Seq: 1,
			Data: []byte("Hello, world!"),
		},
	}
	wb, err := wm.Marshal(nil)
	if err != nil {
		t.Fatalf("failed to marshal ICMP message: %v", err)
	}
	if _, err := c.WriteTo(wb, &net.IPAddr{IP: net.ParseIP("10.42.0.1")}); err != nil {
		t.Fatalf("failed to write to ICMP socket: %v", err)
	}

	rb := make([]byte, 1500)
	t.Logf("reading from ICMP socket")
	n, _, err := c.ReadFrom(rb)
	if err != nil {
		t.Fatalf("failed to read from ICMP socket: %v", err)
	}
	rm, err := icmp.ParseMessage(ipv4.ICMPTypeEchoReply.Protocol(), rb[:n])
	if err != nil {
		t.Fatalf("failed to parse ICMP message: %v", err)
	}
	switch rm.Type {
	case ipv4.ICMPTypeEchoReply:
		t.Logf("got echo reply")
	default:
		t.Fatalf("unexpected ICMP message type: %v", rm.Type)
	}
}

func calculateChecksum(data []byte) uint16 {
	var sum uint32
	for i := 0; i < len(data); i += 2 {
		if i+1 < len(data) {
			sum += uint32(data[i])<<8 | uint32(data[i+1])
		} else {
			sum += uint32(data[i]) << 8
		}
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}

func TestNetworkUDP(t *testing.T) {
	if err := configureInterfaceIP("eth0", net.ParseIP("10.42.0.2"), net.CIDRMask(24, 32)); err != nil {
		t.Fatalf("failed to configure IP address: %v", err)
	}

	conn, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.IPv4(10, 42, 0, 1), Port: 4243})
	if err != nil {
		t.Fatalf("failed to dial udp echo: %v", err)
	}
	defer conn.Close()

	buf := make([]byte, 2048)
	for i := 0; i < 3; i++ {
		payload := []byte{'u', 'd', 'p', '-', byte('0' + i), '-', 'o', 'k'}
		if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
			t.Fatalf("failed to set udp deadline: %v", err)
		}
		if _, err := conn.Write(payload); err != nil {
			t.Fatalf("failed to write udp payload: %v", err)
		}
		n, err := conn.Read(buf)
		if err != nil {
			t.Fatalf("failed to read udp echo: %v", err)
		}
		if !bytes.Equal(buf[:n], payload) {
			t.Fatalf("udp echo mismatch: got %q want %q", buf[:n], payload)
		}
	}
}

// func TestNetworkTCP(t *testing.T) {
// 	if err := configureInterfaceIP("eth0", net.ParseIP("10.42.0.2"), net.CIDRMask(24, 32)); err != nil {
// 		t.Fatalf("failed to configure IP address: %v", err)
// 	}

// 	t.Logf("dialing tcp echo at %s", "10.42.0.1:4242")
// 	c, err := net.DialTimeout("tcp4", "10.42.0.1:4242", 2*time.Second)
// 	if err != nil {
// 		t.Fatalf("failed to dial tcp echo: %v", err)
// 	}
// 	defer c.Close()

// 	for i := 0; i < 3; i++ {
// 		payload := []byte{'t', 'c', 'p', '-', byte('0' + i), '-', 'o', 'k'}
// 		t.Logf("tcp write iteration=%d payload=%q", i, payload)
// 		if _, err := c.Write(payload); err != nil {
// 			t.Fatalf("failed to write tcp payload: %v", err)
// 		}

// 		rb := make([]byte, len(payload))
// 		readErr := make(chan error, 1)
// 		go func() {
// 			_, err := io.ReadFull(c, rb)
// 			if err == nil && !bytes.Equal(rb, payload) {
// 				err = io.ErrUnexpectedEOF
// 			}
// 			readErr <- err
// 		}()

// 		select {
// 		case err := <-readErr:
// 			if err != nil {
// 				t.Fatalf("failed to read tcp echo (iteration=%d): %v", i, err)
// 			}
// 			if !bytes.Equal(rb, payload) {
// 				t.Fatalf("tcp echo mismatch (iteration=%d): got %q want %q", i, rb, payload)
// 			}
// 			t.Logf("tcp read iteration=%d ok payload=%q", i, rb)
// 		case <-time.After(2 * time.Second):
// 			_ = c.Close()
// 			t.Fatalf("timeout waiting for tcp echo (iteration=%d)", i)
// 		}
// 	}
// }
