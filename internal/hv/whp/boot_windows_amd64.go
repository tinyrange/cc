//go:build windows && amd64

package whp

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
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
	return BootInitramfsToMarkerWithFSAndNet(ctx, kernel, initrd, memoryMB, dmesg, marker, fsdevs, nil)
}

func BootInitramfsToMarkerWithFSAndSettle(ctx context.Context, kernel []byte, initrd []byte, memoryMB uint64, dmesg bool, marker string, fsdevs []*virtio.FS, settle time.Duration) (string, error) {
	if strings.TrimSpace(marker) == "" {
		return "", fmt.Errorf("boot marker is required")
	}
	return bootToConditionWithDevices(ctx, kernel, initrd, memoryMB, dmesg, fsdevs, nil, nil, settle, func(serial string) bool {
		return strings.Contains(serial, marker)
	})
}

func BootInitramfsToMarkerWithFSAndNet(ctx context.Context, kernel []byte, initrd []byte, memoryMB uint64, dmesg bool, marker string, fsdevs []*virtio.FS, netdev *virtio.Net) (string, error) {
	if strings.TrimSpace(marker) == "" {
		return "", fmt.Errorf("boot marker is required")
	}
	return bootToConditionWithDevices(ctx, kernel, initrd, memoryMB, dmesg, fsdevs, nil, netdev, 0, func(serial string) bool {
		return strings.Contains(serial, marker)
	})
}

