package sidecar

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"j5.nz/cc/client"
)

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
