//go:build windows && amd64

package whp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/amd64vm"
	"j5.nz/cc/internal/serial"
	"j5.nz/cc/internal/virtio"
	"j5.nz/cc/internal/vmruntime"
)

func RunManagedExecWithFS(ctx context.Context, kernel []byte, initrd []byte, memoryMB uint64, dmesg bool, fsdevs []*virtio.FS, req client.ExecRequest) (client.ExecResponse, string, error) {
	return RunManagedExecWithFSAndNet(ctx, kernel, initrd, memoryMB, dmesg, fsdevs, nil, req)
}

func RunManagedExecWithFSAndNet(ctx context.Context, kernel []byte, initrd []byte, memoryMB uint64, dmesg bool, fsdevs []*virtio.FS, netdev *virtio.Net, req client.ExecRequest) (client.ExecResponse, string, error) {
	if len(req.Command) == 0 {
		return client.ExecResponse{}, "", fmt.Errorf("exec command is required")
	}

	backend := virtio.NewSimpleVsockBackend()
	listener, err := backend.Listen(vmruntime.ControlPort)
	if err != nil {
		return client.ExecResponse{}, "", fmt.Errorf("listen vsock control: %w", err)
	}
	defer listener.Close()

	vsock := virtio.NewVsock(amd64vm.VsockBase, amd64vm.VsockSize, amd64vm.VsockIRQ, vmruntime.GuestCID, backend)
	defer vsock.Close()

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

	vm, platform, serialOut, err := prepareManagedVM(kernel, initrd, memoryMB, dmesg, fsdevs, vsock, netdev, nil)
	if err != nil {
		return client.ExecResponse{}, "", err
	}
	defer vm.Close()
	defer platform.Close()

	const execID = "1"
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var runErrMu sync.Mutex
	var runErr error
	setRunErr := func(err error) {
		if err == nil {
			return
		}
		runErrMu.Lock()
		if runErr == nil {
			runErr = err
		}
		runErrMu.Unlock()
		cancel()
	}
	currentRunErr := func() error {
		runErrMu.Lock()
		defer runErrMu.Unlock()
		return runErr
	}
	withTranscripts := func(err error) error {
		if err == nil {
			return nil
		}
		return transcriptError(err, serialOut.String(), controlTranscript.String())
	}

	go func() {
		setRunErr(runManagedExecVM(runCtx, vm, platform, serialOut))
	}()
	go func() {
		text, err := serialOut.WaitFor(runCtx, 0, vmruntime.HasFatalBootText)
		if err == nil {
			setRunErr(fmt.Errorf("guest reported boot failure\nserial:\n%s\ncontrol:\n%s", text, controlTranscript.String()))
		}
	}()

	var control virtio.VsockConn
	select {
	case err := <-acceptErrCh:
		return client.ExecResponse{Output: serialOut.String()}, serialOut.String(), withTranscripts(err)
	case conn := <-connCh:
		control = conn
		defer control.Close()
	case <-runCtx.Done():
		err := currentRunErr()
		if err == nil {
			err = fmt.Errorf("%w (%s)", runCtx.Err(), platform.Summary())
		}
		return client.ExecResponse{Output: serialOut.String()}, serialOut.String(), withTranscripts(err)
	}

	if _, err := controlTranscript.WaitFor(runCtx, 0, func(text string) bool {
		return strings.Contains(text, vmruntime.InstanceReadyMarker) || vmruntime.HasFatalBootText(text)
	}); err != nil {
		if runErr := currentRunErr(); runErr != nil {
			return client.ExecResponse{Output: serialOut.String()}, serialOut.String(), withTranscripts(runErr)
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			err = fmt.Errorf("%w (%s)", err, platform.Summary())
		}
		return client.ExecResponse{Output: serialOut.String()}, serialOut.String(), withTranscripts(err)
	}
	if vmruntime.HasFatalBootText(controlTranscript.String()) {
		return client.ExecResponse{Output: serialOut.String()}, serialOut.String(), withTranscripts(fmt.Errorf("guest reported boot failure"))
	}
	if err := sendManagedExec(control, execID, req); err != nil {
		return client.ExecResponse{Output: serialOut.String()}, serialOut.String(), withTranscripts(err)
	}
	segment, err := controlTranscript.WaitFor(runCtx, 0, func(text string) bool {
		_, _, _, ok := vmruntime.ExtractManagedExecResult(text, execID, dmesg)
		return ok
	})
	if err != nil {
		if runErr := currentRunErr(); runErr != nil {
			return client.ExecResponse{Output: serialOut.String()}, serialOut.String(), withTranscripts(runErr)
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			err = fmt.Errorf("%w (%s)", err, platform.Summary())
		}
		return client.ExecResponse{Output: serialOut.String()}, serialOut.String(), withTranscripts(err)
	}
	code, output, usage, ok := vmruntime.ExtractManagedExecResult(segment, execID, dmesg)
	if !ok {
		return client.ExecResponse{Output: serialOut.String()}, serialOut.String(), withTranscripts(fmt.Errorf("exec did not produce a complete result"))
	}
	if dmesg {
		output = serialOut.String() + "\n[control]\n" + output
	}
	cancel()
	return client.ExecResponse{ExitCode: code, Output: output, Usage: usage}, serialOut.String(), nil
}

