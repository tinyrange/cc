package virtio

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"

	"github.com/tinyrange/cc/internal/fdt"
	"github.com/tinyrange/cc/internal/hv"
)

const (
	ConsoleDefaultMMIOBase = 0xd0000000
	ConsoleDefaultMMIOSize = 0x200
	ConsoleDefaultIRQLine  = 5
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

type ConsoleTemplate struct {
	Out  io.Writer
	In   io.Reader
	Arch hv.CpuArchitecture
	// IRQLine identifies the interrupt number that should show up in the device
	// tree. When zero an architecture-specific default is used (PIC line 5 on
	// x86, SPI 40 on arm64).
	IRQLine uint32
}

func (t ConsoleTemplate) archOrDefault(vm hv.VirtualMachine) hv.CpuArchitecture {
	if t.Arch != "" && t.Arch != hv.ArchitectureInvalid {
		return t.Arch
	}
	if vm != nil && vm.Hypervisor() != nil {
		return vm.Hypervisor().Architecture()
	}
	return hv.ArchitectureInvalid
}

func (t ConsoleTemplate) irqLineForArch(arch hv.CpuArchitecture) uint32 {
	if t.IRQLine != 0 {
		return t.IRQLine
	}
	if arch == hv.ArchitectureARM64 {
		return armConsoleDefaultIRQ
	}
	return ConsoleDefaultIRQLine
}

// GetLinuxCommandLineParam implements VirtioMMIODevice.
func (t ConsoleTemplate) GetLinuxCommandLineParam() ([]string, error) {
	irqLine := t.irqLineForArch(t.Arch)
	param := fmt.Sprintf(
		"virtio_mmio.device=4k@0x%x:%d",
		ConsoleDefaultMMIOBase,
		irqLine,
	)
	return []string{param}, nil
}

// DeviceTreeNodes implements VirtioMMIODevice.
func (t ConsoleTemplate) DeviceTreeNodes() ([]fdt.Node, error) {
	irqLine := t.irqLineForArch(t.Arch)
	node := fdt.Node{
		Name: fmt.Sprintf("virtio@%x", ConsoleDefaultMMIOBase),
		Properties: map[string]fdt.Property{
			"compatible": {Strings: []string{"virtio,mmio"}},
			"reg":        {U64: []uint64{ConsoleDefaultMMIOBase, ConsoleDefaultMMIOSize}},
			"interrupts": {U32: []uint32{0, irqLine, 4}},
			"status":     {Strings: []string{"okay"}},
		},
	}
	return []fdt.Node{node}, nil
}

func (t ConsoleTemplate) Create(vm hv.VirtualMachine) (hv.Device, error) {
	arch := t.archOrDefault(vm)
	irqLine := t.irqLineForArch(arch)
	encodedLine := encodeConsoleIRQLine(arch, irqLine)
	console := &Console{
		base:    ConsoleDefaultMMIOBase,
		size:    ConsoleDefaultMMIOSize,
		irqLine: encodedLine,
		out:     t.Out,
		in:      t.In,
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
	device  device
	base    uint64
	size    uint64
	irqLine uint32
	out     io.Writer
	in      io.Reader
	mu      sync.Mutex
	pending []byte
}

// Init implements hv.MemoryMappedIODevice.
func (vc *Console) Init(vm hv.VirtualMachine) error {
	if vc.device == nil {
		if vm == nil {
			return fmt.Errorf("virtio-console: virtual machine is nil")
		}
		vc.device = newMMIODevice(vm, vc.base, vc.size, vc.irqLine, consoleDeviceID, consoleVendorID, consoleVersion, []uint64{virtioFeatureVersion1}, vc)
		if vc.in != nil {
			go vc.readInput()
		}
		return nil
	}
	if mmio, ok := vc.device.(*mmioDevice); ok && vm != nil {
		mmio.vm = vm
	}
	return nil
}

// MMIORegions implements hv.MemoryMappedIODevice.
func (vc *Console) MMIORegions() []hv.MMIORegion {
	if vc.size == 0 {
		return nil
	}
	return []hv.MMIORegion{{
		Address: vc.base,
		Size:    vc.size,
	}}
}

// ReadMMIO implements hv.MemoryMappedIODevice.
func (vc *Console) ReadMMIO(addr uint64, data []byte) error {
	dev, err := vc.requireDevice()
	if err != nil {
		return err
	}
	return dev.readMMIO(addr, data)
}

// WriteMMIO implements hv.MemoryMappedIODevice.
func (vc *Console) WriteMMIO(addr uint64, data []byte) error {
	dev, err := vc.requireDevice()
	if err != nil {
		return err
	}
	return dev.writeMMIO(addr, data)
}

func (vc *Console) requireDevice() (device, error) {
	if vc.device == nil {
		return nil, fmt.Errorf("virtio-console: device not initialized")
	}
	return vc.device, nil
}

func encodeConsoleIRQLine(arch hv.CpuArchitecture, irqLine uint32) uint32 {
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
		base:    base,
		size:    size,
		irqLine: irqLine,
		out:     out,
		in:      in,
	}
	console.device = newMMIODevice(vm, base, size, irqLine, consoleDeviceID, consoleVendorID, consoleVersion, []uint64{virtioFeatureVersion1}, console)
	if in != nil {
		go console.readInput()
	}
	return console
}

func (vc *Console) NumQueues() int {
	return consoleQueueCount
}

func (vc *Console) QueueMaxSize(queue int) uint16 {
	return consoleQueueNumMax
}

func (vc *Console) OnReset(device) {
	vc.mu.Lock()
	vc.pending = nil
	vc.mu.Unlock()
}

func (vc *Console) OnQueueNotify(dev device, queue int) error {
	switch queue {
	case queueTransmit:
		return vc.processTransmitQueue(dev, dev.queue(queue))
	case queueReceive:
		return vc.processReceiveQueue(dev, dev.queue(queue))
	}
	return nil
}

func (vc *Console) ReadConfig(device, uint64) (uint32, bool, error) {
	return 0, false, nil
}

func (vc *Console) WriteConfig(device, uint64, uint32) (bool, error) {
	return false, nil
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
			if _, err := vc.out.Write(data); err != nil {
				return total, fmt.Errorf("write console: %w", err)
			}
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
		return nil
	}

	vc.mu.Lock()
	defer vc.mu.Unlock()

	if len(vc.pending) == 0 {
		return nil
	}

	_, availIdx, err := dev.readAvailState(q)
	if err != nil {
		return err
	}
	var interruptNeeded bool

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

		if written > 0 {
			fmt.Fprintf(os.Stderr, "virtio-console: delivered %d bytes to guest (consumed %d)\n", written, consumed)
		}

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

	if dev := vc.device; dev != nil {
		if err := vc.processReceiveQueue(dev, dev.queue(queueReceive)); err != nil {
			slog.Error("virtio-console: process receive queue", "err", err)
		}
	}
}

func (vc *Console) readInput() {
	buf := make([]byte, 4096)
	for {
		n, err := vc.in.Read(buf)
		if n > 0 {
			chunk := append([]byte(nil), buf[:n]...)
			vc.enqueueInput(chunk)
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				slog.Warn("virtio-console: input read error", "err", err)
			}
			return
		}
	}
}

var (
	_ hv.MemoryMappedIODevice = (*Console)(nil)
	_ deviceHandler           = (*Console)(nil)
)
