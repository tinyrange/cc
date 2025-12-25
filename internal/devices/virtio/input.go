package virtio

import (
	"encoding/binary"
	"fmt"
	"log/slog"
	"sync"

	"github.com/tinyrange/cc/internal/fdt"
	"github.com/tinyrange/cc/internal/hv"
)

const (
	InputKeyboardDefaultMMIOBase = 0xd0003000
	InputTabletDefaultMMIOBase   = 0xd0006000 // Changed from 0xd0004000 to avoid conflict with FSDefaultMMIOBase
	InputDefaultMMIOSize         = 0x200
	InputKeyboardDefaultIRQLine  = 13
	InputTabletDefaultIRQLine    = 14
	armInputKeyboardDefaultIRQ   = 43
	armInputTabletDefaultIRQ     = 44

	inputDeviceID     = 18
	inputQueueCount   = 2
	inputQueueNumMax  = 128
	inputVendorID     = 0x554d4551 // "QEMU"
	inputVersion      = 2
	inputInterruptBit = 0x1

	inputQueueEvent  = 0
	inputQueueStatus = 1

	// Virtio input config selects
	VIRTIO_INPUT_CFG_UNSET     = 0x00
	VIRTIO_INPUT_CFG_ID_NAME   = 0x01
	VIRTIO_INPUT_CFG_ID_SERIAL = 0x02
	VIRTIO_INPUT_CFG_ID_DEVIDS = 0x03
	VIRTIO_INPUT_CFG_PROP_BITS = 0x10
	VIRTIO_INPUT_CFG_EV_BITS   = 0x11
	VIRTIO_INPUT_CFG_ABS_INFO  = 0x12
)

// InputType specifies the type of input device
type InputType int

const (
	InputTypeKeyboard InputType = iota
	InputTypeTablet
)

// InputTemplate is the device template for creating a Virtio-Input device.
type InputTemplate struct {
	Arch     hv.CpuArchitecture
	IRQLine  uint32
	Type     InputType
	MMIOBase uint64
	Name     string
}

func (t InputTemplate) archOrDefault(vm hv.VirtualMachine) hv.CpuArchitecture {
	if t.Arch != "" && t.Arch != hv.ArchitectureInvalid {
		return t.Arch
	}
	if vm != nil && vm.Hypervisor() != nil {
		return vm.Hypervisor().Architecture()
	}
	return hv.ArchitectureInvalid
}

func (t InputTemplate) irqLineForArch(arch hv.CpuArchitecture) uint32 {
	if t.IRQLine != 0 {
		return t.IRQLine
	}
	if t.Type == InputTypeKeyboard {
		if arch == hv.ArchitectureARM64 {
			return armInputKeyboardDefaultIRQ
		}
		return InputKeyboardDefaultIRQLine
	}
	if arch == hv.ArchitectureARM64 {
		return armInputTabletDefaultIRQ
	}
	return InputTabletDefaultIRQLine
}

func (t InputTemplate) mmioBase() uint64 {
	if t.MMIOBase != 0 {
		return t.MMIOBase
	}
	if t.Type == InputTypeKeyboard {
		return InputKeyboardDefaultMMIOBase
	}
	return InputTabletDefaultMMIOBase
}

func (t InputTemplate) GetLinuxCommandLineParam() ([]string, error) {
	irqLine := t.irqLineForArch(t.Arch)
	base := t.mmioBase()
	param := fmt.Sprintf(
		"virtio_mmio.device=4k@0x%x:%d",
		base,
		irqLine,
	)
	return []string{param}, nil
}

func (t InputTemplate) DeviceTreeNodes() ([]fdt.Node, error) {
	irqLine := t.irqLineForArch(t.Arch)
	base := t.mmioBase()
	node := fdt.Node{
		Name: fmt.Sprintf("virtio@%x", base),
		Properties: map[string]fdt.Property{
			"compatible": {Strings: []string{"virtio,mmio"}},
			"reg":        {U64: []uint64{base, InputDefaultMMIOSize}},
			"interrupts": {U32: []uint32{0, irqLine, 4}},
			"status":     {Strings: []string{"okay"}},
		},
	}
	return []fdt.Node{node}, nil
}

