//go:build linux && amd64

package kvm

import (
	"context"
	"encoding/json"
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

func RunManagedExecWithFS(ctx context.Context, kernel []byte, initrd []byte, memoryMB uint64, cpus int, dmesg bool, fsdevs []*virtio.FS, req client.ExecRequest) (client.ExecResponse, string, error) {
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
		return client.ExecResponse{}, "", err
	}
	defer vm.Close()

	mem, err := mapAMD64GuestMemory(vm, memoryMB)
	if err != nil {
		return client.ExecResponse{}, "", fmt.Errorf("map guest memory: %w", err)
	}

	serialOut := vmruntime.NewSerialTranscript()
	uart := serial.NewUART8250(amd64vm.COM1Base, 0, serialOut)
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
		return client.ExecResponse{}, serialOut.String(), fmt.Errorf("prepare boot: %w", err)
	}
	if err := vm.SetLongMode(plan.EntryGPA, plan.ZeroPageGPA, plan.StackTopGPA, plan.PagingBase); err != nil {
		return client.ExecResponse{}, serialOut.String(), fmt.Errorf("set long mode: %w", err)
	}

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
		return fmt.Errorf("%w\nserial:\n%s\ncontrol:\n%s", err, serialOut.String(), controlTranscript.String())
	}

	go func() {
		setRunErr(runManagedExecVM(runCtx, vm, uart, fsdevs, vsock, rng, serialOut))
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
			err = runCtx.Err()
		}
		return client.ExecResponse{Output: serialOut.String()}, serialOut.String(), withTranscripts(err)
	}

	if _, err := controlTranscript.WaitFor(runCtx, 0, func(text string) bool {
		return strings.Contains(text, vmruntime.InstanceReadyMarker) || vmruntime.HasFatalBootText(text)
	}); err != nil {
		if runErr := currentRunErr(); runErr != nil {
			return client.ExecResponse{Output: serialOut.String()}, serialOut.String(), withTranscripts(runErr)
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
		_, _, ok := vmruntime.ExtractManagedExecResult(text, execID, dmesg)
		return ok
	})
	if err != nil {
		if runErr := currentRunErr(); runErr != nil {
			return client.ExecResponse{Output: serialOut.String()}, serialOut.String(), withTranscripts(runErr)
		}
		return client.ExecResponse{Output: serialOut.String()}, serialOut.String(), withTranscripts(err)
	}
	code, output, ok := vmruntime.ExtractManagedExecResult(segment, execID, dmesg)
	if !ok {
		return client.ExecResponse{Output: serialOut.String()}, serialOut.String(), withTranscripts(fmt.Errorf("exec did not produce a complete result"))
	}
	if dmesg {
		output = serialOut.String() + "\n[control]\n" + output
	}
	cancel()
	return client.ExecResponse{ExitCode: code, Output: output}, serialOut.String(), nil
}

func runManagedExecVM(ctx context.Context, vm *VM, uart *serial.UART8250, fsdevs []*virtio.FS, vsock *virtio.Vsock, rng *virtio.RNG, serialOut *vmruntime.SerialTranscript) error {
	if vm != nil && len(vm.vcpus) > 1 {
		return runManagedExecVMMulti(ctx, vm, uart, fsdevs, vsock, rng, serialOut)
	}
	var exit Exit
	for step := 0; ; step++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := vm.Run(&exit); err != nil {
			return fmt.Errorf("run step %d: %w", step, err)
		}
		switch exit.Reason {
		case ExitIO:
			if err := handleBootIO(uart, exit.IO); err != nil {
				return err
			}
		case ExitMMIO:
			if err := handleBootMMIO(vm, fsdevs, vsock, rng, exit.MMIO); err != nil {
				return err
			}
		case ExitShutdown:
			return fmt.Errorf("guest shut down before exec completed\nserial:\n%s", serialOut.String())
		case ExitSystemEvent:
			return fmt.Errorf("unexpected system event %d before exec completed\nserial:\n%s", exit.SystemEvent, serialOut.String())
		default:
			pc, _ := vm.GetPC()
			return fmt.Errorf("unexpected exit reason %d at pc=%#x\nserial:\n%s", exit.Reason, pc, serialOut.String())
		}
	}
}

func runManagedExecVMMulti(ctx context.Context, vm *VM, uart *serial.UART8250, fsdevs []*virtio.FS, vsock *virtio.Vsock, rng *virtio.RNG, serialOut *vmruntime.SerialTranscript) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	errCh := make(chan error, len(vm.vcpus))
	var wg sync.WaitGroup
	for index := range vm.vcpus {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			var exit Exit
			for {
				if err := runCtx.Err(); err != nil {
					return
				}
				err := vm.RunVCPU(index, &exit)
				if err != nil {
					reportRunErr(errCh, cancel, fmt.Errorf("run vcpu %d: %w", index, err))
					return
				}
				err = handleManagedExit(vm, index, uart, fsdevs, vsock, rng, serialOut, exit)
				if err != nil {
					reportRunErr(errCh, cancel, err)
					return
				}
			}
		}(index)
	}
	defer func() {
		cancel()
		_ = vm.CancelRun()
		wg.Wait()
	}()

	select {
	case err := <-errCh:
		return err
	case <-runCtx.Done():
		return runCtx.Err()
	}
}

func reportRunErr(errCh chan<- error, cancel context.CancelFunc, err error) {
	cancel()
	select {
	case errCh <- err:
	default:
	}
}

func handleManagedExit(vm *VM, vcpuIndex int, uart *serial.UART8250, fsdevs []*virtio.FS, vsock *virtio.Vsock, rng *virtio.RNG, serialOut *vmruntime.SerialTranscript, exit Exit) error {
	switch exit.Reason {
	case ExitIO:
		if err := handleBootIO(uart, exit.IO); err != nil {
			return err
		}
	case ExitMMIO:
		if err := handleBootMMIOForVCPU(vm, vcpuIndex, fsdevs, vsock, rng, exit.MMIO); err != nil {
			return err
		}
	case ExitHLT:
		return nil
	case ExitShutdown:
		return fmt.Errorf("guest shut down before exec completed\nserial:\n%s", serialOut.String())
	case ExitSystemEvent:
		return fmt.Errorf("unexpected system event %d before exec completed\nserial:\n%s", exit.SystemEvent, serialOut.String())
	default:
		pc, _ := vm.GetVCPUPC(vcpuIndex)
		return fmt.Errorf("unexpected exit reason %d on vcpu %d at pc=%#x\nserial:\n%s", exit.Reason, vcpuIndex, pc, serialOut.String())
	}
	return nil
}

func sendManagedExec(control virtio.VsockConn, id string, req client.ExecRequest) error {
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
	if _, err := control.Write(append(payload, '\n')); err != nil {
		return fmt.Errorf("write exec request: %w", err)
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
