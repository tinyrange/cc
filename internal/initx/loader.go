package initx

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
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
	linux "github.com/tinyrange/cc/internal/linux/defs/amd64"
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
	// Default region sizes for MMIO allocations
	mailboxRegionSize = 0x1000
	configRegionSize  = 4 * 1024 * 1024

	configHeaderMagicValue = 0xcafebabe
	configHeaderSize       = 40
	configHeaderRelocOff   = configHeaderSize
	configDataOffsetField  = 12
	configDataLengthField  = 16

	writeFileLengthPrefix   = 2
	writeFileMaxChunkLen    = (1 << 16) - 1
	writeFileTransferRegion = writeFileMaxChunkLen + writeFileLengthPrefix

	userYieldValue = 0x5553_4552 // "USER"

	// Config region command format (at offset 0x100000):
	// Offset 0: Magic (0x434D4452 = "CMDR")
	// Offset 4: path_len (uint32)
	// Offset 8: argc (uint32)
	// Offset 12: envc (uint32)
	// Offset 16: path\0 + args\0...\0 + envs\0...\0
	execCmdRegionOffset  = 0x100000
	execCmdMagicValue    = 0x434D4452 // "CMDR"
	execCmdPathLenOffset = 4
	execCmdArgcOffset    = 8
	execCmdEnvcOffset    = 12
	execCmdDataOffset    = 16
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

	// Dynamically allocated MMIO addresses
	mailboxPhysAddr      uint64
	configRegionPhysAddr uint64
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

// MMIORegions implements hv.MemoryMappedIODevice.
func (p *programLoader) MMIORegions() []hv.MMIORegion {
	return []hv.MMIORegion{
		{Address: p.mailboxPhysAddr, Size: mailboxRegionSize},
		{Address: p.configRegionPhysAddr, Size: configRegionSize},
	}
}

// ReadMMIO implements hv.MemoryMappedIODevice.
func (p *programLoader) ReadMMIO(ctx hv.ExitContext, addr uint64, data []byte) error {
	if addr >= p.configRegionPhysAddr && addr < p.configRegionPhysAddr+configRegionSize {
		offset := addr - p.configRegionPhysAddr
		copy(data, p.dataRegion[offset:])
		return nil
	}

	return fmt.Errorf("unimplemented read at address 0x%x", addr)
}

// DeviceTreeNodes returns device tree nodes for the initx mailbox and config regions.
// This allows /dev/mem access on ARM64 by declaring these as device I/O regions.
func (p *programLoader) DeviceTreeNodes() ([]fdt.Node, error) {
	if p.arch != hv.ArchitectureARM64 {
		return nil, nil
	}
	return []fdt.Node{
		{
			Name: fmt.Sprintf("initx-mailbox@%x", p.mailboxPhysAddr),
			Properties: map[string]fdt.Property{
				"compatible": {Strings: []string{"tinyrange,initx-mailbox"}},
				"reg":        {U64: []uint64{p.mailboxPhysAddr, mailboxRegionSize}},
				"status":     {Strings: []string{"okay"}},
			},
		},
		{
			Name: fmt.Sprintf("initx-config@%x", p.configRegionPhysAddr),
			Properties: map[string]fdt.Property{
				"compatible": {Strings: []string{"tinyrange,initx-config"}},
				"reg":        {U64: []uint64{p.configRegionPhysAddr, configRegionSize}},
				"status":     {Strings: []string{"okay"}},
			},
		},
	}, nil
}

// WriteMMIO implements hv.MemoryMappedIODevice.
func (p *programLoader) WriteMMIO(ctx hv.ExitContext, addr uint64, data []byte) error {
	// Check if write is to config region (for command loop communication)
	if addr >= p.configRegionPhysAddr && addr < p.configRegionPhysAddr+configRegionSize {
		offset := addr - p.configRegionPhysAddr
		copy(p.dataRegion[offset:], data)
		return nil
	}

	// Handle mailbox writes
	offset := addr - p.mailboxPhysAddr

	switch offset {
	case mailboxRunResultDetailOffset:
		p.runResultDetail = binary.LittleEndian.Uint32(data)
		return nil
	case mailboxRunResultStageOffset:
		p.runResultStage = binary.LittleEndian.Uint32(data)
		return nil
	case mailboxStartResultDetailOffset, mailboxStartResultStageOffset:
		return nil
	case 0x0:
		value := binary.LittleEndian.Uint32(data)
		switch value {
		case 0x444f4e45, snapshotRequestValue:
			// Clear runResultDetail when guest signals done/ready
			// to indicate successful yield (not an error)
			p.runResultDetail = 0
			return hv.ErrYield
		case userYieldValue:
			return hv.ErrUserYield
		}
	default:
		// no-op
	}

	return fmt.Errorf("unimplemented write at address 0x%x", offset)
}