func (t InputTemplate) GetACPIDeviceInfo() ACPIDeviceInfo {
	irqLine := t.irqLineForArch(t.archOrDefault(nil))
	return ACPIDeviceInfo{
		BaseAddr: t.mmioBase(),
		Size:     InputDefaultMMIOSize,
		GSI:      irqLine,
	}
}

func (t InputTemplate) Create(vm hv.VirtualMachine) (hv.Device, error) {
	arch := t.archOrDefault(vm)
	irqLine := t.irqLineForArch(arch)
	encodedLine := EncodeIRQLineForArch(arch, irqLine)
	base := t.mmioBase()

	name := t.Name
	if name == "" {
		if t.Type == InputTypeKeyboard {
			name = "Virtio Keyboard"
		} else {
			name = "Virtio Tablet"
		}
	}

	input := &Input{
		base:      base,
		size:      InputDefaultMMIOSize,
		irqLine:   encodedLine,
		inputType: t.Type,
		name:      name,
	}
	if err := input.Init(vm); err != nil {
		return nil, fmt.Errorf("virtio-input: initialize device: %w", err)
	}
	return input, nil
}

var (
	_ hv.DeviceTemplate = InputTemplate{}
	_ VirtioMMIODevice  = InputTemplate{}
)

// Input is a Virtio-Input device.
type Input struct {
	device    device
	base      uint64
	size      uint64
	irqLine   uint32
	inputType InputType
	name      string
	arch      hv.CpuArchitecture

	mu           sync.Mutex
	configSelect uint8
	configSubsel uint8

	// Pending events to be delivered
	pendingEvents []inputEvent

	// Track available buffers from the guest
	availBuffers []inputBuffer
}

type inputEvent struct {
	evType uint16
	code   uint16
	value  int32
}

type inputBuffer struct {
	addr   uint64
	length uint32
	head   uint16
}

func (i *Input) Init(vm hv.VirtualMachine) error {
	if i.device == nil {
		if vm == nil {
			return fmt.Errorf("virtio-input: virtual machine is nil")
		}
		i.setupDevice(vm)
		return nil
	}
	if mmio, ok := i.device.(*mmioDevice); ok && vm != nil {
		mmio.vm = vm
	}
	return nil
}

func (i *Input) setupDevice(vm hv.VirtualMachine) {
	if vm != nil && vm.Hypervisor() != nil {
		i.arch = vm.Hypervisor().Architecture()
	}
	i.device = newMMIODevice(vm, i.base, i.size, i.irqLine, inputDeviceID, inputVendorID, inputVersion, []uint64{virtioFeatureVersion1}, i)
	if mmio, ok := i.device.(*mmioDevice); ok && vm != nil {
		mmio.vm = vm
	}
}

func (i *Input) MMIORegions() []hv.MMIORegion {
	if i.size == 0 {
		return nil
	}
	return []hv.MMIORegion{{
		Address: i.base,
		Size:    i.size,
	}}
}

func (i *Input) ReadMMIO(addr uint64, data []byte) error {
	dev, err := i.requireDevice()
	if err != nil {
		return err
	}
	return dev.readMMIO(addr, data)
}

func (i *Input) WriteMMIO(addr uint64, data []byte) error {
	dev, err := i.requireDevice()
	if err != nil {
		return err
	}
	return dev.writeMMIO(addr, data)
}

func (i *Input) requireDevice() (device, error) {
	if i.device == nil {
		return nil, fmt.Errorf("virtio-input: device not initialized")
	}
	return i.device, nil
}

// NumQueues implements deviceHandler.
func (i *Input) NumQueues() int {
	return inputQueueCount
}

// QueueMaxSize implements deviceHandler.
func (i *Input) QueueMaxSize(queue int) uint16 {
	return inputQueueNumMax
}

// OnReset implements deviceHandler.
func (i *Input) OnReset(dev device) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.pendingEvents = nil
	i.availBuffers = nil
	i.configSelect = 0
	i.configSubsel = 0
}

