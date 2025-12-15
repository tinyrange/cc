//go:build guest

package main

import (
	"bytes"
	"encoding/binary"
	"net"
	"os"
	"syscall"
	"testing"
	"time"
	"unsafe"

	amd64defs "github.com/tinyrange/cc/internal/linux/defs/amd64"
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

// func TestVirtioFs(t *testing.T) {
// 	// mount the virtio-fs filesystem and verify it works
// 	tmpDir := "/mnt/virtiofs"
// 	err := os.MkdirAll(tmpDir, 0755)
// 	if err != nil {
// 		t.Fatalf("failed to create mount point: %v", err)
// 	}

// 	err = syscall.Mount("bringup", tmpDir, "virtiofs", 0, "")
// 	if err != nil {
// 		t.Fatalf("failed to mount virtio-fs: %v", err)
// 	}
// 	defer syscall.Unmount(tmpDir, 0)

// 	t.Logf("virtio-fs mounted at %s", tmpDir)

// 	testFS(t, tmpDir)
// }

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

	// Bring interface up first
	var reqFlags ifreqFlags
	copy(reqFlags.Name[:], ifName)
	reqFlags.Flags = amd64defs.IFF_UP
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), amd64defs.SIOCSIFFLAGS, uintptr(unsafe.Pointer(&reqFlags)))
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
		// Netmask failure is non-fatal
		_ = errno
	}

	return nil
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

	// Default host IP is 10.42.0.1 (configured in netstack)
	hostIP := net.ParseIP("10.42.0.1")
	if hostIP == nil {
		t.Fatalf("failed to parse host IP")
	}

	// Use ICMP ping via raw socket or net package
	// Try using net package's ICMP support
	conn, err := net.Dial("ip4:icmp", hostIP.String())
	if err != nil {
		t.Fatalf("failed to create ICMP connection: %v", err)
	}
	defer conn.Close()

	// Send 3 ICMP echo requests
	successCount := 0
	for i := 0; i < 3; i++ {
		// Create ICMP echo request packet
		// ICMP header: type(8) + code(0) + checksum(0) + identifier(0x1234) + sequence(i)
		icmp := make([]byte, 8)
		icmp[0] = 8 // ICMP_ECHO
		icmp[1] = 0 // code
		icmp[2] = 0 // checksum (will be calculated)
		icmp[3] = 0
		binary.BigEndian.PutUint16(icmp[4:6], 0x1234)      // identifier
		binary.BigEndian.PutUint16(icmp[6:8], uint16(i+1)) // sequence

		// Calculate checksum
		checksum := calculateChecksum(icmp)
		binary.BigEndian.PutUint16(icmp[2:4], checksum)

		// Send packet
		if _, err := conn.Write(icmp); err != nil {
			t.Logf("failed to send ICMP packet %d: %v", i+1, err)
			continue
		}

		// Try to read reply (with timeout)
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		reply := make([]byte, 64)
		n, err := conn.Read(reply)
		if err != nil {
			t.Logf("failed to receive ICMP reply %d: %v", i+1, err)
			continue
		}

		if n >= 8 {
			// Validate ICMP echo reply
			if reply[0] != 0 {
				t.Logf("unexpected ICMP type in reply %d: got %d, want 0 (echo reply)", i+1, reply[0])
				continue
			}
			if reply[1] != 0 {
				t.Logf("unexpected ICMP code in reply %d: got %d, want 0", i+1, reply[1])
				continue
			}

			// Validate checksum
			replyChecksum := binary.BigEndian.Uint16(reply[2:4])
			replyCopy := make([]byte, n)
			copy(replyCopy, reply[:n])
			binary.BigEndian.PutUint16(replyCopy[2:4], 0)
			calculatedChecksum := calculateChecksum(replyCopy)
			if replyChecksum != calculatedChecksum {
				t.Logf("invalid ICMP checksum in reply %d: got 0x%04x, calculated 0x%04x", i+1, replyChecksum, calculatedChecksum)
				continue
			}

			// Validate identifier matches
			replyID := binary.BigEndian.Uint16(reply[4:6])
			if replyID != 0x1234 {
				t.Logf("unexpected ICMP identifier in reply %d: got 0x%04x, want 0x1234", i+1, replyID)
				continue
			}

			// Validate sequence number matches
			replySeq := binary.BigEndian.Uint16(reply[6:8])
			expectedSeq := uint16(i + 1)
			if replySeq != expectedSeq {
				t.Logf("unexpected ICMP sequence in reply %d: got %d, want %d", i+1, replySeq, expectedSeq)
				continue
			}

			successCount++
			t.Logf("received valid ICMP echo reply %d (id=0x%04x, seq=%d)", i+1, replyID, replySeq)
		}
	}

	if successCount == 0 {
		t.Fatalf("no ICMP echo replies received")
	}
	t.Logf("successfully received %d/3 ICMP echo replies", successCount)
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
