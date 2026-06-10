//go:build windows && amd64

package whp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/amd64vm"
	"j5.nz/cc/internal/virtio"
	"j5.nz/cc/internal/vmruntime"
)

type ManagedSession struct {
	cancel     context.CancelFunc
	doneCh     chan error
	control    virtio.VsockConn
	listener   virtio.VsockListener
	vsock      *virtio.Vsock
	bootWriter *vmruntime.BootEventWriter
	transcript *vmruntime.SerialTranscript
	serialOut  *vmruntime.SerialTranscript
	platform   *bootPlatform
	sendMu     sync.Mutex
	nextID     atomic.Uint64
	dmesg      bool
}

func StartManagedSession(ctx context.Context, kernel []byte, initrd []byte, memoryMB uint64, dmesg bool, fsdevs []*virtio.FS, onEvent func(client.BootEvent) error) (*ManagedSession, error) {
	return StartManagedSessionWithNet(ctx, kernel, initrd, memoryMB, dmesg, fsdevs, nil, onEvent)
}

func StartManagedSessionWithNet(ctx context.Context, kernel []byte, initrd []byte, memoryMB uint64, dmesg bool, fsdevs []*virtio.FS, netdev *virtio.Net, onEvent func(client.BootEvent) error) (*ManagedSession, error) {
	backend := virtio.NewSimpleVsockBackend()
	listener, err := backend.Listen(vmruntime.ControlPort)
	if err != nil {
		return nil, fmt.Errorf("listen vsock control: %w", err)
	}

	vsock := virtio.NewVsock(amd64vm.VsockBase, amd64vm.VsockSize, amd64vm.VsockIRQ, vmruntime.GuestCID, backend)
	connCh := make(chan virtio.VsockConn, 1)
	acceptErrCh := make(chan error, 1)
	controlTranscript := vmruntime.NewSerialTranscript()
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			acceptErrCh <- err
			return
		}
		connCh <- conn
		_, _ = io.Copy(controlTranscript, conn)
	}()

	var bootWriter *vmruntime.BootEventWriter
	var serialWriter io.Writer
	if onEvent != nil {
		bootWriter = vmruntime.NewBootEventWriter(onEvent)
		serialWriter = bootWriter
	}
	vm, platform, serialOut, err := prepareManagedVM(kernel, initrd, memoryMB, dmesg, fsdevs, vsock, netdev, serialWriter)
	if err != nil {
		_ = listener.Close()
		vsock.Close()
		if bootWriter != nil {
			_ = bootWriter.Close()
		}
		return nil, err
	}

	runCtx, cancel := context.WithCancel(context.Background())
	doneCh := make(chan error, 1)
	go func() {
		defer vm.Close()
		defer platform.Close()
		doneCh <- runManagedExecVM(runCtx, vm, platform, serialOut)
	}()

	var control virtio.VsockConn
	select {
	case err := <-acceptErrCh:
		cancel()
		_ = listener.Close()
		vsock.Close()
		if bootWriter != nil {
			_ = bootWriter.Close()
		}
		return nil, transcriptError(err, serialOut.String(), controlTranscript.String())
	case conn := <-connCh:
		control = conn
	case err := <-doneCh:
		cancel()
		_ = listener.Close()
		vsock.Close()
		if bootWriter != nil {
			_ = bootWriter.Close()
		}
		return nil, transcriptError(err, serialOut.String(), controlTranscript.String())
	case <-ctx.Done():
		cancel()
		_ = listener.Close()
		vsock.Close()
		if bootWriter != nil {
			_ = bootWriter.Close()
		}
		return nil, transcriptError(fmt.Errorf("%w (%s)", ctx.Err(), platform.Summary()), serialOut.String(), controlTranscript.String())
	}

	if _, err := controlTranscript.WaitFor(ctx, 0, func(text string) bool {
		return strings.Contains(text, vmruntime.InstanceReadyMarker) || vmruntime.HasFatalBootText(text)
	}); err != nil {
		cancel()
		_ = control.Close()
		_ = listener.Close()
		vsock.Close()
		if bootWriter != nil {
			_ = bootWriter.Close()
		}
		if ctx.Err() != nil {
			err = fmt.Errorf("%w (%s)", err, platform.Summary())
		}
		return nil, transcriptError(err, serialOut.String(), controlTranscript.String())
	}
	if vmruntime.HasFatalBootText(controlTranscript.String()) {
		cancel()
		_ = control.Close()
		_ = listener.Close()
		vsock.Close()
		if bootWriter != nil {
			_ = bootWriter.Close()
		}
		return nil, transcriptError(fmt.Errorf("guest reported boot failure"), serialOut.String(), controlTranscript.String())
	}

	return &ManagedSession{
		cancel:     cancel,
		doneCh:     doneCh,
		control:    control,
		listener:   listener,
		vsock:      vsock,
		bootWriter: bootWriter,
		transcript: controlTranscript,
		serialOut:  serialOut,
		platform:   platform,
		dmesg:      dmesg,
	}, nil
}