// OnQueueNotify implements deviceHandler.
func (i *Input) OnQueueNotify(dev device, queueIdx int) error {
	switch queueIdx {
	case inputQueueEvent:
		return i.processEventQueue(dev, dev.queue(queueIdx))
	case inputQueueStatus:
		// Status queue - ignore for now (used for LED/force-feedback)
		return i.consumeStatusQueue(dev, dev.queue(queueIdx))
	}
	return nil
}

// ReadConfig implements deviceHandler.
func (i *Input) ReadConfig(dev device, offset uint64) (uint32, bool, error) {
	// The offset may be either absolute (0x100+) or relative (0-255)
	// depending on whether we're called directly or via deviceHandlerAdapter.
	rel := offset
	if offset >= VIRTIO_MMIO_CONFIG {
		rel = offset - VIRTIO_MMIO_CONFIG
	}
	cfg := i.configBytes()
	if int(rel) >= len(cfg) {
		return 0, true, nil
	}
	var buf [4]byte
	copy(buf[:], cfg[rel:])
	return littleEndianValue(buf[:], 4), true, nil
}

// WriteConfig implements deviceHandler.
func (i *Input) WriteConfig(dev device, offset uint64, value uint32) (bool, error) {
	// The offset may be either absolute (0x100+) or relative (0-255)
	// depending on whether we're called directly or via deviceHandlerAdapter.
	rel := offset
	if offset >= VIRTIO_MMIO_CONFIG {
		rel = offset - VIRTIO_MMIO_CONFIG
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	// struct virtio_input_config {
	//     u8    select;      // offset 0
	//     u8    subsel;      // offset 1
	//     u8    size;        // offset 2 (read-only)
	//     u8    reserved[5]; // offset 3-7
	//     union { ... };     // offset 8+
	// }
	switch rel {
	case 0:
		i.configSelect = uint8(value)
		return true, nil
	case 1:
		i.configSubsel = uint8(value)
		return true, nil
	}
	return true, nil
}

func (i *Input) configBytes() []byte {
	i.mu.Lock()
	defer i.mu.Unlock()

	// Config structure is 256 bytes
	var buf [256]byte
	buf[0] = i.configSelect
	buf[1] = i.configSubsel

	// Size is at offset 2, data starts at offset 8
	var data []byte

	switch i.configSelect {
	case VIRTIO_INPUT_CFG_ID_NAME:
		data = []byte(i.name)
		if len(data) > 128 {
			data = data[:128]
		}

	case VIRTIO_INPUT_CFG_ID_SERIAL:
		data = []byte("virtio-input-0")

	case VIRTIO_INPUT_CFG_ID_DEVIDS:
		// struct virtio_input_devids { u16 bustype, vendor, product, version; }
		data = make([]byte, 8)
		binary.LittleEndian.PutUint16(data[0:2], 0x06)   // BUS_VIRTUAL
		binary.LittleEndian.PutUint16(data[2:4], 0x1AF4) // Red Hat virtio
		if i.inputType == InputTypeKeyboard {
			binary.LittleEndian.PutUint16(data[4:6], 0x0001)
		} else {
			binary.LittleEndian.PutUint16(data[4:6], 0x0002)
		}
		binary.LittleEndian.PutUint16(data[6:8], 0x0001)

	case VIRTIO_INPUT_CFG_PROP_BITS:
		// No special properties
		data = nil

	case VIRTIO_INPUT_CFG_EV_BITS:
		data = i.getEventBits(i.configSubsel)

	case VIRTIO_INPUT_CFG_ABS_INFO:
		data = i.getAbsInfo(i.configSubsel)
	}

	// Set size at offset 2
	if len(data) > 128 {
		data = data[:128]
	}
	buf[2] = uint8(len(data))

	// Copy data starting at offset 8
	copy(buf[8:], data)

	return buf[:]
}

func (i *Input) getEventBits(subsel uint8) []byte {
	// Return bitmap of supported event codes for the given event type
	switch subsel {
	case EV_KEY:
		if i.inputType == InputTypeKeyboard {
			// Return bitmap for all keyboard keys
			return i.keyBitmap(AllKeyboardKeys())
		}
		// Tablet: support mouse buttons
		return i.keyBitmap(AllTabletButtons())

	case EV_ABS:
		if i.inputType == InputTypeTablet {
			// ABS_X and ABS_Y
			return i.absBitmap([]uint16{ABS_X, ABS_Y})
		}
		return nil

	case EV_SYN:
		// SYN events are always supported
		return []byte{0x01} // SYN_REPORT

	case EV_REL:
		// No relative events for tablet mode
		return nil
	}

	return nil
}

func (i *Input) keyBitmap(keys []uint16) []byte {
	// Find max key code
	maxKey := uint16(0)
	for _, k := range keys {
		if k > maxKey {
			maxKey = k
		}
	}

	// Create bitmap
	numBytes := (maxKey / 8) + 1
	if numBytes > 128 {
		numBytes = 128
	}
	bitmap := make([]byte, numBytes)

	for _, k := range keys {
		if int(k/8) < len(bitmap) {
			bitmap[k/8] |= 1 << (k % 8)
		}
	}

	return bitmap
}

func (i *Input) absBitmap(axes []uint16) []byte {
	maxAxis := uint16(0)
	for _, a := range axes {
		if a > maxAxis {
			maxAxis = a
		}
	}

	numBytes := (maxAxis / 8) + 1
	if numBytes > 128 {
		numBytes = 128
	}
	bitmap := make([]byte, numBytes)

	for _, a := range axes {
		if int(a/8) < len(bitmap) {
			bitmap[a/8] |= 1 << (a % 8)
		}
	}

	return bitmap
}

func (i *Input) getAbsInfo(subsel uint8) []byte {
	if i.inputType != InputTypeTablet {
		return nil
	}

	// struct virtio_input_absinfo {
	//     u32 min;
	//     u32 max;
	//     u32 fuzz;
	//     u32 flat;
	//     u32 res;
	// }
	switch subsel {
	case ABS_X, ABS_Y:
		data := make([]byte, 20)
		binary.LittleEndian.PutUint32(data[0:4], 0)             // min
		binary.LittleEndian.PutUint32(data[4:8], TabletAxisMax) // max
		binary.LittleEndian.PutUint32(data[8:12], 0)            // fuzz
		binary.LittleEndian.PutUint32(data[12:16], 0)           // flat
		binary.LittleEndian.PutUint32(data[16:20], 0)           // res
		return data
	}

	return nil
}

func (i *Input) processEventQueue(dev device, q *queue) error {
	if q == nil || !q.ready || q.size == 0 {
		return nil
	}

	_, availIdx, err := dev.readAvailState(q)
	if err != nil {
		return err
	}

	// Collect available buffers from the guest
	i.mu.Lock()
	for q.lastAvailIdx != availIdx {
		ringIndex := q.lastAvailIdx % q.size
		head, err := dev.readAvailEntry(q, ringIndex)
		if err != nil {
			i.mu.Unlock()
			return err
		}

		desc, err := dev.readDescriptor(q, head)
		if err != nil {
			i.mu.Unlock()
			return err
		}

		if desc.flags&virtqDescFWrite != 0 {
			i.availBuffers = append(i.availBuffers, inputBuffer{
				addr:   desc.addr,
				length: desc.length,
				head:   head,
			})
		}

		q.lastAvailIdx++
	}
	i.mu.Unlock()

	// Try to deliver any pending events
	return i.deliverPendingEvents(dev, q)
}

func (i *Input) consumeStatusQueue(dev device, q *queue) error {
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

		// Just acknowledge without processing
		if err := dev.recordUsedElement(q, head, 0); err != nil {
			return err
		}
		q.lastAvailIdx++
		interruptNeeded = true
	}

	if interruptNeeded && (availFlags&1) == 0 {
		dev.raiseInterrupt(inputInterruptBit)
	}

	return nil
}

