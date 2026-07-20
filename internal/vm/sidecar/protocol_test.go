package sidecar

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"j5.nz/cc/client"
)

type partialWriteConn struct {
	closed bool
}

func (*partialWriteConn) Read([]byte) (int, error) { return 0, io.EOF }
func (c *partialWriteConn) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, io.ErrUnexpectedEOF
	}
	return 1, io.ErrUnexpectedEOF
}
func (c *partialWriteConn) Close() error { c.closed = true; return nil }

func TestWorkerCodecRoundTrip(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()

	sender := NewWorkerCodec(left)
	receiver := NewWorkerCodec(right)
	done := make(chan error, 1)
	go func() {
		frame, err := NewWorkerFrame(7, WorkerServiceControl, WorkerFrameExec, WorkerExecRequest{
			ID:      "vm1",
			Request: client.ExecRequest{Command: []string{"echo", "ok"}},
		})
		if err != nil {
			done <- err
			return
		}
		done <- sender.Send(frame)
	}()

	got, err := receiver.Receive()
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got.ID != 7 || got.Service != WorkerServiceControl || got.Type != WorkerFrameExec {
		t.Fatalf("frame = %+v", got)
	}
	var req WorkerExecRequest
	if err := got.DecodePayload(&req); err != nil {
		t.Fatalf("DecodePayload: %v", err)
	}
	if req.ID != "vm1" || len(req.Request.Command) != 2 || req.Request.Command[1] != "ok" {
		t.Fatalf("request = %+v", req)
	}
}

func TestWorkerCodecContextBoundsBlockedSend(t *testing.T) {
	left, right := net.Pipe()
	defer right.Close()
	codec := NewWorkerCodec(left)
	defer codec.Close()
	frame := mustWorkerFrame(1, WorkerFrameCancel, WorkerCancelRequest{})
	blocked := make(chan error, 1)
	go func() { blocked <- codec.Send(frame) }()
	t.Cleanup(func() { <-blocked })
	time.Sleep(10 * time.Millisecond)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := codec.SendContext(ctx, frame); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("blocked send error = %v", err)
	}
	_ = left.Close()
}

func TestWorkerCodecPoisonsPartialFrameWrite(t *testing.T) {
	conn := &partialWriteConn{}
	codec := NewWorkerCodec(conn)
	err := codec.Send(mustWorkerFrame(1, WorkerFrameCancel, WorkerCancelRequest{}))
	var writeErr *WorkerStreamWriteError
	if !errors.As(err, &writeErr) {
		t.Fatalf("partial write error = %v, want WorkerStreamWriteError", err)
	}
	if !conn.closed {
		t.Fatal("partial JSON frame left stream reusable")
	}
}

func TestExecResponseFromEvents(t *testing.T) {
	resp := ExecResponse([]client.ExecEvent{
		{Kind: "stdout", Output: "out"},
		{Kind: "stderr", Output: "err"},
		{Kind: "exit", ExitCode: 7},
	})
	if resp.Output != "outerr" {
		t.Fatalf("Output = %q", resp.Output)
	}
	if resp.ExitCode != 7 {
		t.Fatalf("ExitCode = %d", resp.ExitCode)
	}
}
