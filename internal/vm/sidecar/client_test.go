package sidecar

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"j5.nz/cc/client"
)

func TestWorkerDialTarget(t *testing.T) {
	network, address := workerDialTarget("tcp://127.0.0.1:1234")
	if network != "tcp" || address != "127.0.0.1:1234" {
		t.Fatalf("tcp target = %q %q", network, address)
	}
	network, address = workerDialTarget("/tmp/worker.sock")
	if network != "unix" || address != "/tmp/worker.sock" {
		t.Fatalf("unix target = %q %q", network, address)
	}
}

func TestDialWorkerReadsHello(t *testing.T) {
	addr, done := serveWorkerFrame(t, WorkerFrame{Type: WorkerFrameHello})
	worker, err := DialWorker(context.Background(), "tcp://"+addr)
	if err != nil {
		t.Fatalf("DialWorker: %v", err)
	}
	if worker == nil || worker.codec == nil {
		t.Fatalf("worker client was not initialized")
	}
	_ = worker.Close()
	if err := <-done; err != nil {
		t.Fatalf("server: %v", err)
	}
}

func TestDialWorkerRejectsNonHello(t *testing.T) {
	addr, done := serveWorkerFrame(t, WorkerFrame{Type: WorkerFrameError})
	worker, err := DialWorker(context.Background(), "tcp://"+addr)
	if worker != nil {
		_ = worker.Close()
	}
	if err == nil {
		t.Fatalf("err = %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("server: %v", err)
	}
}

func TestWorkerHelloTimesOutForSilentPeer(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	started := time.Now()
	_, err := receiveWorkerHello(t.Context(), clientConn, NewWorkerCodec(clientConn), 20*time.Millisecond)
	var netErr net.Error
	if !errors.As(err, &netErr) || !netErr.Timeout() {
		t.Fatalf("hello error = %v, want network timeout", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("silent hello took %s", elapsed)
	}
}

func TestWorkerHelloCancellationClosesConnection(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer serverConn.Close()
	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() {
		_, err := receiveWorkerHello(ctx, clientConn, NewWorkerCodec(clientConn), time.Second)
		result <- err
	}()
	cancel()

	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("hello error = %v, want context cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("hello did not return after cancellation")
	}
	readDone := make(chan error, 1)
	go func() {
		var buf [1]byte
		_, err := serverConn.Read(buf[:])
		readDone <- err
	}()
	select {
	case err := <-readDone:
		if !errors.Is(err, io.EOF) {
			t.Fatalf("peer read error = %v, want closed connection EOF", err)
		}
	case <-time.After(time.Second):
		t.Fatal("peer connection remained open after cancellation")
	}
}

func TestWorkerHelloDeadlineIsClearedAfterSuccess(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()
	clientCodec := NewWorkerCodec(clientConn)
	serverCodec := NewWorkerCodec(serverConn)
	serverErr := make(chan error, 1)
	go func() {
		if err := serverCodec.Send(WorkerFrame{Type: WorkerFrameHello}); err != nil {
			serverErr <- err
			return
		}
		time.Sleep(100 * time.Millisecond)
		serverErr <- serverCodec.Send(WorkerFrame{Type: WorkerFrameDone})
	}()

	frame, err := receiveWorkerHello(t.Context(), clientConn, clientCodec, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("receive hello: %v", err)
	}
	if frame.Type != WorkerFrameHello {
		t.Fatalf("hello frame type = %q", frame.Type)
	}
	frame, err = clientCodec.Receive()
	if err != nil {
		t.Fatalf("receive after handshake timeout elapsed: %v", err)
	}
	if frame.Type != WorkerFrameDone {
		t.Fatalf("post-handshake frame type = %q", frame.Type)
	}
	if err := <-serverErr; err != nil {
		t.Fatalf("server: %v", err)
	}
}

func TestWorkerClientMultiplexesControlWhileExecStreams(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	serverErr := make(chan error, 1)
	releaseExec := make(chan struct{})
	execReceived := make(chan struct{})
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			serverErr <- err
			return
		}
		codec := NewWorkerCodec(conn)
		defer codec.Close()
		if err := codec.Send(WorkerFrame{Type: WorkerFrameHello}); err != nil {
			serverErr <- err
			return
		}
		execFrame, err := codec.Receive()
		if err != nil {
			serverErr <- err
			return
		}
		if execFrame.Type != WorkerFrameExec {
			serverErr <- fmt.Errorf("first frame type = %q", execFrame.Type)
			return
		}
		close(execReceived)
		addShareFrame, err := codec.Receive()
		if err != nil {
			serverErr <- err
			return
		}
		if addShareFrame.Type != WorkerFrameAddShare {
			serverErr <- fmt.Errorf("second frame type = %q", addShareFrame.Type)
			return
		}
		if err := codec.Send(mustWorkerFrame(addShareFrame.ID, WorkerFrameDone, map[string]string{"status": "mounted"})); err != nil {
			serverErr <- err
			return
		}
		<-releaseExec
		if err := codec.Send(mustWorkerFrame(execFrame.ID, WorkerFrameDone, map[string]string{"status": "done"})); err != nil {
			serverErr <- err
			return
		}
		serverErr <- nil
	}()

	worker, err := DialWorker(context.Background(), "tcp://"+ln.Addr().String())
	if err != nil {
		t.Fatalf("DialWorker: %v", err)
	}
	defer worker.Close()

	execErr := make(chan error, 1)
	go func() {
		execErr <- worker.ExecStream(context.Background(), "vm", client.ExecRequest{Command: []string{"sh"}}, make(chan client.ExecInput), nil)
	}()

	select {
	case <-execReceived:
	case err := <-serverErr:
		t.Fatalf("server: %v", err)
	case <-time.After(time.Second):
		t.Fatal("worker did not receive exec frame")
	}

	addShareDone := make(chan error, 1)
	go func() {
		addShareDone <- worker.AddShare(context.Background(), "vm", client.ShareMount{Source: "/host", Mount: "/host"})
	}()

	select {
	case err := <-addShareDone:
		if err != nil {
			select {
			case server := <-serverErr:
				t.Fatalf("AddShare: %v; server: %v", err, server)
			default:
			}
			t.Fatalf("AddShare: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("AddShare blocked behind active exec stream")
	}

	close(releaseExec)
	if err := <-execErr; err != nil {
		t.Fatalf("ExecStream: %v", err)
	}
	if err := <-serverErr; err != nil {
		t.Fatalf("server: %v", err)
	}
}

func mustWorkerFrame(id uint64, frameType string, payload any) WorkerFrame {
	frame, err := NewWorkerFrame(id, WorkerServiceControl, frameType, payload)
	if err != nil {
		panic(err)
	}
	return frame
}

func serveWorkerFrame(t *testing.T, frame WorkerFrame) (string, <-chan error) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		defer ln.Close()
		conn, err := ln.Accept()
		if err != nil {
			done <- err
			return
		}
		codec := NewWorkerCodec(conn)
		if err := codec.Send(frame); err != nil {
			_ = codec.Close()
			done <- err
			return
		}
		done <- codec.Close()
	}()
	return ln.Addr().String(), done
}