func prepareManagedVM(kernel []byte, initrd []byte, memoryMB uint64, dmesg bool, fsdevs []*virtio.FS, vsock *virtio.Vsock, netdev *virtio.Net, serialWriter io.Writer) (*VM, *bootPlatform, *vmruntime.SerialTranscript, error) {
	vm, err := newBootVM(amd64vm.MemorySizeBytes(memoryMB))
	if err != nil {
		return nil, nil, nil, err
	}

	extraCmdline := []string{
		"tsc=reliable",
		"tsc_early_khz=3000000",
		"lpj=10000000",
		"no_timer_check",
	}
	extraCmdline = append(extraCmdline, amd64vm.VirtioFSCommandLineArgs(fsdevs)...)
	if vsock != nil {
		extraCmdline = append(extraCmdline, amd64vm.VirtioMMIODeviceArg(vsock.Base, vsock.IRQ))
	}
	if netdev != nil {
		extraCmdline = append(extraCmdline, amd64vm.VirtioMMIODeviceArg(netdev.Base, netdev.IRQ))
	}
	rng := virtio.NewRNG(amd64vm.RNGBase, amd64vm.RNGSize, amd64vm.RNGIRQ)
	extraCmdline = append(extraCmdline, amd64vm.VirtioMMIODeviceArg(rng.Base, rng.IRQ))
	plan, err := amd64vm.PrepareBoot(vm.Memory(), kernel, initrd, amd64vm.BootConfig{
		MemoryMB:     memoryMB,
		Dmesg:        dmesg,
		ExtraCmdline: extraCmdline,
	})
	if err != nil {
		_ = vm.Close()
		return nil, nil, nil, fmt.Errorf("prepare boot: %w", err)
	}
	if err := installBootACPIForZeroPage(vm.Memory(), plan.ZeroPageGPA); err != nil {
		_ = vm.Close()
		return nil, nil, nil, fmt.Errorf("install acpi: %w", err)
	}
	if err := vm.SetLongMode(plan.EntryGPA, plan.ZeroPageGPA, plan.StackTopGPA, plan.PagingBase); err != nil {
		_ = vm.Close()
		return nil, nil, nil, fmt.Errorf("set long mode: %w", err)
	}

	serialOut := vmruntime.NewSerialTranscript()
	if serialWriter == nil {
		serialWriter = serialOut
	} else {
		serialWriter = io.MultiWriter(serialOut, serialWriter)
	}
	platform := newBootPlatform(vm, serial.NewUART8250(amd64vm.COM1Base, 0, serialWriter))
	for _, fsdev := range fsdevs {
		if fsdev != nil {
			platform.AttachFS(fsdev)
		}
	}
	if vsock != nil {
		platform.AttachVsock(vsock)
	}
	if netdev != nil {
		platform.AttachNet(netdev)
	}
	platform.AttachRNG(rng)
	if err := vm.EnableEmulation(platform); err != nil {
		platform.Close()
		_ = vm.Close()
		return nil, nil, serialOut, fmt.Errorf("enable emulation: %w", err)
	}
	return vm, platform, serialOut, nil
}

