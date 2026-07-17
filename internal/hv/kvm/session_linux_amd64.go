//go:build linux && amd64

package kvm

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/amd64vm"
	managedagent "j5.nz/cc/internal/managed/agent"
	"j5.nz/cc/internal/serial"
	"j5.nz/cc/internal/virtio"
	"j5.nz/cc/internal/vmruntime"
)

type ManagedSession struct {
	cancel     context.CancelFunc
	done       *sessionDone
	control    io.ReadWriteCloser
	listener   io.Closer
	vsock      *virtio.Vsock
	fsdevs     []*virtio.FS
	bootWriter *vmruntime.BootEventWriter
	transcript *vmruntime.SerialTranscript
	serialOut  *vmruntime.SerialTranscript
	cleanup    func()
	sendMu     sync.Mutex
	nextID     atomic.Uint64
	dmesg      bool
	inlineExec bool
}

type ManagedSessionOptions struct {
	SnapshotDir     string
	RestoreSnapshot string
	BalloonMB       uint64
}

func StartManagedSession(ctx context.Context, kernel []byte, initrd []byte, memoryMB uint64, cpus int, dmesg bool, fsdevs []*virtio.FS, onEvent func(client.BootEvent) error) (*ManagedSession, error) {
	return StartManagedSessionWithNet(ctx, kernel, initrd, memoryMB, cpus, dmesg, fsdevs, nil, onEvent)
}

func StartManagedSessionWithNet(ctx context.Context, kernel []byte, initrd []byte, memoryMB uint64, cpus int, dmesg bool, fsdevs []*virtio.FS, netdev *virtio.Net, onEvent func(client.BootEvent) error) (*ManagedSession, error) {
	return StartManagedSessionWithNetOptions(ctx, kernel, initrd, memoryMB, cpus, dmesg, fsdevs, netdev, ManagedSessionOptions{}, onEvent)
}

