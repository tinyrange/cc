//go:build windows && amd64

package whp

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"j5.nz/cc/internal/amd64vm"
	"j5.nz/cc/internal/serial"
	"j5.nz/cc/internal/virtio"
	"j5.nz/cc/internal/vmruntime"
)

const (
	ioapicBaseAddress  = 0xFEC00000
	ioapicMMIOSize     = 0x20
	ioapicRedirEntries = 24

	hpetBaseAddress          = 0xFED00000
	hpetAlternateBaseAddress = 0xFED80000
	hpetMMIOWindowSize       = 0x400

	hpetRegGeneralCapabilities  = 0x000
	hpetRegGeneralConfiguration = 0x010
	hpetRegInterruptStatus      = 0x020
	hpetRegMainCounter          = 0x0F0

	hpetClockPeriodFemtoseconds = 10_000_000
	hpetVendorID                = 0x8086
	hpetNumTimers               = 3
	hpetLegacyReplacementCap    = uint64(1 << 15)
	hpetCounterSizeCap          = uint64(1 << 13)
)

func BootKernelToSerial(ctx context.Context, kernel []byte, memoryMB uint64, dmesg bool) (string, error) {
	return bootToCondition(ctx, kernel, nil, memoryMB, dmesg, func(serial string) bool {
		return serial != ""
	})
}

func BootInitramfsToMarker(ctx context.Context, kernel []byte, initrd []byte, memoryMB uint64, dmesg bool, marker string) (string, error) {
	if marker == "" {
		return "", fmt.Errorf("boot marker is required")
	}
	return bootToCondition(ctx, kernel, initrd, memoryMB, dmesg, func(serial string) bool {
		return bytes.Contains([]byte(serial), []byte(marker))
	})
}

func BootInitramfsToMarkerWithFS(ctx context.Context, kernel []byte, initrd []byte, memoryMB uint64, dmesg bool, marker string, fsdevs []*virtio.FS) (string, error) {
	if strings.TrimSpace(marker) == "" {
		return "", fmt.Errorf("boot marker is required")
	}
	return bootToConditionWithDevices(ctx, kernel, initrd, memoryMB, dmesg, fsdevs, nil, func(serial string) bool {
		return strings.Contains(serial, marker)
	})
}

func bootToCondition(ctx context.Context, kernel []byte, initrd []byte, memoryMB uint64, dmesg bool, done func(string) bool) (string, error) {
	return bootToConditionWithDevices(ctx, kernel, initrd, memoryMB, dmesg, nil, nil, done)
}

func BootInitramfsToVsockMarker(ctx context.Context, kernel []byte, initrd []byte, memoryMB uint64, dmesg bool, port uint32, marker string) (string, string, error) {
	if strings.TrimSpace(marker) == "" {
		return "", "", fmt.Errorf("boot marker is required")
	}
	backend := virtio.NewSimpleVsockBackend()
	listener, err := backend.Listen(port)
	if err != nil {
		return "", "", fmt.Errorf("listen vsock control: %w", err)
	}
	defer listener.Close()

	controlConnCh := make(chan virtio.VsockConn, 1)
	controlErrCh := make(chan error, 1)
	var controlOut bytes.Buffer
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			controlErrCh <- err
			return
		}
		controlConnCh <- conn
		_, copyErr := io.Copy(&controlOut, conn)
		controlErrCh <- copyErr
	}()

	vsock := virtio.NewVsock(amd64vm.VsockBase, amd64vm.VsockSize, amd64vm.VsockIRQ, vmruntime.GuestCID, backend)
	defer vsock.Close()

	serial, err := bootToConditionWithDevices(ctx, kernel, initrd, memoryMB, dmesg, nil, vsock, func(string) bool {
		return strings.Contains(controlOut.String(), marker)
	})
	select {
	case conn := <-controlConnCh:
		_ = conn.Close()
	default:
	}
	if err != nil {
		return serial, controlOut.String(), err
	}
	return serial, controlOut.String(), nil
}

