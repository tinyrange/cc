package sidecar

import (
	"context"
	"errors"
	"fmt"
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
	addr, done := serveWorkerFrame(t, mustWorkerFrame(0, WorkerFrameHello, WorkerHello{
		Version:  WorkerProtocolVersion,
		WorkerID: "worker-1",
		Backend:  "test",
	}))
	worker, err := DialWorker(context.Background(), "tcp://"+addr)
	if err != nil {
		t.Fatalf("DialWorker: %v", err)
	}
	if worker == nil || worker.codec == nil {
		t.Fatalf("worker client was not initialized")
	}
	if hello := worker.Hello(); hello.Version != WorkerProtocolVersion || hello.WorkerID != "worker-1" || hello.Backend != "test" {
		t.Fatalf("worker hello = %+v", hello)
	}
	_ = worker.Close()
	if err := <-done; err != nil {
		t.Fatalf("server: %v", err)
	}
}

func TestDialWorkerRejectsUnsupportedVersion(t *testing.T) {
	addr, done := serveWorkerFrame(t, mustWorkerFrame(0, WorkerFrameHello, WorkerHello{Version: WorkerProtocolVersion + 1}))
	worker, err := DialWorker(context.Background(), "tcp://"+addr)
	if worker != nil {
		_ = worker.Close()
	}
	var versionErr *WorkerProtocolVersionError
	if !errors.As(err, &versionErr) {
		t.Fatalf("DialWorker error = %v, want WorkerProtocolVersionError", err)
	}
	if versionErr.Received != WorkerProtocolVersion+1 || versionErr.Supported != WorkerProtocolVersion {
		t.Fatalf("version error = %+v", versionErr)
	}
	if err := <-done; err != nil {
		t.Fatalf("server: %v", err)
	}
}

func TestDialWorkerChecksRequiredCapabilities(t *testing.T) {
	addr, done := serveWorkerFrame(t, mustWorkerFrame(0, WorkerFrameHello, WorkerHello{
		Version: WorkerProtocolVersion,
		Capabilities: HostCapabilities{
			SupportsFSRPC: true,
		},
	}))
	worker, err := DialWorkerWithRequirements(context.Background(), "tcp://"+addr, WorkerRequirements{
		SupportsFSRPC: true,
		SupportsL2:    true,
	})
	if worker != nil {
		_ = worker.Close()
	}
	var capabilityErr *MissingWorkerCapabilityError
	if !errors.As(err, &capabilityErr) {
		t.Fatalf("DialWorkerWithRequirements error = %v, want MissingWorkerCapabilityError", err)
	}
	if capabilityErr.Capability != "l2-networking" {
		t.Fatalf("missing capability = %q", capabilityErr.Capability)
	}
	if err := <-done; err != nil {
		t.Fatalf("server: %v", err)
	}
}

func TestDialWorkerToleratesOptionalHelloFields(t *testing.T) {
	frame := mustWorkerFrame(0, WorkerFrameHello, map[string]any{
		"version":      WorkerProtocolVersion,
		"worker_id":    "future-worker",
		"future_field": map[string]any{"enabled": true},
		"capabilities": map[string]any{"SupportsFSRPC": true, "SupportsL2": true, "FutureCapability": true},
	})
	addr, done := serveWorkerFrame(t, frame)
	worker, err := DialWorkerWithRequirements(context.Background(), "tcp://"+addr, WorkerRequirements{
		SupportsFSRPC: true,
		SupportsL2:    true,
	})
	if err != nil {
		t.Fatalf("DialWorkerWithRequirements: %v", err)
	}
	if worker.Hello().WorkerID != "future-worker" {
		t.Fatalf("worker hello = %+v", worker.Hello())
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
		if err := codec.Send(mustWorkerFrame(0, WorkerFrameHello, WorkerHello{Version: WorkerProtocolVersion})); err != nil {
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
