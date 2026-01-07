package virtio

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/tinyrange/cc/internal/debug"
	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/timeslice"
)

const (
	ConsoleDefaultMMIOBase = 0xd0000000
	ConsoleDefaultMMIOSize = 0x200
	ConsoleDefaultIRQLine  = 10
	armConsoleDefaultIRQ   = 40

	consoleQueueCount   = 2
	consoleQueueNumMax  = 256
	consoleVendorID     = 0x554d4551 // "QEMU"
	consoleVersion      = 2
	consoleDeviceID     = 3
	consoleInterruptBit = 0x1

	queueReceive  = 0
	queueTransmit = 1
)

const (
	consoleFeatureSize = 1 << 0
)

// consoleDeviceConfig is the shared configuration for console devices.
var consoleDeviceConfig = &MMIODeviceConfig{
	DefaultMMIOBase:   ConsoleDefaultMMIOBase,
	DefaultMMIOSize:   ConsoleDefaultMMIOSize,
	DefaultIRQLine:    ConsoleDefaultIRQLine,
	ArmDefaultIRQLine: armConsoleDefaultIRQ,
	DeviceID:          consoleDeviceID,
	VendorID:          consoleVendorID,
	Version:           consoleVersion,
	QueueCount:        consoleQueueCount,
	QueueMaxSize:      consoleQueueNumMax,
	FeatureBits:       []uint64{virtioFeatureVersion1 | consoleFeatureSize},
	DeviceName:        "virtio-console",
}

// ConsoleDeviceConfig returns the shared configuration for console devices.
// This is useful when constructing ConsoleTemplate with explicit MMIODeviceTemplateBase.
func ConsoleDeviceConfig() *MMIODeviceConfig {
	return consoleDeviceConfig
}

type ConsoleTemplate struct {
	MMIODeviceTemplateBase
	Out io.Writer
	In  io.Reader
}

// NewConsoleTemplate creates a ConsoleTemplate with proper configuration.
func NewConsoleTemplate(out io.Writer, in io.Reader) ConsoleTemplate {
	return ConsoleTemplate{
		MMIODeviceTemplateBase: MMIODeviceTemplateBase{Config: consoleDeviceConfig},
		Out:                    out,
		In:                     in,
	}
}

func (t ConsoleTemplate) Create(vm hv.VirtualMachine) (hv.Device, error) {
	// Ensure config is set (for backward compatibility with direct struct initialization)
	config := t.Config
	if config == nil {
		config = consoleDeviceConfig
	}

	arch := t.ArchOrDefault(vm)
	irqLine := t.IRQLineForArch(arch)
	encodedLine := EncodeIRQLineForArch(arch, irqLine)
	console := &Console{
		MMIODeviceBase: NewMMIODeviceBase(
			config.DefaultMMIOBase,
			config.DefaultMMIOSize,
			encodedLine,
			config,
		),
		out: t.Out,
		in:  t.In,
	}
	if err := console.Init(vm); err != nil {
		return nil, fmt.Errorf("virtio-console: initialize device: %w", err)
	}
	return console, nil
}

var (
	_ hv.DeviceTemplate = ConsoleTemplate{}
	_ VirtioMMIODevice  = ConsoleTemplate{}
)

type Console struct {
	MMIODeviceBase
	out       io.Writer
	in        io.Reader
	mu        sync.Mutex
	pending   []byte
	cfgData   consoleConfig
	inputStop chan struct{}
	inputWG   sync.WaitGroup
}

type consoleConfig struct {
	cols       uint16
	rows       uint16
	maxNrPorts uint32
	emergWrite uint32
}

type consoleSnapshot struct {
	Arch    hv.CpuArchitecture
	Base    uint64
	Size    uint64
	IRQLine uint32
	Pending []byte
}

// Init implements hv.MemoryMappedIODevice.
func (vc *Console) Init(vm hv.VirtualMachine) error {
	if vc.Device() == nil {
		if err := vc.InitBase(vm, vc); err != nil {
			return err
		}
		vc.setDefaultConfig()
		vc.startInputReader()
		return nil
	}
	if mmio, ok := vc.Device().(*mmioDevice); ok && vm != nil {
		mmio.vm = vm
	}
	return nil
}

// Stop implements Stoppable. Call this to clean up the input reader goroutine.
func (vc *Console) Stop() error {
	vc.stopInputReader()
	return nil
}

var (
	tsConsoleRead  = timeslice.RegisterKind("virtio_console_read", 0)
	tsConsoleWrite = timeslice.RegisterKind("virtio_console_write", 0)
)

func init() {
	// Set console timeslices in the config (must be done at init time after registration)
	consoleDeviceConfig.TimesliceRead = tsConsoleRead
	consoleDeviceConfig.TimesliceWrite = tsConsoleWrite
}