func (p *programLoader) LoadProgram(prog *ir.Program) error {
	asmProg, err := ir.BuildStandaloneProgramForArch(p.arch, prog)
	if err != nil {
		return fmt.Errorf("build standalone program: %w", err)
	}
	p.runResultDetail = 0
	p.runResultStage = 0

	if len(p.dataRegion) < configRegionSize {
		p.dataRegion = make([]byte, configRegionSize)
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
	_ hv.MemoryMappedIODevice = &programLoader{}
	_ hv.DeviceTemplate       = &programLoader{}
)

// VsockProgramServer handles program loading via vsock.
// Protocol:
//   - Host → Guest: [len:4][code_len:4][reloc_count:4][relocs:4*count][code:code_len]
//   - Guest → Host: [len:4][exit_code:4]
const (
	VsockProgramPort = 9998
	VsockCmdPort     = 9997 // Port for container command execution via vsock
)

// Vsock command protocol message types
const (
	vsockMsgReady   = 0x52454459 // "REDY" - guest -> host: container ready
	vsockMsgCmdDone = 0x444F4E45 // "DONE" - guest -> host: command done (followed by exit code)
	vsockMsgExecCmd = 0x45584543 // "EXEC" - host -> guest: execute command
)

// VsockProgramServer is the host-side server for vsock-based program loading.
type VsockProgramServer struct {
	listener virtio.VsockListener
	conn     virtio.VsockConn
	arch     hv.CpuArchitecture
	mu       sync.Mutex
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

// Accept waits for a guest connection.
func (s *VsockProgramServer) Accept() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	conn, err := s.listener.Accept()
	if err != nil {
		return fmt.Errorf("accept connection: %w", err)
	}
	s.conn = conn
	return nil
}

// SendProgram sends a compiled program to the guest.
func (s *VsockProgramServer) SendProgram(prog *ir.Program) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.conn == nil {
		return fmt.Errorf("no connection established")
	}

	asmProg, err := ir.BuildStandaloneProgramForArch(s.arch, prog)
	if err != nil {
		return fmt.Errorf("build standalone program: %w", err)
	}

	progBytes := asmProg.Bytes()
	relocs := asmProg.Relocations()

	// Calculate total message size: code_len(4) + reloc_count(4) + relocs(4*N) + code
	payloadLen := 4 + 4 + 4*len(relocs) + len(progBytes)

	// Build the message
	msg := make([]byte, 4+payloadLen) // len prefix + payload

	// Length prefix (excludes itself)
	binary.LittleEndian.PutUint32(msg[0:4], uint32(payloadLen))

	// code_len
	binary.LittleEndian.PutUint32(msg[4:8], uint32(len(progBytes)))

	// reloc_count
	binary.LittleEndian.PutUint32(msg[8:12], uint32(len(relocs)))

	// relocations
	for i, reloc := range relocs {
		binary.LittleEndian.PutUint32(msg[12+4*i:], uint32(reloc))
	}

	// code
	copy(msg[12+4*len(relocs):], progBytes)

	// Write the entire message
	if _, err := s.conn.Write(msg); err != nil {
		return fmt.Errorf("write program: %w", err)
	}

	return nil
}

// WaitResult waits for and returns the exit code from the guest.
func (s *VsockProgramServer) WaitResult(ctx context.Context) (int32, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.conn == nil {
		return -1, fmt.Errorf("no connection established")
	}

	// Read length prefix (4 bytes) + exit code (4 bytes)
	buf := make([]byte, 8)
	done := make(chan error, 1)

	go func() {
		_, err := io.ReadFull(s.conn, buf)
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			return -1, fmt.Errorf("read result: %w", err)
		}
	case <-ctx.Done():
		return -1, ctx.Err()
	}

	length := binary.LittleEndian.Uint32(buf[0:4])
	if length != 4 {
		return -1, fmt.Errorf("unexpected result length: %d", length)
	}

	exitCode := int32(binary.LittleEndian.Uint32(buf[4:8]))
	return exitCode, nil
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