func runManagedExecVM(ctx context.Context, vm *VM, platform *bootPlatform, serialOut *vmruntime.SerialTranscript) error {
	for step := 0; ; step++ {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("%w (%s)", err, platform.Summary())
		}
		if err := platform.armPendingIRQWindow(); err != nil {
			return fmt.Errorf("arm pending irq window: %w", err)
		}
		var raw runVPExitContext
		exit, err := vm.runWithCancel(ctx, &raw)
		if err != nil {
			if ctx.Err() != nil {
				return fmt.Errorf("%w (%s)", ctx.Err(), platform.Summary())
			}
			return fmt.Errorf("run step %d: %w", step, err)
		}
		platform.recordExit(exit, &raw)
		switch exit.Reason {
		case runVPExitReasonX64IoPortAccess:
			if err := vm.emulateIO(&raw); err != nil {
				io := raw.ioPortAccess()
				return fmt.Errorf("emulate io at rip=%#x port=%#x: %w", exit.RIP, io.Port, err)
			}
		case runVPExitReasonMemoryAccess:
			if err := vm.emulateMMIO(&raw); err != nil {
				mem := raw.memoryAccess()
				return fmt.Errorf("emulate mmio at rip=%#x gpa=%#x gva=%#x access=%d insn_len=%d insn=% x: %w", exit.RIP, uint64(mem.GPA), mem.GVA, mem.AccessInfo.accessType(), mem.InstructionByteCount, mem.InstructionBytes[:mem.InstructionByteCount], err)
			}
		case runVPExitReasonX64Halt:
			if !platform.hasPendingIRQ() {
				return fmt.Errorf("guest halted before exec completed\nserial:\n%s\n%s", serialOut.String(), platform.Summary())
			}
		case runVPExitReasonX64ApicEoi:
			platform.HandleEOI(raw.apicEoi().InterruptVector)
		case runVPExitReasonX64InterruptWindow:
		case runVPExitReasonCanceled:
		default:
			return fmt.Errorf("unexpected exit %s at rip=%#x\nserial:\n%s\n%s", exit.Reason, exit.RIP, serialOut.String(), platform.Summary())
		}
		if flushed, err := platform.flushPendingIRQ(&raw); err != nil {
			return fmt.Errorf("flush pending irq after %s at rip=%#x: %w", exit.Reason, exit.RIP, err)
		} else if exit.Reason == runVPExitReasonX64Halt && !flushed {
			return fmt.Errorf("guest halted with pending irq blocked\nserial:\n%s\n%s", serialOut.String(), platform.Summary())
		}
	}
}

func sendManagedExec(control virtio.VsockConn, id string, req client.ExecRequest) error {
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
		ControlFD: req.ControlFD,
		Cols:      req.Cols,
		Rows:      req.Rows,
	})
	if err != nil {
		return fmt.Errorf("marshal exec request: %w", err)
	}
	if _, err := control.Write(append(payload, '\n')); err != nil {
		return fmt.Errorf("write exec request: %w", err)
	}
	if len(req.Stdin) != 0 {
		return nil
	}
	closePayload, err := json.Marshal(vmruntime.ManagedExecRequest{Kind: "stdin_close", ID: id})
	if err != nil {
		return fmt.Errorf("marshal stdin close request: %w", err)
	}
	if _, err := control.Write(append(closePayload, '\n')); err != nil {
		return fmt.Errorf("write stdin close request: %w", err)
	}
	return nil
}