func bootToConditionWithDevices(ctx context.Context, kernel []byte, initrd []byte, memoryMB uint64, dmesg bool, fsdevs []*virtio.FS, vsock *virtio.Vsock, done func(string) bool) (string, error) {
	vm, err := newBootVM(amd64vm.MemorySizeBytes(memoryMB))
	if err != nil {
		return "", err
	}
	defer vm.Close()
	rng := virtio.NewRNG(amd64vm.RNGBase, amd64vm.RNGSize, amd64vm.RNGIRQ)
	if err := installBootACPI(vm.Memory()); err != nil {
		return "", fmt.Errorf("install acpi: %w", err)
	}

	extraCmdline := []string{
		"tsc=reliable",
		"tsc_early_khz=3000000",
		"lpj=10000000",
		"no_timer_check",
	}
	extraCmdline = append(extraCmdline, amd64vm.VirtioFSCommandLineArgs(fsdevs)...)
	if vsock != nil {
		extraCmdline = append(extraCmdline, amd64vm.VirtioMMIODeviceArg(vsock.Base, vsock.IRQ))
	}
	extraCmdline = append(extraCmdline, amd64vm.VirtioMMIODeviceArg(rng.Base, rng.IRQ))
	plan, err := amd64vm.PrepareBoot(vm.Memory(), kernel, initrd, amd64vm.BootConfig{
		MemoryMB:     memoryMB,
		Dmesg:        dmesg,
		ExtraCmdline: extraCmdline,
	})
	if err != nil {
		return "", fmt.Errorf("prepare boot: %w", err)
	}
	if err := vm.SetLongMode(plan.EntryGPA, plan.ZeroPageGPA, plan.StackTopGPA, plan.PagingBase); err != nil {
		return "", fmt.Errorf("set long mode: %w", err)
	}

	var out bytes.Buffer
	platform := newBootPlatform(vm, serial.NewUART8250(amd64vm.COM1Base, 0, &out))
	defer platform.Close()
	for _, fsdev := range fsdevs {
		if fsdev != nil {
			platform.AttachFS(fsdev)
		}
	}
	if vsock != nil {
		platform.AttachVsock(vsock)
	}
	platform.AttachRNG(rng)
	if err := vm.EnableEmulation(platform); err != nil {
		return out.String(), fmt.Errorf("enable emulation: %w", err)
	}

	for step := 0; ; step++ {
		if err := ctx.Err(); err != nil {
			return out.String(), fmt.Errorf("%w (%s)", err, platform.Summary())
		}
		var raw runVPExitContext
		exit, err := vm.runWithCancel(ctx, &raw)
		if err != nil {
			if ctx.Err() != nil {
				return out.String(), fmt.Errorf("run step %d: %w (%s)", step, err, platform.Summary())
			}
			return out.String(), fmt.Errorf("run step %d: %w", step, err)
		}
		if done(out.String()) {
			return out.String(), nil
		}
		switch exit.Reason {
		case runVPExitReasonX64IoPortAccess:
			if err := vm.emulateIO(&raw); err != nil {
				io := raw.ioPortAccess()
				return out.String(), fmt.Errorf("emulate io at rip=%#x port=%#x: %w", exit.RIP, io.Port, err)
			}
		case runVPExitReasonMemoryAccess:
			if err := vm.emulateMMIO(&raw); err != nil {
				mem := raw.memoryAccess()
				return out.String(), fmt.Errorf("emulate mmio at rip=%#x gpa=%#x gva=%#x access=%d insn_len=%d insn=% x: %w", exit.RIP, uint64(mem.GPA), mem.GVA, mem.AccessInfo.accessType(), mem.InstructionByteCount, mem.InstructionBytes[:mem.InstructionByteCount], err)
			}
		case runVPExitReasonX64Halt:
			return out.String(), fmt.Errorf("guest halted before serial output")
		case runVPExitReasonX64ApicEoi:
			platform.HandleEOI(raw.apicEoi().InterruptVector)
		case runVPExitReasonCanceled:
			continue
		default:
			return out.String(), fmt.Errorf("unexpected exit %s at rip=%#x", exit.Reason, exit.RIP)
		}
		platform.ReassertIRQs()
	}
}