// VsockCommandServer is the host-side server for vsock-based container command execution.
// This is separate from VsockProgramServer and handles the command loop protocol.
// Protocol:
//   - Guest -> Host: [type:4=READY]
//   - Host -> Guest: [type:4=EXEC][data_len:4][path_len:4][argc:4][envc:4][path\0][args\0...][envs\0...]
//   - Guest -> Host: [type:4=DONE][exit_code:4]
type VsockCommandServer struct {
	listener virtio.VsockListener
	conn     virtio.VsockConn
	mu       sync.Mutex
	ready    bool
}

// NewVsockCommandServer creates a new vsock command server listening on the specified port.
func NewVsockCommandServer(backend virtio.VsockBackend, port uint32) (*VsockCommandServer, error) {
	listener, err := backend.Listen(port)
	if err != nil {
		return nil, fmt.Errorf("listen on port %d: %w", port, err)
	}
	return &VsockCommandServer{
		listener: listener,
	}, nil
}

// Accept waits for a guest connection.
func (s *VsockCommandServer) Accept() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	conn, err := s.listener.Accept()
	if err != nil {
		return fmt.Errorf("accept connection: %w", err)
	}
	s.conn = conn
	s.ready = false
	return nil
}

// WaitReady waits for the READY message from the guest.
// This should be called after Accept() to confirm the container is ready.
func (s *VsockCommandServer) WaitReady(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.conn == nil {
		return fmt.Errorf("no connection established")
	}

	// Read the 4-byte READY message type
	buf := make([]byte, 4)
	done := make(chan error, 1)

	go func() {
		_, err := io.ReadFull(s.conn, buf)
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("read ready message: %w", err)
		}
	case <-ctx.Done():
		return ctx.Err()
	}

	msgType := binary.LittleEndian.Uint32(buf)
	if msgType != vsockMsgReady {
		return fmt.Errorf("unexpected message type: 0x%x, expected READY (0x%x)", msgType, vsockMsgReady)
	}

	s.ready = true
	debug.Writef("initx.VsockCommandServer", "received READY from guest")
	return nil
}

// SendCommand sends a command to the guest for execution.
// The command is sent as: [type:4=EXEC][data_len:4][path_len:4][argc:4][envc:4][path\0][args\0...][envs\0...]
func (s *VsockCommandServer) SendCommand(path string, args []string, envs []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.conn == nil {
		return fmt.Errorf("no connection established")
	}
	if !s.ready {
		return fmt.Errorf("guest not ready")
	}

	// Calculate data size: path + null + each arg + null + each env + null
	dataSize := len(path) + 1
	for _, arg := range args {
		dataSize += len(arg) + 1
	}
	for _, env := range envs {
		dataSize += len(env) + 1
	}

	// Build the message:
	// Header: type(4) + data_len(4) + path_len(4) + argc(4) + envc(4) = 20 bytes
	// Data: path\0 + args\0... + envs\0...
	headerSize := 20
	msg := make([]byte, headerSize+dataSize)

	// Header
	binary.LittleEndian.PutUint32(msg[0:4], vsockMsgExecCmd)
	binary.LittleEndian.PutUint32(msg[4:8], uint32(dataSize))
	binary.LittleEndian.PutUint32(msg[8:12], uint32(len(path)))
	binary.LittleEndian.PutUint32(msg[12:16], uint32(len(args)))
	binary.LittleEndian.PutUint32(msg[16:20], uint32(len(envs)))

	// Data: path\0 + args\0... + envs\0...
	offset := headerSize
	copy(msg[offset:], path)
	offset += len(path)
	msg[offset] = 0
	offset++

	for _, arg := range args {
		copy(msg[offset:], arg)
		offset += len(arg)
		msg[offset] = 0
		offset++
	}

	for _, env := range envs {
		copy(msg[offset:], env)
		offset += len(env)
		msg[offset] = 0
		offset++
	}

	// Send the message
	if _, err := s.conn.Write(msg); err != nil {
		return fmt.Errorf("write command: %w", err)
	}

	debug.Writef("initx.VsockCommandServer", "sent command: %s with %d args, %d envs", path, len(args), len(envs))
	return nil
}

