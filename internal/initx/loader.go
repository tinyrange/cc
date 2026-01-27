package initx

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/tinyrange/cc/internal/debug"
	"github.com/tinyrange/cc/internal/devices/virtio"
	"github.com/tinyrange/cc/internal/fdt"
	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/ir"
	"github.com/tinyrange/cc/internal/linux/boot"
	"github.com/tinyrange/cc/internal/linux/boot/amd64"
	"github.com/tinyrange/cc/internal/linux/defs"
	"github.com/tinyrange/cc/internal/linux/kernel"
	"github.com/tinyrange/cc/internal/timeslice"
	"github.com/tinyrange/cc/internal/vfs"
)

var (
	tsInitxNewVMStart        = timeslice.RegisterKind("initx_new_vm_start", 0)
	tsInitxNewVMSetupLoader  = timeslice.RegisterKind("initx_new_vm_setup_loader", 0)
	tsInitxNewVMApplyOptions = timeslice.RegisterKind("initx_new_vm_apply_options", 0)
	tsInitxNewVMCreateHVVM   = timeslice.RegisterKind("initx_new_vm_create_hv_vm", 0)
	tsInitxNewVMRestoreSnap  = timeslice.RegisterKind("initx_new_vm_restore_snapshot", 0)
	tsInitxNewVMDone         = timeslice.RegisterKind("initx_new_vm_done", 0)
)

const (
	// Header constants for program data serialization
	configHeaderMagicValue = 0xcafebabe
	configHeaderSize       = 40
	configHeaderRelocOff   = configHeaderSize
	configDataOffsetField  = 12
	configDataLengthField  = 16

	// Default data region size for program loading
	dataRegionSize = 4 * 1024 * 1024
)

type proxyReader struct {
	r      io.Reader
	update chan io.Reader
	eof    bool
}

func (p *proxyReader) Read(b []byte) (int, error) {
	for {
		if p.eof {
			return 0, io.EOF
		}
		if p.r == nil {
			newR, ok := <-p.update
			if !ok {
				p.eof = true
				return 0, io.EOF
			}
			p.r = newR
		}

		n, err := p.r.Read(b)
		if err == io.EOF {
			p.r = nil
			p.eof = true
			if n > 0 {
				return n, nil
			}
			return 0, io.EOF
		}
		return n, err
	}
}

func (p *proxyReader) SetReader(r io.Reader) {
	p.update <- r
}

var (
	_ io.Reader = &proxyReader{}
)

type proxyWriter struct {
	w io.Writer
}

func (p *proxyWriter) Write(b []byte) (int, error) {
	return p.w.Write(b)
}

var (
	_ io.Writer = &proxyWriter{}
)

type KernelLoader interface {
	// GetKernel returns a reader for the kernel image, its size, and an error if any.
	GetKernel() (io.ReaderAt, int64, error)
}

type programLoader struct {
	region hv.MemoryRegion

	arch hv.CpuArchitecture

	requestedDataLen int
	dataRegionOffset int64

	dataRegion []byte

	runResultDetail uint32
	runResultStage  uint32
}

func (p *programLoader) ReserveDataRegion(size int) {
	if size < 0 {
		size = 0
	}
	p.requestedDataLen = size
}

// Create implements hv.DeviceTemplate.
func (p *programLoader) Create(vm hv.VirtualMachine) (hv.Device, error) {
	return p, nil
}

// Init implements hv.MemoryMappedIODevice.
func (p *programLoader) Init(vm hv.VirtualMachine) error {
	p.arch = vm.Hypervisor().Architecture()

	return nil
}

func (p *programLoader) LoadProgram(prog *ir.Program) error {
	asmProg, err := ir.BuildStandaloneProgramForArch(p.arch, prog)
	if err != nil {
		return fmt.Errorf("build standalone program: %w", err)
	}
	p.runResultDetail = 0
	p.runResultStage = 0

	if len(p.dataRegion) < dataRegionSize {
		p.dataRegion = make([]byte, dataRegionSize)
	}

	// assume p.regionBuf is an already-allocated []byte with enough capacity

	// magic value at offset 0
	binary.LittleEndian.PutUint32(p.dataRegion[0:], configHeaderMagicValue)

	// program size at offset 4
	progBytes := asmProg.Bytes()
	binary.LittleEndian.PutUint32(p.dataRegion[4:], uint32(len(progBytes)))
	// relocation count at offset 8
	relocs := asmProg.Relocations()
	binary.LittleEndian.PutUint32(p.dataRegion[8:], uint32(len(relocs)))

	// relocation entries at offset configHeaderRelocOff
	relocBytes := p.dataRegion[configHeaderRelocOff:]
	for i, reloc := range relocs {
		offset := 4 * i
		binary.LittleEndian.PutUint32(relocBytes[offset:], uint32(reloc))
	}
	relocBytes = relocBytes[:4*len(relocs)]

	// program code at offset configHeaderRelocOff + relocation size
	codeOffset := int(configHeaderRelocOff) + len(relocBytes)
	copy(p.dataRegion[codeOffset:], progBytes)

	// data region at offset reloc + program size
	dataOffset := int64(codeOffset + len(progBytes))

	if p.requestedDataLen > 0 {
		p.dataRegionOffset = dataOffset

		// data offset field
		binary.LittleEndian.PutUint32(
			p.dataRegion[configDataOffsetField:],
			uint32(p.dataRegionOffset),
		)

		// data length field
		binary.LittleEndian.PutUint32(
			p.dataRegion[configDataLengthField:],
			uint32(p.requestedDataLen),
		)
	} else {
		p.dataRegionOffset = 0

		binary.LittleEndian.PutUint32(
			p.dataRegion[configDataOffsetField:],
			0,
		)
		binary.LittleEndian.PutUint32(
			p.dataRegion[configDataLengthField:],
			0,
		)
	}

	// current time for guest clock initialization
	now := time.Now()
	binary.LittleEndian.PutUint64(p.dataRegion[configTimeSecField:], uint64(now.Unix()))
	binary.LittleEndian.PutUint64(p.dataRegion[configTimeNsecField:], uint64(now.Nanosecond()))

	if p.region != nil {
		if _, err := p.region.WriteAt(p.dataRegion, 0); err != nil {
			return fmt.Errorf("write program loader region: %w", err)
		}
	}

	return nil
}

var (
	_ hv.DeviceTemplate = &programLoader{}
)

