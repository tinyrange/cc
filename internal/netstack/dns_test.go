package netstack

import (
	"encoding/binary"
	"errors"
	"net"
	"testing"
)

func TestBuildDNSResponseAnswersARecord(t *testing.T) {
	query := dnsQuery(t, 0x1234, "Host.Containers.Internal", dnsTypeA, dnsClassIN)
	resp := buildDNSResponse(query, func(name string) (string, error) {
		if name != "host.containers.internal" {
			t.Fatalf("lookup name = %q", name)
		}
		return "10.42.0.1", nil
	})

	if len(resp) < len(query)+16 {
		t.Fatalf("response length = %d, want answer", len(resp))
	}
	if id := binary.BigEndian.Uint16(resp[0:2]); id != 0x1234 {
		t.Fatalf("response id = %#x", id)
	}
	if flags := binary.BigEndian.Uint16(resp[2:4]); flags&0x8000 == 0 || flags&0x000f != 0 {
		t.Fatalf("response flags = %#x", flags)
	}
	if answers := binary.BigEndian.Uint16(resp[6:8]); answers != 1 {
		t.Fatalf("answers = %d, want 1", answers)
	}
	if got := net.IP(resp[len(resp)-4:]).String(); got != "10.42.0.1" {
		t.Fatalf("answer IP = %s", got)
	}
}

func TestBuildDNSResponseNameErrorAndMalformedQueries(t *testing.T) {
	query := dnsQuery(t, 0x9999, "missing.test", dnsTypeA, dnsClassIN)
	resp := buildDNSResponse(query, func(string) (string, error) {
		return "", errors.New("not found")
	})
	if flags := binary.BigEndian.Uint16(resp[2:4]); flags&0x000f != dnsRCodeNameError {
		t.Fatalf("missing-name flags = %#x, want name error", flags)
	}
	if answers := binary.BigEndian.Uint16(resp[6:8]); answers != 0 {
		t.Fatalf("missing-name answers = %d, want 0", answers)
	}

	if resp := buildDNSResponse([]byte{1, 2, 3}, nil); len(resp) != 0 {
		t.Fatalf("short query response length = %d, want 0", len(resp))
	}

	compressed := append([]byte(nil), query...)
	compressed[12] = 0xc0
	resp = buildDNSResponse(compressed, nil)
	if flags := binary.BigEndian.Uint16(resp[2:4]); flags&0x000f != dnsRCodeFormatError {
		t.Fatalf("compressed question flags = %#x, want format error", flags)
	}
}

func TestHostAccessDisabledHidesSyntheticHostDNSNames(t *testing.T) {
	ns := New(nil)
	ns.SetHostAccessEnabled(false)

	for _, name := range []string{"host.containers.internal", "host.internal", "service.internal"} {
		if got, err := ns.lookupDNSName(name); err == nil {
			t.Fatalf("lookupDNSName(%q) = %q, want host access error", name, got)
		}
	}
	if got, err := ns.lookupDNSName("guest.internal"); err != nil || got != "10.42.0.2" {
		t.Fatalf("lookupDNSName(guest.internal) = %q, %v; want guest address", got, err)
	}
	status := ns.collectDebugStatus()
	if status.HostAccess || status.ServiceProxy {
		t.Fatalf("debug status = %+v, want host access and service proxy disabled", status)
	}
}

func TestAllowedServiceProxyPortBypassesHostAccessBlock(t *testing.T) {
	ns := New(nil)
	ns.SetHostAccessEnabled(false)
	serviceIP := net.IP(ns.serviceIPv4[:])

	if ns.serviceProxyAllowed(serviceIP, 43210) {
		t.Fatalf("serviceProxyAllowed without allowlist = true, want false")
	}
	if ns.serviceARPEnabled() {
		t.Fatalf("serviceARPEnabled without allowlist = true, want false")
	}
	ns.SetAllowedServiceProxyPorts([]int{43210})
	if !ns.serviceARPEnabled() {
		t.Fatalf("serviceARPEnabled with allowlist = false, want true")
	}
	if !ns.serviceProxyAllowed(serviceIP, 43210) {
		t.Fatalf("serviceProxyAllowed allowed port = false, want true")
	}
	if ns.serviceProxyAllowed(serviceIP, 43211) {
		t.Fatalf("serviceProxyAllowed other port = true, want false")
	}
	if ns.serviceProxyAllowed(net.ParseIP("10.42.0.1"), 43210) {
		t.Fatalf("serviceProxyAllowed host address = true, want false")
	}
}

func TestHostLocalIPv4Classification(t *testing.T) {
	tests := []struct {
		ip   string
		want bool
	}{
		{ip: "0.0.0.0", want: true},
		{ip: "10.42.0.100", want: true},
		{ip: "100.100.100.200", want: true},
		{ip: "127.0.0.1", want: true},
		{ip: "169.254.169.254", want: true},
		{ip: "172.16.0.1", want: true},
		{ip: "172.31.255.255", want: true},
		{ip: "172.32.0.1", want: false},
		{ip: "192.168.1.1", want: true},
		{ip: "224.0.0.1", want: true},
		{ip: "1.1.1.1", want: false},
		{ip: "8.8.8.8", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			if got := isHostLocalIPv4(net.ParseIP(tt.ip)); got != tt.want {
				t.Fatalf("isHostLocalIPv4(%s) = %t, want %t", tt.ip, got, tt.want)
			}
		})
	}
}

func dnsQuery(t *testing.T, id uint16, name string, qtype uint16, qclass uint16) []byte {
	t.Helper()
	msg := make([]byte, 12)
	binary.BigEndian.PutUint16(msg[0:2], id)
	binary.BigEndian.PutUint16(msg[2:4], 0x0100)
	binary.BigEndian.PutUint16(msg[4:6], 1)
	for _, label := range splitDNSName(name) {
		if len(label) > 63 {
			t.Fatalf("label %q too long", label)
		}
		msg = append(msg, byte(len(label)))
		msg = append(msg, label...)
	}
	msg = append(msg, 0, 0, 0, 0, 0)
	binary.BigEndian.PutUint16(msg[len(msg)-4:len(msg)-2], qtype)
	binary.BigEndian.PutUint16(msg[len(msg)-2:], qclass)
	return msg
}

func splitDNSName(name string) []string {
	var labels []string
	start := 0
	for i := 0; i <= len(name); i++ {
		if i == len(name) || name[i] == '.' {
			if start != i {
				labels = append(labels, name[start:i])
			}
			start = i + 1
		}
	}
	return labels
}
