package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/tinyrange/cc/internal/asm"
	"github.com/tinyrange/cc/internal/asm/amd64"
	"github.com/tinyrange/cc/internal/asm/arm64"
	riscvasm "github.com/tinyrange/cc/internal/asm/riscv"
	"github.com/tinyrange/cc/internal/debug"
	"github.com/tinyrange/cc/internal/devices/amd64/chipset"
	amd64serial "github.com/tinyrange/cc/internal/devices/amd64/serial"
	"github.com/tinyrange/cc/internal/devices/virtio"
	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/hv/factory"
	"github.com/tinyrange/cc/internal/hv/helpers"
	"github.com/tinyrange/cc/internal/initx"
	"github.com/tinyrange/cc/internal/ir"
	amd64ir "github.com/tinyrange/cc/internal/ir/amd64"
	arm64ir "github.com/tinyrange/cc/internal/ir/arm64"
	_ "github.com/tinyrange/cc/internal/ir/riscv"
	"github.com/tinyrange/cc/internal/linux/boot"
	"github.com/tinyrange/cc/internal/linux/defs"
	amd64defs "github.com/tinyrange/cc/internal/linux/defs/amd64"
	"github.com/tinyrange/cc/internal/linux/kernel"
	"github.com/tinyrange/cc/internal/netstack"
	"github.com/tinyrange/cc/internal/vfs"
)

const (
	psciSystemOff         = 0x84000008
	arm64MMIOAddr         = 0xdead0000
	arm64MessageBuf       = 0x2000
	arm64UARTMMIOBase     = 0x09000000
	arm64SnapshotMMIOAddr = 0xf0000000
	riscvMemoryBase       = 0x80000000

	irqGuestBase       = 0x200000
	irqGuestIDTBase    = irqGuestBase + 0x3000
	irqGuestIDTRBase   = irqGuestBase + 0x3100
	irqGuestCounter    = irqGuestBase + 0x3200
	irqGuestCommand    = irqGuestBase + 0x3210
	irqGuestGDTBase    = irqGuestBase + 0x3300
	irqGuestGDTDesc    = irqGuestBase + 0x3320
	irqGuestStackTop   = irqGuestBase + 0x4000
	irqGuestLapicBase  = 0xFEE00000
	irqGuestIoapicBase = 0xFEC00000
)

const (
	arm64VectorEntrySize  = 0x80
	arm64VectorEntryCount = 16
	arm64NopEncoding      = 0xd503201f
)

var arm64VectorTableBytes = mustBuildArm64VectorTable()

type irqGuestConfig struct {
	vector       uint8
	line         uint8
	dest         uint8
	destMode     uint8
	deliveryMode uint8
	level        bool
	masked       bool
}

const (
	irqCmdNone   = 0
	irqCmdUnmask = 1
)

func buildIDTEntry(dest []byte, handler uint64, selector uint16) {
	if len(dest) < 16 {
		panic("idt entry buffer too small")
	}
	// 64-bit interrupt gate layout.
	offsetLow := uint16(handler & 0xffff)
	offsetMid := uint16((handler >> 16) & 0xffff)
	offsetHigh := uint32((handler >> 32) & 0xffffffff)

	binary.LittleEndian.PutUint16(dest[0:], offsetLow)
	binary.LittleEndian.PutUint16(dest[2:], selector)
	dest[4] = 0 // IST
	dest[5] = 0x8e
	binary.LittleEndian.PutUint16(dest[6:], offsetMid)
	binary.LittleEndian.PutUint32(dest[8:], offsetHigh)
	// bytes 12..15 reserved (zero)
}

func buildIRQGuest(cfg irqGuestConfig) (asm.Program, int, error) {
	// IOAPIC selector indices for the chosen line.
	selectorLow := 0x10 + cfg.line*2
	selectorHigh := selectorLow + 1

	// IOAPIC redirection low dword.
	var redirLow uint32
	redirLow |= uint32(cfg.vector)
	redirLow |= uint32(cfg.deliveryMode&0x7) << 8
	if cfg.destMode == 1 {
		redirLow |= 1 << 11
	}
	redirLow |= 1 << 12 // polarity high
	if cfg.level {
		redirLow |= 1 << 15
	}
	if cfg.masked {
		redirLow |= 1 << 16
	}
	redirHigh := uint32(cfg.dest) << 24

	haltLabel := asm.Label("irq_halt")

	mainProg, err := amd64.EmitProgram(asm.Group{
		amd64.Cli(),
		// Load GDT written by host.
		amd64.MovImmediate(amd64.Reg64(amd64.RAX), int64(irqGuestGDTDesc)),
		amd64.Lgdt(amd64.Mem(amd64.Reg64(amd64.RAX))),

		// Load IDTR descriptor written by host.
		amd64.MovImmediate(amd64.Reg64(amd64.RAX), int64(irqGuestIDTRBase)),
		amd64.Lidt(amd64.Mem(amd64.Reg64(amd64.RAX))),

		// Enable xAPIC via IA32_APIC_BASE MSR (0x1b).
		amd64.MovImmediate(amd64.Reg32(amd64.RCX), 0x1b),
		amd64.MovImmediate(amd64.Reg32(amd64.RAX), 0xfee00000|(1<<11)),
		amd64.MovImmediate(amd64.Reg32(amd64.RDX), 0),
		amd64.Wrmsr(),

		// Enable LAPIC (SVR = 0x1FF)
		amd64.MovImmediate(amd64.Reg64(amd64.RAX), int64(irqGuestLapicBase+0xF0)),
		amd64.MovImmediate(amd64.Reg32(amd64.RBX), 0x1ff),
		amd64.MovToMemory(amd64.Mem(amd64.Reg64(amd64.RAX)), amd64.Reg32(amd64.RBX)),

		// Program IOAPIC redirection entry.
		amd64.MovImmediate(amd64.Reg64(amd64.RAX), int64(irqGuestIoapicBase)),
		amd64.MovStoreImm8(amd64.Mem(amd64.Reg64(amd64.RAX)).WithDisp(0x0), selectorLow),
		amd64.MovImmediate(amd64.Reg32(amd64.RBX), int64(redirLow)),
		amd64.MovToMemory(amd64.Mem(amd64.Reg64(amd64.RAX)).WithDisp(0x10), amd64.Reg32(amd64.RBX)),
		amd64.MovStoreImm8(amd64.Mem(amd64.Reg64(amd64.RAX)).WithDisp(0x0), selectorHigh),
		amd64.MovImmediate(amd64.Reg32(amd64.RBX), int64(redirHigh)),
		amd64.MovToMemory(amd64.Mem(amd64.Reg64(amd64.RAX)).WithDisp(0x10), amd64.Reg32(amd64.RBX)),

		amd64.Sti(),

		asm.MarkLabel(haltLabel),
		// Check command mailbox for unmask request.
		amd64.MovImmediate(amd64.Reg64(amd64.RAX), int64(irqGuestCommand)),
		amd64.MovFromMemory(amd64.Reg32(amd64.RBX), amd64.Mem(amd64.Reg64(amd64.RAX))),
		amd64.TestZero(amd64.RBX),
		amd64.JumpIfZero(asm.Label("halt_path")),
		// Unmask IOAPIC if requested.
		amd64.MovStoreImm32(amd64.Mem(amd64.Reg64(amd64.RAX)), 0),
		amd64.MovImmediate(amd64.Reg64(amd64.RAX), int64(irqGuestIoapicBase)),
		amd64.MovStoreImm8(amd64.Mem(amd64.Reg64(amd64.RAX)).WithDisp(0x0), selectorLow),
		amd64.MovImmediate(amd64.Reg32(amd64.RBX), int64(redirLow&^(1<<16))),
		amd64.MovToMemory(amd64.Mem(amd64.Reg64(amd64.RAX)).WithDisp(0x10), amd64.Reg32(amd64.RBX)),
		asm.MarkLabel(asm.Label("halt_path")),
		amd64.Hlt(),
		amd64.Jump(haltLabel),
	})
	if err != nil {
		return asm.Program{}, 0, err
	}

	handlerProg, err := amd64.EmitProgram(asm.Group{
		amd64.PushReg(amd64.Reg64(amd64.RAX)),
		amd64.PushReg(amd64.Reg64(amd64.RBX)),
		amd64.PushReg(amd64.Reg64(amd64.RCX)),
		amd64.PushReg(amd64.Reg64(amd64.RDX)),

		// Determine APIC ID -> counter slot
		amd64.MovImmediate(amd64.Reg64(amd64.RDX), int64(irqGuestLapicBase)),
		amd64.MovFromMemory(amd64.Reg32(amd64.RAX), amd64.Mem(amd64.Reg64(amd64.RDX)).WithDisp(0x20)),
		amd64.ShrRegImm(amd64.Reg32(amd64.RAX), 24),
		amd64.MovReg(amd64.Reg64(amd64.RBX), amd64.Reg64(amd64.RAX)),
		amd64.ShlRegImm(amd64.Reg64(amd64.RBX), 3), // *8
		amd64.MovImmediate(amd64.Reg64(amd64.RCX), int64(irqGuestCounter)),
		amd64.AddRegReg(amd64.Reg64(amd64.RCX), amd64.Reg64(amd64.RBX)),

		amd64.MovFromMemory(amd64.Reg64(amd64.RAX), amd64.Mem(amd64.Reg64(amd64.RCX))),
		amd64.AddRegImm(amd64.Reg64(amd64.RAX), 1),
		amd64.MovToMemory(amd64.Mem(amd64.Reg64(amd64.RCX)), amd64.Reg64(amd64.RAX)),

		// EOI
		amd64.MovImmediate(amd64.Reg64(amd64.RDX), int64(irqGuestLapicBase+0xB0)),
		amd64.MovStoreImm32(amd64.Mem(amd64.Reg64(amd64.RDX)), 0),

		amd64.PopReg(amd64.Reg64(amd64.RDX)),
		amd64.PopReg(amd64.Reg64(amd64.RCX)),
		amd64.PopReg(amd64.Reg64(amd64.RBX)),
		amd64.PopReg(amd64.Reg64(amd64.RAX)),
		amd64.IRet(),
	})
	if err != nil {
		return asm.Program{}, 0, err
	}

	handlerOffset := len(mainProg.Bytes())
	combined := append(mainProg.Bytes(), handlerProg.Bytes()...)
	return asm.NewProgram(combined, nil, 0), handlerOffset, nil
}

