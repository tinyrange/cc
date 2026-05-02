//go:build linux && amd64

package kvm

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
	"j5.nz/cc/internal/serial"
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
	sendMu     sync.Mutex
	nextID     atomic.Uint64
	dmesg      bool
}

func StartManagedSession(ctx context.Context, kernel []byte, initrd []byte, memoryMB uint64, cpus int, dmesg bool, fsdevs []*virtio.FS, onEvent func(client.BootEvent) error) (*ManagedSession, error) {
	backend := virtio.NewSimpleVsockBackend()
	listener, err := backend.Listen(vmruntime.ControlPort)
	if err != nil {
		return nil, fmt.Errorf("listen vsock control: %w", err)
	}

	vsock := virtio.NewVsock(amd64vm.VsockBase, amd64vm.VsockSize, amd64vm.VsockIRQ, vmruntime.GuestCID, backend)
	rng := virtio.NewRNG(amd64vm.RNGBase, amd64vm.RNGSize, amd64vm.RNGIRQ)
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

	vm, err := NewVMWithCPUs(cpus)
	if err != nil {
		_ = listener.Close()
		vsock.Close()
		return nil, err
	}
	mem, err := mapAMD64GuestMemory(vm, memoryMB)
	if err != nil {
		vm.Close()
		_ = listener.Close()
		vsock.Close()
		return nil, fmt.Errorf("map guest memory: %w", err)
	}

	serialOut := vmruntime.NewSerialTranscript()
	var serialWriter io.Writer = serialOut
	var bootWriter *vmruntime.BootEventWriter
	if onEvent != nil {
		bootWriter = vmruntime.NewBootEventWriter(onEvent)
		serialWriter = io.MultiWriter(serialOut, bootWriter)
	}
	uart := serial.NewUART8250(amd64vm.COM1Base, 0, serialWriter)
	for _, fsdev := range fsdevs {
		if fsdev != nil {
			fsdev.Attach(vm, vm)
		}
	}
	vsock.Attach(vm, vm)
	rng.Attach(vm, vm)

	extraCmdline := amd64vm.VirtioFSCommandLineArgs(fsdevs)
	extraCmdline = append(extraCmdline, amd64vm.VirtioMMIODeviceArg(vsock.Base, vsock.IRQ))
	extraCmdline = append(extraCmdline, amd64vm.VirtioMMIODeviceArg(rng.Base, rng.IRQ))
	plan, err := amd64vm.PrepareBoot(mem, kernel, initrd, amd64vm.BootConfig{
		MemoryMB:     memoryMB,
		NumCPUs:      cpus,
		Dmesg:        dmesg,
		ExtraCmdline: extraCmdline,
	})
	if err != nil {
		vm.Close()
		_ = listener.Close()
		vsock.Close()
		if bootWriter != nil {
			_ = bootWriter.Close()
		}
		return nil, fmt.Errorf("prepare boot: %w", err)
	}
	if err := vm.SetLongMode(plan.EntryGPA, plan.ZeroPageGPA, plan.StackTopGPA, plan.PagingBase); err != nil {
		vm.Close()
		_ = listener.Close()
		vsock.Close()
		if bootWriter != nil {
			_ = bootWriter.Close()
		}
		return nil, fmt.Errorf("set long mode: %w", err)
	}

	runCtx, cancel := context.WithCancel(context.Background())
	doneCh := make(chan error, 1)
	go func() {
		defer vm.Close()
		doneCh <- runManagedExecVM(runCtx, vm, uart, fsdevs, vsock, rng, serialOut)
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
		return nil, transcriptError(ctx.Err(), serialOut.String(), controlTranscript.String())
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
		_, _, ok := vmruntime.ExtractManagedExecResult(text, id, s.dmesg)
		return ok
	})
	if err != nil {
		return client.ExecResponse{}, transcriptError(err, s.serialOut.String(), s.transcript.String())
	}
	code, output, ok := vmruntime.ExtractManagedExecResult(segment, id, s.dmesg)
	if !ok {
		return client.ExecResponse{}, transcriptError(fmt.Errorf("exec did not produce a complete result"), s.serialOut.String(), s.transcript.String())
	}
	if s.dmesg {
		output = s.serialOut.String() + "\n[control]\n" + output
	}
	return client.ExecResponse{ExitCode: code, Output: output}, nil
}

func (s *ManagedSession) ExecStream(ctx context.Context, req client.ExecRequest, inputs <-chan client.ExecInput, onEvent func(client.ExecEvent) error) error {
	if len(req.Command) == 0 {
		return fmt.Errorf("exec command is required")
	}
	id := strconv.FormatUint(s.nextID.Add(1), 10)
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

func (s *ManagedSession) sendExecStart(id string, req client.ExecRequest) error {
	payload, err := json.Marshal(vmruntime.ManagedExecRequest{
		Kind:    "exec",
		ID:      id,
		Command: append([]string(nil), req.Command...),
		Env:     append([]string(nil), req.Env...),
		RootDir: req.RootDir,
		WorkDir: req.WorkDir,
		Stdin:   append([]byte(nil), req.Stdin...),
		TTY:     req.TTY,
		Cols:    req.Cols,
		Rows:    req.Rows,
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
			s.terminateExec(id)
			return ctx.Err()
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (s *ManagedSession) terminateExec(id string) {
	_ = s.sendExecInput(id, client.ExecInput{Kind: "signal", Signal: "TERM"})
	time.Sleep(500 * time.Millisecond)
	_ = s.sendExecInput(id, client.ExecInput{Kind: "signal", Signal: "KILL"})
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