// WaitResult waits for and returns the exit code from the guest.
// The result is received as: [type:4=DONE][exit_code:4]
func (s *VsockCommandServer) WaitResult(ctx context.Context) (int32, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.conn == nil {
		return -1, fmt.Errorf("no connection established")
	}

	// Read the 8-byte result: type(4) + exit_code(4)
	buf := make([]byte, 8)
	done := make(chan error, 1)

	go func() {
		_, err := io.ReadFull(s.conn, buf)
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			return -1, fmt.Errorf("read result: %w", err)
		}
	case <-ctx.Done():
		return -1, ctx.Err()
	}

	msgType := binary.LittleEndian.Uint32(buf[0:4])
	if msgType != vsockMsgCmdDone {
		return -1, fmt.Errorf("unexpected message type: 0x%x, expected CMD_DONE (0x%x)", msgType, vsockMsgCmdDone)
	}

	exitCode := int32(binary.LittleEndian.Uint32(buf[4:8]))
	debug.Writef("initx.VsockCommandServer", "received exit code: %d", exitCode)
	return exitCode, nil
}

// Close closes the server and any active connection.
func (s *VsockCommandServer) Close() error {
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
	configRegion hv.MemoryRegion
	loader       *programLoader

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

	configRegion hv.MemoryRegion

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

	// Dynamically allocated MMIO addresses for initx regions
	mailboxPhysAddr      uint64
	configRegionPhysAddr uint64

	// pendingSnapshot holds a snapshot to restore after VM creation.
	// Set by WithSnapshot option.
	pendingSnapshot hv.Snapshot

	// Vsock program loading
	vsockProgramServer  *VsockProgramServer
	vsockProgramBackend virtio.VsockBackend
	vsockProgramPort    uint32
	useVsockLoader      bool

	// Vsock command execution (for container command loop mode)
	vsockCommandServer *VsockCommandServer
	vsockCmdPort       uint32
	useVsockCommands   bool
}