type bringUpQuest struct {
	dev          hv.Hypervisor
	riscv        hv.Hypervisor
	architecture hv.CpuArchitecture
}

func (q *bringUpQuest) hypervisorForArch(arch hv.CpuArchitecture) hv.Hypervisor {
	if q.dev != nil && q.dev.Architecture() == arch {
		return q.dev
	}
	if q.riscv != nil && q.riscv.Architecture() == arch {
		return q.riscv
	}
	return nil
}

type resumeRunConfig struct{}

// Run implements hv.RunConfig.
func (resumeRunConfig) Run(ctx context.Context, vcpu hv.VirtualCPU) error {
	if err := vcpu.Run(ctx); err != nil {
		if errors.Is(err, hv.ErrVMHalted) ||
			errors.Is(err, hv.ErrGuestRequestedReboot) ||
			errors.Is(err, hv.ErrYield) {
			return nil
		}

		return err
	}

	return nil
}

func runWithTiming(name string, fn func() error) error {
	start := time.Now()
	err := fn()
	duration := time.Since(start)
	if err != nil {
		slog.Error("Step failed", "step", name, "duration", duration, "error", err)
		return err
	}
	slog.Info("Step completed", "step", name, "duration", duration)
	return nil
}

type ioapicAssert struct {
	vector       uint8
	dest         uint8
	destMode     uint8
	deliveryMode uint8
	level        bool
}

type ioapicHarness struct {
	ioapic   *chipset.IOAPIC
	captures []ioapicAssert
}

func newIoapicHarness(entries int) *ioapicHarness {
	h := &ioapicHarness{
		ioapic: chipset.NewIOAPIC(entries),
	}
	h.ioapic.SetRouting(chipset.IoApicRoutingFunc(func(vector uint8, dest uint8, destMode uint8, deliveryMode uint8, level bool) {
		h.captures = append(h.captures, ioapicAssert{
			vector:       vector,
			dest:         dest,
			destMode:     destMode,
			deliveryMode: deliveryMode,
			level:        level,
		})
	}))
	return h
}

func (h *ioapicHarness) writeRegister(index uint8, value uint32) error {
	// IOAPIC MMIO window offsets: 0x0 = select, 0x10 = data.
	if err := h.ioapic.WriteMMIO(chipset.IOAPICBaseAddress+0x00, []byte{index}); err != nil {
		return fmt.Errorf("write select: %w", err)
	}
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, value)
	if err := h.ioapic.WriteMMIO(chipset.IOAPICBaseAddress+0x10, buf); err != nil {
		return fmt.Errorf("write data: %w", err)
	}
	return nil
}

func (h *ioapicHarness) programEntry(line uint8, vector uint8, dest uint8, logical bool, delivery uint8, level bool, masked bool) error {
	lowIndex := uint8(0x10 + line*2)
	highIndex := lowIndex + 1

	var low uint32
	low |= uint32(vector)
	low |= uint32(delivery) << 8
	if logical {
		low |= 1 << 11
	}
	if level {
		low |= 1 << 15
	}
	if masked {
		low |= 1 << 16
	}

	high := uint32(dest) << 24 // Destination field is bits 56-63.

	if err := h.writeRegister(lowIndex, low); err != nil {
		return err
	}
	return h.writeRegister(highIndex, high)
}

func linuxMemoryBaseForArch(arch hv.CpuArchitecture) uint64 {
	switch arch {
	case hv.ArchitectureARM64:
		return 0x80000000
	case hv.ArchitectureRISCV64:
		return riscvMemoryBase
	default:
		return 0
	}
}

func hostArchitecture() (hv.CpuArchitecture, error) {
	switch runtime.GOARCH {
	case "amd64":
		return hv.ArchitectureX86_64, nil
	case "arm64":
		return hv.ArchitectureARM64, nil
	default:
		return hv.ArchitectureInvalid, fmt.Errorf("unsupported host architecture %q", runtime.GOARCH)
	}
}

func parseArchitecture(value string) (hv.CpuArchitecture, error) {
	switch value {
	case "", "all":
		return hv.ArchitectureInvalid, nil
	case "x86_64", "amd64":
		return hv.ArchitectureX86_64, nil
	case "arm64", "aarch64":
		return hv.ArchitectureARM64, nil
	case "riscv64":
		return hv.ArchitectureRISCV64, nil
	default:
		return hv.ArchitectureInvalid, fmt.Errorf("unknown architecture %q", value)
	}
}

func arm64ExceptionVectorInit(loader *helpers.ProgramLoader) {
	if loader == nil {
		panic("arm64ExceptionVectorInit requires a loader")
	}
	loader.Arm64ExceptionVectors = &helpers.Arm64ExceptionVectorConfig{
		Table: arm64VectorTableBytes,
		Align: arm64VectorEntrySize * arm64VectorEntryCount,
	}
}

func mustBuildArm64VectorTable() []byte {
	handlerBytes, err := arm64.EmitBytes(arm64VectorHandlerFragment())
	if err != nil {
		panic(fmt.Errorf("build arm64 vector handler: %w", err))
	}
	if len(handlerBytes) > arm64VectorEntrySize {
		panic(fmt.Errorf("arm64 vector handler too large (%d bytes)", len(handlerBytes)))
	}

	entry := make([]byte, arm64VectorEntrySize)
	copy(entry, handlerBytes)
	for off := len(handlerBytes); off < arm64VectorEntrySize; off += 4 {
		binary.LittleEndian.PutUint32(entry[off:], arm64NopEncoding)
	}

	table := make([]byte, arm64VectorEntrySize*arm64VectorEntryCount)
	for i := range arm64VectorEntryCount {
		copy(table[i*arm64VectorEntrySize:], entry)
	}
	return table
}

func arm64VectorHandlerFragment() asm.Fragment {
	handlerLoop := asm.Label("arm64_vector_loop")
	return asm.Group{
		asm.MarkLabel(handlerLoop),
		arm64.MovImmediate(arm64.Reg64(arm64.X0), psciSystemOff),
		arm64.Hvc(),
		arm64.Jump(handlerLoop),
	}
}

func validateIRQEncoding(arch hv.CpuArchitecture) error {
	const irq = 58
	encoded := virtio.EncodeIRQLineForArch(arch, irq)
	switch arch {
	case hv.ArchitectureARM64:
		const armTypeShift = 24
		const armTypeSPI = 1
		want := uint32((armTypeSPI << armTypeShift) | (irq & 0xffff))
		if encoded != want {
			return fmt.Errorf("arm64 irq encoding: got %#x want %#x", encoded, want)
		}
	case hv.ArchitectureX86_64:
		if encoded != irq {
			return fmt.Errorf("amd64 irq encoding: got %d want %d", encoded, irq)
		}
	default:
		// Other arches currently don't use EncodeIRQLineForArch; skip.
	}
	return nil
}

func (q *bringUpQuest) runTask(name string, f func() error) error {
	return runWithTiming(name, f)
}

func (q *bringUpQuest) runArchitectureTask(name string, arch hv.CpuArchitecture, f func() error) error {
	if q.architecture != hv.ArchitectureInvalid && q.architecture != arch {
		return nil
	}
	if q.hypervisorForArch(arch) == nil {
		return nil
	}

	return q.runTask(fmt.Sprintf("%s (%s)", name, arch), f)
}

func (q *bringUpQuest) runVMTask(
	name string,
	arch hv.CpuArchitecture,
	prog ir.Program,
	f func(cpu hv.VirtualCPU) error,
	devs ...hv.Device,
) error {
	return q.runArchitectureTask(name, arch, func() error {
		dev := q.hypervisorForArch(arch)
		if dev == nil {
			return fmt.Errorf("hypervisor not initialized for %s", arch)
		}

		baseAddr := uint64(0)
		if arch == hv.ArchitectureRISCV64 {
			baseAddr = linuxMemoryBaseForArch(arch)
		}

		loader := helpers.ProgramLoader{
			Program:           prog,
			BaseAddr:          baseAddr,
			Mode:              helpers.Mode64BitIdentityMapping,
			MaxLoopIterations: 128,
		}
		if arch == hv.ArchitectureARM64 {
			arm64ExceptionVectorInit(&loader)
		}

		vm, err := dev.NewVirtualMachine(hv.SimpleVMConfig{
			NumCPUs: 1,
			MemSize: 64 * 1024 * 1024,
			MemBase: baseAddr,

			VMLoader: &loader,
		})
		if err != nil {
			return fmt.Errorf("create virtual machine: %w", err)
		}
		defer vm.Close()

		for _, dev := range devs {
			if err := vm.AddDevice(dev); err != nil {
				return fmt.Errorf("add device to virtual machine: %w", err)
			}
		}

		err = vm.Run(context.Background(), &loader)
		if !errors.Is(err, hv.ErrVMHalted) {
			return fmt.Errorf("run virtual machine: %w", err)
		}

		if err := vm.VirtualCPUCall(0, f); err != nil {
			return fmt.Errorf("sync vCPU: %w", err)
		}

		return nil
	})
}