func BootKernelToSerialWithTimeout(kernel []byte, memoryMB uint64, dmesg bool, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return BootKernelToSerial(ctx, kernel, memoryMB, dmesg)
}

func BootInitramfsToMarkerWithTimeout(kernel []byte, initrd []byte, memoryMB uint64, dmesg bool, marker string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return BootInitramfsToMarker(ctx, kernel, initrd, memoryMB, dmesg, marker)
}

type bootPlatform struct {
	vm            *VM
	uart          *serial.UART8250
	pic           bootPIC
	pit           *bootPIT
	ioapic        bootIOAPIC
	hpet          bootHPET
	fsdevs        []*virtio.FS
	vsock         *virtio.Vsock
	rng           *virtio.RNG
	start         time.Time
	irqAttempts   uint64
	irqDelivered  uint64
	irqFailed     uint64
	irqSuppressed uint64
	irqLine       [16]uint64
	irqMu         sync.Mutex
	irqAsserted   [16]bool
	lastReassert  time.Time
}

func newBootPlatform(vm *VM, uart *serial.UART8250) *bootPlatform {
	p := &bootPlatform{vm: vm, uart: uart, start: time.Now()}
	p.pic.master.vectorBase = 0x20
	p.pic.slave.vectorBase = 0x28
	p.pic.master.mask = 0xff
	p.pic.slave.mask = 0xff
	p.ioapic.init()
	p.pit = newBootPIT(func() {
		p.raiseTimerIRQ()
	})
	return p
}

func (p *bootPlatform) Close() {
	if p != nil && p.pit != nil {
		p.pit.Close()
	}
	if p != nil && p.vsock != nil {
		_ = p.vsock.Close()
	}
}

func (p *bootPlatform) AttachFS(fsdev *virtio.FS) {
	p.fsdevs = append(p.fsdevs, fsdev)
	fsdev.Attach(p.vm, p)
}

func (p *bootPlatform) AttachVsock(vsock *virtio.Vsock) {
	p.vsock = vsock
	vsock.Attach(p.vm, p)
}

func (p *bootPlatform) AttachRNG(rng *virtio.RNG) {
	p.rng = rng
	rng.Attach(p.vm, p)
}

func (p *bootPlatform) ReadIO(port uint16, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	if port >= amd64vm.COM1Base && port+uint16(len(data)) <= amd64vm.COM1Base+8 {
		for i := range data {
			value, err := p.uart.ReadValue(uint64(port)+uint64(i), 1)
			if err != nil {
				return err
			}
			data[i] = byte(value)
		}
		return nil
	}
	if p.pic.Read(port, data) {
		return nil
	}
	if p.pit.Read(port, data) {
		return nil
	}
	for i := range data {
		data[i] = defaultIOReadByte(port + uint16(i))
	}
	return nil
}

func (p *bootPlatform) WriteIO(port uint16, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	if port >= amd64vm.COM1Base && port+uint16(len(data)) <= amd64vm.COM1Base+8 {
		for i := range data {
			if err := p.uart.Write(uint64(port)+uint64(i), data[i:i+1]); err != nil {
				return err
			}
		}
		return nil
	}
	if p.pic.Write(port, data) {
		return nil
	}
	if p.pit.Write(port, data) {
		return nil
	}
	return nil
}

func (p *bootPlatform) ReadMMIO(addr uint64, data []byte) error {
	for _, fsdev := range p.fsdevs {
		if fsdev != nil && fsdev.Contains(addr, len(data)) {
			value, err := fsdev.Read(addr, len(data))
			if err != nil {
				return err
			}
			putUint64(data, value)
			return nil
		}
	}
	if p.vsock != nil && p.vsock.Contains(addr, len(data)) {
		value, err := p.vsock.Read(addr, len(data))
		if err != nil {
			return err
		}
		putUint64(data, value)
		return nil
	}
	if p.rng != nil && p.rng.Contains(addr, len(data)) {
		value, err := p.rng.Read(addr, len(data))
		if err != nil {
			return err
		}
		putUint64(data, value)
		return nil
	}
	if p.ioapic.Read(addr, data) {
		return nil
	}
	if off, ok := hpetOffset(addr, len(data)); ok {
		putUint64(data, p.hpet.read(off))
		return nil
	}
	for i := range data {
		data[i] = 0
	}
	return nil
}

