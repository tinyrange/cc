//go:build windows && amd64

package whp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/amd64vm"
	managedagent "j5.nz/cc/internal/managed/agent"
	managedsession "j5.nz/cc/internal/managed/session"
	"j5.nz/cc/internal/virtio"
	"j5.nz/cc/internal/vmruntime"
)

type ManagedSession struct {
	cancel     context.CancelFunc
	doneCh     chan error
	control    io.ReadWriteCloser
	listener   io.Closer
	vsock      *virtio.Vsock
	bootWriter *vmruntime.BootEventWriter
	transcript *vmruntime.SerialTranscript
	serialOut  *vmruntime.SerialTranscript
	platform   *bootPlatform
	waitOnce   sync.Once
	waitErr    error
	sendMu     sync.Mutex
	nextID     atomic.Uint64
	dmesg      bool
}

type ManagedSessionOptions struct {
	SnapshotDir     string
	RestoreSnapshot string
}

const (
	execTerminateGrace = 500 * time.Millisecond
	execKillWait       = 2 * time.Second
)

func StartManagedSession(ctx context.Context, kernel []byte, initrd []byte, memoryMB uint64, dmesg bool, fsdevs []*virtio.FS, onEvent func(client.BootEvent) error) (*ManagedSession, error) {
	return StartManagedSessionWithNet(ctx, kernel, initrd, memoryMB, dmesg, fsdevs, nil, onEvent)
}

func StartManagedSessionWithNet(ctx context.Context, kernel []byte, initrd []byte, memoryMB uint64, dmesg bool, fsdevs []*virtio.FS, netdev *virtio.Net, onEvent func(client.BootEvent) error) (*ManagedSession, error) {
	return StartManagedSessionWithNetOptions(ctx, kernel, initrd, memoryMB, dmesg, fsdevs, netdev, ManagedSessionOptions{}, onEvent)
}

func StartManagedSessionWithNetOptions(ctx context.Context, kernel []byte, initrd []byte, memoryMB uint64, dmesg bool, fsdevs []*virtio.FS, netdev *virtio.Net, opts ManagedSessionOptions, onEvent func(client.BootEvent) error) (*ManagedSession, error) {
	if snapshotPath := strings.TrimSpace(opts.RestoreSnapshot); snapshotPath != "" {
		return StartManagedSessionFromSnapshot(ctx, snapshotPath, memoryMB, dmesg, fsdevs, netdev, onEvent)
	}
	if err := emitManagedBootStatus(onEvent, "starting VM"); err != nil {
		return nil, err
	}
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
	vm, platform, serialOut, err := prepareManagedVM(kernel, initrd, memoryMB, dmesg, fsdevs, vsock, netdev, strings.TrimSpace(opts.SnapshotDir), serialWriter)
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
		err := runManagedExecVM(runCtx, vm, platform, serialOut)
		platform.Close()
		err = errors.Join(err, vm.Close())
		doneCh <- err
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
	if err := emitManagedBootStatus(onEvent, "guest ready"); err != nil {
		cancel()
		_ = control.Close()
		_ = listener.Close()
		vsock.Close()
		if bootWriter != nil {
			_ = bootWriter.Close()
		}
		return nil, err
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
	execReq := req
	execReq.Kind = "exec_inline"
	if err := s.sendExecMessage(managedagent.ExecRequest(id, execReq)); err != nil {
		return client.ExecResponse{}, transcriptError(err, s.serialOut.String(), s.transcript.String())
	}
	segment, err := s.transcript.WaitFor(ctx, start, func(text string) bool {
		_, _, _, ok := vmruntime.ExtractManagedExecResult(text, id, s.dmesg)
		return ok
	})
	if err != nil {
		if ctx.Err() != nil {
			s.terminateExecAndWait(id, start)
		}
		return client.ExecResponse{}, transcriptError(s.withPlatformSummary(err), s.serialOut.String(), s.transcript.String())
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
	if err := s.sendExecMessage(managedagent.SyncRequest(id)); err != nil {
		return transcriptError(err, s.serialOut.String(), s.transcript.String())
	}
	segment, err := s.transcript.WaitFor(ctx, start, func(text string) bool {
		_, _, _, ok := vmruntime.ExtractManagedExecResult(text, id, s.dmesg)
		return ok
	})
	if err != nil {
		return transcriptError(s.withPlatformSummary(err), s.serialOut.String(), s.transcript.String())
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
	id := s.nextExecID()
	start := s.transcript.Len()
	if err := s.sendExecStart(id, req); err != nil {
		return transcriptError(err, s.serialOut.String(), s.transcript.String())
	}
	var cancelInputs context.CancelFunc
	var inputsDone chan struct{}
	if inputs != nil {
		inputCtx, cancel := context.WithCancel(ctx)
		cancelInputs = cancel
		inputsDone = make(chan struct{})
		go func() {
			defer close(inputsDone)
			s.forwardExecInputs(inputCtx, id, inputs)
		}()
	} else if len(req.Stdin) == 0 && !req.TTY {
		if err := s.sendStdinClose(id); err != nil {
			return transcriptError(err, s.serialOut.String(), s.transcript.String())
		}
	}
	err := s.streamExecEvents(ctx, start, id, onEvent)
	if cancelInputs != nil {
		cancelInputs()
		<-inputsDone
	}
	return err
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
	if err := managedagent.Send(s.control, msg); err != nil {
		return err
	}
	if s.vsock != nil {
		return s.vsock.Kick()
	}
	return nil
}

func (s *ManagedSession) streamExecEvents(ctx context.Context, start int, id string, onEvent func(client.ExecEvent) error) error {
	err := managedsession.StreamExecEvents(ctx, managedsession.StreamExecOptions{
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
				return nil
			}
		},
	})
	if err != nil {
		return transcriptError(s.withPlatformSummary(err), s.serialOut.String(), s.transcript.String())
	}
	return nil
}

func (s *ManagedSession) withPlatformSummary(err error) error {
	if err == nil || s == nil || s.platform == nil {
		return err
	}
	return fmt.Errorf("%w (%s)", err, s.platform.Summary())
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

func (s *ManagedSession) Wait() error {
	return s.waitDone()
}

func (s *ManagedSession) waitDone() error {
	if s == nil || s.doneCh == nil {
		return nil
	}
	s.waitOnce.Do(func() {
		s.waitErr = <-s.doneCh
	})
	return s.waitErr
}

func (s *ManagedSession) waitDoneTimeout(timeout time.Duration) error {
	if s == nil || s.doneCh == nil {
		return nil
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	errCh := make(chan error, 1)
	go func() {
		errCh <- s.waitDone()
	}()
	select {
	case err := <-errCh:
		return err
	case <-timer.C:
		return fmt.Errorf("timed out waiting for managed session to stop")
	}
}

func (s *ManagedSession) Close() error {
	if s == nil {
		return nil
	}
	if s.cancel != nil {
		s.cancel()
	}
	waitErr := s.waitDoneTimeout(15 * time.Second)
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
	if waitErr != nil && !errors.Is(waitErr, context.Canceled) {
		return waitErr
	}
	return nil
}

func transcriptError(err error, serialText, controlText string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w\nserial:\n%s\ncontrol:\n%s", err, serialText, controlText)
}