func (q *bringUpQuest) runVMTaskWithTimeout(
	name string,
	arch hv.CpuArchitecture,
	timeout time.Duration,
	prog ir.Program,
	f func(cpu hv.VirtualCPU) error,
	devs ...hv.Device,
) error {
	return q.runArchitectureTask(name, arch, func() error {
		dev := q.hypervisorForArch(arch)
		if dev == nil {
			return fmt.Errorf("hypervisor not initialized for %s", arch)
		}

		baseAddr := uint64(0)
		if arch == hv.ArchitectureRISCV64 {
			baseAddr = linuxMemoryBaseForArch(arch)
		}

		loader := helpers.ProgramLoader{
			Program:           prog,
			BaseAddr:          baseAddr,
			Mode:              helpers.Mode64BitIdentityMapping,
			MaxLoopIterations: 128,
		}
		if arch == hv.ArchitectureARM64 {
			arm64ExceptionVectorInit(&loader)
		}

		vm, err := dev.NewVirtualMachine(hv.SimpleVMConfig{
			NumCPUs: 1,
			MemSize: 64 * 1024 * 1024,
			MemBase: baseAddr,

			VMLoader: &loader,
		})
		if err != nil {
			return fmt.Errorf("create virtual machine: %w", err)
		}
		defer vm.Close()

		for _, dev := range devs {
			if err := vm.AddDevice(dev); err != nil {
				return fmt.Errorf("add device to virtual machine: %w", err)
			}
		}

		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()

		closed := make(chan struct{})

		// just in case the timeout doesn't work, we set a deadline on the process
		go func() {
			select {
			case <-closed:
				return
			case <-time.After(timeout * 4):
				slog.Error("VM task timed out, killing process", "name", name)
				os.Exit(1)
			}
		}()

		err = vm.Run(ctx, &loader)
		if !errors.Is(err, hv.ErrVMHalted) && !errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("run virtual machine: %w", err)
		}

		if err := vm.VirtualCPUCall(0, f); err != nil {
			return fmt.Errorf("sync vCPU: %w", err)
		}

		close(closed)

		return nil
	})
}

