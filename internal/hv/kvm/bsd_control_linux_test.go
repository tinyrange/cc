//go:build linux && (amd64 || arm64)

package kvm

import (
	"bufio"
	"net"
	"strings"
	"testing"
	"time"

	"j5.nz/cc/internal/vmruntime"
)

func TestManagedTranscriptErrorsRetainABoundedTail(t *testing.T) {
	text := strings.Repeat("a", managedTranscriptErrorLimit) + "diagnostic-tail"
	got := boundedManagedTranscript(text)
	if len(got) > managedTranscriptErrorLimit+64 {
		t.Fatalf("bounded transcript length = %d", len(got))
	}
	if !strings.HasSuffix(got, "diagnostic-tail") {
		t.Fatal("bounded transcript discarded the most recent diagnostics")
	}
}

func TestBSDControlAcceptsReplacementConnection(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	transcript := vmruntime.NewSerialTranscript()
	control, connected, _ := acceptBSDControlConnections(listener, transcript)
	defer control.Close()

	first, err := net.Dial("tcp4", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-connected:
	case <-time.After(time.Second):
		t.Fatal("first control connection was not accepted")
	}
	if _, err := first.Write([]byte("first-control\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := transcript.WaitFor(t.Context(), 0, func(text string) bool { return text == "first-control\n" }); err != nil {
		t.Fatal(err)
	}
	second, err := net.Dial("tcp4", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if _, err := second.Write([]byte("second-control\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := transcript.WaitFor(t.Context(), 0, func(text string) bool { return strings.Contains(text, "second-control\n") }); err != nil {
		t.Fatal(err)
	}
	writeDone := make(chan error, 1)
	go func() {
		_, err := control.Write([]byte("after-reconnect\n"))
		writeDone <- err
	}()
	if err := second.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(second).ReadString('\n')
	if err != nil {
		t.Fatalf("read replacement request: %v", err)
	}
	if line != "after-reconnect\n" {
		t.Fatalf("replacement request = %q", line)
	}
	select {
	case err := <-writeDone:
		if err != nil {
			t.Fatalf("write through replacement control: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("write did not resume on replacement control")
	}
	_ = first.Close()
}
