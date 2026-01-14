package initx

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
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
)

const (
	mailboxPhysAddr        = 0xf000_0000
	mailboxRegionSize      = 0x1000
	configRegionPhysAddr   = 0xf000_3000
	configRegionSize       = 4 * 1024 * 1024
	configRegionPageOffset = configRegionPhysAddr - mailboxPhysAddr

	configHeaderMagicValue = 0xcafebabe
	configHeaderSize       = 40
	configHeaderRelocOff   = configHeaderSize
	configDataOffsetField  = 12
	configDataLengthField  = 16

	writeFileLengthPrefix   = 2
	writeFileMaxChunkLen    = (1 << 16) - 1
	writeFileTransferRegion = writeFileMaxChunkLen + writeFileLengthPrefix

	userYieldValue = 0x5553_4552 // "USER"

	// arm64MaxMemoryMB is the maximum memory for ARM64 to avoid overlap
	// with the mailbox/config regions at 0xf0000000.
	// Max = (0xf0000000 - 0x80000000) / (1024*1024) = 1792 MB
	arm64MaxMemoryMB = 1792
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

// MMIORegions implements hv.MemoryMappedIODevice.
func (p *programLoader) MMIORegions() []hv.MMIORegion {
	return []hv.MMIORegion{
		{Address: mailboxPhysAddr, Size: mailboxRegionSize},
		{Address: configRegionPhysAddr, Size: configRegionSize},
	}
}

// ReadMMIO implements hv.MemoryMappedIODevice.
func (p *programLoader) ReadMMIO(ctx hv.ExitContext, addr uint64, data []byte) error {
	if addr >= configRegionPhysAddr && addr < configRegionPhysAddr+configRegionSize {
		offset := addr - configRegionPhysAddr
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
			Name: fmt.Sprintf("initx-mailbox@%x", mailboxPhysAddr),
			Properties: map[string]fdt.Property{
				"compatible": {Strings: []string{"tinyrange,initx-mailbox"}},
				"reg":        {U64: []uint64{mailboxPhysAddr, mailboxRegionSize}},
				"status":     {Strings: []string{"okay"}},
			},
		},
		{
			Name: fmt.Sprintf("initx-config@%x", configRegionPhysAddr),
			Properties: map[string]fdt.Property{
				"compatible": {Strings: []string{"tinyrange,initx-config"}},
				"reg":        {U64: []uint64{configRegionPhysAddr, configRegionSize}},
				"status":     {Strings: []string{"okay"}},
			},
		},
	}, nil
}

// WriteMMIO implements hv.MemoryMappedIODevice.
func (p *programLoader) WriteMMIO(ctx hv.ExitContext, addr uint64, data []byte) error {
	offset := addr - mailboxPhysAddr

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
}

func (vm *VirtualMachine) Close() error {
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
	return vm.runProgram(ctx, prog, nil)
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

	// Cap ARM64 memory to avoid overlap with mailbox/config regions at 0xf0000000
	memSize := memSizeMB
	if h.Architecture() == hv.ArchitectureARM64 && memSize > arm64MaxMemoryMB {
		memSize = arm64MaxMemoryMB
	}

	ret.loader = &boot.LinuxLoader{
		NumCPUs: numCPUs,

		MemSize: memSize << 20,
		MemBase: func() uint64 {
			switch h.Architecture() {
			case hv.ArchitectureARM64:
				return 0x80000000
			default:
				return 0
			}
		}(),

		GetKernel: func() (io.ReaderAt, int64, error) {
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
			return kernelLoader.GetSystemMap()
		},

		GetInit: func(arch hv.CpuArchitecture) (*ir.Program, error) {
			cfg := BuilderConfig{
				Arch: arch,
			}

			modules, err := kernelLoader.PlanModuleLoad(
				[]string{
					"CONFIG_VIRTIO_MMIO",
					"CONFIG_VIRTIO_BLK",
					"CONFIG_VIRTIO_NET",
					"CONFIG_VIRTIO_CONSOLE",
					"CONFIG_VIRTIO_FS",
					"CONFIG_PACKET",
				},
				map[string]string{
					"CONFIG_VIRTIO_BLK":  "kernel/drivers/block/virtio_blk.ko.gz",
					"CONFIG_VIRTIO_NET":  "kernel/drivers/net/virtio_net.ko.gz",
					"CONFIG_VIRTIO_MMIO": "kernel/drivers/virtio/virtio_mmio.ko.gz",
					"CONFIG_VIRTIO_FS":   "kernel/fs/fuse/virtiofs.ko.gz",
					"CONFIG_PACKET":      "kernel/net/packet/af_packet.ko.gz",
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
		if runtime.GOOS == "linux" && h.Architecture() == hv.ArchitectureARM64 {
			return nil
		}

		if runtime.GOOS == "darwin" && h.Architecture() == hv.ArchitectureARM64 {
			return nil
		}

		mem, err := vm.AllocateMemory(configRegionPhysAddr, configRegionSize)
		if err != nil {
			return fmt.Errorf("allocate initx config region: %v", err)
		}

		ret.configRegion = mem
		programLoader.region = mem

		return nil
	}

	for _, option := range options {
		if err := option.apply(&ret); err != nil {
			return nil, err
		}
	}

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
					ir.Int64(mailboxPhysAddr),
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
					ir.Int64(configRegionPhysAddr),
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