func (q *bringUpQuest) Run() error {
	slog.Info("Starting Bringup Quest")

	hostArch, err := hostArchitecture()
	if err != nil {
		return fmt.Errorf("detect host architecture: %w", err)
	}

	if err := q.runTask("Compile x86_64 test program", func() error {
		frag, err := amd64ir.Compile(ir.Method{
			ir.Printf("bringup-quest-ok\n"),
			ir.Return(ir.Int64(42)),
		})
		if err != nil {
			return fmt.Errorf("compile test program: %w", err)
		}

		if _, err := amd64.EmitStandaloneELF(frag); err != nil {
			return fmt.Errorf("emit ELF: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}

	if err := q.runTask("Compile arm64 test program", func() error {
		frag, err := arm64ir.Compile(ir.Method{
			ir.Printf("bringup-quest-ok\n"),
			ir.Return(ir.Int64(42)),
		})
		if err != nil {
			return fmt.Errorf("compile test program: %w", err)
		}

		if _, err := arm64.EmitStandaloneELF(frag); err != nil {
			return fmt.Errorf("emit ELF: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}

	if q.architecture != hv.ArchitectureRISCV64 {
		if err := runWithTiming("Open Hypervisor", func() error {
			dev, err := factory.Open()
			if err != nil {
				return err
			}
			q.dev = dev
			return nil
		}); err != nil {
			return fmt.Errorf("open hypervisor factory: %w", err)
		}
		defer q.dev.Close()
		if q.architecture != hv.ArchitectureInvalid && q.architecture != q.dev.Architecture() {
			return fmt.Errorf("requested architecture %s not available on this host (%s)", q.architecture, hostArch)
		}
	}

	if q.dev != nil {
		if err := q.runTask(fmt.Sprintf("IRQ smoke (%s)", q.dev.Architecture()), func() error {
			arch := q.dev.Architecture()
			vm, err := q.dev.NewVirtualMachine(hv.SimpleVMConfig{
				NumCPUs:          1,
				MemSize:          64 << 20, // 64 MiB
				MemBase:          0,
				InterruptSupport: true,
			})
			if err != nil {
				return fmt.Errorf("create vm: %w", err)
			}
			defer vm.Close()

			setter, ok := vm.(interface {
				SetIRQ(uint32, bool) error
			})
			if !ok {
				return fmt.Errorf("hypervisor %s does not expose SetIRQ", arch)
			}

			irq := virtio.EncodeIRQLineForArch(arch, 40)
			if err := setter.SetIRQ(irq, true); err != nil {
				return fmt.Errorf("assert irq: %w", err)
			}
			if err := setter.SetIRQ(irq, false); err != nil {
				return fmt.Errorf("deassert irq: %w", err)
			}
			return nil
		}); err != nil {
			return err
		}

		if q.dev.Architecture() == hv.ArchitectureX86_64 {
			// runIRQ := func(name string, cfg irqGuestConfig, scenario func(vm hv.VirtualMachine, inject func(uint8, uint8, uint8, uint8) error, runOnce func() error, readCounter func(int) (uint64, error), readTotal func(int) (uint64, error), setCommand func(uint32) error) error) error {
			// 	return q.runTask(name, func() error {
			// 		vm, err := q.dev.NewVirtualMachine(hv.SimpleVMConfig{
			// 			NumCPUs:          1,
			// 			MemSize:          32 << 20,
			// 			MemBase:          0,
			// 			InterruptSupport: true,
			// 		})
			// 		if err != nil {
			// 			return fmt.Errorf("create vm: %w", err)
			// 		}
			// 		defer vm.Close()

			// 		prog, handlerOff, err := buildIRQGuest(cfg)
			// 		if err != nil {
			// 			return fmt.Errorf("build guest: %w", err)
			// 		}
			// 		if _, err := vm.WriteAt(prog.Bytes(), int64(irqGuestBase)); err != nil {
			// 			return fmt.Errorf("write program: %w", err)
			// 		}

			// 		idtEntry := make([]byte, 16)
			// 		buildIDTEntry(idtEntry, irqGuestBase+uint64(handlerOff), 0x08)
			// 		if _, err := vm.WriteAt(idtEntry, int64(irqGuestIDTBase)); err != nil {
			// 			return fmt.Errorf("write idt entry: %w", err)
			// 		}
			// 		slog.Info("irq guest idt", "handler", fmt.Sprintf("%#x", irqGuestBase+uint64(handlerOff)))
			// 		idtr := make([]byte, 10)
			// 		binary.LittleEndian.PutUint16(idtr[0:], uint16(16-1))
			// 		binary.LittleEndian.PutUint64(idtr[2:], irqGuestIDTBase)
			// 		if _, err := vm.WriteAt(idtr, int64(irqGuestIDTRBase)); err != nil {
			// 			return fmt.Errorf("write idtr: %w", err)
			// 		}

			// 		zero := make([]byte, 0x40)
			// 		if _, err := vm.WriteAt(zero, int64(irqGuestCounter)); err != nil {
			// 			return fmt.Errorf("zero counters: %w", err)
			// 		}
			// 		if _, err := vm.WriteAt(zero[:4], int64(irqGuestCommand)); err != nil {
			// 			return fmt.Errorf("zero command: %w", err)
			// 		}

			// 		// Build minimal GDT: null, 64-bit code, data.
			// 		gdt := make([]byte, 24)
			// 		binary.LittleEndian.PutUint64(gdt[8:], 0x00AF9B000000FFFF)  // code
			// 		binary.LittleEndian.PutUint64(gdt[16:], 0x00AF93000000FFFF) // data
			// 		if _, err := vm.WriteAt(gdt, int64(irqGuestGDTBase)); err != nil {
			// 			return fmt.Errorf("write gdt: %w", err)
			// 		}
			// 		gdtr := make([]byte, 10)
			// 		binary.LittleEndian.PutUint16(gdtr[0:], uint16(len(gdt)-1))
			// 		binary.LittleEndian.PutUint64(gdtr[2:], irqGuestGDTBase)
			// 		if _, err := vm.WriteAt(gdtr, int64(irqGuestGDTDesc)); err != nil {
			// 			return fmt.Errorf("write gdtr: %w", err)
			// 		}

			// 		if err := vm.VirtualCPUCall(0, func(vcpu hv.VirtualCPU) error {
			// 			if err := vcpu.(hv.VirtualCPUAmd64).SetLongModeWithSelectors(
			// 				0x20000, // paging base
			// 				4,       // 4 GiB
			// 				0x08,    // code selector
			// 				0x10,    // data selector
			// 			); err != nil {
			// 				return fmt.Errorf("set long mode: %w", err)
			// 			}
			// 			regs := map[hv.Register]hv.RegisterValue{
			// 				hv.RegisterAMD64Rip:    hv.Register64(irqGuestBase),
			// 				hv.RegisterAMD64Rsp:    hv.Register64(irqGuestStackTop),
			// 				hv.RegisterAMD64Rflags: hv.Register64(0x202), // IF=1
			// 			}
			// 			if err := vcpu.SetRegisters(regs); err != nil {
			// 				return fmt.Errorf("set regs: %w", err)
			// 			}
			// 			return nil
			// 		}); err != nil {
			// 			return err
			// 		}

			// 		runOnce := func() error {
			// 			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			// 			defer cancel()
			// 			return vm.VirtualCPUCall(0, func(vcpu hv.VirtualCPU) error {
			// 				if err := vcpu.Run(ctx); err != nil {
			// 					if errors.Is(err, hv.ErrVMHalted) || errors.Is(err, hv.ErrGuestRequestedReboot) {
			// 						return nil
			// 					}
			// 					return err
			// 				}
			// 				return nil
			// 			})
			// 		}

			// 		if err := runOnce(); err != nil {
			// 			return fmt.Errorf("initial run: %w", err)
			// 		}

			// 		injector, ok := vm.(interface {
			// 			InjectInterrupt(uint8, uint8, uint8, uint8) error
			// 		})
			// 		if !ok {
			// 			return fmt.Errorf("hypervisor does not expose InjectInterrupt")
			// 		}

			// 		readCounter := func(idx int) (uint64, error) {
			// 			buf := make([]byte, 8)
			// 			_, err := vm.ReadAt(buf, int64(irqGuestCounter+uint64(idx*8)))
			// 			if err != nil {
			// 				return 0, err
			// 			}
			// 			return binary.LittleEndian.Uint64(buf), nil
			// 		}

			// 		setCommand := func(val uint32) error {
			// 			buf := make([]byte, 4)
			// 			binary.LittleEndian.PutUint32(buf, val)
			// 			_, err := vm.WriteAt(buf, int64(irqGuestCommand))
			// 			return err
			// 		}

			// 		readTotal := func(count int) (uint64, error) {
			// 			var sum uint64
			// 			for i := 0; i < count; i++ {
			// 				v, err := readCounter(i)
			// 				if err != nil {
			// 					return 0, err
			// 				}
			// 				sum += v
			// 			}
			// 			return sum, nil
			// 		}

			// 		return scenario(vm, injector.InjectInterrupt, runOnce, readCounter, readTotal, setCommand)
			// 	})
			// }

			// if err := runIRQ("IRQ e2e edge delivery", irqGuestConfig{
			// 	vector:       0x45,
			// 	line:         3,
			// 	dest:         0,
			// 	destMode:     0,
			// 	deliveryMode: 0,
			// 	level:        false,
			// 	masked:       false,
			// }, func(vm hv.VirtualMachine, inject func(uint8, uint8, uint8, uint8) error, runOnce func() error, readCounter func(int) (uint64, error), readTotal func(int) (uint64, error), setCommand func(uint32) error) error {
			// 	if err := inject(0x45, 0, 0, 0); err != nil {
			// 		return err
			// 	}
			// 	var count uint64
			// 	for i := 0; i < 10; i++ {
			// 		if err := runOnce(); err != nil {
			// 			return err
			// 		}
			// 		val, err := readTotal(16)
			// 		if err != nil {
			// 			return err
			// 		}
			// 		count = val
			// 		if count > 0 {
			// 			break
			// 		}
			// 	}
			// 	if count != 1 {
			// 		if err := inject(0x45, 0, 0, 0); err != nil {
			// 			return err
			// 		}
			// 		for i := 0; i < 5; i++ {
			// 			if err := runOnce(); err != nil {
			// 				return err
			// 			}
			// 			val, err := readTotal(16)
			// 			if err != nil {
			// 				return err
			// 			}
			// 			count = val
			// 			if count > 0 {
			// 				break
			// 			}
			// 		}
			// 		if count != 1 {
			// 			return fmt.Errorf("edge: counter=%d want 1", count)
			// 		}
			// 	}
			// 	return nil
			// }); err != nil {
			// 	return err
			// }

			// if err := runIRQ("IRQ e2e level + EOI gating", irqGuestConfig{
			// 	vector:       0x52,
			// 	line:         4,
			// 	dest:         0,
			// 	destMode:     0,
			// 	deliveryMode: 0,
			// 	level:        true,
			// 	masked:       false,
			// }, func(vm hv.VirtualMachine, inject func(uint8, uint8, uint8, uint8) error, runOnce func() error, readCounter func(int) (uint64, error), readTotal func(int) (uint64, error), setCommand func(uint32) error) error {
			// 	if err := inject(0x52, 0, 0, 0); err != nil {
			// 		return err
			// 	}
			// 	if err := inject(0x52, 0, 0, 0); err != nil {
			// 		return err
			// 	}
			// 	var count uint64
			// 	for i := 0; i < 10; i++ {
			// 		if err := runOnce(); err != nil {
			// 			return err
			// 		}
			// 		val, err := readTotal(16)
			// 		if err != nil {
			// 			return err
			// 		}
			// 		count = val
			// 		if count > 0 {
			// 			break
			// 		}
			// 	}
			// 	if count != 1 {
			// 		return fmt.Errorf("level first assert: counter=%d want 1", count)
			// 	}
			// 	if err := inject(0x52, 0, 0, 0); err != nil {
			// 		return err
			// 	}
			// 	if err := inject(0x52, 0, 0, 0); err != nil {
			// 		return err
			// 	}
			// 	for i := 0; i < 10; i++ {
			// 		if err := runOnce(); err != nil {
			// 			return err
			// 		}
			// 		val, err := readTotal(16)
			// 		if err != nil {
			// 			return err
			// 		}
			// 		count = val
			// 		if count >= 2 {
			// 			break
			// 		}
			// 	}
			// 	if count != 2 {
			// 		return fmt.Errorf("level second assert: counter=%d want 2", count)
			// 	}
			// 	return nil
			// }); err != nil {
			// 	return err
			// }

			// if err := runIRQ("IRQ e2e mask/unmask", irqGuestConfig{
			// 	vector:       0x60,
			// 	line:         5,
			// 	dest:         0,
			// 	destMode:     0,
			// 	deliveryMode: 0,
			// 	level:        false,
			// 	masked:       true,
			// }, func(vm hv.VirtualMachine, inject func(uint8, uint8, uint8, uint8) error, runOnce func() error, readCounter func(int) (uint64, error), readTotal func(int) (uint64, error), setCommand func(uint32) error) error {
			// 	var count uint64
			// 	for i := 0; i < 5; i++ {
			// 		if err := runOnce(); err != nil {
			// 			return err
			// 		}
			// 		val, err := readTotal(16)
			// 		if err != nil {
			// 			return err
			// 		}
			// 		count = val
			// 		if count > 0 {
			// 			break
			// 		}
			// 	}
			// 	if count != 0 {
			// 		return fmt.Errorf("mask: counter=%d want 0", count)
			// 	}
			// 	if err := setCommand(irqCmdUnmask); err != nil {
			// 		return fmt.Errorf("set command: %w", err)
			// 	}
			// 	if err := runOnce(); err != nil {
			// 		return err
			// 	}
			// 	if err := inject(0x60, 0, 0, 0); err != nil {
			// 		return err
			// 	}
			// 	for i := 0; i < 10; i++ {
			// 		if err := runOnce(); err != nil {
			// 			return err
			// 		}
			// 		val, err := readTotal(16)
			// 		if err != nil {
			// 			return err
			// 		}
			// 		count = val
			// 		if count > 0 {
			// 			break
			// 		}
			// 	}
			// 	if count != 1 {
			// 		return fmt.Errorf("unmask: counter=%d want 1", count)
			// 	}
			// 	return nil
			// }); err != nil {
			// 	return err
			// }
		}
	}

	if q.architecture == hv.ArchitectureInvalid || q.architecture == hv.ArchitectureRISCV64 {
		if err := q.runTask("Open RISC-V Hypervisor", func() error {
			dev, err := factory.NewWithArchitecture(hv.ArchitectureRISCV64)
			if err != nil {
				return err
			}
			q.riscv = dev
			return nil
		}); err != nil {
			return fmt.Errorf("open riscv hypervisor: %w", err)
		}
		if q.riscv != nil {
			defer q.riscv.Close()
		}
	}

	if err := q.runVMTask("HLT Test", hv.ArchitectureX86_64, ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": {
				amd64.Hlt(),
			},
		},
	}, func(cpu hv.VirtualCPU) error {
		return nil
	}); err != nil {
		return err
	}

	if err := q.runVMTask("Addition Test", hv.ArchitectureX86_64, ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": {
				amd64.MovImmediate(amd64.Reg32(amd64.RAX), 40),
				amd64.AddRegImm(amd64.Reg32(amd64.RAX), 2),
				amd64.Hlt(),
			},
		},
	}, func(cpu hv.VirtualCPU) error {
		regs := map[hv.Register]hv.RegisterValue{
			hv.RegisterAMD64Rax: hv.Register64(0),
		}

		if err := cpu.GetRegisters(regs); err != nil {
			return fmt.Errorf("get RAX register: %w", err)
		}

		rax := uint64(regs[hv.RegisterAMD64Rax].(hv.Register64))
		if rax != 42 {
			return fmt.Errorf("unexpected RAX value: got %d, want 42", rax)
		}

		return nil
	}); err != nil {
		return err
	}

	// Test reading and writing memory
	if err := q.runVMTask("Memory Read/Write Test", hv.ArchitectureX86_64, ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": {
				// load RIP+0x1000 into RCX
				amd64.LeaRelative(amd64.Reg64(amd64.R8), 0x1000),
				// write value to memory at address in RCX
				// load 0xcafebabe into RAX
				amd64.MovImmediate(amd64.Reg64(amd64.R9), 0x12345678),
				amd64.MovToMemory(amd64.Mem(amd64.Reg64(amd64.R8)), amd64.Reg64(amd64.R9)),
				amd64.MovFromMemory(amd64.Reg64(amd64.R10), amd64.Mem(amd64.Reg64(amd64.R8))),
				amd64.Hlt(),
			},
		},
	}, func(cpu hv.VirtualCPU) error {
		regs := map[hv.Register]hv.RegisterValue{
			hv.RegisterAMD64R9:  hv.Register64(0),
			hv.RegisterAMD64R10: hv.Register64(0),
		}

		if err := cpu.GetRegisters(regs); err != nil {
			return fmt.Errorf("get RBX register: %w", err)
		}

		r9 := uint64(regs[hv.RegisterAMD64R9].(hv.Register64))
		if r9 != 0x12345678 {
			return fmt.Errorf("unexpected R9 value: got 0x%08x, want 0x12345678", r9)
		}

		r10 := uint64(regs[hv.RegisterAMD64R10].(hv.Register64))
		if r10 != 0x12345678 {
			return fmt.Errorf("unexpected R10 value: got 0x%08x, want 0x12345678", r10)
		}

		return nil
	}); err != nil {
		return err
	}

	// Test simple I/O
	const dataMessage asm.Variable = 100
	loop := asm.Label("next_char")
	done := asm.Label("done")

	ioHelloWorld := ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": {
				asm.Group{
					amd64.LoadConstantBytes(dataMessage, append([]byte("Hello, World!"), 0)),
					amd64.LoadAddress(amd64.Reg64(amd64.RSI), dataMessage),
					amd64.MovImmediate(amd64.Reg16(amd64.RDX), 0x3f8),
					asm.MarkLabel(loop),
					amd64.MovFromMemory(amd64.Reg8(amd64.RAX), amd64.Mem(amd64.Reg64(amd64.RSI))),
					amd64.AddRegImm(amd64.Reg64(amd64.RSI), 1),
					amd64.CmpRegImm(amd64.Reg8(amd64.RAX), 0),
					amd64.JumpIfZero(done),
					amd64.OutDXAL(),
					amd64.Jump(loop),
					asm.MarkLabel(done),
					amd64.Hlt(),
				},
			},
		},
	}

	result := make([]byte, 0, 64)
	if err := q.runVMTask("I/O Test", hv.ArchitectureX86_64, ioHelloWorld, func(cpu hv.VirtualCPU) error {
		if !bytes.Equal(result, []byte("Hello, World!")) {
			return fmt.Errorf("unexpected I/O result: got %q, want %q", result, "Hello, World!")
		}

		return nil
	}, hv.SimpleX86IOPortDevice{
		Ports: []uint16{0x3f8},
		WriteFunc: func(port uint16, data []byte) error {
			result = append(result, data...)
			return nil
		},
	}); err != nil {
		return err
	}

	serialBuf := &bytes.Buffer{}
	serialDev := amd64serial.NewSerial16550WithIRQ(0x3f8, 4, serialBuf)
	if err := q.runVMTask("Serial Port Test", hv.ArchitectureX86_64, ioHelloWorld, func(cpu hv.VirtualCPU) error {
		serialOutput := serialBuf.String()
		if serialOutput != "Hello, World!" {
			return fmt.Errorf("unexpected serial output: got %q, want %q", serialOutput, "Hello, World!")
		}
		return nil
	}, serialDev); err != nil {
		return err
	}

	// Test simple MMIO
	result = make([]byte, 0, 64)

	if err := q.runVMTask("MMIO Test", hv.ArchitectureX86_64, ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": {
				asm.Group{
					amd64.LoadConstantBytes(dataMessage, append([]byte("Hello"), 0)),
					amd64.LoadAddress(amd64.Reg64(amd64.RSI), dataMessage),
					amd64.MovImmediate(amd64.Reg64(amd64.RDX), 0xdead0000),
					amd64.MovImmediate(amd64.Reg64(amd64.RAX), 0),
					asm.MarkLabel(loop),
					amd64.MovFromMemory(amd64.Reg8(amd64.RAX), amd64.Mem(amd64.Reg64(amd64.RSI))),
					amd64.AddRegImm(amd64.Reg64(amd64.RSI), 1),
					amd64.CmpRegImm(amd64.Reg8(amd64.RAX), 0),
					amd64.JumpIfZero(done),
					amd64.MovToMemory(amd64.Mem(amd64.Reg64(amd64.RDX)), amd64.Reg8(amd64.RAX)),
					amd64.Jump(loop),
					asm.MarkLabel(done),
					amd64.Hlt(),
				},
			},
		},
	}, func(cpu hv.VirtualCPU) error {
		if !bytes.Equal(result, []byte("Hello")) {
			return fmt.Errorf("unexpected MMIO result: got %q, want %q", result, "Hello, World!")
		}

		return nil
	}, hv.SimpleMMIODevice{
		Regions: []hv.MMIORegion{
			{Address: 0xdead0000, Size: 0x1000},
		},
		WriteFunc: func(addr uint64, data []byte) error {
			// Collect the MMIO writes to verify the output
			if addr == 0xdead0000 {
				result = append(result, data[0])
				return nil
			}

			return fmt.Errorf("unexpected MMIO write to address 0x%08x", addr)
		},
	}); err != nil {
		return err
	}

	// Timeout test
	if err := q.runVMTaskWithTimeout("Timeout Test", hv.ArchitectureX86_64, 100*time.Millisecond, ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": {
				asm.MarkLabel(asm.Label("loop")),
				amd64.Jump(asm.Label("loop")),
			},
		},
	}, func(cpu hv.VirtualCPU) error {
		return nil
	}); err != nil {
		return err
	}

	// Snapshot quest
	if err := q.runArchitectureTask("Snapshot Quest", hv.ArchitectureX86_64, func() error {
		loader := &helpers.ProgramLoader{
			Program: ir.Program{
				Entrypoint: "main",
				Methods: map[string]ir.Method{
					"main": {
						// Set RCX to 0 to indicate start
						amd64.MovImmediate(amd64.Reg64(amd64.RCX), 0),

						// do a raw memory write to 0xf000_0000 to trigger the yield
						amd64.MovImmediate(amd64.Reg64(amd64.RAX), 0xdeadbeef),
						amd64.MovImmediate(amd64.Reg64(amd64.RBX), 0xf0000000),
						amd64.MovToMemory(amd64.Mem(amd64.Reg64(amd64.RBX)), amd64.Reg64(amd64.RAX)),

						// Set RCX to 42 to indicate success
						amd64.MovImmediate(amd64.Reg64(amd64.RCX), 42),

						// Do a raw memory write to 0xf000_0000 again to trigger another yield
						amd64.MovImmediate(amd64.Reg64(amd64.RAX), 0xdeadbeef),
						amd64.MovImmediate(amd64.Reg64(amd64.RBX), 0xf0000000),
						amd64.MovToMemory(amd64.Mem(amd64.Reg64(amd64.RBX)), amd64.Reg64(amd64.RAX)),

						// Halt the CPU
						amd64.Hlt(),
					},
				},
			},
			BaseAddr:          0,
			Mode:              helpers.Mode64BitIdentityMapping,
			MaxLoopIterations: 1,
		}

		vm, err := q.dev.NewVirtualMachine(hv.SimpleVMConfig{
			NumCPUs:  1,
			MemSize:  64 * 1024 * 1024,
			MemBase:  0,
			VMLoader: loader,
		})
		if err != nil {
			return fmt.Errorf("create virtual machine: %w", err)
		}
		defer vm.Close()

		if err := vm.AddDevice(hv.SimpleMMIODevice{
			Regions: []hv.MMIORegion{
				{Address: 0xf000_0000, Size: 4096},
			},
			WriteFunc: func(addr uint64, data []byte) error {
				if addr != 0xf000_0000 {
					return fmt.Errorf("unexpected MMIO write to address 0x%08x", addr)
				}

				// get the data as a uint32
				value := binary.LittleEndian.Uint32(data)

				if value != 0xdeadbeef {
					return fmt.Errorf("unexpected MMIO write value: 0x%08x", value)
				}

				// Yield the VM
				return hv.ErrYield
			},
		}); err != nil {
			return fmt.Errorf("add MMIO device: %w", err)
		}

		if err := vm.Run(context.Background(), loader); err != nil && !errors.Is(err, hv.ErrYield) {
			return fmt.Errorf("run virtual machine: %w", err)
		}

		if err := vm.VirtualCPUCall(0, func(cpu hv.VirtualCPU) error {
			// Verify that RCX is 0
			regs := map[hv.Register]hv.RegisterValue{
				hv.RegisterAMD64Rcx: hv.Register64(0),
			}

			if err := cpu.GetRegisters(regs); err != nil {
				return fmt.Errorf("get RCX register: %w", err)
			}

			rcx := uint64(regs[hv.RegisterAMD64Rcx].(hv.Register64))
			if rcx != 0 {
				return fmt.Errorf("unexpected RCX value after first yield: got %d, want 0", rcx)
			}

			return nil
		}); err != nil {
			return fmt.Errorf("sync vCPU after first yield: %w", err)
		}

		// Capture an initial snapshot
		snapshot, err := vm.CaptureSnapshot()
		if err != nil {
			return fmt.Errorf("create snapshot: %w", err)
		}

		// Let the virtual machine continue and yield again
		if err := vm.Run(context.Background(), resumeRunConfig{}); err != nil {
			return fmt.Errorf("run virtual machine: %w", err)
		}

		// Verify that RCX is 42
		if err := vm.VirtualCPUCall(0, func(cpu hv.VirtualCPU) error {
			regs := map[hv.Register]hv.RegisterValue{
				hv.RegisterAMD64Rcx: hv.Register64(0),
			}

			if err := cpu.GetRegisters(regs); err != nil {
				return fmt.Errorf("get RCX register: %w", err)
			}

			rcx := uint64(regs[hv.RegisterAMD64Rcx].(hv.Register64))
			if rcx != 42 {
				return fmt.Errorf("unexpected RCX value after second yield: got %d, want 42", rcx)
			}

			return nil
		}); err != nil {
			return fmt.Errorf("sync vCPU after second yield: %w", err)
		}

		// Restore from the snapshot
		if err := vm.RestoreSnapshot(snapshot); err != nil {
			return fmt.Errorf("restore snapshot: %w", err)
		}

		// Verify that RCX is back to 0
		if err := vm.VirtualCPUCall(0, func(cpu hv.VirtualCPU) error {
			regs := map[hv.Register]hv.RegisterValue{
				hv.RegisterAMD64Rcx: hv.Register64(0),
			}

			if err := cpu.GetRegisters(regs); err != nil {
				return fmt.Errorf("get RCX register: %w", err)
			}

			rcx := uint64(regs[hv.RegisterAMD64Rcx].(hv.Register64))
			if rcx != 0 {
				return fmt.Errorf("unexpected RCX value after restore: got %d, want 0", rcx)
			}

			return nil
		}); err != nil {
			return fmt.Errorf("sync vCPU after restore: %w", err)
		}

		return nil
	}); err != nil {
		return err
	}

	// ARM64 tests
	if err := q.runVMTask("PSCI SYSTEM_OFF Test", hv.ArchitectureARM64, ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": {
				arm64.MovImmediate(arm64.Reg64(arm64.X0), psciSystemOff),
				arm64.Hvc(),
			},
		},
	}, func(cpu hv.VirtualCPU) error {
		return nil
	}); err != nil {
		return err
	}

	if err := q.runVMTask("Addition Test", hv.ArchitectureARM64, ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": {
				arm64.MovImmediate(arm64.Reg64(arm64.X5), 40),
				arm64.AddRegImm(arm64.Reg64(arm64.X5), 2),
				arm64.MovImmediate(arm64.Reg64(arm64.X0), psciSystemOff),
				arm64.Hvc(),
			},
		},
	}, func(cpu hv.VirtualCPU) error {
		regs := map[hv.Register]hv.RegisterValue{
			hv.RegisterARM64X5: hv.Register64(0),
		}

		if err := cpu.GetRegisters(regs); err != nil {
			return fmt.Errorf("get X5 register: %w", err)
		}

		x5 := uint64(regs[hv.RegisterARM64X5].(hv.Register64))
		if x5 != 42 {
			return fmt.Errorf("unexpected X5 value: got %d, want 42", x5)
		}

		return nil
	}); err != nil {
		return err
	}

	if err := q.runVMTask("Memory Read/Write Test", hv.ArchitectureARM64, ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": {
				asm.Group{
					arm64.MovImmediate(arm64.Reg64(arm64.X10), 0x1000),
					arm64.MovImmediate(arm64.Reg64(arm64.X11), 0xcafebabe),
					arm64.MovToMemory(arm64.Mem(arm64.Reg64(arm64.X10)), arm64.Reg64(arm64.X11)),
					arm64.MovFromMemory(arm64.Reg64(arm64.X12), arm64.Mem(arm64.Reg64(arm64.X10))),
					arm64.MovImmediate(arm64.Reg64(arm64.X0), psciSystemOff),
					arm64.Hvc(),
				},
			},
		},
	}, func(cpu hv.VirtualCPU) error {
		regs := map[hv.Register]hv.RegisterValue{
			hv.RegisterARM64X10: hv.Register64(0),
			hv.RegisterARM64X11: hv.Register64(0),
			hv.RegisterARM64X12: hv.Register64(0),
		}

		if err := cpu.GetRegisters(regs); err != nil {
			return fmt.Errorf("get X12 register: %w", err)
		}

		x10 := uint64(regs[hv.RegisterARM64X10].(hv.Register64))
		x11 := uint64(regs[hv.RegisterARM64X11].(hv.Register64))
		if x11 != 0xcafebabe {
			return fmt.Errorf("unexpected X11 value: got 0x%08x (X10=0x%016x), want 0xcafebabe", x11, x10)
		}

		x12 := uint64(regs[hv.RegisterARM64X12].(hv.Register64))
		if x12 != 0xcafebabe {
			return fmt.Errorf("unexpected X12 value: got 0x%08x, want 0xcafebabe", x12)
		}

		return nil
	}); err != nil {
		return err
	}

	if err := q.runVMTask("HLT Test", hv.ArchitectureRISCV64, ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": {
				riscvasm.Halt(),
			},
		},
	}, func(cpu hv.VirtualCPU) error {
		return nil
	}); err != nil {
		return err
	}

	if err := q.runVMTask("Addition Test", hv.ArchitectureRISCV64, ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": {
				riscvasm.MovImmediate(riscvasm.Reg64(riscvasm.X5), 40),
				riscvasm.AddRegImm(riscvasm.Reg64(riscvasm.X5), 2),
				riscvasm.Halt(),
			},
		},
	}, func(cpu hv.VirtualCPU) error {
		regs := map[hv.Register]hv.RegisterValue{
			hv.RegisterRISCVX5: hv.Register64(0),
		}

		if err := cpu.GetRegisters(regs); err != nil {
			return fmt.Errorf("get X5 register: %w", err)
		}

		x5 := uint64(regs[hv.RegisterRISCVX5].(hv.Register64))
		if x5 != 42 {
			return fmt.Errorf("unexpected X5 value: got %d, want 42", x5)
		}

		return nil
	}); err != nil {
		return err
	}

	if err := q.runVMTask("Memory Read/Write Test", hv.ArchitectureRISCV64, ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": {
				riscvasm.MovImmediate(riscvasm.Reg64(riscvasm.X10), int64(riscvMemoryBase+0x1000)),
				riscvasm.MovImmediate(riscvasm.Reg64(riscvasm.X11), 0xcafebabe),
				riscvasm.MovToMemory(riscvasm.Reg64(riscvasm.X10), riscvasm.Reg64(riscvasm.X11), 0),
				riscvasm.MovFromMemory(riscvasm.Reg64(riscvasm.X12), riscvasm.Reg64(riscvasm.X10), 0),
				riscvasm.Halt(),
			},
		},
	}, func(cpu hv.VirtualCPU) error {
		regs := map[hv.Register]hv.RegisterValue{
			hv.RegisterRISCVX10: hv.Register64(0),
			hv.RegisterRISCVX11: hv.Register64(0),
			hv.RegisterRISCVX12: hv.Register64(0),
		}

		if err := cpu.GetRegisters(regs); err != nil {
			return fmt.Errorf("get riscv registers: %w", err)
		}

		if val := uint64(regs[hv.RegisterRISCVX11].(hv.Register64)); val != 0xcafebabe {
			return fmt.Errorf("unexpected X11 value: got 0x%08x, want 0xcafebabe", val)
		}

		if val := uint64(regs[hv.RegisterRISCVX12].(hv.Register64)); val != 0xcafebabe {
			return fmt.Errorf("unexpected X12 value: got 0x%08x, want 0xcafebabe", val)
		}

		return nil
	}); err != nil {
		return err
	}

	const arm64MMIOMessage asm.Variable = 201
	arm64Loop := asm.Label("arm64_mmio_loop")
	arm64Done := asm.Label("arm64_mmio_done")

	arm64Result := make([]byte, 0, 64)

	arm64Message := append([]byte("Hello, World!"), 0)
	arm64MMIOInit := make([]asm.Fragment, 0, len(arm64Message)*2+1)
	arm64MMIOInit = append(arm64MMIOInit, arm64.MovImmediate(arm64.Reg64(arm64.X1), arm64MessageBuf))
	for idx, b := range arm64Message {
		arm64MMIOInit = append(arm64MMIOInit,
			arm64.MovImmediate(arm64.Reg32(arm64.X3), int64(b)),
			arm64.MovToMemory8(arm64.Mem(arm64.Reg64(arm64.X1)).WithDisp(int32(idx)), arm64.Reg32(arm64.X3)),
		)
	}
	arm64MMIOBody := append(arm64MMIOInit, []asm.Fragment{
		arm64.MovImmediate(arm64.Reg64(arm64.X1), arm64MessageBuf),
		arm64.MovImmediate(arm64.Reg64(arm64.X2), arm64MMIOAddr),
		asm.MarkLabel(arm64Loop),
		arm64.MovZX8(arm64.Reg64(arm64.X3), arm64.Mem(arm64.Reg64(arm64.X1))),
		arm64.CmpRegImm(arm64.Reg64(arm64.X3), 0),
		arm64.JumpIfZero(arm64Done),
		arm64.MovToMemory8(arm64.Mem(arm64.Reg64(arm64.X2)), arm64.Reg32(arm64.X3)),
		arm64.AddRegImm(arm64.Reg64(arm64.X1), 1),
		arm64.Jump(arm64Loop),
		asm.MarkLabel(arm64Done),
		arm64.MovImmediate(arm64.Reg64(arm64.X0), psciSystemOff),
		arm64.Hvc(),
	}...)

	if err := q.runVMTask("MMIO Test", hv.ArchitectureARM64, ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": {
				asm.Group(arm64MMIOBody),
			},
		},
	}, func(cpu hv.VirtualCPU) error {
		if !bytes.Equal(arm64Result, []byte("Hello, World!")) {
			return fmt.Errorf("unexpected MMIO result: got %q, want %q", arm64Result, "Hello, World!")
		}

		return nil
	}, hv.SimpleMMIODevice{
		Regions: []hv.MMIORegion{
			{Address: arm64MMIOAddr, Size: 0x1000},
		},
		WriteFunc: func(addr uint64, data []byte) error {
			if addr == arm64MMIOAddr {
				arm64Result = append(arm64Result, data[0])
				return nil
			}

			return fmt.Errorf("unexpected MMIO write to address 0x%08x", addr)
		},
	}); err != nil {
		return err
	}

	// Timeout test (ARM64)
	if err := q.runVMTaskWithTimeout("Timeout Test", hv.ArchitectureARM64, 100*time.Millisecond, ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": {
				asm.MarkLabel(asm.Label("loop")),
				arm64.Jump(asm.Label("loop")),
			},
		},
	}, func(cpu hv.VirtualCPU) error {
		return nil
	}); err != nil {
		return err
	}

	// Snapshot quest (ARM64)
	if err := q.runArchitectureTask("Snapshot Quest", hv.ArchitectureARM64, func() error {
		loader := &helpers.ProgramLoader{
			Program: ir.Program{
				Entrypoint: "main",
				Methods: map[string]ir.Method{
					"main": {
						arm64.MovImmediate(arm64.Reg64(arm64.X1), 0),
						arm64.MovImmediate(arm64.Reg64(arm64.X2), arm64SnapshotMMIOAddr),
						arm64.MovImmediate(arm64.Reg32(arm64.X3), 0xdeadbeef),

						// Trigger first yield.
						arm64.MovToMemory32(arm64.Mem(arm64.Reg64(arm64.X2)), arm64.Reg32(arm64.X3)),

						// Indicate success and trigger another yield.
						arm64.MovImmediate(arm64.Reg64(arm64.X1), 42),
						arm64.MovToMemory32(arm64.Mem(arm64.Reg64(arm64.X2)), arm64.Reg32(arm64.X3)),

						arm64.MovImmediate(arm64.Reg64(arm64.X0), psciSystemOff),
						arm64.Hvc(),
					},
				},
			},
			BaseAddr:          linuxMemoryBaseForArch(hv.ArchitectureARM64),
			Mode:              helpers.Mode64BitIdentityMapping,
			MaxLoopIterations: 1,
		}

		vm, err := q.dev.NewVirtualMachine(hv.SimpleVMConfig{
			NumCPUs:  1,
			MemSize:  64 * 1024 * 1024,
			MemBase:  linuxMemoryBaseForArch(hv.ArchitectureARM64),
			VMLoader: loader,
		})
		if err != nil {
			return fmt.Errorf("create virtual machine: %w", err)
		}
		defer vm.Close()

		if err := vm.AddDevice(hv.SimpleMMIODevice{
			Regions: []hv.MMIORegion{
				{Address: arm64SnapshotMMIOAddr, Size: 4096},
			},
			WriteFunc: func(addr uint64, data []byte) error {
				if addr != arm64SnapshotMMIOAddr {
					return fmt.Errorf("unexpected MMIO write to address 0x%08x", addr)
				}
				if len(data) < 4 {
					return fmt.Errorf("unexpected MMIO data size: got %d, want >=4", len(data))
				}

				value := binary.LittleEndian.Uint32(data)
				if value != 0xdeadbeef {
					return fmt.Errorf("unexpected MMIO write value: 0x%08x", value)
				}

				return hv.ErrYield
			},
		}); err != nil {
			return fmt.Errorf("add MMIO device: %w", err)
		}

		if err := vm.Run(context.Background(), loader); err != nil && !errors.Is(err, hv.ErrYield) {
			return fmt.Errorf("run virtual machine: %w", err)
		}

		if err := vm.VirtualCPUCall(0, func(cpu hv.VirtualCPU) error {
			regs := map[hv.Register]hv.RegisterValue{
				hv.RegisterARM64X1: hv.Register64(0),
			}
			if err := cpu.GetRegisters(regs); err != nil {
				return fmt.Errorf("get X1 register: %w", err)
			}

			if val := uint64(regs[hv.RegisterARM64X1].(hv.Register64)); val != 0 {
				return fmt.Errorf("unexpected X1 value after first yield: got %d, want 0", val)
			}
			return nil
		}); err != nil {
			return fmt.Errorf("sync vCPU after first yield: %w", err)
		}

		snapshot, err := vm.CaptureSnapshot()
		if err != nil {
			return fmt.Errorf("create snapshot: %w", err)
		}

		if err := vm.Run(context.Background(), resumeRunConfig{}); err != nil {
			return fmt.Errorf("run virtual machine: %w", err)
		}

		if err := vm.VirtualCPUCall(0, func(cpu hv.VirtualCPU) error {
			regs := map[hv.Register]hv.RegisterValue{
				hv.RegisterARM64X1: hv.Register64(0),
			}
			if err := cpu.GetRegisters(regs); err != nil {
				return fmt.Errorf("get X1 register: %w", err)
			}
			if val := uint64(regs[hv.RegisterARM64X1].(hv.Register64)); val != 42 {
				return fmt.Errorf("unexpected X1 value after second yield: got %d, want 42", val)
			}
			return nil
		}); err != nil {
			return fmt.Errorf("sync vCPU after second yield: %w", err)
		}

		if err := vm.RestoreSnapshot(snapshot); err != nil {
			return fmt.Errorf("restore snapshot: %w", err)
		}

		if err := vm.VirtualCPUCall(0, func(cpu hv.VirtualCPU) error {
			regs := map[hv.Register]hv.RegisterValue{
				hv.RegisterARM64X1: hv.Register64(0),
			}
			if err := cpu.GetRegisters(regs); err != nil {
				return fmt.Errorf("get X1 register: %w", err)
			}
			if val := uint64(regs[hv.RegisterARM64X1].(hv.Register64)); val != 0 {
				return fmt.Errorf("unexpected X1 value after restore: got %d, want 0", val)
			}
			return nil
		}); err != nil {
			return fmt.Errorf("sync vCPU after restore: %w", err)
		}

		return nil
	}); err != nil {
		return err
	}

	slog.Info("Bringup Quest Completed")

	return q.RunLinux()
}

