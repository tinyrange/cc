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

func TestCancelingExecDoesNotDisruptConcurrentCall(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	serverErr := make(chan error, 1)
	execReceived := make(chan struct{})
	controlReceived := make(chan struct{})
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
		controlFrame, err := codec.Receive()
		if err != nil {
			serverErr <- err
			return
		}
		if controlFrame.Type != WorkerFrameAddShare {
			serverErr <- fmt.Errorf("second frame type = %q", controlFrame.Type)
			return
		}
		close(controlReceived)
		cancelFrame, err := codec.Receive()
		if err != nil {
			serverErr <- err
			return
		}
		if cancelFrame.Type != WorkerFrameCancel || cancelFrame.ID != execFrame.ID {
			serverErr <- fmt.Errorf("cancel frame = %+v", cancelFrame)
			return
		}
		if err := codec.Send(mustWorkerFrame(controlFrame.ID, WorkerFrameDone, map[string]string{"status": "mounted"})); err != nil {
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

	execCtx, cancelExec := context.WithCancel(t.Context())
	execDone := make(chan error, 1)
	go func() {
		execDone <- worker.ExecStream(execCtx, "vm", client.ExecRequest{Command: []string{"sleep"}}, nil, nil)
	}()
	select {
	case <-execReceived:
	case err := <-serverErr:
		t.Fatalf("server: %v", err)
	case <-time.After(time.Second):
		t.Fatal("worker did not receive exec call")
	}
	controlDone := make(chan error, 1)
	go func() {
		controlDone <- worker.AddShare(t.Context(), "vm", client.ShareMount{Source: "/host", Mount: "/host"})
	}()
	select {
	case <-controlReceived:
	case err := <-serverErr:
		t.Fatalf("server: %v", err)
	case <-time.After(time.Second):
		t.Fatal("worker did not receive concurrent control call")
	}
	cancelExec()

	select {
	case err := <-execDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("ExecStream error = %v, want context cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("ExecStream did not return after cancellation")
	}
	select {
	case err := <-controlDone:
		if err != nil {
			t.Fatalf("concurrent AddShare: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("concurrent AddShare did not complete")
	}
	if err := <-serverErr; err != nil {
		t.Fatalf("server: %v", err)
	}
}

func TestWorkerConnectionFailureReachesAllCalls(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	execReceived := make(chan struct{})
	serverErr := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			serverErr <- err
			return
		}
		codec := NewWorkerCodec(conn)
		if err := codec.Send(WorkerFrame{Type: WorkerFrameHello}); err != nil {
			serverErr <- err
			return
		}
		frame, err := codec.Receive()
		if err != nil {
			serverErr <- err
			return
		}
		if frame.Type != WorkerFrameExec {
			serverErr <- fmt.Errorf("first frame type = %q", frame.Type)
			return
		}
		close(execReceived)
		frame, err = codec.Receive()
		if err != nil {
			serverErr <- err
			return
		}
		if frame.Type != WorkerFrameAddShare {
			serverErr <- fmt.Errorf("second frame type = %q", frame.Type)
			return
		}
		serverErr <- codec.Close()
	}()

	worker, err := DialWorker(context.Background(), "tcp://"+ln.Addr().String())
	if err != nil {
		t.Fatalf("DialWorker: %v", err)
	}
	defer worker.Close()

	execDone := make(chan error, 1)
	go func() {
		execDone <- worker.ExecStream(t.Context(), "vm", client.ExecRequest{Command: []string{"sleep"}}, nil, nil)
	}()
	select {
	case <-execReceived:
	case err := <-serverErr:
		t.Fatalf("server: %v", err)
	case <-time.After(time.Second):
		t.Fatal("worker did not receive exec call")
	}
	controlDone := make(chan error, 1)
	go func() {
		controlDone <- worker.AddShare(t.Context(), "vm", client.ShareMount{Source: "/host", Mount: "/host"})
	}()

	for name, result := range map[string]<-chan error{
		"exec":    execDone,
		"control": controlDone,
	} {
		select {
		case err := <-result:
			if !errors.Is(err, io.EOF) {
				t.Fatalf("%s error = %v, want connection EOF", name, err)
			}
		case <-time.After(time.Second):
			t.Fatalf("%s call did not receive connection failure", name)
		}
	}
	if err := <-serverErr; err != nil {
		t.Fatalf("server: %v", err)
	}
}

func TestWorkerCallOverflowDoesNotBlockUnrelatedCall(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	callbackStarted := make(chan struct{})
	releaseCallback := make(chan struct{})
	eventsSent := make(chan struct{})
	serverErr := make(chan error, 1)
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
		event := mustWorkerFrame(execFrame.ID, WorkerFrameEvent, client.ExecEvent{Kind: "stdout", Output: "x"})
		if err := codec.Send(event); err != nil {
			serverErr <- err
			return
		}
		<-callbackStarted
		for range 257 {
			if err := codec.Send(event); err != nil {
				serverErr <- err
				return
			}
		}
		close(eventsSent)

		gotCancel := false
		gotControl := false
		for !gotCancel || !gotControl {
			frame, err := codec.Receive()
			if err != nil {
				serverErr <- err
				return
			}
			switch frame.Type {
			case WorkerFrameCancel:
				if frame.ID != execFrame.ID {
					serverErr <- fmt.Errorf("cancel request id = %d, want %d", frame.ID, execFrame.ID)
					return
				}
				gotCancel = true
			case WorkerFrameAddShare:
				gotControl = true
				if err := codec.Send(mustWorkerFrame(frame.ID, WorkerFrameDone, map[string]string{"status": "mounted"})); err != nil {
					serverErr <- err
					return
				}
			default:
				serverErr <- fmt.Errorf("unexpected frame type = %q", frame.Type)
				return
			}
		}
		serverErr <- nil
	}()

	worker, err := DialWorker(context.Background(), "tcp://"+ln.Addr().String())
	if err != nil {
		t.Fatalf("DialWorker: %v", err)
	}
	defer worker.Close()

	execDone := make(chan error, 1)
	go func() {
		execDone <- worker.ExecStream(t.Context(), "vm", client.ExecRequest{Command: []string{"yes"}}, nil, func(client.ExecEvent) error {
			close(callbackStarted)
			<-releaseCallback
			return nil
		})
	}()
	select {
	case <-eventsSent:
	case err := <-serverErr:
		t.Fatalf("server: %v", err)
	case <-time.After(time.Second):
		t.Fatal("worker did not receive overflowing event stream")
	}

	controlDone := make(chan error, 1)
	go func() {
		controlDone <- worker.AddShare(t.Context(), "vm", client.ShareMount{Source: "/host", Mount: "/host"})
	}()
	select {
	case err := <-controlDone:
		if err != nil {
			t.Fatalf("unrelated AddShare: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("unrelated AddShare blocked behind overflowing call")
	}
	close(releaseCallback)
	select {
	case err := <-execDone:
		if !errors.Is(err, ErrWorkerCallOverflow) {
			t.Fatalf("ExecStream error = %v, want call overflow", err)
		}
	case <-time.After(time.Second):
		t.Fatal("overflowing ExecStream did not return")
	}
	if err := <-serverErr; err != nil {
		t.Fatalf("server: %v", err)
	}
}

func TestClosePendingDoesNotBlockFullCall(t *testing.T) {
	call := newWorkerCall()
	for i := 0; i < cap(call.frames); i++ {
		call.frames <- WorkerFrame{ID: 1, Type: WorkerFrameEvent}
	}
	c := &Client{pending: map[uint64]*workerCall{1: call}}
	closed := make(chan struct{})
	go func() {
		c.closePending(io.EOF)
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("connection close blocked on a full call buffer")
	}
	if _, err := c.nextFrame(t.Context(), call); !errors.Is(err, io.EOF) {
		t.Fatalf("call error = %v, want connection EOF", err)
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