func (i *Input) deliverPendingEvents(dev device, q *queue) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	if len(i.pendingEvents) == 0 || len(i.availBuffers) == 0 {
		return nil
	}

	var interruptNeeded bool

	for len(i.pendingEvents) > 0 && len(i.availBuffers) > 0 {
		ev := i.pendingEvents[0]
		buf := i.availBuffers[0]

		// struct virtio_input_event {
		//     le16 type;
		//     le16 code;
		//     le32 value;
		// }
		eventData := make([]byte, 8)
		binary.LittleEndian.PutUint16(eventData[0:2], ev.evType)
		binary.LittleEndian.PutUint16(eventData[2:4], ev.code)
		binary.LittleEndian.PutUint32(eventData[4:8], uint32(ev.value))

		if buf.length < uint32(len(eventData)) {
			slog.Error("virtio-input: event buffer too small", "len", buf.length)
			if err := dev.recordUsedElement(q, buf.head, 0); err != nil {
				return err
			}
			i.availBuffers = i.availBuffers[1:]
			interruptNeeded = true
			continue
		}

		if err := dev.writeGuest(buf.addr, eventData); err != nil {
			slog.Error("virtio-input: failed to write event", "err", err)
			return err
		}

		if err := dev.recordUsedElement(q, buf.head, uint32(len(eventData))); err != nil {
			return err
		}

		i.pendingEvents = i.pendingEvents[1:]
		i.availBuffers = i.availBuffers[1:]
		interruptNeeded = true
	}

	if interruptNeeded {
		dev.raiseInterrupt(inputInterruptBit)
	}

	return nil
}