func (p *bootPlatform) WriteMMIO(addr uint64, data []byte) error {
	for _, fsdev := range p.fsdevs {
		if fsdev != nil && fsdev.Contains(addr, len(data)) {
			return fsdev.Write(addr, len(data), readUint64(data))
		}
	}
	if p.vsock != nil && p.vsock.Contains(addr, len(data)) {
		return p.vsock.Write(addr, len(data), readUint64(data))
	}
	if p.rng != nil && p.rng.Contains(addr, len(data)) {
		return p.rng.Write(addr, len(data), readUint64(data))
	}
	if p.ioapic.Write(addr, data) {
		return nil
	}
	if off, ok := hpetOffset(addr, len(data)); ok {
		p.hpet.write(off, readUint64(data))
		return nil
	}
	return nil
}

func (p *bootPlatform) SetIRQ(irq uint32, level bool) error {
	if irq > 0xff {
		return fmt.Errorf("irq line %d out of range", irq)
	}
	if irq < uint32(len(p.irqAsserted)) {
		p.irqMu.Lock()
		p.irqAsserted[irq] = level
		p.irqMu.Unlock()
	}
	if !level {
		return nil
	}
	p.raiseIRQ(uint8(irq))
	return nil
}

func (p *bootPlatform) raiseTimerIRQ() {
	if time.Since(p.start) < 500*time.Millisecond {
		atomic.AddUint64(&p.irqAttempts, 1)
		atomic.AddUint64(&p.irqSuppressed, 1)
		return
	}
	if p.ioapic.enabled(2) {
		p.raiseIRQ(2)
		return
	}
	p.raiseIRQ(0)
}

func (p *bootPlatform) raiseIRQ(line uint8) {
	atomic.AddUint64(&p.irqAttempts, 1)
	if int(line) < len(p.irqLine) {
		atomic.AddUint64(&p.irqLine[line], 1)
	}
	if p.ioapic.enabled(line) {
		vector := p.ioapic.vector(line)
		if vector >= 0x10 {
			if !p.ioapic.levelTriggered(line) {
				if err := p.vm.RequestInterrupt(uint32(vector)); err != nil {
					atomic.AddUint64(&p.irqFailed, 1)
					return
				}
				atomic.AddUint64(&p.irqDelivered, 1)
				return
			}
			if !p.ioapic.beginInterrupt(vector) {
				atomic.AddUint64(&p.irqSuppressed, 1)
				return
			}
			if err := p.vm.RequestInterruptWithTrigger(uint32(vector), interruptTriggerLevel); err != nil {
				atomic.AddUint64(&p.irqFailed, 1)
				p.ioapic.endInterrupt(vector)
			} else {
				atomic.AddUint64(&p.irqDelivered, 1)
			}
			return
		}
	}
	if vector, ok := p.pic.Acknowledge(line); ok {
		if err := p.vm.RequestInterrupt(uint32(vector)); err != nil {
			atomic.AddUint64(&p.irqFailed, 1)
		} else {
			atomic.AddUint64(&p.irqDelivered, 1)
		}
	}
}

func (p *bootPlatform) HandleEOI(vector uint32) {
	p.ioapic.endInterrupt(uint8(vector))
}

func (p *bootPlatform) ReassertIRQs() {
	if p == nil {
		return
	}
	now := time.Now()
	p.irqMu.Lock()
	if !p.lastReassert.IsZero() && now.Sub(p.lastReassert) < 2*time.Millisecond {
		p.irqMu.Unlock()
		return
	}
	p.lastReassert = now
	var lines []uint8
	for line, asserted := range p.irqAsserted {
		if asserted {
			lines = append(lines, uint8(line))
		}
	}
	p.irqMu.Unlock()
	for _, line := range lines {
		p.raiseIRQ(line)
	}
}