// EncodeIRQLineForArch returns the hypervisor-specific IRQ line encoding. On
// arm64 we embed the SPI type in the high bits as expected by KVM/WHP; on other
// architectures the line is returned unchanged.
//
// For ARM64, irqLine is the SPI offset (as used in device tree interrupts property).
// The encoded value contains the SPI offset in the low 16 bits. Each hypervisor's
// SetIRQ decoder is responsible for adding the SPI base (32) to convert to INTID.
func EncodeIRQLineForArch(arch hv.CpuArchitecture, irqLine uint32) uint32 {
	if arch != hv.ArchitectureARM64 {
		return irqLine
	}
	const (
		armKVMIRQTypeShift = 24
		armKVMIRQTypeSPI   = 1
	)
	return (armKVMIRQTypeSPI << armKVMIRQTypeShift) | (irqLine & 0xFFFF)
}

func NewConsole(vm hv.VirtualMachine, base uint64, size uint64, irqLine uint32, out io.Writer, in io.Reader) *Console {
	console := &Console{
		MMIODeviceBase: NewMMIODeviceBase(base, size, irqLine, consoleDeviceConfig),
		out:            out,
		in:             in,
	}
	if err := console.InitBase(vm, console); err != nil {
		// For backwards compatibility, just log the error
		// The original code didn't return errors from this path
	}
	console.setDefaultConfig()
	console.startInputReader()
	return console
}

func (vc *Console) OnReset(device) {
	// Don't clear pending input data on reset - this data came from the host
	// and should be preserved for delivery when the device is re-initialized.
}

func (vc *Console) OnQueueNotify(ctx hv.ExitContext, dev device, queue int) error {
	debug.Writef("virtio-console.OnQueueNotify", "queue=%d", queue)
	switch queue {
	case queueTransmit:
		return vc.processTransmitQueue(dev, dev.queue(queue))
	case queueReceive:
		return vc.processReceiveQueue(dev, dev.queue(queue))
	}
	return nil
}

func (vc *Console) ReadConfig(ctx hv.ExitContext, dev device, offset uint64) (uint32, bool, error) {
	if offset < VIRTIO_MMIO_CONFIG {
		return 0, false, nil
	}

	rel := offset - VIRTIO_MMIO_CONFIG
	cfg := vc.configBytes()
	if int(rel) >= len(cfg) {
		return 0, true, nil
	}

	var buf [4]byte
	copy(buf[:], cfg[rel:])
	return binary.LittleEndian.Uint32(buf[:]), true, nil
}

func (vc *Console) WriteConfig(ctx hv.ExitContext, dev device, offset uint64, value uint32) (bool, error) {
	if offset < VIRTIO_MMIO_CONFIG {
		return false, nil
	}
	// The current console device exposes read-only config.
	_ = value
	return true, nil
}

func (vc *Console) processTransmitQueue(dev device, q *queue) error {
	if q == nil || !q.ready || q.size == 0 {
		return nil
	}

	availFlags, availIdx, err := dev.readAvailState(q)
	if err != nil {
		return err
	}

	var interruptNeeded bool

	for q.lastAvailIdx != availIdx {
		ringIndex := q.lastAvailIdx % q.size
		head, err := dev.readAvailEntry(q, ringIndex)
		if err != nil {
			return err
		}
		written, err := vc.consumeDescriptorChain(dev, q, head)
		if err != nil {
			return err
		}
		if err := dev.recordUsedElement(q, head, written); err != nil {
			return err
		}
		q.lastAvailIdx++
		interruptNeeded = true
	}

	if interruptNeeded && (availFlags&1) == 0 {
		dev.raiseInterrupt(consoleInterruptBit)
	}

	return nil
}

func (vc *Console) consumeDescriptorChain(dev device, q *queue, head uint16) (uint32, error) {
	index := head
	total := uint32(0)
	for i := uint16(0); i < q.size; i++ {
		desc, err := dev.readDescriptor(q, index)
		if err != nil {
			return total, err
		}

		if desc.flags&virtqDescFWrite != 0 {
			return total, fmt.Errorf("unexpected writable descriptor in transmit queue")
		}

		if desc.length > 0 {
			data, err := dev.readGuest(desc.addr, desc.length)
			if err != nil {
				return total, err
			}
			_, err = vc.out.Write(data)
			if err != nil {
				return total, fmt.Errorf("write console: %w", err)
			}
			debug.Writef("virtio-console consumeDescriptorChain wrote", "data=% x", data)
			total += desc.length
		}
		if desc.flags&virtqDescFNext == 0 {
			break
		}
		index = desc.next
	}
	return total, nil
}

