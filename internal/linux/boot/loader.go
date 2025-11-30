package boot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/tinyrange/cc/internal/asm/amd64"
	"github.com/tinyrange/cc/internal/asm/arm64"
	chipset "github.com/tinyrange/cc/internal/devices/amd64/chipset"
	amd64input "github.com/tinyrange/cc/internal/devices/amd64/input"
	"github.com/tinyrange/cc/internal/devices/amd64/pci"
	amd64serial "github.com/tinyrange/cc/internal/devices/amd64/serial"
	"github.com/tinyrange/cc/internal/devices/serial"
	"github.com/tinyrange/cc/internal/devices/virtio"
	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/ir"
	_ "github.com/tinyrange/cc/internal/ir/amd64"
	amd64boot "github.com/tinyrange/cc/internal/linux/boot/amd64"
	arm64boot "github.com/tinyrange/cc/internal/linux/boot/arm64"
)

type bootPlan interface {
	ConfigureVCPU(vcpu hv.VirtualCPU) error
}

const (
	arm64UARTMMIOBase = 0x09000000
	arm64UARTRegShift = 0
	arm64UARTBaudRate = 115200
)

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
	MemBase uint64

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
func (l *LinuxLoader) MemoryBase() uint64          { return l.MemBase }
func (l *LinuxLoader) MemorySize() uint64          { return l.MemSize }
func (l *LinuxLoader) NeedsInterruptSupport() bool { return true }

// Load implements hv.VMLoader.
func (l *LinuxLoader) Load(vm hv.VirtualMachine) error {
	if l.GetKernel == nil {
		return errors.New("linux loader missing kernel provider")
	}

	kernelReader, kernelSize, err := l.GetKernel()
	if err != nil {
		return fmt.Errorf("get kernel: %w", err)
	}

	arch := vm.Hypervisor().Architecture()

	initPayload, err := l.buildInitPayload(arch)
	if err != nil {
		return err
	}

	files := []initramfsFile{
		{Path: "/init", Data: initPayload, Mode: os.FileMode(0o755)},
		// add /dev/mem as /mem
		{Path: "/mem", Data: nil, Mode: os.FileMode(0o600), DevMajor: 1, DevMinor: 1},
	}
	initrd, err := buildInitramfs(files)
	if err != nil {
		return fmt.Errorf("build initramfs: %w", err)
	}

	cmdline := append([]string(nil), l.Cmdline...)
	for _, dev := range l.Devices {
		if vdev, ok := dev.(virtio.VirtioMMIODevice); ok {
			params, err := vdev.GetLinuxCommandLineParam()
			if err != nil {
				return fmt.Errorf("get virtio mmio device linux cmdline param: %w", err)
			}
			cmdline = append(cmdline, params...)
		}
	}
	cmdlineStr := strings.Join(cmdline, " ")

	switch arch {
	case hv.ArchitectureX86_64:
		return l.loadAMD64(vm, kernelReader, kernelSize, cmdlineStr, initrd)
	case hv.ArchitectureARM64:
		return l.loadARM64(vm, kernelReader, kernelSize, cmdlineStr, initrd)
	default:
		return fmt.Errorf("unsupported architecture: %v", arch)
	}
}

func (l *LinuxLoader) buildInitPayload(arch hv.CpuArchitecture) ([]byte, error) {
	if l.GetInit == nil {
		return nil, errors.New("linux loader missing init program provider")
	}

	initProg, err := l.GetInit(arch)
	if err != nil {
		return nil, fmt.Errorf("get init program: %w", err)
	}
	if initProg == nil {
		return nil, fmt.Errorf("init program for %s is nil", arch)
	}

	var irArch ir.Architecture
	switch arch {
	case hv.ArchitectureX86_64:
		irArch = ir.ArchitectureX86_64
	case hv.ArchitectureARM64:
		irArch = ir.ArchitectureARM64
	default:
		return nil, fmt.Errorf("unsupported architecture for init payload: %s", arch)
	}

	fac, err := ir.BuildStandaloneProgramForArch(irArch, initProg)
	if err != nil {
		return nil, fmt.Errorf("build init program: %w", err)
	}

	switch arch {
	case hv.ArchitectureX86_64:
		payload, err := amd64.StandaloneELF(fac)
		if err != nil {
			return nil, fmt.Errorf("build init payload ELF: %w", err)
		}
		return payload, nil
	case hv.ArchitectureARM64:
		payload, err := arm64.StandaloneELF(fac)
		if err != nil {
			return nil, fmt.Errorf("build init payload ELF: %w", err)
		}
		return payload, nil
	default:
		return nil, fmt.Errorf("unsupported architecture for init payload: %s", arch)
	}
}