// VsockProgramServer handles program loading via vsock.
// Protocol:
//   - Host → Guest: [len:4][time_sec:8][time_nsec:8][flags:4][code_len:4][reloc_count:4][relocs:4*count][code:code_len]
//   - Guest → Host: [len:4][exit_code:4][stdout_len:4][stdout_data][stderr_len:4][stderr_data]
//
// Note: time_sec and time_nsec come before flags to maintain 8-byte alignment for ARM64.
// When flags=0, guest response is just [len:4][exit_code:4] for backward compatibility.
const (
	VsockProgramPort = 9998

	// Capture flags for stdout/stderr capture and stdin delivery
	CaptureFlagNone    uint32 = 0x00
	CaptureFlagStdout  uint32 = 0x01
	CaptureFlagStderr  uint32 = 0x02
	CaptureFlagCombine uint32 = 0x04 // Combine stdout+stderr into single stream
	CaptureFlagStdin   uint32 = 0x08 // Stdin data is included in the message
)

// ProgramResult contains the result of a program execution including captured output.
type ProgramResult struct {
	ExitCode int32
	Stdout   []byte
	Stderr   []byte
}

// VsockProgramServer is the host-side server for vsock-based program loading.
type VsockProgramServer struct {
	listener virtio.VsockListener
	conn     virtio.VsockConn
	arch     hv.CpuArchitecture
	mu       sync.Mutex

	// drainBuf is reused for draining stale data
	drainBuf []byte

	// vmTerminated is closed when the VM terminates unexpectedly.
	// This allows WaitResult to detect early termination.
	vmTerminated <-chan struct{}
}

// NewVsockProgramServer creates a new vsock program server listening on the specified port.
func NewVsockProgramServer(backend virtio.VsockBackend, port uint32, arch hv.CpuArchitecture) (*VsockProgramServer, error) {
	listener, err := backend.Listen(port)
	if err != nil {
		return nil, fmt.Errorf("listen on port %d: %w", port, err)
	}
	return &VsockProgramServer{
		listener: listener,
		arch:     arch,
	}, nil
}

// SetVMTerminatedChannel configures the channel that signals VM termination.
func (s *VsockProgramServer) SetVMTerminatedChannel(ch <-chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.vmTerminated = ch
}

// Accept waits for a guest connection.
func (s *VsockProgramServer) Accept() error {
	// Get listener under lock, but don't hold lock during blocking accept
	s.mu.Lock()
	listener := s.listener
	s.mu.Unlock()

	if listener == nil {
		return fmt.Errorf("listener is nil")
	}

	conn, err := listener.Accept()
	if err != nil {
		return fmt.Errorf("accept connection: %w", err)
	}

	// Store connection under lock
	s.mu.Lock()
	s.conn = conn
	s.mu.Unlock()

	return nil
}

// drainStaleData attempts to drain any stale data from the connection.
// This is called before sending a new program to ensure buffer state is clean.
// It uses a non-blocking approach that reads whatever is available without blocking.
func (s *VsockProgramServer) drainStaleData() {
	if s.conn == nil {
		return
	}

	// Try to drain using the Drain interface if available
	if drainer, ok := s.conn.(interface{ Drain() }); ok {
		drainer.Drain()
		return
	}

	// Fallback: allocate drain buffer if needed
	if s.drainBuf == nil {
		s.drainBuf = make([]byte, 4096)
	}

	// For connections that support deadlines, set a very short deadline
	if deadline, ok := s.conn.(interface {
		SetReadDeadline(time.Time) error
	}); ok {
		deadline.SetReadDeadline(time.Now().Add(1 * time.Millisecond))
		for {
			n, err := s.conn.Read(s.drainBuf)
			if n > 0 {
				debug.Writef("initx.drainStaleData", "drained %d stale bytes", n)
			}
			if err != nil || n == 0 {
				break
			}
		}
		deadline.SetReadDeadline(time.Time{})
	}
}

// SendProgram sends a compiled program to the guest without capture flags.
// This is a convenience wrapper around SendProgramWithFlags with flags=0.
func (s *VsockProgramServer) SendProgram(prog *ir.Program) error {
	return s.SendProgramWithFlags(prog, CaptureFlagNone)
}

// SendProgramWithFlags sends a compiled program to the guest with capture flags.
// Protocol: [len:4][time_sec:8][time_nsec:8][flags:4][stdin_len:4][code_len:4][reloc_count:4][relocs:4*count][code:code_len][stdin_data]
// Note: time_sec and time_nsec come first to maintain 8-byte alignment for ARM64.
func (s *VsockProgramServer) SendProgramWithFlags(prog *ir.Program, flags uint32) error {
	return s.SendProgramWithFlagsAndStdin(prog, flags, nil)
}

// SendProgramWithFlagsAndStdin sends a compiled program to the guest with capture flags and optional stdin data.
// Protocol: [len:4][time_sec:8][time_nsec:8][flags:4][stdin_len:4][code_len:4][reloc_count:4][relocs:4*count][code:code_len][stdin_data]
// Note: time_sec and time_nsec come first to maintain 8-byte alignment for ARM64.
func (s *VsockProgramServer) SendProgramWithFlagsAndStdin(prog *ir.Program, flags uint32, stdin []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.conn == nil {
		return fmt.Errorf("no connection established")
	}

	// Drain any stale data from previous program runs to ensure clean buffer state
	s.drainStaleData()

	asmProg, err := ir.BuildStandaloneProgramForArch(s.arch, prog)
	if err != nil {
		return fmt.Errorf("build standalone program: %w", err)
	}

	progBytes := asmProg.Bytes()
	relocs := asmProg.Relocations()

	// Note: CaptureFlagStdin should be set by the caller if stdin is needed.
	// This allows the caller to signal EOF even with empty stdin data.

	// Calculate total message size: time_sec(8) + time_nsec(8) + flags(4) + stdin_len(4) + code_len(4) + reloc_count(4) + relocs(4*N) + code + stdin
	payloadLen := 8 + 8 + 4 + 4 + 4 + 4 + 4*len(relocs) + len(progBytes) + len(stdin)

	// Build the message
	msg := make([]byte, 4+payloadLen) // len prefix + payload

	// Length prefix (excludes itself)
	binary.LittleEndian.PutUint32(msg[0:4], uint32(payloadLen))

	// Current time for guest clock synchronization (8-byte aligned)
	now := time.Now()
	binary.LittleEndian.PutUint64(msg[4:12], uint64(now.Unix()))
	binary.LittleEndian.PutUint64(msg[12:20], uint64(now.Nanosecond()))

	// Flags
	binary.LittleEndian.PutUint32(msg[20:24], flags)

	// stdin_len
	binary.LittleEndian.PutUint32(msg[24:28], uint32(len(stdin)))

	// code_len
	binary.LittleEndian.PutUint32(msg[28:32], uint32(len(progBytes)))

	// reloc_count
	binary.LittleEndian.PutUint32(msg[32:36], uint32(len(relocs)))

	// relocations
	for i, reloc := range relocs {
		binary.LittleEndian.PutUint32(msg[36+4*i:], uint32(reloc))
	}

	// code
	codeOffset := 36 + 4*len(relocs)
	copy(msg[codeOffset:], progBytes)

	// stdin data
	if len(stdin) > 0 {
		stdinOffset := codeOffset + len(progBytes)
		copy(msg[stdinOffset:], stdin)
	}

	// Write the entire message
	if _, err := s.conn.Write(msg); err != nil {
		return fmt.Errorf("write program: %w", err)
	}

	return nil
}

