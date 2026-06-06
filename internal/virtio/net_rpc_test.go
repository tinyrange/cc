package virtio

import (
	"context"
	"net"
	"testing"
)

func TestNetRemoteBackendSendsTXPacket(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	worker := NewNetPacketCodec(left)
	coordinator := NewNetPacketCodec(right)

	backend := NewNetRemoteBackend(worker, "vm-one", "eth0")
	sendDone := make(chan error, 1)
	go func() {
		sendDone <- backend.HandleTxPacket([]byte{0xde, 0xad, 0xbe, 0xef})
	}()
	packet, err := coordinator.Receive()
	if err != nil {
		t.Fatalf("Receive() error = %v", err)
	}
	if err := <-sendDone; err != nil {
		t.Fatalf("HandleTxPacket() error = %v", err)
	}
	if packet.Kind != NetPacketTX || packet.VMID != "vm-one" || packet.DeviceID != "eth0" {
		t.Fatalf("packet metadata = %#v, want tx packet for vm-one/eth0", packet)
	}
	if got := string(packet.Frame); got != string([]byte{0xde, 0xad, 0xbe, 0xef}) {
		t.Fatalf("packet frame = %x", packet.Frame)
	}
}

func TestReceiveNetPacketsReceivesRXPacket(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	worker := NewNetPacketCodec(left)
	coordinator := NewNetPacketCodec(right)

	got := make(chan NetPacket, 1)
	done := make(chan error, 1)
	go func() {
		done <- ReceiveNetPackets(context.Background(), worker, func(packet NetPacket) error {
			got <- packet
			return nil
		})
	}()

	sendDone := make(chan error, 1)
	go func() {
		sendDone <- coordinator.Send(NetPacket{Kind: NetPacketRX, VMID: "vm-two", DeviceID: "eth0", Frame: []byte("frame")})
	}()
	packet := <-got
	if err := <-sendDone; err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if packet.Kind != NetPacketRX || packet.VMID != "vm-two" || packet.DeviceID != "eth0" || string(packet.Frame) != "frame" {
		t.Fatalf("received packet = %#v", packet)
	}
	_ = coordinator.Close()
	if err := <-done; err != nil {
		t.Fatalf("ReceiveNetPackets() error = %v", err)
	}
}