func (l *LinuxLoader) loadAMD64(vm hv.VirtualMachine, kernelReader io.ReaderAt, kernelSize int64, cmdline string, initrd []byte) error {
	kernelImage, err := amd64boot.LoadKernel(kernelReader, kernelSize)
	if err != nil {
		return fmt.Errorf("load kernel: %w", err)
	}

	plan, err := kernelImage.Prepare(vm, amd64boot.BootOptions{
		Cmdline: cmdline,
		Initrd:  initrd,
	})
	if err != nil {
		return fmt.Errorf("prepare kernel: %w", err)
	}
	l.plan = plan

	consoleSerial := amd64serial.NewSerial16550(0x3F8, 4, &convertCRLF{l.SerialStdout})
	if err := vm.AddDevice(consoleSerial); err != nil {
		return fmt.Errorf("add serial device: %w", err)
	}

	auxSerial := amd64serial.NewSerial16550(0x2F8, 3, io.Discard)
	if err := vm.AddDevice(auxSerial); err != nil {
		return fmt.Errorf("add aux serial device: %w", err)
	}

	if err := vm.AddDevice(pci.NewHostBridge()); err != nil {
		return fmt.Errorf("add pci host bridge: %w", err)
	}

	pic := chipset.NewDualPIC()
	if err := vm.AddDevice(pic); err != nil {
		return fmt.Errorf("add dual PIC: %w", err)
	}

	if err := vm.AddDevice(chipset.NewPIT(pic)); err != nil {
		return fmt.Errorf("add PIT: %w", err)
	}

	if err := vm.AddDevice(chipset.NewCMOS(pic)); err != nil {
		return fmt.Errorf("add CMOS/RTC: %w", err)
	}

	if err := vm.AddDevice(chipset.NewResetControlPort()); err != nil {
		return fmt.Errorf("add reset control port: %w", err)
	}

	var legacyPorts []uint16
	for _, rng := range []struct {
		start uint16
		end   uint16
	}{
		{0x0, 0xf},
		{0x11, 0x1f},
		{0x80, 0x8f},
		{0xBD, 0xBD}, // scratch port
		{0x2e8, 0x2ef},
		{0x3e8, 0x3ef},
		{0xbb00, 0xbbff},
	} {
		for port := rng.start; port <= rng.end; port++ {
			legacyPorts = append(legacyPorts, port)
		}
	}

	legacy := hv.SimpleX86IOPortDevice{
		Ports: legacyPorts,
		ReadFunc: func(port uint16, data []byte) error {
			if port == 0x12 {
				return hv.ErrGuestRequestedReboot
			}
			// slog.Info("legacy port read", "port", fmt.Sprintf("0x%04x", port), "size", len(data))
			for i := range data {
				data[i] = 0
			}
			return nil
		},
		WriteFunc: func(port uint16, data []byte) error {
			// slog.Info("legacy port write", "port", fmt.Sprintf("0x%04x", port), "size", len(data), "data", data)
			return nil
		},
	}
	if err := vm.AddDevice(legacy); err != nil {
		return fmt.Errorf("add legacy port stub: %w", err)
	}

	if err := vm.AddDevice(amd64input.NewI8042()); err != nil {
		return fmt.Errorf("add i8042 controller: %w", err)
	}

	for _, dev := range l.Devices {
		if err := vm.AddDeviceFromTemplate(dev); err != nil {
			return fmt.Errorf("add device from template: %w", err)
		}
	}

	return nil
}

func (l *LinuxLoader) loadARM64(vm hv.VirtualMachine, kernelReader io.ReaderAt, kernelSize int64, cmdline string, initrd []byte) error {
	kernelImage, err := arm64boot.LoadKernel(kernelReader, kernelSize)
	if err != nil {
		return fmt.Errorf("load kernel: %w", err)
	}

	numCPUs := l.NumCPUs
	if numCPUs <= 0 {
		numCPUs = 1
	}

	plan, err := kernelImage.Prepare(vm, arm64boot.BootOptions{
		Cmdline: cmdline,
		Initrd:  initrd,
		NumCPUs: numCPUs,
		UART: &arm64boot.UARTConfig{
			Base:     arm64UARTMMIOBase,
			Size:     serial.UART8250MMIOSize,
			ClockHz:  serial.UART8250DefaultClock,
			RegShift: arm64UARTRegShift,
			BaudRate: arm64UARTBaudRate,
		},
	})
	if err != nil {
		return fmt.Errorf("prepare kernel: %w", err)
	}
	l.plan = plan

	uartDev := serial.NewUART8250MMIO(arm64UARTMMIOBase, arm64UARTRegShift, &convertCRLF{l.SerialStdout})
	if err := vm.AddDevice(uartDev); err != nil {
		return fmt.Errorf("add arm64 uart device: %w", err)
	}

	for _, dev := range l.Devices {
		if err := vm.AddDeviceFromTemplate(dev); err != nil {
			return fmt.Errorf("add device from template: %w", err)
		}
	}

	return nil
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