// WaitResult waits for and returns the exit code from the guest.
// This is a convenience wrapper for backward compatibility when capture is not needed.
func (s *VsockProgramServer) WaitResult(ctx context.Context) (int32, error) {
	result, err := s.WaitResultWithCapture(ctx, CaptureFlagNone)
	if err != nil {
		return -1, err
	}
	return result.ExitCode, nil
}

// WaitResultWithCapture waits for the result from the guest including captured output.
// Protocol response: [len:4][exit_code:4][stdout_len:4][stdout_data][stderr_len:4][stderr_data]
// When flags=0, response is just [len:4][exit_code:4].
func (s *VsockProgramServer) WaitResultWithCapture(ctx context.Context, flags uint32) (*ProgramResult, error) {
	// Get connection and vmTerminated under lock, but don't hold lock during blocking read
	s.mu.Lock()
	conn := s.conn
	vmTerminated := s.vmTerminated
	s.mu.Unlock()

	if conn == nil {
		return nil, fmt.Errorf("no connection established")
	}

	// Read length prefix (4 bytes)
	lenBuf := make([]byte, 4)
	done := make(chan error, 1)

	go func() {
		_, err := io.ReadFull(conn, lenBuf)
		done <- err
	}()

	// Wait for result - timeout is controlled by the caller via context
	select {
	case err := <-done:
		if err != nil {
			return nil, fmt.Errorf("read result length: %w", err)
		}
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-vmTerminated:
		return nil, ErrVMTerminated
	}

	length := binary.LittleEndian.Uint32(lenBuf)
	if length < 4 {
		return nil, fmt.Errorf("result length too small: %d", length)
	}

	// Read the rest of the response
	dataBuf := make([]byte, length)
	done = make(chan error, 1)

	go func() {
		_, err := io.ReadFull(conn, dataBuf)
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			return nil, fmt.Errorf("read result data: %w", err)
		}
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-vmTerminated:
		return nil, ErrVMTerminated
	}

	result := &ProgramResult{
		ExitCode: int32(binary.LittleEndian.Uint32(dataBuf[0:4])),
	}

	// If capture flags were set, parse stdout and stderr
	if flags != CaptureFlagNone && length > 4 {
		offset := 4

		// Parse stdout
		if offset+4 <= int(length) {
			stdoutLen := binary.LittleEndian.Uint32(dataBuf[offset : offset+4])
			offset += 4
			if offset+int(stdoutLen) <= int(length) {
				result.Stdout = make([]byte, stdoutLen)
				copy(result.Stdout, dataBuf[offset:offset+int(stdoutLen)])
				offset += int(stdoutLen)
			}
		}

		// Parse stderr
		// Parse stderr
		if offset+4 <= int(length) {
			stderrLen := binary.LittleEndian.Uint32(dataBuf[offset : offset+4])
			offset += 4
			if offset+int(stderrLen) <= int(length) {
				result.Stderr = make([]byte, stderrLen)
				copy(result.Stderr, dataBuf[offset:offset+int(stderrLen)])
			}
		}
	}

	return result, nil
}

// Close closes the server and any active connection.
func (s *VsockProgramServer) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var errs []error
	if s.conn != nil {
		if err := s.conn.Close(); err != nil {
			errs = append(errs, err)
		}
		s.conn = nil
	}
	if s.listener != nil {
		if err := s.listener.Close(); err != nil {
			errs = append(errs, err)
		}
		s.listener = nil
	}
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

type programRunner struct {
	loader *programLoader

	program       *ir.Program
	configureVcpu func(vcpu hv.VirtualCPU) error
	onUserYield   func(context.Context, hv.VirtualCPU) error
}

// Run implements hv.RunConfig.
func (p *programRunner) Run(ctx context.Context, vcpu hv.VirtualCPU) error {
	if p.configureVcpu != nil {
		if err := p.configureVcpu(vcpu); err != nil {
			return fmt.Errorf("configure vCPU: %w", err)
		}
	}

	if err := p.loader.LoadProgram(p.program); err != nil {
		return fmt.Errorf("load program into loader: %w", err)
	}

	p.loader.runResultDetail = 0xdeadbeef

	for {
		if err := vcpu.Run(ctx); err != nil {
			if errors.Is(err, hv.ErrVMHalted) {
				break
			}
			if errors.Is(err, hv.ErrGuestRequestedReboot) {
				break
			}
			if errors.Is(err, hv.ErrYield) {
				break
			}
			if errors.Is(err, hv.ErrUserYield) {
				if p.onUserYield != nil {
					if err := p.onUserYield(ctx, vcpu); err != nil {
						return err
					}
					continue
				}
				return hv.ErrUserYield
			}
			return fmt.Errorf("run vCPU: %w", err)
		}
	}

	if code := p.loader.runResultDetail; code != 0 {
		return &ExitError{Code: int(code)}
	}

	return nil
}

var (
	_ hv.RunConfig = &programRunner{}
)

