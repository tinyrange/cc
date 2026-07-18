package virtio

import (
	"testing"
	"time"
)

func TestVsockBackendDeliveryWaitsForPeerCredit(t *testing.T) {
	backend := NewSimpleVsockBackend()
	listener, err := backend.Listen(1024)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	clientConn, err := backend.Connect(1024)
	if err != nil {
		t.Fatal(err)
	}
	defer clientConn.Close()
	serverConn, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer serverConn.Close()

	device := NewVsock(0, 0, 0, 3, backend)
	defer device.Close()
	key := vsockConnKey{localPort: 1024, remotePort: 2048}
	device.connections[key] = &vsockConnection{
		key: key, state: vsockConnStateConnected, peerAlloc: 4, backend: serverConn,
	}
	device.wg.Add(1)
	go device.readFromBackend(serverConn, key)
	if _, err := clientConn.Write([]byte("abcdefgh")); err != nil {
		t.Fatal(err)
	}
	waitForVsockTxCount(t, device, key, 4)
	time.Sleep(20 * time.Millisecond)
	device.mu.Lock()
	if got := device.connections[key].txCnt; got != 4 {
		device.mu.Unlock()
		t.Fatalf("sent %d bytes through a 4-byte peer window", got)
	}
	device.connections[key].peerCnt = 4
	device.connections[key].creditRequestPending = false
	device.creditCond.Broadcast()
	device.mu.Unlock()
	waitForVsockTxCount(t, device, key, 8)
}

func waitForVsockTxCount(t *testing.T, device *Vsock, key vsockConnKey, want uint32) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		device.mu.Lock()
		got := device.connections[key].txCnt
		device.mu.Unlock()
		if got == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("vsock tx count did not reach %d", want)
}