// InjectKeyEvent injects a key press/release event
func (i *Input) InjectKeyEvent(code uint16, pressed bool) {
	value := int32(0)
	if pressed {
		value = 1
	}

	i.mu.Lock()
	i.pendingEvents = append(i.pendingEvents, inputEvent{
		evType: EV_KEY,
		code:   code,
		value:  value,
	})
	// Add SYN_REPORT
	i.pendingEvents = append(i.pendingEvents, inputEvent{
		evType: EV_SYN,
		code:   SYN_REPORT,
		value:  0,
	})
	i.mu.Unlock()

	// Try to deliver
	if i.device != nil {
		q := i.device.queue(inputQueueEvent)
		if q != nil {
			_ = i.deliverPendingEvents(i.device, q)
		}
	}
}

// InjectAbsEvent injects an absolute axis event (for tablet)
func (i *Input) InjectAbsEvent(axis uint16, value int32) {
	i.mu.Lock()
	i.pendingEvents = append(i.pendingEvents, inputEvent{
		evType: EV_ABS,
		code:   axis,
		value:  value,
	})
	i.mu.Unlock()
}

// InjectButtonEvent injects a button press/release event
func (i *Input) InjectButtonEvent(code uint16, pressed bool) {
	value := int32(0)
	if pressed {
		value = 1
	}

	i.mu.Lock()
	i.pendingEvents = append(i.pendingEvents, inputEvent{
		evType: EV_KEY,
		code:   code,
		value:  value,
	})
	i.mu.Unlock()
}

// InjectSynReport injects a SYN_REPORT event
func (i *Input) InjectSynReport() {
	i.mu.Lock()
	i.pendingEvents = append(i.pendingEvents, inputEvent{
		evType: EV_SYN,
		code:   SYN_REPORT,
		value:  0,
	})
	i.mu.Unlock()

	// Try to deliver
	if i.device != nil {
		q := i.device.queue(inputQueueEvent)
		if q != nil {
			_ = i.deliverPendingEvents(i.device, q)
		}
	}
}

// InjectMouseMove injects a mouse movement event (for tablet, with absolute coords)
func (i *Input) InjectMouseMove(x, y int32) {
	i.InjectAbsEvent(ABS_X, x)
	i.InjectAbsEvent(ABS_Y, y)
	i.InjectSynReport()
}

var (
	_ hv.MemoryMappedIODevice = (*Input)(nil)
	_ deviceHandler           = (*Input)(nil)
)