func bootToCondition(ctx context.Context, kernel []byte, initrd []byte, memoryMB uint64, dmesg bool, done func(string) bool) (string, error) {
	return bootToConditionWithDevices(ctx, kernel, initrd, memoryMB, dmesg, nil, nil, nil, 0, done)
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
	var controlOut lockedBuffer
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

	serial, err := bootToConditionWithDevices(ctx, kernel, initrd, memoryMB, dmesg, nil, vsock, nil, time.Second, func(string) bool {
		return strings.Contains(controlOut.String(), marker)
	})
	select {
	case conn := <-controlConnCh:
		_ = conn.Close()
	default:
	}
	_ = listener.Close()
	select {
	case copyErr := <-controlErrCh:
		if copyErr != nil && !errors.Is(copyErr, io.EOF) && err == nil {
			err = fmt.Errorf("copy vsock control: %w", copyErr)
		}
	case <-time.After(time.Second):
		if err == nil {
			err = fmt.Errorf("copy vsock control did not exit")
		}
	}
	if err != nil {
		return serial, controlOut.String(), err
	}
	return serial, controlOut.String(), nil
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(data)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func bootToConditionWithDevices(ctx context.Context, kernel []byte, initrd []byte, memoryMB uint64, dmesg bool, fsdevs []*virtio.FS, vsock *virtio.Vsock, netdev *virtio.Net, settleAfterDone time.Duration, done func(string) bool) (string, error) {
	vm, err := newBootVM(amd64vm.MemorySizeBytes(memoryMB))
	if err != nil {
		return "", err
	}
	defer vm.Close()
	rng := virtio.NewRNG(amd64vm.RNGBase, amd64vm.RNGSize, amd64vm.RNGIRQ)

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
	if netdev != nil {
		extraCmdline = append(extraCmdline, amd64vm.VirtioMMIODeviceArg(netdev.Base, netdev.IRQ))
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
	if err := installBootACPIForZeroPage(vm.Memory(), plan.ZeroPageGPA); err != nil {
		return "", fmt.Errorf("install acpi: %w", err)
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
	if netdev != nil {
		platform.AttachNet(netdev)
	}
	platform.AttachRNG(rng)
	if err := vm.EnableEmulation(platform); err != nil {
		return out.String(), fmt.Errorf("enable emulation: %w", err)
	}

	doneSeen := false
	var settleDeadline time.Time
	checkDone := func() bool {
		if doneSeen || !done(out.String()) {
			return false
		}
		if settleAfterDone <= 0 {
			return true
		}
		doneSeen = true
		settleDeadline = time.Now().Add(settleAfterDone)
		return false
	}
	for step := 0; ; step++ {
		if checkDone() {
			return out.String(), nil
		}
		if doneSeen && !settleDeadline.IsZero() && time.Now().After(settleDeadline) {
			return out.String(), nil
		}
		if err := ctx.Err(); err != nil {
			return out.String(), fmt.Errorf("%w (%s)", err, platform.Summary())
		}
		if err := platform.armPendingIRQWindow(); err != nil {
			return out.String(), fmt.Errorf("arm pending irq window: %w", err)
		}
		runCtx := ctx
		var cancelRun context.CancelFunc
		if doneSeen && !settleDeadline.IsZero() {
			runCtx, cancelRun = context.WithDeadline(ctx, settleDeadline)
		}
		var raw runVPExitContext
		exit, err := vm.runWithCancel(runCtx, &raw)
		if cancelRun != nil {
			cancelRun()
		}
		if err != nil {
			if doneSeen && !settleDeadline.IsZero() && time.Now().After(settleDeadline) && ctx.Err() == nil {
				return out.String(), nil
			}
			if ctx.Err() != nil {
				return out.String(), fmt.Errorf("run step %d: %w (%s)", step, err, platform.Summary())
			}
			return out.String(), fmt.Errorf("run step %d: %w", step, err)
		}
		platform.recordExit(exit, &raw)
		if checkDone() {
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
			if !platform.hasPendingIRQ() {
				if doneSeen {
					return out.String(), nil
				}
				return out.String(), fmt.Errorf("guest halted before serial output")
			}
		case runVPExitReasonX64ApicEoi:
			platform.HandleEOI(raw.apicEoi().InterruptVector)
		case runVPExitReasonX64InterruptWindow:
		case runVPExitReasonCanceled:
		default:
			return out.String(), fmt.Errorf("unexpected exit %s at rip=%#x", exit.Reason, exit.RIP)
		}
		if flushed, err := platform.flushPendingIRQ(&raw); err != nil {
			return out.String(), fmt.Errorf("flush pending irq after %s at rip=%#x: %w", exit.Reason, exit.RIP, err)
		} else if exit.Reason == runVPExitReasonX64Halt && !flushed && !platform.hasPendingIRQ() {
			return out.String(), fmt.Errorf("guest halted before serial output")
		}
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
	netdev        *virtio.Net
	start         time.Time
	irqAttempts   uint64
	irqDelivered  uint64
	irqFailed     uint64
	irqSuppressed uint64
	irqLine       [16]uint64
	deviceIRQLine [ioapicRedirEntries]bool
	pendingMu     sync.Mutex
	pendingIRQ    [256]bool
	pendingIRQs   []pendingIRQ
	lastExitMu    sync.Mutex
	lastExit      bootPlatformExitSnapshot
}

type pendingIRQ struct {
	route   bootIOAPICRoute
	trigger interruptTriggerMode
}

type bootPlatformExitSnapshot struct {
	reason runVPExitReason
	rip    uint64
	ctx    vpExitContext
	ok     bool
}

func (p *bootPlatform) recordExit(exit Exit, raw *runVPExitContext) {
	if p == nil || raw == nil {
		return
	}
	p.lastExitMu.Lock()
	p.lastExit = bootPlatformExitSnapshot{
		reason: exit.Reason,
		rip:    exit.RIP,
		ctx:    raw.VpContext,
		ok:     true,
	}
	p.lastExitMu.Unlock()
}

func (p *bootPlatform) lastExitSummary() string {
	if p == nil {
		return ""
	}
	p.lastExitMu.Lock()
	snap := p.lastExit
	p.lastExitMu.Unlock()
	if !snap.ok {
		return ""
	}
	return fmt.Sprintf(
		"last_exit={reason=%s,rip=%#x,ctx_rip=%#x,rflags=%#x,cr8=%d,pending=%t,shadow=%t}",
		snap.reason,
		snap.rip,
		snap.ctx.Rip,
		snap.ctx.Rflags,
		snap.ctx.cr8(),
		snap.ctx.ExecutionState.interruptionPending(),
		snap.ctx.ExecutionState.interruptShadow(),
	)
}

func (p *bootPlatform) vpRegisterSummary() string {
	if p == nil || p.vm == nil || p.vm.part == 0 {
		return ""
	}
	names := []registerName{
		registerRip,
		registerRflags,
		registerPendingInterruption,
		registerDeliverabilityNotifications,
		registerInternalActivityState,
	}
	values := make([]registerValue, len(names))
	if err := getVirtualProcessorRegisters(p.vm.part, 0, names, values); err != nil {
		return fmt.Sprintf("vp_regs_error=%q", err)
	}
	return fmt.Sprintf(
		"vp={rip=%#x,rflags=%#x,pending=%#x,deliverability=%#x,activity=%#x}",
		values[0].uint64(),
		values[1].uint64(),
		values[2].uint64(),
		values[3].uint64(),
		values[4].uint64(),
	)
}

func newBootPlatform(vm *VM, uart *serial.UART8250) *bootPlatform {
	p := &bootPlatform{vm: vm, uart: uart, start: time.Now()}
	p.pic.master.vectorBase = 0x20
	p.pic.slave.vectorBase = 0x28
	p.pic.master.mask = 0xff
	p.pic.slave.mask = 0xff
	p.ioapic.init()
	if uart != nil {
		uart.AttachIRQ(p, amd64vm.COM1IRQ)
	}
	p.pit = newBootPIT(func() {
		p.raiseTimerIRQ()
	})
	return p
}

func (p *bootPlatform) Close() {
	if p == nil {
		return
	}
	if p.pit != nil {
		p.pit.Close()
	}
	if p.vsock != nil {
		_ = p.vsock.Close()
	}
	for _, fsdev := range p.fsdevs {
		_ = fsdev.Close()
	}
	p.deassertDeviceIRQs()
}

func (p *bootPlatform) deassertDeviceIRQs() {
	for line, isDevice := range p.deviceIRQLine {
		if !isDevice {
			continue
		}
		_ = p.SetIRQ(uint32(line), false)
	}
}

func (p *bootPlatform) AttachFS(fsdev *virtio.FS) {
	p.fsdevs = append(p.fsdevs, fsdev)
	p.markDeviceIRQ(fsdev.IRQ)
	fsdev.Attach(p.vm, p)
}

func (p *bootPlatform) AttachVsock(vsock *virtio.Vsock) {
	p.vsock = vsock
	p.markDeviceIRQ(vsock.IRQ)
	vsock.Attach(p.vm, p)
}

func (p *bootPlatform) AttachRNG(rng *virtio.RNG) {
	p.rng = rng
	p.markDeviceIRQ(rng.IRQ)
	rng.Attach(p.vm, p)
}

func (p *bootPlatform) AttachNet(netdev *virtio.Net) {
	p.netdev = netdev
	p.markDeviceIRQ(netdev.IRQ)
	netdev.Attach(p.vm, p)
}

func (p *bootPlatform) markDeviceIRQ(irq uint32) {
	if irq < ioapicRedirEntries {
		p.deviceIRQLine[irq] = true
	}
}

func (p *bootPlatform) isDeviceIRQ(line uint8) bool {
	return int(line) < len(p.deviceIRQLine) && p.deviceIRQLine[line]
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
	if p.netdev != nil && p.netdev.Contains(addr, len(data)) {
		value, err := p.netdev.Read(addr, len(data))
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
	if p.netdev != nil && p.netdev.Contains(addr, len(data)) {
		return p.netdev.Write(addr, len(data), readUint64(data))
	}
	if handled, route, pending := p.ioapic.Write(addr, data); handled {
		if pending {
			p.injectIOAPIC(route, p.isDeviceIRQ(route.line))
		}
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
	line := uint8(irq)
	if level {
		p.recordIRQAttempt(line)
	} else {
		p.clearPendingIRQForLine(line)
	}
	if int(line) < ioapicRedirEntries {
		ioapicEnabled := p.ioapic.enabled(line)
		if route, pending := p.ioapic.assert(line, level); pending {
			p.injectIOAPIC(route, true)
			return nil
		}
		if !level || ioapicEnabled {
			if level {
				atomic.AddUint64(&p.irqSuppressed, 1)
			}
			return nil
		}
	}
	if level {
		p.injectPIC(line, true)
	}
	return nil
}

func (p *bootPlatform) raiseTimerIRQ() {
	if time.Since(p.start) < 500*time.Millisecond {
		atomic.AddUint64(&p.irqAttempts, 1)
		atomic.AddUint64(&p.irqSuppressed, 1)
		return
	}
	if p.reassertDeviceIRQ() {
		return
	}
	if p.ioapic.enabled(2) {
		p.raiseIRQ(2)
		return
	}
	p.raiseIRQ(0)
}

func (p *bootPlatform) raiseIRQ(line uint8) {
	p.recordIRQAttempt(line)
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
	p.injectPIC(line, false)
}

func (p *bootPlatform) reassertDeviceIRQ() bool {
	for line, isDevice := range p.deviceIRQLine {
		if !isDevice {
			continue
		}
		route, ok := p.ioapic.deviceHighRoute(uint8(line))
		if !ok {
			continue
		}
		p.recordIRQAttempt(uint8(line))
		p.injectIOAPIC(route, true)
		return true
	}
	return false
}

func (p *bootPlatform) recordIRQAttempt(line uint8) {
	atomic.AddUint64(&p.irqAttempts, 1)
	if int(line) < len(p.irqLine) {
		atomic.AddUint64(&p.irqLine[line], 1)
	}
}

func (p *bootPlatform) injectIOAPIC(route bootIOAPICRoute, deviceIRQ bool) {
	if route.vector < 0x10 {
		atomic.AddUint64(&p.irqSuppressed, 1)
		p.ioapic.cancel(route)
		return
	}
	trigger := interruptTriggerEdge
	if route.level {
		trigger = interruptTriggerLevel
	}
	if deviceIRQ {
		p.queuePendingIRQ(route, trigger)
		_ = p.vm.kickOutOfHLT()
		p.vm.kickIfRunning()
		return
	}
	if route.level && !p.ioapic.beginInterrupt(route.vector) {
		atomic.AddUint64(&p.irqSuppressed, 1)
		return
	}
	if err := p.vm.RequestInterruptWithTrigger(uint32(route.vector), trigger); err != nil {
		atomic.AddUint64(&p.irqFailed, 1)
		p.ioapic.cancel(route)
		return
	}
	atomic.AddUint64(&p.irqDelivered, 1)
	if deviceIRQ {
		p.vm.kickIfRunning()
	}
}

func (p *bootPlatform) armPendingIRQWindow() error {
	if !p.hasPendingIRQ() {
		return nil
	}
	return p.vm.NotifyInterruptWindow()
}

func (p *bootPlatform) injectPIC(line uint8, kick bool) {
	if vector, ok := p.pic.Acknowledge(line); ok {
		if err := p.vm.RequestInterrupt(uint32(vector)); err != nil {
			atomic.AddUint64(&p.irqFailed, 1)
		} else {
			atomic.AddUint64(&p.irqDelivered, 1)
			if kick {
				p.vm.kickIfRunning()
			}
		}
	}
}

func (p *bootPlatform) HandleEOI(vector uint32) {
	if route, pending := p.ioapic.handleEOI(vector); pending {
		atomic.AddUint64(&p.irqAttempts, 1)
		p.injectIOAPIC(route, p.isDeviceIRQ(route.line))
	}
}

func (p *bootPlatform) queuePendingIRQ(route bootIOAPICRoute, trigger interruptTriggerMode) {
	p.pendingMu.Lock()
	defer p.pendingMu.Unlock()
	if p.pendingIRQ[route.vector] {
		return
	}
	p.pendingIRQ[route.vector] = true
	p.pendingIRQs = append(p.pendingIRQs, pendingIRQ{
		route:   route,
		trigger: trigger,
	})
}

func (p *bootPlatform) clearPendingIRQForLine(line uint8) {
	p.pendingMu.Lock()
	defer p.pendingMu.Unlock()
	for i := 0; i < len(p.pendingIRQs); {
		pending := p.pendingIRQs[i]
		if pending.route.line != line {
			i++
			continue
		}
		p.pendingIRQ[pending.route.vector] = false
		copy(p.pendingIRQs[i:], p.pendingIRQs[i+1:])
		p.pendingIRQs = p.pendingIRQs[:len(p.pendingIRQs)-1]
	}
}

func (p *bootPlatform) flushPendingIRQ(ctx *runVPExitContext) (bool, error) {
	p.pendingMu.Lock()
	if len(p.pendingIRQs) == 0 {
		p.pendingMu.Unlock()
		return false, nil
	}
	pending := p.pendingIRQs[0]
	if ctx == nil {
		p.pendingMu.Unlock()
		return false, nil
	}
	windowExit := ctx.ExitReason == runVPExitReasonX64InterruptWindow
	if !windowExit && !canAcceptInterrupt(ctx, pending.route.vector) {
		p.pendingMu.Unlock()
		return false, nil
	}
	if pending.route.level {
		if _, ok := p.ioapic.deviceHighRoute(pending.route.line); !ok {
			copy(p.pendingIRQs, p.pendingIRQs[1:])
			p.pendingIRQs = p.pendingIRQs[:len(p.pendingIRQs)-1]
			p.pendingIRQ[pending.route.vector] = false
			p.pendingMu.Unlock()
			return false, nil
		}
	}
	if !windowExit {
		p.pendingMu.Unlock()
		var err error
		if p.usePendingInterruptionFallback(pending.route.line) {
			err = p.vm.SetPendingInterruption(pending.route.vector)
		} else {
			err = p.vm.RequestInterruptWithTrigger(uint32(pending.route.vector), pending.trigger)
		}
		if err != nil {
			atomic.AddUint64(&p.irqFailed, 1)
			return false, err
		}
		atomic.AddUint64(&p.irqDelivered, 1)
		return true, nil
	}
	copy(p.pendingIRQs, p.pendingIRQs[1:])
	p.pendingIRQs = p.pendingIRQs[:len(p.pendingIRQs)-1]
	p.pendingIRQ[pending.route.vector] = false
	p.pendingMu.Unlock()

	_ = p.vm.kickOutOfHLT()
	if pending.route.level && !p.ioapic.beginInterrupt(pending.route.vector) {
		atomic.AddUint64(&p.irqSuppressed, 1)
		return false, nil
	}
	if err := p.vm.RequestInterruptWithTrigger(uint32(pending.route.vector), pending.trigger); err != nil {
		atomic.AddUint64(&p.irqFailed, 1)
		p.ioapic.cancel(pending.route)
		return false, err
	}
	atomic.AddUint64(&p.irqDelivered, 1)
	return true, nil
}

func (p *bootPlatform) usePendingInterruptionFallback(line uint8) bool {
	if p.vsock != nil && p.vsock.IRQ == uint32(line) {
		return true
	}
	for _, fsdev := range p.fsdevs {
		if fsdev != nil && fsdev.IRQ == uint32(line) {
			return true
		}
	}
	return false
}

func (p *bootPlatform) hasPendingIRQ() bool {
	p.pendingMu.Lock()
	defer p.pendingMu.Unlock()
	return len(p.pendingIRQs) != 0
}

func (p *bootPlatform) pendingIRQCount() int {
	p.pendingMu.Lock()
	defer p.pendingMu.Unlock()
	return len(p.pendingIRQs)
}

func canAcceptInterrupt(ctx *runVPExitContext, vector uint8) bool {
	if ctx == nil {
		return true
	}
	if ctx.VpContext.ExecutionState.interruptionPending() || ctx.VpContext.ExecutionState.interruptShadow() {
		return false
	}
	const rflagsInterruptEnable = uint64(1 << 9)
	if ctx.VpContext.Rflags&rflagsInterruptEnable == 0 {
		return false
	}
	priority := vector >> 4
	return priority == 0 || priority > ctx.VpContext.cr8()
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
	if pending := p.pendingIRQCount(); pending != 0 {
		summary += fmt.Sprintf(" irq_pending=%d", pending)
	}
	if lastExit := p.lastExitSummary(); lastExit != "" {
		summary += " " + lastExit
	}
	if vp := p.vpRegisterSummary(); vp != "" {
		summary += " " + vp
	}
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