func (s *ManagedSession) Exec(ctx context.Context, req client.ExecRequest) (client.ExecResponse, error) {
	if len(req.Command) == 0 {
		return client.ExecResponse{}, fmt.Errorf("exec command is required")
	}
	id := strconv.FormatUint(s.nextID.Add(1), 10)
	start := s.transcript.Len()
	s.sendMu.Lock()
	err := sendManagedExec(s.control, id, req)
	s.sendMu.Unlock()
	if err != nil {
		return client.ExecResponse{}, transcriptError(err, s.serialOut.String(), s.transcript.String())
	}
	segment, err := s.transcript.WaitFor(ctx, start, func(text string) bool {
		_, _, _, ok := vmruntime.ExtractManagedExecResult(text, id, s.dmesg)
		return ok
	})
	if err != nil {
		return client.ExecResponse{}, transcriptError(err, s.serialOut.String(), s.transcript.String())
	}
	code, output, usage, ok := vmruntime.ExtractManagedExecResult(segment, id, s.dmesg)
	if !ok {
		return client.ExecResponse{}, transcriptError(fmt.Errorf("exec did not produce a complete result"), s.serialOut.String(), s.transcript.String())
	}
	if s.dmesg {
		output = s.serialOut.String() + "\n[control]\n" + output
	}
	return client.ExecResponse{ExitCode: code, Output: output, Usage: usage}, nil
}

func (s *ManagedSession) Flush(ctx context.Context) error {
	id := strconv.FormatUint(s.nextID.Add(1), 10)
	start := s.transcript.Len()
	if err := s.sendExecMessage(vmruntime.ManagedExecRequest{Kind: "sync", ID: id}); err != nil {
		return transcriptError(err, s.serialOut.String(), s.transcript.String())
	}
	segment, err := s.transcript.WaitFor(ctx, start, func(text string) bool {
		_, _, _, ok := vmruntime.ExtractManagedExecResult(text, id, s.dmesg)
		return ok
	})
	if err != nil {
		return transcriptError(err, s.serialOut.String(), s.transcript.String())
	}
	code, output, _, ok := vmruntime.ExtractManagedExecResult(segment, id, s.dmesg)
	if !ok {
		return transcriptError(fmt.Errorf("sync did not produce a complete result"), s.serialOut.String(), s.transcript.String())
	}
	if code != 0 {
		return transcriptError(fmt.Errorf("sync exited with status %d: %s", code, output), s.serialOut.String(), s.transcript.String())
	}
	return nil
}

func (s *ManagedSession) ConsoleHistory(context.Context) (string, error) {
	if s == nil || s.serialOut == nil {
		return "", nil
	}
	return s.serialOut.String(), nil
}

func (s *ManagedSession) ExecStream(ctx context.Context, req client.ExecRequest, inputs <-chan client.ExecInput, onEvent func(client.ExecEvent) error) error {
	if (req.Kind == "" || req.Kind == "exec") && len(req.Command) == 0 {
		return fmt.Errorf("exec command is required")
	}
	id := strconv.FormatUint(s.nextID.Add(1), 10)
	start := s.transcript.Len()
	if err := s.sendExecStart(id, req); err != nil {
		return transcriptError(err, s.serialOut.String(), s.transcript.String())
	}
	if inputs != nil {
		go s.forwardExecInputs(ctx, id, inputs)
	} else if len(req.Stdin) == 0 {
		if err := s.sendStdinClose(id); err != nil {
			return transcriptError(err, s.serialOut.String(), s.transcript.String())
		}
	}
	return s.streamExecEvents(ctx, start, id, onEvent)
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
	stdinClosed := false
	for {
		select {
		case <-ctx.Done():
			return
		case input, ok := <-inputs:
			if !ok {
				if !stdinClosed {
					_ = s.sendStdinClose(id)
				}
				return
			}
			if input.Kind == "stdin_close" {
				if stdinClosed {
					continue
				}
				stdinClosed = true
			} else if input.Kind == "stdin" && stdinClosed {
				continue
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
	payload, err := json.Marshal(msg)
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
			return ctx.Err()
		}
		select {
		case vmErr := <-s.doneCh:
			select {
			case s.doneCh <- vmErr:
			default:
			}
			if vmErr == nil {
				return fmt.Errorf("VM exited during exec")
			}
			return fmt.Errorf("VM exited during exec: %w", vmErr)
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func (s *ManagedSession) Wait() error {
	if s == nil || s.doneCh == nil {
		return nil
	}
	return <-s.doneCh
}

func (s *ManagedSession) Close() error {
	if s == nil {
		return nil
	}
	if s.control != nil {
		_ = s.control.Close()
	}
	if s.listener != nil {
		_ = s.listener.Close()
	}
	if s.vsock != nil {
		_ = s.vsock.Close()
	}
	if s.bootWriter != nil {
		_ = s.bootWriter.Close()
	}
	if s.cancel != nil {
		s.cancel()
	}
	return nil
}

func transcriptError(err error, serialText, controlText string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w\nserial:\n%s\ncontrol:\n%s", err, serialText, controlText)
}