func (q *bringUpQuest) RunLinux() error {
	slog.Info("Starting Bringup Quest: Linux Boot")

	hostArch, err := hostArchitecture()
	if err != nil {
		return fmt.Errorf("detect host architecture: %w", err)
	}

	targetArch := q.architecture
	if targetArch == hv.ArchitectureInvalid {
		targetArch = hostArch
	}

	dev, err := factory.OpenWithArchitecture(targetArch)
	if err != nil {
		return fmt.Errorf("open hypervisor factory: %w", err)
	}
	q.dev = dev
	defer q.dev.Close()

	if targetArch != q.dev.Architecture() {
		return fmt.Errorf("linux boot requested for %s but host hypervisor is %s", targetArch, q.dev.Architecture())
	}

	if err := validateIRQEncoding(targetArch); err != nil {
		return fmt.Errorf("irq encoding validation failed: %w", err)
	}

	buf := &bytes.Buffer{}

	devices := []hv.DeviceTemplate{
		virtio.ConsoleTemplate{Out: io.MultiWriter(os.Stdout, buf), In: os.Stdin, Arch: dev.Architecture()},
	}

	loader := &boot.LinuxLoader{
		NumCPUs: 1,
		MemSize: 256 * 1024 * 1024, // 256 MiB
		MemBase: linuxMemoryBaseForArch(q.dev.Architecture()),

		GetCmdline: func(arch hv.CpuArchitecture) ([]string, error) {
			switch q.dev.Architecture() {
			case hv.ArchitectureARM64:
				return []string{
					"console=ttyS0,115200n8",
					fmt.Sprintf("earlycon=uart8250,mmio,0x%x", arm64UARTMMIOBase),
				}, nil
			case hv.ArchitectureX86_64:
				return []string{
					"console=hvc0",
					"quiet",
					// "console=ttyS0,115200n8",
					// "earlycon=uart8250,io,0x3f8",
					"reboot=k",
					"panic=-1",
					"tsc=reliable",
					"tsc_early_khz=3000000",
				}, nil
			case hv.ArchitectureRISCV64:
				return []string{
					"console=hvc0",
					"panic=-1",
				}, nil
			default:
				return nil, fmt.Errorf("unsupported architecture for linux boot: %s", q.dev.Architecture())
			}
		},

		GetKernel: func() (io.ReaderAt, int64, error) {
			kernel, err := kernel.LoadForArchitecture(q.dev.Architecture())
			if err != nil {
				return nil, 0, fmt.Errorf("load kernel for architecture %s: %w", q.dev.Architecture(), err)
			}

			f, err := kernel.Open()
			if err != nil {
				return nil, 0, fmt.Errorf("open kernel image: %w", err)
			}

			size, err := kernel.Size()
			if err != nil {
				return nil, 0, fmt.Errorf("get kernel size: %w", err)
			}

			return f, size, nil
		},

		GetSystemMap: func() (io.ReaderAt, error) {
			f, err := os.Open(filepath.Join("local", "System.map"))
			if err != nil {
				return nil, fmt.Errorf("open System.map: %w", err)
			}
			return f, nil
		},

		GetInit: func(arch hv.CpuArchitecture) (*ir.Program, error) {
			return &ir.Program{
				Entrypoint: "main",
				Methods: map[string]ir.Method{
					"main": {
						ir.Printf("Hello, World\n"),
						ir.Syscall(
							defs.SYS_REBOOT,
							ir.Int64(amd64defs.LINUX_REBOOT_MAGIC1),
							ir.Int64(amd64defs.LINUX_REBOOT_MAGIC2),
							ir.Int64(amd64defs.LINUX_REBOOT_CMD_RESTART),
							ir.Int64(0),
						),
					},
				},
			}, nil
		},

		SerialStdout: os.Stdout,

		Devices: devices,
	}

	var vm hv.VirtualMachine
	if err := runWithTiming("Create Virtual Machine", func() error {
		var err error
		vm, err = q.dev.NewVirtualMachine(loader)
		return err
	}); err != nil {
		return fmt.Errorf("create virtual machine: %w", err)
	}
	defer vm.Close()

	var run hv.RunConfig
	if err := runWithTiming("Prepare Run Config", func() error {
		var err error
		run, err = loader.RunConfig()
		return err
	}); err != nil {
		return fmt.Errorf("prepare linux boot: %w", err)
	}

	if err := runWithTiming("Run Linux VM", func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		return vm.Run(ctx, run)
	}); err != nil {
		return fmt.Errorf("run linux boot: %w", err)
	}

	if !bytes.Contains(buf.Bytes(), []byte("Hello, World")) {
		return fmt.Errorf("linux did not print expected output")
	}

	slog.Info("Linux Boot Completed Successfully")

	return nil
}

