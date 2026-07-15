//go:build linux && (amd64 || arm64)

package kvm

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"j5.nz/cc/client"
	managedagent "j5.nz/cc/internal/managed/agent"
	managedsession "j5.nz/cc/internal/managed/session"
	"j5.nz/cc/internal/virtio"
	"j5.nz/cc/internal/vmruntime"
)

const (
	execTerminateGrace = 500 * time.Millisecond
	execKillWait       = 2 * time.Second
	execKeepalive      = time.Second
)

func (s *ManagedSession) ExecStream(ctx context.Context, req client.ExecRequest, inputs <-chan client.ExecInput, onEvent func(client.ExecEvent) error) error {
	if (req.Kind == "" || req.Kind == "exec") && len(req.Command) == 0 {
		return fmt.Errorf("exec command is required")
	}
	id := s.nextExecID()
	start := s.transcript.Len()
	if err := s.sendExecStart(id, req); err != nil {
		return transcriptError(err, s.serialOut.String(), s.transcript.String())
	}
	stopKeepalive := s.startExecKeepalive(ctx, execKeepalive)
	defer stopKeepalive()
	if inputs != nil {
		go s.forwardExecInputs(ctx, id, inputs)
	} else if len(req.Stdin) == 0 && !req.TTY {
		if err := s.sendStdinClose(id); err != nil {
			return transcriptError(err, s.serialOut.String(), s.transcript.String())
		}
	}
	return s.streamExecEvents(ctx, start, id, onEvent)
}

// startExecKeepalive gives a blocked restored vCPU a periodic interrupt while
// an exec is outstanding. It does not consume vsock credit or write control
// bytes, either of which could block behind the command it is trying to wake.
func (s *ManagedSession) startExecKeepalive(ctx context.Context, interval time.Duration) context.CancelFunc {
	if s == nil {
		_, cancel := context.WithCancel(ctx)
		return cancel
	}
	return startVsockKeepalive(ctx, s.vsock, interval)
}

func startVsockKeepalive(ctx context.Context, vsock *virtio.Vsock, interval time.Duration) context.CancelFunc {
	keepaliveCtx, cancel := context.WithCancel(ctx)
	if vsock == nil || interval <= 0 {
		return cancel
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-keepaliveCtx.Done():
				return
			case <-ticker.C:
				_ = vsock.Poke()
			}
		}
	}()
	return cancel
}

func (s *ManagedSession) nextExecID() string {
	return strconv.FormatUint(s.nextID.Add(1), 10)
}

func (s *ManagedSession) sendExecStart(id string, req client.ExecRequest) error {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	return managedagent.Send(s.control, managedagent.ExecRequest(id, req))
}

func (s *ManagedSession) forwardExecInputs(ctx context.Context, id string, inputs <-chan client.ExecInput) {
	managedagent.ForwardInputs(ctx, id, inputs, s.sendExecMessage)
}

func (s *ManagedSession) sendExecInput(id string, input client.ExecInput) error {
	msg, ok := managedagent.InputRequest(id, input)
	if !ok {
		return nil
	}
	return s.sendExecMessage(msg)
}

func (s *ManagedSession) sendStdinClose(id string) error {
	return s.sendExecMessage(managedagent.StdinCloseRequest(id))
}

func (s *ManagedSession) sendExecMessage(msg vmruntime.ManagedExecRequest) error {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	return managedagent.Send(s.control, msg)
}

func (s *ManagedSession) streamExecEvents(ctx context.Context, start int, id string, onEvent func(client.ExecEvent) error) error {
	return managedsession.StreamExecEvents(ctx, managedsession.StreamExecOptions{
		Transcript: s.transcript,
		Start:      start,
		ID:         id,
		OnEvent:    onEvent,
		OnCallbackFail: func() {
			s.terminateExecAndWait(id, start)
		},
		OnContextDone: func() {
			s.terminateExecAndWait(id, start)
		},
		Wait: func(context.Context) error {
			select {
			case <-s.done.done():
				return sessionExitError(s.done.result())
			case <-ctx.Done():
				s.terminateExecAndWait(id, start)
				return ctx.Err()
			case <-time.After(5 * time.Millisecond):
				return nil
			}
		},
	})
}

func (s *ManagedSession) terminateExecAndWait(id string, start int) {
	_ = s.sendExecInput(id, client.ExecInput{Kind: "signal", Signal: "TERM"})
	if s.waitForExecExit(id, start, execTerminateGrace) {
		return
	}
	_ = s.sendExecInput(id, client.ExecInput{Kind: "signal", Signal: "KILL"})
	_ = s.waitForExecExit(id, start, execKillWait)
}

func (s *ManagedSession) waitForExecExit(id string, start int, timeout time.Duration) bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	_, err := s.waitForTranscript(ctx, start, func(text string) bool {
		_, _, _, ok := vmruntime.ExtractManagedExecResult(text, id, s.dmesg)
		return ok
	})
	return err == nil
}