func (vc *Console) processReceiveQueue(dev device, q *queue) error {
	if q == nil || !q.ready || q.size == 0 {
		var ready bool
		var size uint16
		if q != nil {
			ready = q.ready
			size = q.size
		}
		debug.Writef("virtio-console.processReceiveQueue skip", "q=%v ready=%v size=%v", q != nil, ready, size)
		return nil
	}

	// Process transmit queue proactively when processing receive queue
	// This handles cases where the guest OS doesn't send queue notifications for the transmit queue
	if txQueue := dev.queue(queueTransmit); txQueue != nil && txQueue.ready && txQueue.size > 0 {
		if err := vc.processTransmitQueue(dev, txQueue); err != nil {
			// Log error but don't fail receive queue processing
			slog.Warn("virtio-console: process transmit queue", "err", err)
		}
	}

	vc.mu.Lock()
	defer vc.mu.Unlock()

	debug.Writef("virtio-console.processReceiveQueue", "pending=%d", len(vc.pending))
	if len(vc.pending) == 0 {
		return nil
	}

	_, availIdx, err := dev.readAvailState(q)
	if err != nil {
		return err
	}
	var interruptNeeded bool

	debug.Writef("virtio-console.processReceiveQueue", "availIdx=%d lastAvailIdx=%d", availIdx, q.lastAvailIdx)
	for q.lastAvailIdx != availIdx && len(vc.pending) > 0 {
		ringIndex := q.lastAvailIdx % q.size
		head, err := dev.readAvailEntry(q, ringIndex)
		if err != nil {
			return err
		}

		written, consumed, err := vc.fillReceiveDescriptorChain(dev, q, head, vc.pending)
		if err != nil {
			return err
		}
		debug.Writef("virtio-console.processReceiveQueue", "delivered written=%d consumed=%d remaining=%d", written, consumed, len(vc.pending)-consumed)

		vc.pending = vc.pending[consumed:]

		if err := dev.recordUsedElement(q, head, written); err != nil {
			return err
		}

		q.lastAvailIdx++
		if written > 0 {
			interruptNeeded = true
		}
	}

	if interruptNeeded {
		debug.Writef("virtio-console.processReceiveQueue", "raising interrupt")
		dev.raiseInterrupt(consoleInterruptBit)
	}

	return nil
}

func (vc *Console) fillReceiveDescriptorChain(dev device, q *queue, head uint16, data []byte) (uint32, int, error) {
	index := head
	totalWritten := uint32(0)
	consumed := 0

	for i := uint16(0); i < q.size && consumed < len(data); i++ {
		desc, err := dev.readDescriptor(q, index)
		if err != nil {
			return totalWritten, consumed, err
		}

		if desc.flags&virtqDescFWrite == 0 {
			return totalWritten, consumed, fmt.Errorf("unexpected read-only descriptor in receive queue")
		}

		if desc.length > 0 {
			toCopy := int(desc.length)
			remaining := len(data) - consumed
			if toCopy > remaining {
				toCopy = remaining
			}
			if toCopy > 0 {
				if err := dev.writeGuest(desc.addr, data[consumed:consumed+toCopy]); err != nil {
					return totalWritten, consumed, err
				}
				totalWritten += uint32(toCopy)
				consumed += toCopy
			}
			if uint32(toCopy) < desc.length {
				break
			}
		}

		if desc.flags&virtqDescFNext == 0 {
			break
		}
		index = desc.next
	}

	return totalWritten, consumed, nil
}

func (vc *Console) enqueueInput(data []byte) {
	if len(data) == 0 {
		return
	}
	vc.mu.Lock()
	vc.pending = append(vc.pending, data...)
	vc.mu.Unlock()

	if dev := vc.Device(); dev != nil {
		q := dev.queue(queueReceive)
		if err := vc.processReceiveQueue(dev, q); err != nil {
			slog.Error("virtio-console: process receive queue", "err", err)
		}
	} else {
		debug.Writef("virtio-console.enqueueInput drop", "data=% x", data)
	}

	debug.Writef("virtio-console.enqueueInput enqueued", "data=% x", data)
}

func (vc *Console) readInput() {
	defer vc.inputWG.Done()
	debug.Writef("virtio-console.readInput", "started in=%T", vc.in)

	buf := make([]byte, 4096)
	for {
		select {
		case <-vc.inputStop:
			debug.Writef("virtio-console.readInput", "stopped via channel")
			return
		default:
		}

		// Set read deadline before blocking read
		if closer, ok := vc.in.(interface{ SetReadDeadline(time.Time) error }); ok {
			_ = closer.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		}

		n, err := vc.in.Read(buf)
		debug.Writef("virtio-console.readInput", "read n=%d err=%v", n, err)
		if n > 0 {
			chunk := append([]byte(nil), buf[:n]...)
			vc.enqueueInput(chunk)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				debug.Writef("virtio-console.readInput", "EOF, sending Ctrl+D to guest")
				// Send Ctrl+D (EOT) to signal EOF to the guest
				vc.enqueueInput([]byte{0x04})
				return
			}
			select {
			case <-vc.inputStop:
				return
			default:
				slog.Warn("virtio-console: input read error", "err", err)
			}
			return
		}
	}
}