func RunInitX(debug bool) error {
	slog.Info("Starting Bringup Quest: InitX Boot")

	hv, err := factory.Open()
	if err != nil {
		return fmt.Errorf("open hypervisor factory: %w", err)
	}
	defer hv.Close()

	kernel, err := kernel.LoadForArchitecture(hv.Architecture())
	if err != nil {
		return fmt.Errorf("load kernel for architecture %s: %w", hv.Architecture(), err)
	}

	vm, err := initx.NewVirtualMachine(hv, 1, 256, kernel,
		initx.WithDebugLogging(debug),
	)
	if err != nil {
		return fmt.Errorf("create initx virtual machine: %w", err)
	}
	defer vm.Close()

	for i := range 10 {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)

		err := vm.Run(
			ctx,
			&ir.Program{
				Entrypoint: "main",
				Methods: map[string]ir.Method{
					"main": {
						ir.Printf(fmt.Sprintf("initx-bringup-quest-%d\n", i)),
						ir.Return(ir.Int64(0)),
					},
				},
			},
		)
		cancel()
		if err != nil {
			return fmt.Errorf("run initx virtual machine: %w", err)
		}
	}

	slog.Info("InitX Bringup Quest Completed Successfully")

	return nil
}

func RunExecutable(path string) error {
	slog.Info("Starting Bringup Quest: Run Executable", "path", path)

	if os.Getenv("CC_DEBUG_FILE") != "" {
		if os.Getenv("CC_DEBUG_MEMORY") != "" {
			mem, err := debug.OpenMemory()
			if err != nil {
				return fmt.Errorf("open debug memory: %w", err)
			}
			defer func() {
				f, err := os.Create(os.Getenv("CC_DEBUG_FILE"))
				if err != nil {
					slog.Warn("failed to create debug file", "error", err)
					return
				}
				defer f.Close()
				if _, err := mem.WriteTo(f); err != nil {
					slog.Warn("failed to write debug memory to file", "error", err)
					return
				}
			}()
			debug.Writef("bringup", "debug memory enabled")
		} else {
			if err := debug.OpenFile(os.Getenv("CC_DEBUG_FILE")); err != nil {
				return fmt.Errorf("open debug file: %w", err)
			}
			defer debug.Close()
			debug.Writef("bringup", "debug file: %s", os.Getenv("CC_DEBUG_FILE"))
		}
		debug.Writef("bringup", "debug memory enabled")
	}

	hv, err := factory.Open()
	if err != nil {
		return fmt.Errorf("open hypervisor factory: %w", err)
	}
	defer hv.Close()

	kernel, err := kernel.LoadForArchitecture(hv.Architecture())
	if err != nil {
		return fmt.Errorf("load kernel for architecture %s: %w", hv.Architecture(), err)
	}

	fileData, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read executable file: %w", err)
	}

	// Create netstack backend to handle and echo packets
	ns := netstack.New(slog.Default())
	defer ns.Close()
	guestMAC := net.HardwareAddr{0x02, 0x00, 0x00, 0x00, 0x00, 0x01}
	netBackend, err := virtio.NewNetstackBackend(ns, guestMAC)
	if err != nil {
		return fmt.Errorf("create netstack backend: %w", err)
	}
	if dir := os.Getenv("CC_NETSTACK_PCAP_DIR"); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create pcap dir: %w", err)
		}
		name := fmt.Sprintf("bringup-%s-%d.pcap", time.Now().UTC().Format("20060102T150405.000000000Z"), os.Getpid())
		path := filepath.Join(dir, name)
		f, err := os.Create(path)
		if err != nil {
			return fmt.Errorf("create pcap file: %w", err)
		}
		defer f.Close()
		if err := netBackend.OpenPacketCapture(f); err != nil {
			return fmt.Errorf("enable pcap: %w", err)
		}
		slog.Info("netstack pcap enabled", "path", path)
	}

	const (
		bringupTCPEchoPort = 4242
		bringupUDPEchoPort = 4243
		bringupHTTPPort    = 4244
	)

	tcpEchoListener, err := ns.ListenInternal("tcp", fmt.Sprintf(":%d", bringupTCPEchoPort))
	if err != nil {
		return fmt.Errorf("start tcp echo listener: %w", err)
	}
	defer tcpEchoListener.Close()

	go func() {
		for {
			conn, err := tcpEchoListener.Accept()
			if err != nil {
				if errors.Is(err, net.ErrClosed) {
					return
				}
				slog.Warn("tcp echo accept failed", "err", err)
				return
			}

			go func(c net.Conn) {
				defer c.Close()
				slog.Info("tcp echo accepted", "remote", c.RemoteAddr().String(), "local", c.LocalAddr().String())
				buf := make([]byte, 2048)
				for {
					_ = c.SetDeadline(time.Now().Add(2 * time.Second))
					n, err := c.Read(buf)
					if err != nil {
						if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
							return
						}
						// Deadline or other transient error; just close.
						return
					}
					if n == 0 {
						continue
					}
					slog.Info("tcp echo read", "n", n)
					if _, err := c.Write(buf[:n]); err != nil {
						slog.Warn("tcp echo write failed", "err", err)
						return
					}
				}
			}(conn)
		}
	}()

	udpEchoConn, err := ns.ListenPacketInternal("udp", fmt.Sprintf(":%d", bringupUDPEchoPort))
	if err != nil {
		return fmt.Errorf("start udp echo listener: %w", err)
	}
	defer udpEchoConn.Close()

	go func() {
		buf := make([]byte, 2048)
		for {
			n, addr, err := udpEchoConn.ReadFrom(buf)
			if err != nil {
				if errors.Is(err, net.ErrClosed) {
					return
				}
				slog.Warn("udp echo read failed", "err", err)
				return
			}
			if n == 0 {
				continue
			}
			if _, err := udpEchoConn.WriteTo(buf[:n], addr); err != nil {
				if errors.Is(err, net.ErrClosed) {
					return
				}
				slog.Warn("udp echo write failed", "err", err)
				return
			}
		}
	}()

	httpLn, err := ns.ListenInternal("tcp", fmt.Sprintf(":%d", bringupHTTPPort))
	if err != nil {
		return fmt.Errorf("start bringup http listener: %w", err)
	}
	defer httpLn.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("/download/{size}", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		b := r.PathValue("size")
		if b == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		totalBytes, err := strconv.Atoi(b)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", totalBytes))
		w.WriteHeader(http.StatusOK)

		buf := make([]byte, 32*1024)
		remaining := totalBytes
		writtenTotal := 0
		for remaining > 0 {
			n := min(remaining, len(buf))
			start := time.Now()
			wrote, err := w.Write(buf[:n])
			elapsed := time.Since(start)
			if elapsed > 100*time.Millisecond {
				slog.Warn("bringup http download write slow",
					"size", totalBytes,
					"wrote", wrote,
					"remaining", remaining,
					"elapsed", elapsed,
					"err", err,
				)
			}
			if err != nil {
				slog.Warn("bringup http download write failed",
					"size", totalBytes,
					"writtenTotal", writtenTotal,
					"remaining", remaining,
					"wrote", wrote,
					"err", err,
				)
				return
			}
			writtenTotal += wrote
			remaining -= wrote
		}
		slog.Debug("bringup http download complete", "size", totalBytes, "writtenTotal", writtenTotal)
	})

	httpSrv := &http.Server{Handler: mux}
	go func() {
		if err := httpSrv.Serve(httpLn); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Warn("bringup http server exited", "err", err)
		}
	}()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = httpSrv.Shutdown(ctx)
		cancel()
	}()

	vm, err := initx.NewVirtualMachine(hv, 1, 256, kernel,
		initx.WithFileFromBytes("/initx-exec", fileData, fs.FileMode(0755)),
		initx.WithDeviceTemplate(virtio.FSTemplate{
			Tag:     "bringup",
			Backend: vfs.NewVirtioFsBackend(),
			Arch:    hv.Architecture(),
		}),
		initx.WithDeviceTemplate(virtio.NetTemplate{
			Backend: netBackend,
			MAC:     guestMAC,
			Arch:    hv.Architecture(),
		}),
	)
	if err != nil {
		return fmt.Errorf("create initx virtual machine: %w", err)
	}
	defer vm.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()

	slog.Info("Running Executable in InitX Virtual Machine", "path", path)

	args := []string{"-test.v", "-test.timeout=30s"}
	// Enable large bringup guest-side tests (e.g. 1MiB HTTP download) by passing
	// custom test binary flags into the guest.
	if os.Getenv("CC_BRINGUP_LARGE") != "" {
		args = append(args, "-bringup.large")
		if iters := os.Getenv("CC_BRINGUP_LARGE_ITERS"); iters != "" {
			args = append(args, "-bringup.large.iters="+iters)
		}
	}

	if err := vm.Spawn(ctx, "/initx-exec", args...); err != nil {
		return fmt.Errorf("run executable in initx virtual machine: %w", err)
	}

	slog.Info("Executable Run Completed Successfully")

	return nil
}

