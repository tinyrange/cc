package initx

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/tinyrange/cc/internal/devices/virtio"
	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/ir"
	"github.com/tinyrange/cc/internal/linux/boot"
	"github.com/tinyrange/cc/internal/linux/defs"
	"github.com/tinyrange/cc/internal/linux/kernel"
)

var ErrYield = errors.New("yield to host")

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
	arch hv.CpuArchitecture

	code            []byte
	relocationBytes []byte
	relocationCount uint32
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
		{Address: 0xf000_0000, Size: 0x1000},
		{Address: 0xf000_3000, Size: 4 * 1024 * 1024},
	}
}

// ReadMMIO implements hv.MemoryMappedIODevice.
func (p *programLoader) ReadMMIO(addr uint64, data []byte) error {
	addr = addr - 0xf000_0000

	relocOffset := uint64(0x3010)
	codeOffset := relocOffset + uint64(len(p.relocationBytes))

	switch true {
	case addr == 0x3000:
		// put the 0xcafebabe magic value
		binary.LittleEndian.PutUint32(data[0:], 0xcafebabe)
		return nil
	case addr == 0x3004:
		// code
		binary.LittleEndian.PutUint32(data[0:], uint32(len(p.code)))
		return nil
	case addr == 0x3008:
		// relocation count
		binary.LittleEndian.PutUint32(data[0:], p.relocationCount)
		return nil
	case addr >= relocOffset && addr < relocOffset+uint64(len(p.relocationBytes)):
		// relocation bytes
		offset := addr - relocOffset
		copy(data, p.relocationBytes[offset:])
		return nil
	case addr >= codeOffset && addr < codeOffset+uint64(len(p.code)): // code comes right after relocation bytes
		// code bytes
		offset := addr - codeOffset
		copy(data, p.code[offset:])
		return nil
	case addr >= codeOffset+uint64(len(p.code)): // handle reads after the end of the code
		for i := range data {
			data[i] = 0
		}
		return nil
	}

	return fmt.Errorf("unimplemented read at address 0x%x", addr)
}

// WriteMMIO implements hv.MemoryMappedIODevice.
func (p *programLoader) WriteMMIO(addr uint64, data []byte) error {
	addr = addr - 0xf000_0000

	// ignore stage information writes
	if addr == 8 || addr == 12 || addr == 16 || addr == 20 {
		return nil
	}

	if addr == 0x0 && binary.LittleEndian.Uint32(data) == 0x444f4e45 {
		return ErrYield
	}

	return fmt.Errorf("unimplemented write at address 0x%x", addr)
}

func (p *programLoader) LoadProgram(prog *ir.Program) error {
	asmProg, err := ir.BuildStandaloneProgramForArch(p.arch, prog)
	if err != nil {
		return fmt.Errorf("build standalone program: %w", err)
	}

	p.code = asmProg.Bytes()
	relocs := asmProg.Relocations()
	p.relocationBytes = make([]byte, 4*len(relocs))
	p.relocationCount = uint32(len(relocs))
	for i, reloc := range relocs {
		offset := 4 * i
		binary.LittleEndian.PutUint32(p.relocationBytes[offset:], uint32(reloc))
	}
	return nil
}

var (
	_ hv.MemoryMappedIODevice = &programLoader{}
	_ hv.DeviceTemplate       = &programLoader{}
)

type programRunner struct {
	loader *programLoader

	program       *ir.Program
	configureVcpu func(vcpu hv.VirtualCPU) error
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
}

func (vm *VirtualMachine) Close() error {
	return vm.vm.Close()
}

func (vm *VirtualMachine) Run(ctx context.Context, prog *ir.Program) error {
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
					}
				default:
					panic("unsupported architecture for initx cmdline")
				}
			}(),
		},
	}

	var err error
	ret.vm, err = h.NewVirtualMachine(ret.loader)
	if err != nil {
		return nil, err
	}

	return ret, nil
}

// WriteFile copies the host file at hostPath into the guest at guestPath.
func (vm *VirtualMachine) WriteFile(ctx context.Context, in io.Reader, size int64, guestPath string) error {
	if vm == nil {
		return fmt.Errorf("initx: virtual machine is nil")
	}
	if guestPath == "" {
		return fmt.Errorf("initx: guest path must not be empty")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	errLabel := ir.Label("__initx_write_file_err")
	errVar := ir.Var("__initx_write_file_errno")
	errorFmt := fmt.Sprintf("initx: failed to create %s errno=0x%%x\n", guestPath)

	vm.inBuffer.SetReader(in)

	prog := &ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": {
				CreateFileFromStdin(guestPath, size, 0o644, errLabel, errVar),
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
				SpawnExecutable(path, args, nil, errLabel, errVar),
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