type VirtualMachine struct {
	loader *boot.LinuxLoader
	vm     hv.VirtualMachine

	firstRunComplete bool

	outBuffer *proxyWriter
	inBuffer  *proxyReader

	// pendingStdin holds the stdin reader until StartStdinForwarding is called.
	// This allows delaying stdin forwarding until after VM boot completes.
	pendingStdin io.Reader

	programLoader *programLoader

	debugLogging         bool
	dmesgLogging         bool
	gpuEnabled           bool
	qemuEmulationEnabled bool

	// kernelLoader stores the kernel for module loading
	kernelLoader kernel.Kernel

	// GPU devices - stored when gpuEnabled is true
	gpuDevice      *virtio.GPU
	keyboardDevice *virtio.Input
	tabletDevice   *virtio.Input

	// consoleDevice is the virtio-console device for interactive console I/O.
	consoleDevice *virtio.Console

	// timesliceMMIOPhysAddr is the physical address of the timeslice MMIO region.
	// Used for performance instrumentation.
	timesliceMMIOPhysAddr uint64

	// pendingSnapshot holds a snapshot to restore after VM creation.
	// Set by WithSnapshot option.
	pendingSnapshot hv.Snapshot

	// Vsock program loading
	vsockProgramServer  *VsockProgramServer
	vsockProgramBackend virtio.VsockBackend
	vsockProgramPort    uint32
	useVsockLoader      bool

	// vsockDevice is the virtio-vsock device for proper cleanup
	vsockDevice *virtio.Vsock

	// vmCtx is a long-lived context for the VM run loop.
	// It's separate from boot/session contexts and only canceled on Close().
	vmCtx    context.Context
	vmCancel context.CancelFunc

	// vmRunDone is signaled when the VM run loop goroutine exits.
	// This is used to ensure proper cleanup ordering in Close().
	vmRunDone chan struct{}
}

func (vm *VirtualMachine) Close() error {
	// Cancel VM context to stop the VM run loop
	if vm.vmCancel != nil {
		vm.vmCancel()
	}

	// Wait for VM run loop goroutine to exit before cleaning up resources.
	// This ensures the guest has fully stopped before we close vsock/memory.
	if vm.vmRunDone != nil {
		<-vm.vmRunDone
	}

	if vm.vsockProgramServer != nil {
		vm.vsockProgramServer.Close()
	}
	// Close vsock device before VM to stop all goroutines
	// that might be writing to guest memory
	if vm.vsockDevice != nil {
		vm.vsockDevice.Close()
	}
	return vm.vm.Close()
}

// SetConsoleSize updates the virtio-console configuration so the guest can see
// the correct terminal size. This is best-effort (no-op if console is unavailable).
func (vm *VirtualMachine) SetConsoleSize(cols, rows int) {
	if vm == nil || vm.consoleDevice == nil {
		return
	}
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	vm.consoleDevice.SetSize(uint16(cols), uint16(rows))
}

// GPU returns the virtio-gpu device if GPU is enabled, nil otherwise.
func (vm *VirtualMachine) GPU() *virtio.GPU {
	return vm.gpuDevice
}

// Keyboard returns the virtio-input keyboard device if GPU is enabled, nil otherwise.
func (vm *VirtualMachine) Keyboard() *virtio.Input {
	return vm.keyboardDevice
}

// Tablet returns the virtio-input tablet device if GPU is enabled, nil otherwise.
func (vm *VirtualMachine) Tablet() *virtio.Input {
	return vm.tabletDevice
}

// StartStdinForwarding activates stdin forwarding to the guest console.
// This should be called after the VM has booted and before the user command runs,
// to ensure stdin data is delivered to the user command rather than the init process.
func (vm *VirtualMachine) StartStdinForwarding() {
	if vm.pendingStdin != nil {
		debug.Writef("initx.StartStdinForwarding", "activating stdin reader %T", vm.pendingStdin)
		vm.inBuffer.SetReader(vm.pendingStdin)
		vm.pendingStdin = nil
	}
}

func (vm *VirtualMachine) Run(ctx context.Context, prog *ir.Program) error {
	// If vsock loader is enabled and we have a server, use vsock path
	if vm.useVsockLoader && vm.vsockProgramServer != nil {
		// First run boots the kernel, then accept vsock connection
		if !vm.firstRunComplete {
			if err := vm.runFirstRunVsock(ctx); err != nil {
				return err
			}
		}
		return vm.runProgramVsock(ctx, prog)
	}
	return vm.runProgram(ctx, prog, nil)
}

