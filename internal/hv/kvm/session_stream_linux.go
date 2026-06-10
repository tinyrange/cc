//go:build linux && (amd64 || arm64)

package kvm

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/virtio"
	"j5.nz/cc/internal/vmruntime"
)

const (
	execTerminateGrace = 500 * time.Millisecond
	execKillWait       = 2 * time.Second
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
	if inputs != nil {
		go s.forwardExecInputs(ctx, id, inputs)
	} else if err := s.sendStdinClose(id); err != nil {
		return transcriptError(err, s.serialOut.String(), s.transcript.String())
	}
	return s.streamExecEvents(ctx, start, id, onEvent)
}

func (s *ManagedSession) nextExecID() string {
	return strconv.FormatUint(s.nextID.Add(1), 10)
}

func (s *ManagedSession) sendExecStart(id string, req client.ExecRequest) error {
	payload, err := json.Marshal(vmruntime.ManagedExecRequest{
		Kind:      execRequestKind(req.Kind),
		ID:        id,
		Command:   append([]string(nil), req.Command...),
		Env:       append([]string(nil), req.Env...),
		RootDir:   req.RootDir,
		Path:      req.Path,
		Directory: req.Directory,
		WorkDir:   req.WorkDir,
		User:      req.User,
		Stdin:     append([]byte(nil), req.Stdin...),
		TTY:       req.TTY,
		Cols:      req.Cols,
		Rows:      req.Rows,
	})
	if err != nil {
		return fmt.Errorf("marshal exec request: %w", err)
	}
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	if _, err := s.control.Write(append(payload, '\n')); err != nil {
		return fmt.Errorf("write exec request: %w", err)
	}
	return nil
}

func (s *ManagedSession) forwardExecInputs(ctx context.Context, id string, inputs <-chan client.ExecInput) {
	for {
		select {
		case <-ctx.Done():
			return
		case input, ok := <-inputs:
			if !ok {
				_ = s.sendStdinClose(id)
				return
			}
			_ = s.sendExecInput(id, input)
		}
	}
}

func (s *ManagedSession) sendExecInput(id string, input client.ExecInput) error {
	msg := vmruntime.ManagedExecRequest{ID: id, Kind: input.Kind}
	switch input.Kind {
	case "stdin":
		if len(input.Data) > 0 {
			msg.Stdin = append([]byte(nil), input.Data...)
		} else if input.Input != "" {
			msg.Stdin = []byte(input.Input)
		}
	case "stdin_close":
	case "signal":
		msg.Signal = input.Signal
	case "resize":
		msg.Cols = input.Cols
		msg.Rows = input.Rows
	default:
		return nil
	}
	return s.sendExecMessage(msg)
}

func (s *ManagedSession) sendStdinClose(id string) error {
	return s.sendExecMessage(vmruntime.ManagedExecRequest{ID: id, Kind: "stdin_close"})
}

func (s *ManagedSession) sendExecMessage(msg vmruntime.ManagedExecRequest) error {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	return sendManagedExecMessage(s.control, msg)
}

func sendManagedExecMessage(control virtio.VsockConn, msg vmruntime.ManagedExecRequest) error {
	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal exec request: %w", err)
	}
	if _, err := control.Write(append(payload, '\n')); err != nil {
		return fmt.Errorf("write exec request: %w", err)
	}
	return nil
}

func (s *ManagedSession) streamExecEvents(ctx context.Context, start int, id string, onEvent func(client.ExecEvent) error) error {
	offset := start
	var pending string
	for {
		text := s.transcript.String()
		if offset < len(text) {
			pending += text[offset:]
			offset = len(text)
			for {
				lineEnd := strings.IndexByte(pending, '\n')
				if lineEnd < 0 {
					break
				}
				line := strings.TrimSpace(pending[:lineEnd])
				pending = pending[lineEnd+1:]
				event, done, ok, err := vmruntime.ParseManagedExecEventLine(line, id)
				if err != nil {
					return err
				}
				if !ok {
					continue
				}
				if onEvent != nil {
					if err := onEvent(event); err != nil {
						s.terminateExecAndWait(id, start)
						return err
					}
				}
				if done {
					return nil
				}
			}
			continue
		}
		if ctx.Err() != nil {
			s.terminateExecAndWait(id, start)
			return ctx.Err()
		}
		time.Sleep(5 * time.Millisecond)
	}
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
	_, err := s.transcript.WaitFor(ctx, start, func(text string) bool {
		_, _, _, ok := vmruntime.ExtractManagedExecResult(text, id, s.dmesg)
		return ok
	})
	return err == nil
}