func (p *bootPlatform) Summary() string {
	summary := fmt.Sprintf(
		"whp platform irq_attempts=%d irq_delivered=%d irq_failed=%d irq_suppressed=%d irq_lines=%s %s",
		atomic.LoadUint64(&p.irqAttempts),
		atomic.LoadUint64(&p.irqDelivered),
		atomic.LoadUint64(&p.irqFailed),
		atomic.LoadUint64(&p.irqSuppressed),
		p.irqLineSummary(),
		p.ioapic.summaryForLines(p.activeIRQLines()),
	)
	if p.vsock != nil {
		summary += " " + p.vsock.Summary()
	}
	for _, fsdev := range p.fsdevs {
		if fsdev != nil {
			summary += " " + fsdev.Summary()
		}
	}
	return summary
}

func (p *bootPlatform) irqLineSummary() string {
	var b strings.Builder
	b.WriteByte('[')
	first := true
	for line := range p.irqLine {
		count := atomic.LoadUint64(&p.irqLine[line])
		if count == 0 {
			continue
		}
		if !first {
			b.WriteByte(',')
		}
		first = false
		fmt.Fprintf(&b, "%d:%d", line, count)
	}
	b.WriteByte(']')
	return b.String()
}

func (p *bootPlatform) activeIRQLines() []uint8 {
	lines := make([]uint8, 0, len(p.irqLine))
	for line := range p.irqLine {
		if atomic.LoadUint64(&p.irqLine[line]) != 0 {
			lines = append(lines, uint8(line))
		}
	}
	return lines
}

func defaultIOReadByte(port uint16) byte {
	switch {
	case port >= 0xcfc && port <= 0xcff:
		return 0xff
	default:
		return 0
	}
}

func hpetOffset(addr uint64, size int) (uint64, bool) {
	if size == 0 {
		return 0, false
	}
	for _, base := range [...]uint64{hpetBaseAddress, hpetAlternateBaseAddress} {
		if addr >= base && addr+uint64(size) <= base+hpetMMIOWindowSize {
			return addr - base, true
		}
	}
	return 0, false
}

type bootHPET struct {
	config     uint64
	counter    uint64
	lastUpdate time.Time
}

func (h *bootHPET) read(offset uint64) uint64 {
	h.update()
	switch offset {
	case hpetRegGeneralCapabilities:
		return uint64(hpetClockPeriodFemtoseconds)<<32 |
			uint64(hpetVendorID)<<16 |
			hpetCounterSizeCap |
			hpetLegacyReplacementCap |
			uint64(hpetNumTimers-1)<<8 |
			1
	case hpetRegGeneralConfiguration:
		return h.config
	case hpetRegInterruptStatus:
		return 0
	case hpetRegMainCounter:
		return h.counter
	default:
		return 0
	}
}

func (h *bootHPET) write(offset uint64, value uint64) {
	h.update()
	switch offset {
	case hpetRegGeneralConfiguration:
		h.config = value & 0x3
		if h.config&1 != 0 && h.lastUpdate.IsZero() {
			h.lastUpdate = time.Now()
		}
	case hpetRegMainCounter:
		h.counter = value
		h.lastUpdate = time.Now()
	}
}

func (h *bootHPET) update() {
	if h.config&1 == 0 {
		return
	}
	now := time.Now()
	if h.lastUpdate.IsZero() {
		h.lastUpdate = now
		return
	}
	elapsed := now.Sub(h.lastUpdate)
	if elapsed <= 0 {
		return
	}
	h.counter += uint64(elapsed.Nanoseconds()) * 1_000_000 / hpetClockPeriodFemtoseconds
	h.lastUpdate = now
}

func readUint64(data []byte) uint64 {
	var tmp [8]byte
	copy(tmp[:], data)
	return binary.LittleEndian.Uint64(tmp[:])
}

func putUint64(data []byte, value uint64) {
	var tmp [8]byte
	binary.LittleEndian.PutUint64(tmp[:], value)
	copy(data, tmp[:])
}