// runFirstRunVsock handles the initial boot and vsock connection acceptance.
func (vm *VirtualMachine) runFirstRunVsock(ctx context.Context) error {
	// Boot the kernel (this will run init which connects to vsock)
	// We use a dummy program that immediately returns - init will handle the actual
	// program loading via vsock
	configureVcpu := func(vcpu hv.VirtualCPU) error {
		return vm.loader.ConfigureVCPU(vcpu)
	}
	vm.firstRunComplete = true

	// Create VM context if not already created. This context is used for the VM
	// run loop and outlives the boot context. It's only canceled on Close().
	if vm.vmCtx == nil {
		vm.vmCtx, vm.vmCancel = context.WithCancel(context.Background())
	}

	// Initialize vmRunDone channel to track when the VM run loop exits
	if vm.vmRunDone == nil {
		vm.vmRunDone = make(chan struct{})
	}

	// Connect VM termination signal to vsock server for early error detection
	if vm.vsockProgramServer != nil {
		vm.vsockProgramServer.SetVMTerminatedChannel(vm.vmRunDone)
	}

	// Start the VM in a goroutine so we can accept the vsock connection
	// Use vm.vmCtx so the VM keeps running after boot completes
	vmDone := make(chan error, 1)
	go func() {
		defer close(vm.vmRunDone) // Signal that VM run loop has exited
		err := vm.vm.Run(vm.vmCtx, &programRunner{
			program: &ir.Program{
				Entrypoint: "main",
				Methods: map[string]ir.Method{
					"main": {ir.Return(ir.Int64(0))},
				},
			},
			loader:        vm.programLoader,
			configureVcpu: configureVcpu,
		})
		// The VM will keep running (init's main loop), but we need to accept
		// the vsock connection. Any error here is not necessarily fatal.
		vmDone <- err
	}()

	// Accept the vsock connection from the guest
	acceptDone := make(chan error, 1)
	go func() {
		acceptDone <- vm.vsockProgramServer.Accept()
	}()

	// Wait for either the accept to complete or the VM to exit
	select {
	case err := <-acceptDone:
		if err != nil {
			return fmt.Errorf("accept vsock connection: %w", err)
		}
		debug.Writef("initx.runFirstRunVsock", "vsock connection accepted")
		return nil
	case err := <-vmDone:
		if err != nil && !errors.Is(err, hv.ErrYield) {
			return fmt.Errorf("VM exited during vsock accept: %w", err)
		}
		// VM yielded but we haven't accepted yet - wait for accept
		select {
		case err := <-acceptDone:
			if err != nil {
				return fmt.Errorf("accept vsock connection after yield: %w", err)
			}
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	case <-ctx.Done():
		return ctx.Err()
	}
}

// runProgramVsock runs a program using the vsock protocol.
func (vm *VirtualMachine) runProgramVsock(ctx context.Context, prog *ir.Program) error {
	debug.Writef("initx.runProgramVsock", "sending program")

	// Send program over vsock
	if err := vm.vsockProgramServer.SendProgram(prog); err != nil {
		return fmt.Errorf("send program via vsock: %w", err)
	}

	debug.Writef("initx.runProgramVsock", "waiting for result")

	// Wait for result
	exitCode, err := vm.vsockProgramServer.WaitResult(ctx)
	if err != nil {
		debug.Writef("initx.runProgramVsock", "wait error: %v", err)
		return fmt.Errorf("wait for vsock result: %w", err)
	}

	debug.Writef("initx.runProgramVsock", "got exit code: %d", exitCode)

	if exitCode != 0 {
		return &ExitError{Code: int(exitCode)}
	}

	return nil
}

// RunWithCapture runs a program and captures stdout/stderr based on the flags.
// Returns a ProgramResult containing the exit code and captured output.
func (vm *VirtualMachine) RunWithCapture(ctx context.Context, prog *ir.Program, flags uint32) (*ProgramResult, error) {
	return vm.RunWithCaptureAndStdin(ctx, prog, flags, nil)
}

// RunWithCaptureAndStdin runs a program with optional stdin data and captures stdout/stderr.
// Returns a ProgramResult containing the exit code and captured output.
func (vm *VirtualMachine) RunWithCaptureAndStdin(ctx context.Context, prog *ir.Program, flags uint32, stdin []byte) (*ProgramResult, error) {
	// If vsock loader is enabled and we have a server, use vsock path
	if vm.useVsockLoader && vm.vsockProgramServer != nil {
		// First run boots the kernel, then accept vsock connection
		if !vm.firstRunComplete {
			if err := vm.runFirstRunVsock(ctx); err != nil {
				return nil, err
			}
		}
		return vm.runProgramVsockWithCaptureAndStdin(ctx, prog, flags, stdin)
	}

	// Fallback to non-capture mode for non-vsock path
	err := vm.runProgram(ctx, prog, nil)
	if err != nil {
		if exitErr, ok := err.(*ExitError); ok {
			return &ProgramResult{ExitCode: int32(exitErr.Code)}, nil
		}
		return nil, err
	}
	return &ProgramResult{ExitCode: 0}, nil
}

// runProgramVsockWithCapture runs a program using vsock with output capture.
func (vm *VirtualMachine) runProgramVsockWithCapture(ctx context.Context, prog *ir.Program, flags uint32) (*ProgramResult, error) {
	return vm.runProgramVsockWithCaptureAndStdin(ctx, prog, flags, nil)
}

// runProgramVsockWithCaptureAndStdin runs a program using vsock with output capture and optional stdin.
func (vm *VirtualMachine) runProgramVsockWithCaptureAndStdin(ctx context.Context, prog *ir.Program, flags uint32, stdin []byte) (*ProgramResult, error) {
	debug.Writef("initx.runProgramVsockWithCaptureAndStdin", "sending program with flags=0x%x, stdin=%d bytes", flags, len(stdin))

	// Send program over vsock with capture flags and stdin
	if err := vm.vsockProgramServer.SendProgramWithFlagsAndStdin(prog, flags, stdin); err != nil {
		return nil, fmt.Errorf("send program via vsock: %w", err)
	}

	debug.Writef("initx.runProgramVsockWithCaptureAndStdin", "waiting for result")

	// Wait for result with capture
	result, err := vm.vsockProgramServer.WaitResultWithCapture(ctx, flags)
	if err != nil {
		debug.Writef("initx.runProgramVsockWithCaptureAndStdin", "wait error: %v", err)
		return nil, fmt.Errorf("wait for vsock result: %w", err)
	}

	debug.Writef("initx.runProgramVsockWithCaptureAndStdin", "got exit code: %d, stdout=%d bytes, stderr=%d bytes",
		result.ExitCode, len(result.Stdout), len(result.Stderr))

	return result, nil
}

func (vm *VirtualMachine) Architecture() hv.CpuArchitecture {
	return vm.vm.Hypervisor().Architecture()
}

// CaptureSnapshot captures a snapshot of the VM state.
func (vm *VirtualMachine) CaptureSnapshot() (hv.Snapshot, error) {
	return vm.vm.CaptureSnapshot()
}

// RestoreSnapshot restores a VM from a snapshot.
func (vm *VirtualMachine) RestoreSnapshot(snap hv.Snapshot) error {
	if err := vm.vm.RestoreSnapshot(snap); err != nil {
		return err
	}
	// Mark first run as complete since the vCPU state is already configured from the snapshot
	vm.firstRunComplete = true
	return nil
}

// HVVirtualMachine returns the underlying hv.VirtualMachine for low-level operations.
func (vm *VirtualMachine) HVVirtualMachine() hv.VirtualMachine {
	return vm.vm
}

// TimesliceMMIOPhysAddr returns the physical address of the timeslice MMIO region.
func (vm *VirtualMachine) TimesliceMMIOPhysAddr() uint64 {
	if vm.timesliceMMIOPhysAddr != 0 {
		return vm.timesliceMMIOPhysAddr
	}
	return 0xf0001000 // fallback to default
}

func (vm *VirtualMachine) VirtualCPUCall(id int, f func(vcpu hv.VirtualCPU) error) error {
	return vm.vm.VirtualCPUCall(id, f)
}

func (vm *VirtualMachine) DumpStackTrace(vcpu hv.VirtualCPU) (int64, error) {
	kernel, _, err := vm.loader.GetKernel()
	if err != nil {
		return -1, fmt.Errorf("get kernel for stack trace: %w", err)
	}

	systemMap, err := vm.loader.GetSystemMap()
	if err != nil {
		return -1, fmt.Errorf("get system map for stack trace: %w", err)
	}

	trace, err := amd64.CaptureStackTrace(vcpu, kernel, func() (io.ReaderAt, error) {
		return systemMap, nil
	}, 16)

	for i, trace := range trace {
		fmt.Fprintf(os.Stderr, "%02d | 0x%x: %s+0x%x\n", i, trace.PC, trace.Symbol, trace.Offset)
	}

	if err != nil {
		// slog.Error("capture stack trace", "error", err)
	}

	if len(trace) == 0 {
		return -1, errors.New("no stack trace available")
	}

	return int64(trace[0].PhysAddr), nil
}

func (vm *VirtualMachine) runProgram(
	ctx context.Context,
	prog *ir.Program,
	onUserYield func(context.Context, hv.VirtualCPU) error,
) error {
	var configureVcpu func(vcpu hv.VirtualCPU) error

	if !vm.firstRunComplete {
		configureVcpu = func(vcpu hv.VirtualCPU) error {
			return vm.loader.ConfigureVCPU(vcpu)
		}
		vm.firstRunComplete = true
	}

	return vm.vm.Run(ctx, &programRunner{
		program:       prog,
		loader:        vm.programLoader,
		configureVcpu: configureVcpu,
		onUserYield:   onUserYield,
	})
}

type Option interface {
	apply(vm *VirtualMachine) error
}

type funcOption func(vm *VirtualMachine) error

// apply implements Option.
func (f funcOption) apply(vm *VirtualMachine) error {
	return f(vm)
}

var (
	_ Option = funcOption(nil)
)

func WithDeviceTemplate(dev hv.DeviceTemplate) Option {
	return funcOption(func(vm *VirtualMachine) error {
		// Wrap VsockTemplate to capture the device for proper cleanup
		if vsockTemplate, ok := dev.(virtio.VsockTemplate); ok {
			vm.loader.Devices = append(vm.loader.Devices, &vsockCapturingTemplate{
				inner:  vsockTemplate,
				target: &vm.vsockDevice,
			})
			return nil
		}
		vm.loader.Devices = append(vm.loader.Devices, dev)
		return nil
	})
}

func WithFileFromBytes(guestPath string, data []byte, mode os.FileMode) Option {
	return funcOption(func(vm *VirtualMachine) error {
		vm.loader.AdditionalFiles = append(vm.loader.AdditionalFiles, boot.InitFile{
			Path: guestPath,
			Data: data,
			Mode: mode,
		})
		return nil
	})
}

func WithDebugLogging(enabled bool) Option {
	return funcOption(func(vm *VirtualMachine) error {
		vm.debugLogging = enabled
		return nil
	})
}

func WithDmesgLogging(enabled bool) Option {
	return funcOption(func(vm *VirtualMachine) error {
		if enabled {
			vm.dmesgLogging = true
		}
		return nil
	})
}

func WithGPUEnabled(enabled bool) Option {
	return funcOption(func(vm *VirtualMachine) error {
		vm.gpuEnabled = enabled
		return nil
	})
}

func WithQEMUEmulationEnabled(enabled bool) Option {
	return funcOption(func(vm *VirtualMachine) error {
		vm.qemuEmulationEnabled = enabled
		return nil
	})
}

func WithStdin(r io.Reader) Option {
	return funcOption(func(vm *VirtualMachine) error {
		if r != nil {
			// Store the reader for later - it will be activated when StartStdinForwarding is called.
			// This allows the stdin to be forwarded after VM boot, avoiding early data delivery
			// to the init process instead of the user command.
			debug.Writef("initx.WithStdin", "storing stdin reader %T for later activation", r)
			vm.pendingStdin = r
		} else {
			debug.Writef("initx.WithStdin", "stdin reader is nil")
		}
		return nil
	})
}

// WithConsoleOutput redirects the VM console output (virtio-console) to w.
// If nil, the default (stderr) is used.
func WithConsoleOutput(w io.Writer) Option {
	return funcOption(func(vm *VirtualMachine) error {
		if w != nil {
			vm.outBuffer.w = w
		}
		return nil
	})
}

// WithSnapshot configures the VM to restore from the given snapshot after creation.
// This skips kernel loading since the snapshot contains the full VM state.
// The snapshot will be restored automatically after VM creation.
func WithSnapshot(snap hv.Snapshot) Option {
	return funcOption(func(vm *VirtualMachine) error {
		vm.pendingSnapshot = snap
		vm.loader.SkipKernelLoad = true
		return nil
	})
}

// WithVsockProgramLoader configures the VM to use vsock for program loading.
// The vsock backend must already have a vsock device added to the VM.
// This option should be used together with a vsock device template.
func WithVsockProgramLoader(backend virtio.VsockBackend, port uint32) Option {
	return funcOption(func(vm *VirtualMachine) error {
		vm.vsockProgramBackend = backend
		vm.vsockProgramPort = port
		vm.useVsockLoader = true
		return nil
	})
}

// consoleCapturingTemplate wraps a ConsoleTemplate to capture the created device reference.
type consoleCapturingTemplate struct {
	inner  virtio.ConsoleTemplate
	target **virtio.Console
}

func (t *consoleCapturingTemplate) Create(vm hv.VirtualMachine) (hv.Device, error) {
	dev, err := t.inner.Create(vm)
	if err != nil {
		return nil, err
	}
	if c, ok := dev.(*virtio.Console); ok {
		*t.target = c
	}
	return dev, nil
}

func (t *consoleCapturingTemplate) GetLinuxCommandLineParam() ([]string, error) {
	return t.inner.GetLinuxCommandLineParam()
}

func (t *consoleCapturingTemplate) DeviceTreeNodes() ([]fdt.Node, error) {
	return t.inner.DeviceTreeNodes()
}

func (t *consoleCapturingTemplate) GetACPIDeviceInfo() virtio.ACPIDeviceInfo {
	return t.inner.GetACPIDeviceInfo()
}

// gpuCapturingTemplate wraps a GPUTemplate to capture the created device reference
type gpuCapturingTemplate struct {
	inner  virtio.GPUTemplate
	target **virtio.GPU
}

func (t *gpuCapturingTemplate) Create(vm hv.VirtualMachine) (hv.Device, error) {
	dev, err := t.inner.Create(vm)
	if err != nil {
		return nil, err
	}
	if gpu, ok := dev.(*virtio.GPU); ok {
		*t.target = gpu
	}
	return dev, nil
}

func (t *gpuCapturingTemplate) GetLinuxCommandLineParam() ([]string, error) {
	return t.inner.GetLinuxCommandLineParam()
}

func (t *gpuCapturingTemplate) DeviceTreeNodes() ([]fdt.Node, error) {
	return t.inner.DeviceTreeNodes()
}

func (t *gpuCapturingTemplate) GetACPIDeviceInfo() virtio.ACPIDeviceInfo {
	return t.inner.GetACPIDeviceInfo()
}

// inputCapturingTemplate wraps an InputTemplate to capture the created device reference
type inputCapturingTemplate struct {
	inner  virtio.InputTemplate
	target **virtio.Input
}

func (t *inputCapturingTemplate) Create(vm hv.VirtualMachine) (hv.Device, error) {
	dev, err := t.inner.Create(vm)
	if err != nil {
		return nil, err
	}
	if input, ok := dev.(*virtio.Input); ok {
		*t.target = input
	}
	return dev, nil
}

func (t *inputCapturingTemplate) GetLinuxCommandLineParam() ([]string, error) {
	return t.inner.GetLinuxCommandLineParam()
}

func (t *inputCapturingTemplate) DeviceTreeNodes() ([]fdt.Node, error) {
	return t.inner.DeviceTreeNodes()
}

func (t *inputCapturingTemplate) GetACPIDeviceInfo() virtio.ACPIDeviceInfo {
	return t.inner.GetACPIDeviceInfo()
}

// vsockCapturingTemplate wraps a VsockTemplate to capture the created device reference
type vsockCapturingTemplate struct {
	inner  virtio.VsockTemplate
	target **virtio.Vsock
}

func (t *vsockCapturingTemplate) Create(vm hv.VirtualMachine) (hv.Device, error) {
	dev, err := t.inner.Create(vm)
	if err != nil {
		return nil, err
	}
	if vsock, ok := dev.(*virtio.Vsock); ok {
		*t.target = vsock
	}
	return dev, nil
}

func (t *vsockCapturingTemplate) GetLinuxCommandLineParam() ([]string, error) {
	return t.inner.GetLinuxCommandLineParam()
}

func (t *vsockCapturingTemplate) DeviceTreeNodes() ([]fdt.Node, error) {
	return t.inner.DeviceTreeNodes()
}

func (t *vsockCapturingTemplate) GetACPIDeviceInfo() virtio.ACPIDeviceInfo {
	return t.inner.GetACPIDeviceInfo()
}

func NewVirtualMachine(
	h hv.Hypervisor,
	numCPUs int,
	memSizeMB uint64,
	kernelLoader kernel.Kernel,
	options ...Option,
) (*VirtualMachine, error) {
	rec := timeslice.NewState()
	rec.Record(tsInitxNewVMStart)

	in := &proxyReader{update: make(chan io.Reader)}
	out := &proxyWriter{w: os.Stderr} // default to stderr so we can see debugging output

	programLoader := &programLoader{
		arch: h.Architecture(),
	}

	var ret VirtualMachine

	ret.outBuffer = out
	ret.inBuffer = in

	ret.programLoader = programLoader
	ret.kernelLoader = kernelLoader

	// Get cache directory for kernel decompression caching
	cacheDir, _ := kernel.GetDefaultCachePath()

	// No longer cap memory - MMIO regions are now allocated dynamically above RAM
	ret.loader = &boot.LinuxLoader{
		NumCPUs:  numCPUs,
		CacheDir: cacheDir,

		MemSize: memSizeMB << 20,
		MemBase: func() uint64 {
			switch h.Architecture() {
			case hv.ArchitectureARM64:
				return 0x80000000
			default:
				return 0
			}
		}(),

		GetKernel: func() (io.ReaderAt, int64, error) {
			if kernelLoader == nil {
				return nil, 0, fmt.Errorf("kernel not loaded (use WithSnapshot for snapshot restore)")
			}

			size, err := kernelLoader.Size()
			if err != nil {
				return nil, 0, fmt.Errorf("get kernel size: %v", err)
			}

			kernel, err := kernelLoader.Open()
			if err != nil {
				return nil, 0, fmt.Errorf("open kernel: %v", err)
			}

			return kernel, size, nil
		},

		GetSystemMap: func() (io.ReaderAt, error) {
			if kernelLoader == nil {
				return nil, fmt.Errorf("kernel not loaded (use WithSnapshot for snapshot restore)")
			}
			return kernelLoader.GetSystemMap()
		},

		GetInit: func(arch hv.CpuArchitecture) (*ir.Program, error) {
			if kernelLoader == nil {
				return nil, fmt.Errorf("kernel not loaded (use WithSnapshot for snapshot restore)")
			}

			// Vsock is always enabled (MMIO paths have been removed)
			cfg := BuilderConfig{
				Arch:                  arch,
				TimesliceMMIOPhysAddr: ret.timesliceMMIOPhysAddr,
				VsockPort:             ret.vsockProgramPort,
			}

			modules, err := kernelLoader.PlanModuleLoad(
				[]string{
					"CONFIG_VIRTIO_MMIO",
					"CONFIG_VIRTIO_BLK",
					"CONFIG_VIRTIO_NET",
					"CONFIG_VIRTIO_CONSOLE",
					"CONFIG_VIRTIO_FS",
					"CONFIG_PACKET",
					"CONFIG_VSOCKETS",
					"CONFIG_VIRTIO_VSOCKETS",
				},
				map[string]string{
					"CONFIG_VIRTIO_BLK":      "kernel/drivers/block/virtio_blk.ko.gz",
					"CONFIG_VIRTIO_NET":      "kernel/drivers/net/virtio_net.ko.gz",
					"CONFIG_VIRTIO_MMIO":     "kernel/drivers/virtio/virtio_mmio.ko.gz",
					"CONFIG_VIRTIO_FS":       "kernel/fs/fuse/virtiofs.ko.gz",
					"CONFIG_PACKET":          "kernel/net/packet/af_packet.ko.gz",
					"CONFIG_VSOCKETS":        "kernel/net/vmw_vsock/vsock.ko.gz",
					"CONFIG_VIRTIO_VSOCKETS": "kernel/net/vmw_vsock/vmw_vsock_virtio_transport.ko.gz",
				},
			)
			if err != nil {
				return nil, fmt.Errorf("plan module load: %v", err)
			}

			cfg.PreloadModules = append(cfg.PreloadModules, modules...)

			// Load GPU modules if GPU is enabled
			if ret.gpuEnabled {
				gpuModules, err := kernelLoader.PlanModuleLoad(
					kernel.GPUModuleConfigs,
					kernel.GPUModuleMap,
				)
				if err != nil {
					return nil, fmt.Errorf("plan GPU module load: %v", err)
				}
				cfg.PreloadModules = append(cfg.PreloadModules, gpuModules...)
			}

			// Load binfmt_misc module if QEMU emulation is enabled
			if ret.qemuEmulationEnabled {
				binfmtModules, err := kernelLoader.PlanModuleLoad(
					kernel.BinfmtMiscModuleConfigs,
					kernel.BinfmtMiscModuleMap,
				)
				if err != nil {
					return nil, fmt.Errorf("plan binfmt_misc module load: %v", err)
				}
				cfg.PreloadModules = append(cfg.PreloadModules, binfmtModules...)
			}

			return Build(cfg)
		},

		SerialStdout: out,
		// SerialStdin is intentionally not set - stdin goes to virtio-console (hvc0) instead
		// SerialStdin:  in,

		GetCmdline: func(arch hv.CpuArchitecture) ([]string, error) {
			var args []string

			if ret.debugLogging {
				if arch == hv.ArchitectureX86_64 {
					args = append(args,
						"console=ttyS0,115200n8",
						"earlycon=uart8250,io,0x3f8",
					)
				} else {
					args = append(args,
						"console=ttyS0,115200n8",
						"earlycon=uart,mmio,0x09000000,115200",
					)
				}
			} else {
				args = append(args, "console=hvc0")
			}

			if ret.dmesgLogging {
				args = append(args, "loglevel=7")
			} else {
				args = append(args, "loglevel=3")
			}

			args = append(args,
				"reboot=k",
				"panic=-1",
			)

			switch h.Architecture() {
			case hv.ArchitectureX86_64:
				// HACK: Some systems don't have kvm_clock, so we need to use tsc=reliable
				args = append(args, []string{
					"tsc=reliable",
					"tsc_early_khz=3000000",
				}...)
			case hv.ArchitectureARM64:
				args = append(args, "iomem=relaxed")
			default:
				return nil, fmt.Errorf("unsupported architecture for cmdline: %v", arch)
			}

			// slog.Info("booting with cmdline", "args", args)

			return args, nil
		},
	}

	ret.loader.Devices = append(ret.loader.Devices,
		&consoleCapturingTemplate{
			inner: virtio.ConsoleTemplate{
				MMIODeviceTemplateBase: virtio.MMIODeviceTemplateBase{
					Arch:   h.Architecture(),
					Config: virtio.ConsoleDeviceConfig(),
				},
				Out: out,
				In:  in,
			},
			target: &ret.consoleDevice,
		},
		programLoader,
	)

	ret.loader.CreateVMWithMemory = func(vm hv.VirtualMachine) error {
		// Note: Timeslice MMIO is no longer allocated since the MMIO handlers
		// were removed as part of the vsock migration. The guest init will
		// fail to mmap the timeslice region (since it doesn't exist) and
		// gracefully skip timeslice recording.
		// ret.timesliceMMIOPhysAddr remains 0, causing the guest to use the
		// default address (0xf0001000) which won't be mapped.
		return nil
	}

	rec.Record(tsInitxNewVMSetupLoader)

	for _, option := range options {
		if err := option.apply(&ret); err != nil {
			return nil, err
		}
	}

	rec.Record(tsInitxNewVMApplyOptions)

	// Add GPU and Input devices if GPU is enabled
	if ret.gpuEnabled {
		ret.loader.Devices = append(ret.loader.Devices,
			&gpuCapturingTemplate{
				inner:  virtio.GPUTemplate{Arch: h.Architecture()},
				target: &ret.gpuDevice,
			},
			&inputCapturingTemplate{
				inner:  virtio.InputTemplate{Arch: h.Architecture(), Type: virtio.InputTypeKeyboard},
				target: &ret.keyboardDevice,
			},
			&inputCapturingTemplate{
				inner:  virtio.InputTemplate{Arch: h.Architecture(), Type: virtio.InputTypeTablet},
				target: &ret.tabletDevice,
			},
		)
	}

	var err error
	ret.vm, err = h.NewVirtualMachine(ret.loader)
	if err != nil {
		return nil, err
	}

	rec.Record(tsInitxNewVMCreateHVVM)

	// Restore pending snapshot if one was provided via WithSnapshot option
	if ret.pendingSnapshot != nil {
		if err := ret.vm.RestoreSnapshot(ret.pendingSnapshot); err != nil {
			ret.vm.Close()
			return nil, fmt.Errorf("restore snapshot: %w", err)
		}
		ret.firstRunComplete = true // Mark as booted since snapshot is post-boot state
		rec.Record(tsInitxNewVMRestoreSnap)
	}

	// Create vsock program server if vsock loader is enabled
	if ret.useVsockLoader && ret.vsockProgramBackend != nil {
		port := ret.vsockProgramPort
		if port == 0 {
			port = VsockProgramPort
		}
		server, err := NewVsockProgramServer(ret.vsockProgramBackend, port, h.Architecture())
		if err != nil {
			ret.vm.Close()
			return nil, fmt.Errorf("create vsock program server: %w", err)
		}
		ret.vsockProgramServer = server
	}

	rec.Record(tsInitxNewVMDone)

	return &ret, nil
}

// Spawn executes path inside the guest using fork/exec, waiting for it to complete.
func (vm *VirtualMachine) Spawn(ctx context.Context, path string, args ...string) error {
	if vm == nil {
		return fmt.Errorf("initx: virtual machine is nil")
	}
	if path == "" {
		return fmt.Errorf("initx: executable path must not be empty")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	errLabel := ir.Label("__initx_spawn_err")
	execErrLabel := ir.Label("__initx_spawn_child_err")
	errVar := ir.Var("__initx_spawn_errno")
	errorFmt := fmt.Sprintf("initx: failed to spawn %s errno=0x%%x\n", path)

	prog := &ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": {
				ForkExecWait(path, args, nil, errLabel, execErrLabel, errVar),
				ir.Return(errVar),
				ir.DeclareLabel(errLabel, ir.Block{
					ir.Printf(errorFmt, ir.Op(ir.OpSub, ir.Int64(0), errVar)),
					ir.Return(errVar),
				}),
				ir.DeclareLabel(execErrLabel, ir.Block{
					ir.Syscall(defs.SYS_EXIT, ir.Int64(1)),
				}),
			},
		},
	}

	return vm.Run(ctx, prog)
}

// AddKernelModulesToVFS adds kernel module files to the VFS backend for modprobe support.
// This function should be called after creating the VFS backend and loading the kernel,
// before creating the VirtualMachine.
func AddKernelModulesToVFS(fsBackend vfs.VirtioFsBackend, kernelLoader kernel.Kernel) error {
	kernelVersion, err := kernelLoader.GetKernelVersion()
	if err != nil {
		return fmt.Errorf("get kernel version: %w", err)
	}
	moduleFiles, err := kernelLoader.GetModulesDirectory()
	if err != nil {
		return fmt.Errorf("get kernel modules: %w", err)
	}

	// Convert kernel.ModuleFile to vfs.ModuleFile
	vfsModuleFiles := make([]vfs.ModuleFile, len(moduleFiles))
	for i, mf := range moduleFiles {
		vfsModuleFiles[i] = vfs.ModuleFile{
			Path: mf.Path,
			Data: mf.Data,
			Mode: mf.Mode,
		}
	}

	if err := fsBackend.AddKernelModules(kernelVersion, vfsModuleFiles); err != nil {
		return fmt.Errorf("add kernel modules: %w", err)
	}

	return nil
}