func main() {
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	linux := fs.Bool("linux", false, "Try booting Linux")
	initX := fs.Bool("initx", false, "Run bringup tests for initx")
	exec := fs.String("exec", "", "Run the executable using initx")
	debug := fs.Bool("debug", false, "Enable debug logging")
	hostArch, err := hostArchitecture()
	if err != nil {
		slog.Error("failed to determine host architecture", "error", err)
		os.Exit(1)
	}
	archFlag := fs.String("arch", string(hostArch), "Architecture to run (x86_64|arm64|riscv64|all)")

	if err := fs.Parse(os.Args[1:]); err != nil {
		slog.Error("failed to parse flags", "error", err)
		os.Exit(1)
	}

	selectedArch, err := parseArchitecture(*archFlag)
	if err != nil {
		slog.Error("invalid architecture", "arch", *archFlag, "error", err)
		os.Exit(1)
	}
	if selectedArch != hv.ArchitectureInvalid &&
		selectedArch != hv.ArchitectureRISCV64 &&
		selectedArch != hostArch {
		slog.Error("requested architecture not supported on this host", "arch", selectedArch, "host", hostArch)
		os.Exit(1)
	}

	q := &bringUpQuest{
		architecture: selectedArch,
	}

	if *initX {
		if err := RunInitX(*debug); err != nil {
			slog.Error("failed bringup quest initx", "error", err)
			os.Exit(1)
		}
		return
	}

	if *exec != "" {
		if err := RunExecutable(*exec); err != nil {
			slog.Error("failed to run executable", "error", err)
			os.Exit(1)
		}
		return
	}

	if *linux {
		if err := q.RunLinux(); err != nil {
			slog.Error("failed bringup quest linux", "error", err)
			os.Exit(1)
		}
		return
	}

	if err := q.Run(); err != nil {
		slog.Error("failed bringup quest", "error", err)
		os.Exit(1)
	}
}
