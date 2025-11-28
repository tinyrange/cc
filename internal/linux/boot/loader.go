package boot

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/tinyrange/cc/internal/asm/amd64"
	"github.com/tinyrange/cc/internal/devices/amd64/serial"
	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/ir"
	_ "github.com/tinyrange/cc/internal/ir/amd64"
)

type programRunner struct {
	loader *LinuxLoader
}

// Run implements hv.RunConfig.
func (p *programRunner) Run(ctx context.Context, vcpu hv.VirtualCPU) error {
	if err := p.loader.plan.ConfigureVCPU(vcpu); err != nil {
		return fmt.Errorf("configure vCPU: %w", err)
	}

	for {
		if err := vcpu.Run(ctx); err != nil {
			return fmt.Errorf("run vCPU: %w", err)
		}
	}
}

var (
	_ hv.RunConfig = &programRunner{}
)

type convertCRLF struct {
	io.Writer
}

func (c *convertCRLF) Write(p []byte) (n int, err error) {
	var converted []byte
	for i := range p {
		if p[i] == '\n' {
			converted = append(converted, '\r')
		}
		converted = append(converted, p[i])
	}
	return c.Writer.Write(converted)
}

type LinuxLoader struct {
	NumCPUs int
	MemSize uint64

	Cmdline   []string
	Init      ir.Program
	GetKernel func() (io.ReaderAt, int64, error)

	Stdout io.Writer

	plan *BootPlan
}

// OnCreateVCPU implements hv.VMCallbacks.
func (l *LinuxLoader) OnCreateVCPU(vCpu hv.VirtualCPU) error {
	return nil
}

// OnCreateVM implements hv.VMCallbacks.
func (l *LinuxLoader) OnCreateVM(vm hv.VirtualMachine) error {
	return nil
}

// implements hv.VMConfig.
func (l *LinuxLoader) CPUCount() int               { return l.NumCPUs }
func (l *LinuxLoader) Callbacks() hv.VMCallbacks   { return l }
func (l *LinuxLoader) Loader() hv.VMLoader         { return l }
func (l *LinuxLoader) MemoryBase() uint64          { return 0x0 }
func (l *LinuxLoader) MemorySize() uint64          { return l.MemSize }
func (l *LinuxLoader) NeedsInterruptSupport() bool { return true }

// Load implements hv.VMLoader.
func (l *LinuxLoader) Load(vm hv.VirtualMachine) error {
	kernelReader, kernelSize, err := l.GetKernel()
	if err != nil {
		return fmt.Errorf("get kernel: %w", err)
	}

	kernelImage, err := LoadKernel(kernelReader, kernelSize)
	if err != nil {
		return fmt.Errorf("load kernel: %w", err)
	}

	// build the init payload
	fac, err := ir.BuildStandaloneProgramForArch(ir.ArchitectureX86_64, &l.Init)
	if err != nil {
		return fmt.Errorf("build init program: %w", err)
	}

	initPayload, err := amd64.StandaloneELF(fac)
	if err != nil {
		return fmt.Errorf("build init payload ELF: %w", err)
	}

	// build the initramfs
	files := []initramfsFile{
		{Path: "/init", Data: initPayload, Mode: os.FileMode(0o755)},
		// add /dev/mem as /mem
		{Path: "/mem", Data: nil, Mode: os.FileMode(0o600), DevMajor: 1, DevMinor: 1},
	}
	initrd, err := buildInitramfs(files)
	if err != nil {
		return fmt.Errorf("build initramfs: %w", err)
	}

	// prepare the kernel
	plan, err := kernelImage.Prepare(vm, bootOptions{
		Cmdline: strings.Join(l.Cmdline, " "),
		Initrd:  initrd,
	})
	if err != nil {
		return fmt.Errorf("prepare kernel: %w", err)
	}
	l.plan = plan

	// Add devices
	serial := serial.NewSerial16550(0x3F8, 4, &convertCRLF{l.Stdout})
	if err := vm.AddDevice(serial); err != nil {
		return fmt.Errorf("add serial device: %w", err)
	}

	return nil
}

func (l *LinuxLoader) RunConfig() (hv.RunConfig, error) {
	return &programRunner{loader: l}, nil
}

var (
	_ hv.VMLoader    = &LinuxLoader{}
	_ hv.VMConfig    = &LinuxLoader{}
	_ hv.VMCallbacks = &LinuxLoader{}
)