func (vm *VirtualMachine) Close() error {
	if vm.vsockProgramServer != nil {
		vm.vsockProgramServer.Close()
	}
	if vm.vsockCommandServer != nil {
		vm.vsockCommandServer.Close()
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

	// Start the VM in a goroutine so we can accept the vsock connection
	vmDone := make(chan error, 1)
	go func() {
		err := vm.vm.Run(ctx, &programRunner{
			configRegion: vm.configRegion,
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
	// Send program over vsock
	if err := vm.vsockProgramServer.SendProgram(prog); err != nil {
		return fmt.Errorf("send program via vsock: %w", err)
	}

	// Wait for result
	exitCode, err := vm.vsockProgramServer.WaitResult(ctx)
	if err != nil {
		return fmt.Errorf("wait for vsock result: %w", err)
	}

	if exitCode != 0 {
		return &ExitError{Code: int(exitCode)}
	}

	return nil
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

// WriteExecCommand writes a command to the config region for the guest command loop.
// The guest will read this command and execute it via fork/exec/wait.
// Format at offset 0x100000:
// - Magic (4 bytes): 0x434D4452 ("CMDR")
// - path_len (4 bytes): length of path string (excluding null terminator)
// - argc (4 bytes): number of arguments (including argv[0])
// - envc (4 bytes): number of environment variables
// - data: path\0 + args\0...\0 + envs\0...\0
func (vm *VirtualMachine) WriteExecCommand(path string, args []string, env []string) error {
	// Calculate total size needed
	dataSize := len(path) + 1 // path + null terminator
	for _, arg := range args {
		dataSize += len(arg) + 1
	}
	for _, e := range env {
		dataSize += len(e) + 1
	}

	// Check if data fits in the available region
	// Config region is 4MB, command region starts at 0x100000, leaving ~3MB for command data
	maxDataSize := configRegionSize - execCmdRegionOffset - execCmdDataOffset
	if dataSize > maxDataSize {
		return fmt.Errorf("command data too large: %d bytes (max %d)", dataSize, maxDataSize)
	}

	// Build the command buffer
	totalSize := execCmdDataOffset + dataSize
	buf := make([]byte, totalSize)

	// Write header
	binary.LittleEndian.PutUint32(buf[0:4], execCmdMagicValue)
	binary.LittleEndian.PutUint32(buf[execCmdPathLenOffset:], uint32(len(path)))
	binary.LittleEndian.PutUint32(buf[execCmdArgcOffset:], uint32(len(args)))
	binary.LittleEndian.PutUint32(buf[execCmdEnvcOffset:], uint32(len(env)))

	// Write data: path + args + env (all null-terminated)
	offset := execCmdDataOffset
	copy(buf[offset:], path)
	offset += len(path)
	buf[offset] = 0
	offset++

	for _, arg := range args {
		copy(buf[offset:], arg)
		offset += len(arg)
		buf[offset] = 0
		offset++
	}

	for _, e := range env {
		copy(buf[offset:], e)
		offset += len(e)
		buf[offset] = 0
		offset++
	}

	// Write to config region
	// Always write to dataRegion so the command survives LoadProgram calls
	// (LoadProgram copies dataRegion to region, which would overwrite the command)
	if len(vm.programLoader.dataRegion) < configRegionSize {
		vm.programLoader.dataRegion = make([]byte, configRegionSize)
	}
	copy(vm.programLoader.dataRegion[execCmdRegionOffset:], buf)

	// Also write directly to backing memory for immediate visibility
	if vm.programLoader.region != nil {
		if _, err := vm.programLoader.region.WriteAt(buf, int64(execCmdRegionOffset)); err != nil {
			return fmt.Errorf("write exec command to config region: %w", err)
		}
	}

	return nil
}

// ClearExecCommand clears the command magic in the config region.
// This should be called before capturing a snapshot to ensure the guest
// doesn't try to execute a stale command after restore.
func (vm *VirtualMachine) ClearExecCommand() error {
	// Write zero to the magic field
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, 0)

	// Always write to dataRegion so it survives LoadProgram calls
	if len(vm.programLoader.dataRegion) < configRegionSize {
		vm.programLoader.dataRegion = make([]byte, configRegionSize)
	}
	copy(vm.programLoader.dataRegion[execCmdRegionOffset:], buf)

	// Also write directly to backing memory for immediate visibility
	if vm.programLoader.region != nil {
		if _, err := vm.programLoader.region.WriteAt(buf, int64(execCmdRegionOffset)); err != nil {
			return fmt.Errorf("clear exec command magic: %w", err)
		}
	}

	return nil
}

// SetupVsockCommands initializes the vsock command server for container command execution.
// This should be called after the vsock backend is available and before starting the VM.
func (vm *VirtualMachine) SetupVsockCommands(backend virtio.VsockBackend, port uint32) error {
	if port == 0 {
		port = VsockCmdPort
	}
	server, err := NewVsockCommandServer(backend, port)
	if err != nil {
		return fmt.Errorf("create vsock command server: %w", err)
	}
	vm.vsockCommandServer = server
	vm.vsockCmdPort = port
	vm.useVsockCommands = true
	return nil
}

// AcceptVsockCommandConnection waits for the guest to connect to the vsock command server.
// This should be called after the VM has started and the container is initializing.
func (vm *VirtualMachine) AcceptVsockCommandConnection() error {
	if vm.vsockCommandServer == nil {
		return fmt.Errorf("vsock command server not initialized")
	}
	return vm.vsockCommandServer.Accept()
}

// WaitVsockReady waits for the READY message from the guest over vsock.
// This indicates the container is ready to receive commands.
func (vm *VirtualMachine) WaitVsockReady(ctx context.Context) error {
	if vm.vsockCommandServer == nil {
		return fmt.Errorf("vsock command server not initialized")
	}
	return vm.vsockCommandServer.WaitReady(ctx)
}

// SendVsockCommand sends a command to the guest for execution via vsock.
// This is the vsock-based alternative to WriteExecCommand.
func (vm *VirtualMachine) SendVsockCommand(path string, args []string, envs []string) error {
	if vm.vsockCommandServer == nil {
		return fmt.Errorf("vsock command server not initialized")
	}
	return vm.vsockCommandServer.SendCommand(path, args, envs)
}

// WaitVsockResult waits for the command execution result from the guest via vsock.
// Returns the exit code from the executed command.
func (vm *VirtualMachine) WaitVsockResult(ctx context.Context) (int32, error) {
	if vm.vsockCommandServer == nil {
		return -1, fmt.Errorf("vsock command server not initialized")
	}
	return vm.vsockCommandServer.WaitResult(ctx)
}

// UseVsockCommands returns true if vsock-based command execution is enabled.
func (vm *VirtualMachine) UseVsockCommands() bool {
	return vm.useVsockCommands
}

// HVVirtualMachine returns the underlying hv.VirtualMachine for low-level operations.
func (vm *VirtualMachine) HVVirtualMachine() hv.VirtualMachine {
	return vm.vm
}

// MailboxPhysAddr returns the physical address of the mailbox MMIO region.
func (vm *VirtualMachine) MailboxPhysAddr() uint64 {
	return vm.mailboxPhysAddr
}

// ConfigRegionPhysAddr returns the physical address of the config MMIO region.
func (vm *VirtualMachine) ConfigRegionPhysAddr() uint64 {
	return vm.configRegionPhysAddr
}

// TimesliceMMIOPhysAddr returns the physical address of the timeslice MMIO region.
// This is derived from the config region address (timeslice is at config - 0x2000).
func (vm *VirtualMachine) TimesliceMMIOPhysAddr() uint64 {
	// Timeslice MMIO is at a fixed offset before the config region
	// Default: 0xf0001000 when config is at 0xf0003000
	if vm.configRegionPhysAddr >= 0x2000 {
		return vm.configRegionPhysAddr - 0x2000
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
		configRegion:  vm.configRegion,
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

			// Calculate timeslice MMIO address (fixed offset before config region)
			timesliceAddr := uint64(0xf0001000) // default
			if ret.configRegionPhysAddr >= 0x2000 {
				timesliceAddr = ret.configRegionPhysAddr - 0x2000
			}

			cfg := BuilderConfig{
				Arch:                  arch,
				MailboxPhysAddr:       ret.mailboxPhysAddr,
				TimesliceMMIOPhysAddr: timesliceAddr,
				ConfigRegionPhysAddr:  ret.configRegionPhysAddr,
				UseVsock:              ret.useVsockLoader,
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
		// Allocate mailbox MMIO region dynamically above RAM
		mailboxAlloc, err := vm.AllocateMMIO(hv.MMIOAllocationRequest{
			Name:      "initx-mailbox",
			Size:      mailboxRegionSize,
			Alignment: 0x1000,
		})
		if err != nil {
			return fmt.Errorf("allocate initx mailbox region: %v", err)
		}
		ret.mailboxPhysAddr = mailboxAlloc.Base
		programLoader.mailboxPhysAddr = mailboxAlloc.Base

		// Allocate config MMIO region dynamically above RAM
		configAlloc, err := vm.AllocateMMIO(hv.MMIOAllocationRequest{
			Name:      "initx-config",
			Size:      configRegionSize,
			Alignment: 0x1000,
		})
		if err != nil {
			return fmt.Errorf("allocate initx config region: %v", err)
		}
		ret.configRegionPhysAddr = configAlloc.Base
		programLoader.configRegionPhysAddr = configAlloc.Base

		// On x86 (and non-ARM64 Linux/Darwin), we need to allocate backing memory
		// for the config region since it's not handled by the MMU like ARM64
		needsBackingMemory := true
		if runtime.GOOS == "linux" && h.Architecture() == hv.ArchitectureARM64 {
			needsBackingMemory = false
		}
		if runtime.GOOS == "darwin" && h.Architecture() == hv.ArchitectureARM64 {
			needsBackingMemory = false
		}

		if needsBackingMemory {
			mem, err := vm.AllocateMemory(configAlloc.Base, configRegionSize)
			if err != nil {
				return fmt.Errorf("allocate initx config backing memory: %v", err)
			}
			ret.configRegion = mem
			programLoader.region = mem
		}

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

// WriteFile copies data from in into the guest at guestPath using a shared buffer
// handshake between host and guest.
func (vm *VirtualMachine) WriteFile(ctx context.Context, in io.Reader, size int64, guestPath string) error {
	if vm == nil {
		return fmt.Errorf("initx: virtual machine is nil")
	}
	if guestPath == "" {
		return fmt.Errorf("initx: guest path must not be empty")
	}
	if in == nil {
		return fmt.Errorf("initx: reader must not be nil")
	}
	if size < 0 {
		return fmt.Errorf("initx: file size must be non-negative (got %d)", size)
	}
	if ctx == nil {
		ctx = context.Background()
	}

	vm.programLoader.ReserveDataRegion(writeFileTransferRegion)
	defer vm.programLoader.ReserveDataRegion(0)

	errLabel := ir.Label("__initx_write_file_err")
	errVar := ir.Var("__initx_write_file_errno")
	errorFmt := fmt.Sprintf("initx: failed to write %s errno=0x%%x\n", guestPath)

	fd := ir.Var("__initx_write_file_fd")
	memFd := ir.Var("__initx_write_file_mem_fd")
	mailboxPtr := ir.Var("__initx_write_file_mailbox_ptr")
	configPtr := ir.Var("__initx_write_file_config_ptr")
	bufferOffset := ir.Var("__initx_write_file_buffer_offset")
	bufferPtr := ir.Var("__initx_write_file_buffer_ptr")
	bufferLen := ir.Var("__initx_write_file_buffer_len")
	chunkLen := ir.Var("__initx_write_file_chunk_len")
	chunkPtr := ir.Var("__initx_write_file_chunk_ptr")

	cleanup := func() ir.Fragment {
		return ir.Block{
			ir.Syscall(defs.SYS_MUNMAP, configPtr, ir.Int64(configRegionSize)),
			ir.Syscall(defs.SYS_MUNMAP, mailboxPtr, ir.Int64(mailboxRegionSize)),
			ir.Syscall(defs.SYS_CLOSE, memFd),
			ir.Syscall(defs.SYS_CLOSE, fd),
		}
	}

	requestChunk := func() ir.Fragment {
		signal := ir.Var("__initx_write_file_signal")
		return ir.Block{
			ir.Assign(signal, ir.Int64(userYieldValue)),
			ir.Assign(mailboxPtr.Mem().As32(), signal.As32()),
		}
	}

	loopLabel := nextHelperLabel("write_file_loop")
	doneLabel := nextHelperLabel("write_file_done")

	prog := &ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": {
				ir.Assign(fd, ir.Syscall(
					defs.SYS_OPENAT,
					ir.Int64(linux.AT_FDCWD),
					guestPath,
					ir.Int64(linux.O_WRONLY|linux.O_CREAT|linux.O_TRUNC),
					ir.Int64(0o755),
				)),
				ir.Assign(errVar, fd),
				ir.If(ir.IsNegative(errVar), ir.Goto(errLabel)),

				ir.Assign(memFd, ir.Syscall(
					defs.SYS_OPENAT,
					ir.Int64(linux.AT_FDCWD),
					"/dev/mem",
					ir.Int64(linux.O_RDWR|linux.O_SYNC),
					ir.Int64(0),
				)),
				ir.Assign(errVar, memFd),
				ir.If(ir.IsNegative(errVar), ir.Block{
					ir.Syscall(defs.SYS_CLOSE, fd),
					ir.Goto(errLabel),
				}),

				ir.Assign(mailboxPtr, ir.Syscall(
					defs.SYS_MMAP,
					ir.Int64(0),
					ir.Int64(mailboxRegionSize),
					ir.Int64(linux.PROT_READ|linux.PROT_WRITE),
					ir.Int64(linux.MAP_SHARED),
					memFd,
					ir.Int64(int64(vm.mailboxPhysAddr)),
				)),
				ir.Assign(errVar, mailboxPtr),
				ir.If(ir.IsNegative(errVar), ir.Block{
					ir.Syscall(defs.SYS_CLOSE, memFd),
					ir.Syscall(defs.SYS_CLOSE, fd),
					ir.Goto(errLabel),
				}),

				ir.Assign(configPtr, ir.Syscall(
					defs.SYS_MMAP,
					ir.Int64(0),
					ir.Int64(configRegionSize),
					ir.Int64(linux.PROT_READ),
					ir.Int64(linux.MAP_SHARED),
					memFd,
					ir.Int64(int64(vm.configRegionPhysAddr)),
				)),
				ir.Assign(errVar, configPtr),
				ir.If(ir.IsNegative(errVar), ir.Block{
					ir.Syscall(defs.SYS_MUNMAP, mailboxPtr, ir.Int64(mailboxRegionSize)),
					ir.Syscall(defs.SYS_CLOSE, memFd),
					ir.Syscall(defs.SYS_CLOSE, fd),
					ir.Goto(errLabel),
				}),

				ir.Assign(bufferOffset, configPtr.MemWithDisp(configDataOffsetField).As32()),
				ir.Assign(bufferLen, configPtr.MemWithDisp(configDataLengthField).As32()),
				ir.Assign(bufferPtr, ir.Op(ir.OpAdd, configPtr, bufferOffset)),
				ir.If(ir.IsLessThan(bufferLen, ir.Int64(writeFileTransferRegion)), ir.Block{
					cleanup(),
					ir.Assign(errVar, ir.Int64(-int64(linux.EINVAL))),
					ir.Goto(errLabel),
				}),

				ir.Assign(chunkPtr, ir.Op(ir.OpAdd, bufferPtr, ir.Int64(writeFileLengthPrefix))),
				ir.Goto(loopLabel),

				ir.DeclareLabel(loopLabel, ir.Block{
					requestChunk(),
					ir.Assign(chunkLen, bufferPtr.Mem().As16()),
					ir.Assign(chunkLen, ir.Op(ir.OpAnd, chunkLen, ir.Int64(0xffff))),
					ir.If(ir.IsZero(chunkLen), ir.Goto(doneLabel)),
					ir.Assign(errVar, ir.Syscall(
						defs.SYS_WRITE,
						fd,
						chunkPtr,
						chunkLen,
					)),
					ir.If(ir.IsNegative(errVar), ir.Block{
						cleanup(),
						ir.Goto(errLabel),
					}),
					ir.Goto(loopLabel),
				}),

				ir.DeclareLabel(doneLabel, ir.Block{
					cleanup(),
					ir.Return(ir.Int64(0)),
				}),

				ir.DeclareLabel(errLabel, ir.Block{
					ir.Printf(errorFmt, ir.Op(ir.OpSub, ir.Int64(0), errVar)),
					ir.Syscall(defs.SYS_EXIT, ir.Int64(1)),
				}),
			},
		},
	}

	buf := make([]byte, writeFileTransferRegion)
	payloadBuf := buf[writeFileLengthPrefix : writeFileLengthPrefix+writeFileMaxChunkLen]

	handler := func(ctx context.Context, vcpu hv.VirtualCPU) error {
		if vm.programLoader.dataRegionOffset == 0 {
			return fmt.Errorf("initx: transfer buffer offset unavailable")
		}

		n, err := in.Read(payloadBuf)
		if err != nil && err != io.EOF {
			return fmt.Errorf("initx: read input data: %w", err)
		}

		if n == 0 {
			// signal zero-length chunk to guest
			binary.LittleEndian.PutUint16(buf[:writeFileLengthPrefix], 0)
		} else {
			if n > writeFileMaxChunkLen {
				n = writeFileMaxChunkLen
			}
			binary.LittleEndian.PutUint16(buf[:writeFileLengthPrefix], uint16(n))
		}

		if vm.programLoader.region != nil {
			if _, err := vm.programLoader.region.WriteAt(
				buf[:writeFileTransferRegion],
				vm.programLoader.dataRegionOffset,
			); err != nil {
				return fmt.Errorf("initx: write transfer chunk: %w", err)
			}
		} else {
			copy(vm.programLoader.dataRegion[vm.programLoader.dataRegionOffset:], buf)
		}

		return nil
	}

	return vm.runProgram(ctx, prog, handler)
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
	errVar := ir.Var("__initx_spawn_errno")
	errorFmt := fmt.Sprintf("initx: failed to spawn %s errno=0x%%x\n", path)

	prog := &ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": {
				ForkExecWait(path, args, nil, errLabel, errVar),
				ir.Return(errVar),
				ir.DeclareLabel(errLabel, ir.Block{
					ir.Printf(errorFmt, ir.Op(ir.OpSub, ir.Int64(0), errVar)),
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
