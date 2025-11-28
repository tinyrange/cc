package boot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/tinyrange/cc/internal/asm/amd64"
	"github.com/tinyrange/cc/internal/devices/amd64/input"
	"github.com/tinyrange/cc/internal/devices/amd64/pci"
	"github.com/tinyrange/cc/internal/devices/amd64/serial"
	"github.com/tinyrange/cc/internal/devices/virtio"
	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/ir"
	_ "github.com/tinyrange/cc/internal/ir/amd64"
	amd64boot "github.com/tinyrange/cc/internal/linux/boot/amd64"
)

type bootPlan interface {
	ConfigureVCPU(vcpu hv.VirtualCPU) error
}

type programRunner struct {
	loader *LinuxLoader
	linux  io.ReaderAt
}

// Run implements hv.RunConfig.
func (p *programRunner) Run(ctx context.Context, vcpu hv.VirtualCPU) error {
	if err := p.loader.plan.ConfigureVCPU(vcpu); err != nil {
		return fmt.Errorf("configure vCPU: %w", err)
	}

	for {
		if err := vcpu.Run(ctx); err != nil {
			if errors.Is(err, hv.ErrVMHalted) {
				return nil
			}
			if errors.Is(err, hv.ErrGuestRequestedReboot) {
				return nil
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
	GetInit      func(arch hv.CpuArchitecture) (*ir.Program, error)
	GetKernel    func() (io.ReaderAt, int64, error)
	GetSystemMap func() (io.ReaderAt, error)

	SerialStdout io.Writer

	Devices []hv.DeviceTemplate

	plan bootPlan
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

	if vm.Hypervisor().Architecture() == hv.ArchitectureX86_64 {
		kernelImage, err := amd64boot.LoadKernel(kernelReader, kernelSize)
		if err != nil {
			return fmt.Errorf("load kernel: %w", err)
		}

		initProg, err := l.GetInit(hv.ArchitectureX86_64)
		if err != nil {
			return fmt.Errorf("get init program: %w", err)
		}

		// build the init payload
		fac, err := ir.BuildStandaloneProgramForArch(ir.ArchitectureX86_64, initProg)
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

		for _, dev := range l.Devices {
			if vdev, ok := dev.(virtio.VirtioMMIODevice); ok {
				params, err := vdev.GetLinuxCommandLineParam()
				if err != nil {
					return fmt.Errorf("get virtio mmio device linux cmdline param: %w", err)
				}
				l.Cmdline = append(l.Cmdline, params...)
			}
		}

		// prepare the kernel
		plan, err := kernelImage.Prepare(vm, amd64boot.BootOptions{
			Cmdline: strings.Join(l.Cmdline, " "),
			Initrd:  initrd,
		})
		if err != nil {
			return fmt.Errorf("prepare kernel: %w", err)
		}
		l.plan = plan

		// Add devices
		consoleSerial := serial.NewSerial16550(0x3F8, 4, &convertCRLF{l.SerialStdout})
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
			{0x21, 0x22},
			{0x40, 0x41},
			{0x42, 0x43},
			{0x60, 0x61},
			{0x70, 0x71},
			{0x80, 0x8f},
			{0xA1, 0xA1},
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
				for i := range data {
					data[i] = 0
				}
				return nil
			},
			WriteFunc: func(port uint16, data []byte) error {
				return nil
			},
		}
		if err := vm.AddDevice(legacy); err != nil {
			return fmt.Errorf("add legacy port stub: %w", err)
		}

		if err := vm.AddDevice(input.NewI8042()); err != nil {
			return fmt.Errorf("add i8042 controller: %w", err)
		}

		for _, dev := range l.Devices {
			if err := vm.AddDeviceFromTemplate(dev); err != nil {
				return fmt.Errorf("add device from template: %w", err)
			}
		}

		return nil
	} else if vm.Hypervisor().Architecture() == hv.ArchitectureARM64 {
		return fmt.Errorf("ARM64 boot not implemented yet")
	} else {
		return fmt.Errorf("unsupported architecture: %v", vm.Hypervisor().Architecture())
	}
}

func (l *LinuxLoader) RunConfig() (hv.RunConfig, error) {
	linux, _, err := l.GetKernel()
	if err != nil {
		return nil, fmt.Errorf("get kernel: %w", err)
	}

	loader := &programRunner{loader: l, linux: linux}

	return loader, nil
}

var (
	_ hv.VMLoader    = &LinuxLoader{}
	_ hv.VMConfig    = &LinuxLoader{}
	_ hv.VMCallbacks = &LinuxLoader{}
)