func (vc *Console) startInputReader() {
	if vc.in == nil || vc.inputStop != nil {
		debug.Writef("virtio-console.startInputReader skip", "in=%v inputStop=%v", vc.in != nil, vc.inputStop != nil)
		return
	}
	debug.Writef("virtio-console.startInputReader", "starting input reader goroutine")
	vc.inputStop = make(chan struct{})
	vc.inputWG.Add(1)
	go vc.readInput()
}

// StartInputForwarding begins reading from the host's stdin and forwarding to the guest.
// This should be called after the VM has booted and the user command is about to start,
// to avoid delivering stdin data to the init process instead of the user command.
func (vc *Console) StartInputForwarding() {
	vc.startInputReader()
}

func (vc *Console) stopInputReader() {
	if vc.inputStop == nil {
		return
	}
	close(vc.inputStop)
	if closer, ok := vc.in.(io.Closer); ok {
		_ = closer.Close()
	}
	done := make(chan struct{})
	go func() {
		vc.inputWG.Wait()
		close(done)
	}()
	select {
	case <-done:
		vc.inputStop = nil
	case <-time.After(time.Second):
		slog.Warn("virtio-console: timed out stopping input reader")
	}
}

var (
	_ hv.MemoryMappedIODevice = (*Console)(nil)
	_ deviceHandler           = (*Console)(nil)
	_ hv.DeviceSnapshotter    = (*Console)(nil)
	_ Stoppable               = (*Console)(nil)
)

// DeviceSnapshot support ----------------------------------------------------

func (vc *Console) DeviceId() string { return "virtio-console" }

func (vc *Console) CaptureSnapshot() (hv.DeviceSnapshot, error) {
	vc.mu.Lock()
	defer vc.mu.Unlock()

	snap := &consoleSnapshot{
		Arch:    vc.Arch(),
		Base:    vc.Base(),
		Size:    vc.Size(),
		IRQLine: vc.IRQLine(),
	}
	if len(vc.pending) > 0 {
		snap.Pending = append([]byte(nil), vc.pending...)
	}

	return snap, nil
}

func (vc *Console) RestoreSnapshot(snap hv.DeviceSnapshot) error {
	data, ok := snap.(*consoleSnapshot)
	if !ok {
		return fmt.Errorf("virtio-console: invalid snapshot type")
	}

	vc.mu.Lock()
	defer vc.mu.Unlock()

	vc.RestoreBase(data.Arch, data.Base, data.Size, data.IRQLine)
	vc.pending = append(vc.pending[:0], data.Pending...)
	vc.setDefaultConfig()
	vc.SyncToTransport()

	return nil
}

func (vc *Console) setDefaultConfig() {
	vc.cfgData = consoleConfig{
		cols:       80,
		rows:       25,
		maxNrPorts: 1,
		emergWrite: 0,
	}
}

func (vc *Console) configBytes() []byte {
	vc.mu.Lock()
	cfg := vc.cfgData
	vc.mu.Unlock()
	var buf [12]byte
	binary.LittleEndian.PutUint16(buf[0:2], cfg.cols)
	binary.LittleEndian.PutUint16(buf[2:4], cfg.rows)
	binary.LittleEndian.PutUint32(buf[4:8], cfg.maxNrPorts)
	binary.LittleEndian.PutUint32(buf[8:12], cfg.emergWrite)
	return buf[:]
}

// SetSize updates the terminal grid size reported to the guest. This triggers a
// virtio configuration change interrupt so Linux will re-read the console size.
// Best-effort: if the device isn't initialized yet, it updates cached config only.
func (vc *Console) SetSize(cols, rows uint16) {
	if cols == 0 {
		cols = 1
	}
	if rows == 0 {
		rows = 1
	}

	vc.mu.Lock()
	changed := vc.cfgData.cols != cols || vc.cfgData.rows != rows
	vc.cfgData.cols = cols
	vc.cfgData.rows = rows
	dev := vc.Device()
	vc.mu.Unlock()

	if !changed || dev == nil {
		return
	}

	// Bump config generation if we're on virtio-mmio.
	if mmio, ok := dev.(*mmioDevice); ok {
		mmio.configGeneration++
	}

	_ = dev.raiseInterrupt(VIRTIO_MMIO_INT_CONFIG)
}
