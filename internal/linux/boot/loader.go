package boot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/tinyrange/cc/internal/asm/amd64"
	"github.com/tinyrange/cc/internal/devices/amd64/input"
	"github.com/tinyrange/cc/internal/devices/amd64/pci"
	"github.com/tinyrange/cc/internal/devices/amd64/serial"
	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/ir"
	_ "github.com/tinyrange/cc/internal/ir/amd64"
	"golang.org/x/sys/unix"
)

type programRunner struct {
	loader    *LinuxLoader
	linux     io.ReaderAt
	systemMap io.ReaderAt
}

// Run implements hv.RunConfig.
func (p *programRunner) Run(ctx context.Context, vcpu hv.VirtualCPU) error {
	if err := p.loader.plan.ConfigureVCPU(vcpu); err != nil {
		return fmt.Errorf("configure vCPU: %w", err)
	}

	for {
		subCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()

		if err := vcpu.Run(subCtx); err != nil {
			if errors.Is(err, hv.ErrVMHalted) {
				return nil
			}
			if errors.Is(err, hv.ErrGuestRequestedReboot) {
				return nil
			}
			if errors.Is(err, unix.EINTR) {
				continue
			}
			if errors.Is(err, context.DeadlineExceeded) {
				// dump a stacktrace
				frames, err := CaptureStackTrace(vcpu, p.linux, p.systemMap, 0)
				if err != nil {
					fmt.Printf("capture stack trace: %v\n", err)
				} else {
					fmt.Printf("ran for over 100ms, stack trace:\n")
					for _, frame := range frames {
						fmt.Printf("  %s+%#x (%#x)\n", frame.Symbol, frame.Offset, frame.PC)
					}
				}
				continue
			}
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

	Cmdline      []string
	Init         ir.Program
	GetKernel    func() (io.ReaderAt, int64, error)
	GetSystemMap func() (io.ReaderAt, error)

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
	consoleSerial := serial.NewSerial16550(0x3F8, 4, &convertCRLF{l.Stdout})
	if err := vm.AddDevice(consoleSerial); err != nil {
		return fmt.Errorf("add serial device: %w", err)
	}

	auxSerial := serial.NewSerial16550(0x2F8, 3, io.Discard)
	if err := vm.AddDevice(auxSerial); err != nil {
		return fmt.Errorf("add aux serial device: %w", err)
	}

	if err := vm.AddDevice(pci.NewHostBridge()); err != nil {
		return fmt.Errorf("add pci host bridge: %w", err)
	}

	var legacyPorts []uint16
	for _, rng := range []struct {
		start uint16
		end   uint16
	}{
		{0x70, 0x71},
		{0x80, 0x8f},
		{0x2e8, 0x2ef},
		{0x3e8, 0x3ef},
	} {
		for port := rng.start; port <= rng.end; port++ {
			legacyPorts = append(legacyPorts, port)
		}
	}

	legacy := hv.SimpleX86IOPortDevice{
		Ports: legacyPorts,
		ReadFunc: func(port uint16, data []byte) error {
			fmt.Printf("legacy port read 0x%X\n", port)
			for i := range data {
				data[i] = 0
			}
			return nil
		},
		WriteFunc: func(port uint16, data []byte) error {
			fmt.Printf("legacy port write 0x%X\n", port)
			return nil
		},
	}
	if err := vm.AddDevice(legacy); err != nil {
		return fmt.Errorf("add legacy port stub: %w", err)
	}

	if err := vm.AddDevice(input.NewI8042()); err != nil {
		return fmt.Errorf("add i8042 controller: %w", err)
	}

	return nil
}

func (l *LinuxLoader) RunConfig() (hv.RunConfig, error) {
	linux, _, err := l.GetKernel()
	if err != nil {
		return nil, fmt.Errorf("get kernel: %w", err)
	}

	loader := &programRunner{loader: l, linux: linux}

	if l.GetSystemMap != nil {
		systemMapReader, err := l.GetSystemMap()
		if err != nil {
			return nil, fmt.Errorf("get System.map: %w", err)
		}

		loader.systemMap = systemMapReader
	}

	return loader, nil
}

var (
	_ hv.VMLoader    = &LinuxLoader{}
	_ hv.VMConfig    = &LinuxLoader{}
	_ hv.VMCallbacks = &LinuxLoader{}
)
