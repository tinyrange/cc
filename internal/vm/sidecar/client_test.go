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
	_, err := workerDialTarget("tcp://127.0.0.1:1234")
	var securityErr *WorkerSecurityError
	if !errors.As(err, &securityErr) {
		t.Fatalf("plaintext TCP error type = %T", err)
	}
	if securityErr.Reason != WorkerSecurityPlaintextTCPRejected {
		t.Fatalf("plaintext TCP reason = %q", securityErr.Reason)
	}
	target, err := workerDialTarget("tls://127.0.0.1:1234")
	if err != nil {
		t.Fatalf("TLS target: %v", err)
	}
	if target.network != "tcp" || target.address != "127.0.0.1:1234" || !target.secure {
		t.Fatalf("TLS target = %+v", target)
	}
	target, err = workerDialTarget("/tmp/worker.sock")
	if err != nil {
		t.Fatalf("Unix target: %v", err)
	}
	if target.network != "unix" || target.address != "/tmp/worker.sock" || target.secure {
		t.Fatalf("Unix target = %+v", target)
	}
}

func TestDialWorkerReadsHello(t *testing.T) {
	endpoint, clientConfig, done := serveWorkerFrame(t, func(scope string) WorkerFrame {
		return mustWorkerFrame(0, WorkerFrameHello, WorkerHello{Version: WorkerProtocolVersion, WorkerID: scope})
	})
	worker, err := DialWorkerTLS(context.Background(), endpoint, clientConfig)
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
	endpoint, clientConfig, done := serveWorkerFrame(t, func(string) WorkerFrame {
		return WorkerFrame{Type: WorkerFrameError}
	})
	worker, err := DialWorkerTLS(context.Background(), endpoint, clientConfig)
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
	releaseExec := make(chan struct{})
	execReceived := make(chan struct{})
	endpoint, clientConfig, serverErr := serveWorkerTLS(t, func(codec *WorkerCodec, scope string) error {
		if err := codec.Send(mustWorkerFrame(0, WorkerFrameHello, WorkerHello{Version: WorkerProtocolVersion, WorkerID: scope})); err != nil {
			return err
		}
		execFrame, err := codec.Receive()
		if err != nil {
			return err
		}
		if execFrame.Type != WorkerFrameExec {
			return fmt.Errorf("first frame type = %q", execFrame.Type)
		}
		close(execReceived)
		addShareFrame, err := codec.Receive()
		if err != nil {
			return err
		}
		if addShareFrame.Type != WorkerFrameAddShare {
			return fmt.Errorf("second frame type = %q", addShareFrame.Type)
		}
		if err := codec.Send(mustWorkerFrame(addShareFrame.ID, WorkerFrameDone, map[string]string{"status": "mounted"})); err != nil {
			return err
		}
		<-releaseExec
		if err := codec.Send(mustWorkerFrame(execFrame.ID, WorkerFrameDone, map[string]string{"status": "done"})); err != nil {
			return err
		}
		return nil
	})

	worker, err := DialWorkerTLS(context.Background(), endpoint, clientConfig)
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

func serveWorkerFrame(t *testing.T, frame func(scope string) WorkerFrame) (string, string, <-chan error) {
	t.Helper()
	return serveWorkerTLS(t, func(codec *WorkerCodec, scope string) error {
		return codec.Send(frame(scope))
	})
}

func serveWorkerTLS(t *testing.T, serve func(*WorkerCodec, string) error) (string, string, <-chan error) {
	t.Helper()
	credentials, err := NewEphemeralWorkerSecurity(t.TempDir())
	if err != nil {
		t.Fatalf("create worker credentials: %v", err)
	}
	t.Cleanup(credentials.Close)
	security, err := LoadWorkerServerSecurity(credentials.ServerConfigPath)
	if err != nil {
		t.Fatalf("load worker server security: %v", err)
	}
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
		endpoint := WorkerTLSScheme + ln.Addr().String()
		authenticated, err := HandshakeWorkerServer(context.Background(), conn, endpoint, security)
		if err != nil {
			_ = conn.Close()
			done <- err
			return
		}
		codec := NewWorkerCodec(authenticated)
		err = serve(codec, security.Scope)
		closeErr := codec.Close()
		if err == nil {
			err = closeErr
		}
		done <- err
	}()
	return WorkerTLSScheme + ln.Addr().String(), credentials.ClientConfigPath, done
}
