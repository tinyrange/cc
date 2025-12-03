package initx

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"

	"github.com/tinyrange/cc/internal/devices/virtio"
	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/ir"
	"github.com/tinyrange/cc/internal/linux/boot"
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
	configHeaderSize       = 24
	configHeaderRelocOff   = configHeaderSize
	configDataOffsetField  = 12
	configDataLengthField  = 16

	writeFileLengthPrefix   = 2
	writeFileMaxChunkLen    = (1 << 16) - 1
	writeFileTransferRegion = writeFileMaxChunkLen + writeFileLengthPrefix

	userYieldValue = 0x5553_4552 // "USER"
)

var ErrYield = errors.New("yield to host")
var ErrUserYield = errors.New("user yield to host")

type proxyReader struct {
	r      io.Reader
	update chan io.Reader
}

func (p *proxyReader) Read(b []byte) (int, error) {
	for {
		if p.r == nil {
			newR, ok := <-p.update
			if !ok {
				return 0, io.EOF
			}
			p.r = newR
		}

		n, err := p.r.Read(b)
		if err == io.EOF {
			p.r = nil
			if n > 0 {
				return n, nil
			}
			continue
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
func (p *programLoader) ReadMMIO(addr uint64, data []byte) error {
	if addr >= configRegionPhysAddr && addr < configRegionPhysAddr+configRegionSize {
		offset := addr - configRegionPhysAddr
		copy(data, p.dataRegion[offset:])
		return nil
	}

	return fmt.Errorf("unimplemented read at address 0x%x", addr)
}

// WriteMMIO implements hv.MemoryMappedIODevice.
func (p *programLoader) WriteMMIO(addr uint64, data []byte) error {
	addr = addr - mailboxPhysAddr

	// ignore stage information writes
	if addr == 8 || addr == 12 || addr == 16 || addr == 20 {
		return nil
	}

	if addr == 0x0 {
		value := binary.LittleEndian.Uint32(data)
		switch value {
		case 0x444f4e45:
			return ErrYield
		case userYieldValue:
			return ErrUserYield
		}
	}

	return fmt.Errorf("unimplemented write at address 0x%x", addr)
}

func (p *programLoader) LoadProgram(prog *ir.Program) error {
	asmProg, err := ir.BuildStandaloneProgramForArch(p.arch, prog)
	if err != nil {
		return fmt.Errorf("build standalone program: %w", err)
	}

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

	for {
		if err := vcpu.Run(ctx); err != nil {
			if errors.Is(err, hv.ErrVMHalted) {
				return nil
			}
			if errors.Is(err, hv.ErrGuestRequestedReboot) {
				return nil
			}
			if errors.Is(err, ErrYield) {
				return nil
			}
			if errors.Is(err, ErrUserYield) {
				if p.onUserYield != nil {
					if err := p.onUserYield(ctx, vcpu); err != nil {
						return err
					}
					continue
				}
				return ErrUserYield
			}
			return fmt.Errorf("run vCPU: %w", err)
		}
	}
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

	programLoader *programLoader

	configRegion hv.MemoryRegion
}

func (vm *VirtualMachine) Close() error {
	return vm.vm.Close()
}

func (vm *VirtualMachine) Run(ctx context.Context, prog *ir.Program) error {
	return vm.runProgram(ctx, prog, nil)
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

func NewVirtualMachine(
	h hv.Hypervisor,
	numCPUs int,
	memSizeMB uint64,
	kernelLoader kernel.Kernel,
	devices ...hv.DeviceTemplate,
) (*VirtualMachine, error) {
	in := &proxyReader{update: make(chan io.Reader)}
	out := &proxyWriter{w: os.Stderr} // default to stderr so we can see debugging output

	programLoader := &programLoader{}

	ret := &VirtualMachine{
		outBuffer: out,
		inBuffer:  in,

		programLoader: programLoader,

		loader: &boot.LinuxLoader{
			NumCPUs: numCPUs,

			MemSize: memSizeMB << 20,
			MemBase: func() uint64 {
				switch h.Architecture() {
				case hv.ArchitectureARM64:
					return 0x80000000
				default:
					return 0
				}
			}(),

			Devices: append(
				devices,
				virtio.ConsoleTemplate{Out: out, In: in, Arch: h.Architecture()},
				programLoader,
			),

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
					},
					map[string]string{
						"CONFIG_VIRTIO_BLK":  "kernel/drivers/block/virtio_blk.ko.gz",
						"CONFIG_VIRTIO_NET":  "kernel/drivers/net/virtio_net.ko.gz",
						"CONFIG_VIRTIO_MMIO": "kernel/drivers/virtio/virtio_mmio.ko.gz",
					},
				)
				if err != nil {
					return nil, fmt.Errorf("plan module load: %v", err)
				}

				cfg.PreloadModules = append(cfg.PreloadModules, modules...)

				return Build(cfg)
			},

			SerialStdout: out,

			Cmdline: func() []string {
				switch h.Architecture() {
				case hv.ArchitectureX86_64:
					return []string{
						"console=hvc0",
						"quiet",
						"reboot=k",
						"panic=-1",
						"tsc=reliable",
						"tsc_early_khz=3000000",
					}
				case hv.ArchitectureARM64:
					return []string{
						"console=hvc0",
						"quiet",
						"reboot=k",
						"panic=-1",
						"iomem=relaxed",
						"memmap=0xf0003000$0x400000",
					}
				default:
					panic("unsupported architecture for initx cmdline")
				}
			}(),
		},
	}

	ret.loader.CreateVMWithMemory = func(vm hv.VirtualMachine) error {
		if runtime.GOOS == "linux" && h.Architecture() == hv.ArchitectureARM64 {
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

	var err error
	ret.vm, err = h.NewVirtualMachine(ret.loader)
	if err != nil {
		return nil, err
	}

	return ret, nil
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
				ir.Printf(fmt.Sprintf("initx: spawning %s\n", path)),
				ForkExecWait(path, args, nil, errLabel, errVar),
				ir.Printf(fmt.Sprintf("initx: %s completed\n", path)),
				ir.Return(ir.Int64(0)),
				ir.DeclareLabel(errLabel, ir.Block{
					ir.Printf(errorFmt, ir.Op(ir.OpSub, ir.Int64(0), errVar)),
					ir.Syscall(defs.SYS_EXIT, ir.Int64(1)),
				}),
			},
		},
	}

	return vm.Run(ctx, prog)
}