func StartManagedSessionWithNetOptions(ctx context.Context, kernel []byte, initrd []byte, memoryMB uint64, cpus int, dmesg bool, fsdevs []*virtio.FS, netdev *virtio.Net, opts ManagedSessionOptions, onEvent func(client.BootEvent) error) (*ManagedSession, error) {
	if strings.TrimSpace(opts.SnapshotDir) != "" || strings.TrimSpace(opts.RestoreSnapshot) != "" {
		if cpus > 1 {
			return nil, fmt.Errorf("KVM startup snapshots currently support only one vCPU")
		}
	}
	if snapshotPath := strings.TrimSpace(opts.RestoreSnapshot); snapshotPath != "" {
		return StartManagedSessionFromSnapshot(ctx, snapshotPath, memoryMB, opts.BalloonMB, dmesg, fsdevs, netdev, onEvent)
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
	rng := virtio.NewRNG(amd64vm.RNGBase, amd64vm.RNGSize, amd64vm.RNGIRQ)
	balloon := virtio.NewBalloon(amd64vm.BalloonBase, amd64vm.BalloonSize, amd64vm.BalloonIRQ)
	if targetPages := balloonTargetPages(opts.BalloonMB); targetPages != 0 {
		if err := balloon.SetTargetPages(targetPages); err != nil {
			_ = listener.Close()
			vsock.Close()
			return nil, fmt.Errorf("set balloon target: %w", err)
		}
	}
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
		closeVMWithFS(vm, fsdevs)
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
	snapshot := newSnapshotTrigger(opts.SnapshotDir, mem)
	serialWriter = snapshot.wrapSerialWriter(serialWriter)
	uart := serial.NewUART8250(amd64vm.COM1Base, 0, serialWriter)
	for _, fsdev := range fsdevs {
		if fsdev != nil {
			fsdev.Attach(vm, vm)
		}
	}
	vsock.Attach(vm, vm)
	rng.Attach(vm, vm)
	balloon.Attach(vm, vm)
	if netdev != nil {
		netdev.Attach(vm, vm)
	}

	extraCmdline := amd64vm.VirtioFSCommandLineArgs(fsdevs)
	extraCmdline = append(extraCmdline, amd64vm.VirtioMMIODeviceArg(vsock.Base, vsock.IRQ))
	extraCmdline = append(extraCmdline, amd64vm.VirtioMMIODeviceArg(rng.Base, rng.IRQ))
	extraCmdline = append(extraCmdline, amd64vm.VirtioMMIODeviceArg(balloon.Base, balloon.IRQ))
	if netdev != nil {
		extraCmdline = append(extraCmdline, amd64vm.VirtioMMIODeviceArg(netdev.Base, netdev.IRQ))
	}
	extraCmdline = append(extraCmdline, linuxKVMHostKernelArgs()...)
	plan, err := amd64vm.PrepareBoot(mem, kernel, initrd, amd64vm.BootConfig{
		MemoryMB:     memoryMB,
		NumCPUs:      cpus,
		Dmesg:        dmesg,
		ExtraCmdline: extraCmdline,
	})
	if err != nil {
		closeVMWithFS(vm, fsdevs)
		_ = listener.Close()
		vsock.Close()
		if bootWriter != nil {
			_ = bootWriter.Close()
		}
		return nil, fmt.Errorf("prepare boot: %w", err)
	}
	if err := vm.SetLongMode(plan.EntryGPA, plan.ZeroPageGPA, plan.StackTopGPA, plan.PagingBase); err != nil {
		closeVMWithFS(vm, fsdevs)
		_ = listener.Close()
		vsock.Close()
		if bootWriter != nil {
			_ = bootWriter.Close()
		}
		return nil, fmt.Errorf("set long mode: %w", err)
	}

	runCtx, cancel := context.WithCancel(context.Background())
	done := newSessionDone()
	go func() {
		err := runManagedExecVMWithSnapshot(runCtx, vm, uart, fsdevs, vsock, rng, balloon, netdev, serialOut, snapshot)
		closeVMWithFS(vm, fsdevs)
		done.finish(err)
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
	case <-done.done():
		err := done.result()
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
	if err := emitManagedBootStatus(onEvent, "guest ready"); err != nil {
		cancel()
		_ = control.Close()
		_ = listener.Close()
		vsock.Close()
		if bootWriter != nil {
			_ = bootWriter.Close()
		}
		return nil, transcriptError(err, serialOut.String(), controlTranscript.String())
	}

	return &ManagedSession{
		cancel:     cancel,
		done:       done,
		control:    control,
		listener:   listener,
		vsock:      vsock,
		fsdevs:     fsdevs,
		bootWriter: bootWriter,
		cleanup: func() {
			_ = vm.CancelRun()
		},
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
	if s.inlineExec {
		execReq := req
		execReq.Kind = "exec_inline"
		if err := s.sendExecMessage(managedagent.ExecRequest(id, execReq)); err != nil {
			return client.ExecResponse{}, transcriptError(err, s.serialOut.String(), s.transcript.String())
		}
	} else {
		s.sendMu.Lock()
		err := managedagent.SendExec(s.control, id, req)
		s.sendMu.Unlock()
		if err != nil {
			return client.ExecResponse{}, transcriptError(err, s.serialOut.String(), s.transcript.String())
		}
	}
	stopKeepalive := s.startExecKeepalive(ctx, execKeepalive)
	defer stopKeepalive()
	segment, err := s.waitForTranscript(ctx, start, func(text string) bool {
		_, _, _, ok := vmruntime.ExtractManagedExecResult(text, id, s.dmesg)
		return ok
	})
	if err != nil {
		if ctx.Err() != nil {
			s.terminateExecAndWait(id, start)
		}
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
	if err := s.sendExecMessage(managedagent.SyncRequest(id)); err != nil {
		return transcriptError(err, s.serialOut.String(), s.transcript.String())
	}
	segment, err := s.waitForTranscript(ctx, start, func(text string) bool {
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

func (s *ManagedSession) Wait() error {
	if s == nil || s.done == nil {
		return nil
	}
	return s.done.wait()
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
	if s.cleanup != nil {
		s.cleanup()
	}
	if s.done != nil {
		_ = s.done.wait()
	}
	return nil
}

func transcriptError(err error, serialText, controlText string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w\nserial:\n%s\ncontrol:\n%s", err, serialText, controlText)
}
