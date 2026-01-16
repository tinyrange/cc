package virtio

import (
	"fmt"
	"log/slog"
	"sync"

	"github.com/tinyrange/cc/internal/fdt"
	"github.com/tinyrange/cc/internal/hv"
)

const (
	GPUDefaultMMIOBase = 0xd0005000
	GPUDefaultMMIOSize = 0x200
	GPUDefaultIRQLine  = 12
	armGPUDefaultIRQ   = 42

	gpuQueueCount      = 2
	gpuQueueNumMax     = 256
	gpuVendorID        = 0x554d4551 // "QEMU"
	gpuVersion         = 2
	gpuInterruptBit    = 0x1
	gpuConfigInterrupt = 0x2

	gpuQueueControl = 0
	gpuQueueCursor  = 1
)

// GPUTemplate is the device template for creating a Virtio-GPU device.
type GPUTemplate struct {
	Arch    hv.CpuArchitecture
	IRQLine uint32
	Width   uint32
	Height  uint32
}

func (t GPUTemplate) archOrDefault(vm hv.VirtualMachine) hv.CpuArchitecture {
	if t.Arch != "" && t.Arch != hv.ArchitectureInvalid {
		return t.Arch
	}
	if vm != nil && vm.Hypervisor() != nil {
		return vm.Hypervisor().Architecture()
	}
	return hv.ArchitectureInvalid
}

func (t GPUTemplate) irqLineForArch(arch hv.CpuArchitecture) uint32 {
	if t.IRQLine != 0 {
		return t.IRQLine
	}
	if arch == hv.ArchitectureARM64 {
		return armGPUDefaultIRQ
	}
	return GPUDefaultIRQLine
}

func (t GPUTemplate) GetLinuxCommandLineParam() ([]string, error) {
	irqLine := t.irqLineForArch(t.Arch)
	param := fmt.Sprintf(
		"virtio_mmio.device=4k@0x%x:%d",
		GPUDefaultMMIOBase,
		irqLine,
	)
	return []string{param}, nil
}

func (t GPUTemplate) DeviceTreeNodes() ([]fdt.Node, error) {
	irqLine := t.irqLineForArch(t.Arch)
	node := fdt.Node{
		Name: fmt.Sprintf("virtio@%x", GPUDefaultMMIOBase),
		Properties: map[string]fdt.Property{
			"compatible": {Strings: []string{"virtio,mmio"}},
			"reg":        {U64: []uint64{GPUDefaultMMIOBase, GPUDefaultMMIOSize}},
			"interrupts": {U32: []uint32{0, irqLine, 4}},
			"status":     {Strings: []string{"okay"}},
		},
	}
	return []fdt.Node{node}, nil
}

func (t GPUTemplate) GetACPIDeviceInfo() ACPIDeviceInfo {
	irqLine := t.irqLineForArch(t.archOrDefault(nil))
	return ACPIDeviceInfo{
		BaseAddr: GPUDefaultMMIOBase,
		Size:     GPUDefaultMMIOSize,
		GSI:      irqLine,
	}
}

func (t GPUTemplate) Create(vm hv.VirtualMachine) (hv.Device, error) {
	arch := t.archOrDefault(vm)
	irqLine := t.irqLineForArch(arch)
	encodedLine := EncodeIRQLineForArch(arch, irqLine)

	width := t.Width
	if width == 0 {
		width = 1024
	}
	height := t.Height
	if height == 0 {
		height = 768
	}

	// Allocate MMIO region dynamically
	mmioBase := uint64(GPUDefaultMMIOBase)
	if vm != nil {
		alloc, err := vm.AllocateMMIO(hv.MMIOAllocationRequest{
			Name:      "virtio-gpu",
			Size:      GPUDefaultMMIOSize,
			Alignment: 0x1000,
		})
		if err != nil {
			return nil, fmt.Errorf("virtio-gpu: allocate MMIO: %w", err)
		}
		mmioBase = alloc.Base
	}

	gpu := &GPU{
		base:    mmioBase,
		size:    GPUDefaultMMIOSize,
		irqLine: encodedLine,
		width:   width,
		height:  height,
	}
	if err := gpu.Init(vm); err != nil {
		return nil, fmt.Errorf("virtio-gpu: initialize device: %w", err)
	}
	return gpu, nil
}

var (
	_ hv.DeviceTemplate = GPUTemplate{}
	_ VirtioMMIODevice  = GPUTemplate{}
)

// GPU is a Virtio-GPU 2D device.
type GPU struct {
	device  device
	base    uint64
	size    uint64
	irqLine uint32
	width   uint32
	height  uint32
	arch    hv.CpuArchitecture

	mu        sync.Mutex
	resources map[uint32]*gpuResource2D
	scanouts  [VIRTIO_GPU_MAX_SCANOUTS]*gpuScanout

	// Callback for when framebuffer is flushed
	OnFlush func(resourceID uint32, x, y, w, h uint32, pixels []byte, stride uint32)
}

type gpuScanout struct {
	resourceID uint32
	x, y       uint32
	width      uint32
	height     uint32
	enabled    bool
}

func (g *GPU) Init(vm hv.VirtualMachine) error {
	if g.device == nil {
		if vm == nil {
			return fmt.Errorf("virtio-gpu: virtual machine is nil")
		}
		g.setupDevice(vm)
		return nil
	}
	if mmio, ok := g.device.(*mmioDevice); ok && vm != nil {
		mmio.vm = vm
	}
	return nil
}

func (g *GPU) setupDevice(vm hv.VirtualMachine) {
	if vm != nil && vm.Hypervisor() != nil {
		g.arch = vm.Hypervisor().Architecture()
	}
	g.resources = make(map[uint32]*gpuResource2D)
	g.device = newMMIODevice(vm, g.base, g.size, g.irqLine, gpuDeviceID, gpuVendorID, gpuVersion, []uint64{virtioFeatureVersion1}, g)
	if mmio, ok := g.device.(*mmioDevice); ok && vm != nil {
		mmio.vm = vm
	}
}

func (g *GPU) MMIORegions() []hv.MMIORegion {
	if g.size == 0 {
		return nil
	}
	return []hv.MMIORegion{{
		Address: g.base,
		Size:    g.size,
	}}
}

func (g *GPU) ReadMMIO(ctx hv.ExitContext, addr uint64, data []byte) error {
	dev, err := g.requireDevice()
	if err != nil {
		return err
	}
	return dev.readMMIO(ctx, addr, data)
}

func (g *GPU) WriteMMIO(ctx hv.ExitContext, addr uint64, data []byte) error {
	dev, err := g.requireDevice()
	if err != nil {
		return err
	}
	return dev.writeMMIO(ctx, addr, data)
}

func (g *GPU) requireDevice() (device, error) {
	if g.device == nil {
		return nil, fmt.Errorf("virtio-gpu: device not initialized")
	}
	return g.device, nil
}

// NumQueues implements deviceHandler.
func (g *GPU) NumQueues() int {
	return gpuQueueCount
}

// QueueMaxSize implements deviceHandler.
func (g *GPU) QueueMaxSize(queue int) uint16 {
	return gpuQueueNumMax
}

// OnReset implements deviceHandler.
func (g *GPU) OnReset(dev device) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.resources = make(map[uint32]*gpuResource2D)
	for i := range g.scanouts {
		g.scanouts[i] = nil
	}
}

// OnQueueNotify implements deviceHandler.
func (g *GPU) OnQueueNotify(ctx hv.ExitContext, dev device, queueIdx int) error {
	switch queueIdx {
	case gpuQueueControl:
		return g.processControlQueue(dev, dev.queue(queueIdx))
	case gpuQueueCursor:
		return g.processCursorQueue(dev, dev.queue(queueIdx))
	}
	return nil
}

// ReadConfig implements deviceHandler.
func (g *GPU) ReadConfig(ctx hv.ExitContext, dev device, offset uint64) (uint32, bool, error) {
	// The offset may be either absolute (0x100+) or relative (0-15)
	// depending on whether we're called directly or via deviceHandlerAdapter.
	rel := offset
	if offset >= VIRTIO_MMIO_CONFIG {
		rel = offset - VIRTIO_MMIO_CONFIG
	}
	cfg := g.configBytes()
	if int(rel) >= len(cfg) {
		return 0, true, nil
	}
	var buf [4]byte
	copy(buf[:], cfg[rel:])
	return littleEndianValue(buf[:], 4), true, nil
}

// WriteConfig implements deviceHandler.
func (g *GPU) WriteConfig(ctx hv.ExitContext, dev device, offset uint64, value uint32) (bool, error) {
	if offset < VIRTIO_MMIO_CONFIG {
		return false, nil
	}
	// GPU config is read-only
	return true, nil
}

func (g *GPU) configBytes() []byte {
	// struct virtio_gpu_config {
	//     __le32 events_read;
	//     __le32 events_clear;
	//     __le32 num_scanouts;
	//     __le32 num_capsets;
	// }
	var buf [16]byte
	// events_read: 0
	// events_clear: 0
	// num_scanouts: 1
	buf[8] = 1
	// num_capsets: 0
	return buf[:]
}

func (g *GPU) processControlQueue(dev device, q *queue) error {
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

		written, err := g.processControlCommand(dev, q, head)
		if err != nil {
			slog.Error("virtio-gpu: control command error", "err", err)
		}

		if err := dev.recordUsedElement(q, head, written); err != nil {
			return err
		}
		q.lastAvailIdx++
		interruptNeeded = true
	}

	if interruptNeeded && (availFlags&1) == 0 {
		dev.raiseInterrupt(gpuInterruptBit)
	}

	return nil
}

func (g *GPU) processControlCommand(dev device, q *queue, head uint16) (uint32, error) {
	// Read the descriptor chain to get command and response buffers
	var cmdBuf []byte
	var respDesc *virtqDescriptor

	index := head
	for i := uint16(0); i < q.size; i++ {
		desc, err := dev.readDescriptor(q, index)
		if err != nil {
			return 0, err
		}

		if desc.flags&virtqDescFWrite == 0 {
			// Read-only descriptor: command data
			data, err := dev.readGuest(desc.addr, desc.length)
			if err != nil {
				return 0, err
			}
			cmdBuf = append(cmdBuf, data...)
		} else {
			// Writable descriptor: response buffer
			respDesc = &desc
		}

		if desc.flags&virtqDescFNext == 0 {
			break
		}
		index = desc.next
	}

	if len(cmdBuf) < virtioGPUCtrlHdrSize {
		return 0, fmt.Errorf("command buffer too small")
	}

	hdr := parseCtrlHdr(cmdBuf)
	var resp []byte

	switch hdr.Type {
	case VIRTIO_GPU_CMD_GET_DISPLAY_INFO:
		resp = g.handleGetDisplayInfo(hdr)
	case VIRTIO_GPU_CMD_RESOURCE_CREATE_2D:
		resp = g.handleResourceCreate2D(cmdBuf)
	case VIRTIO_GPU_CMD_RESOURCE_UNREF:
		resp = g.handleResourceUnref(cmdBuf)
	case VIRTIO_GPU_CMD_SET_SCANOUT:
		resp = g.handleSetScanout(cmdBuf)
	case VIRTIO_GPU_CMD_RESOURCE_FLUSH:
		resp = g.handleResourceFlush(dev, cmdBuf)
	case VIRTIO_GPU_CMD_TRANSFER_TO_HOST_2D:
		resp = g.handleTransferToHost2D(dev, cmdBuf)
	case VIRTIO_GPU_CMD_RESOURCE_ATTACH_BACKING:
		resp = g.handleResourceAttachBacking(dev, cmdBuf)
	case VIRTIO_GPU_CMD_RESOURCE_DETACH_BACKING:
		resp = g.handleResourceDetachBacking(cmdBuf)
	default:
		slog.Warn("virtio-gpu: unknown command", "type", fmt.Sprintf("0x%x", hdr.Type))
		resp = g.makeErrorResponse(hdr, VIRTIO_GPU_RESP_ERR_UNSPEC)
	}

	// Write response
	written := uint32(0)
	if respDesc != nil && len(resp) > 0 {
		toWrite := resp
		if uint32(len(toWrite)) > respDesc.length {
			toWrite = toWrite[:respDesc.length]
		}
		if err := dev.writeGuest(respDesc.addr, toWrite); err != nil {
			return 0, err
		}
		written = uint32(len(toWrite))
	}

	return written, nil
}

func (g *GPU) handleGetDisplayInfo(hdr virtioGPUCtrlHdr) []byte {
	g.mu.Lock()
	defer g.mu.Unlock()

	resp := virtioGPURespDisplayInfo{}
	resp.Hdr.Type = VIRTIO_GPU_RESP_OK_DISPLAY_INFO
	resp.Hdr.Flags = hdr.Flags
	resp.Hdr.FenceID = hdr.FenceID

	// Set up primary display
	resp.PModes[0].R.X = 0
	resp.PModes[0].R.Y = 0
	resp.PModes[0].R.Width = g.width
	resp.PModes[0].R.Height = g.height
	resp.PModes[0].Enabled = 1

	buf := make([]byte, virtioGPURespDisplayInfoSize)
	resp.encode(buf)
	return buf
}

func (g *GPU) handleResourceCreate2D(cmdBuf []byte) []byte {
	cmd := parseResourceCreate2D(cmdBuf)

	g.mu.Lock()
	defer g.mu.Unlock()

	if _, exists := g.resources[cmd.ResourceID]; exists {
		return g.makeErrorResponse(cmd.Hdr, VIRTIO_GPU_RESP_ERR_INVALID_RESOURCE_ID)
	}

	bpp := bytesPerPixel(cmd.Format)
	pixelSize := cmd.Width * cmd.Height * bpp

	g.resources[cmd.ResourceID] = &gpuResource2D{
		id:     cmd.ResourceID,
		format: cmd.Format,
		width:  cmd.Width,
		height: cmd.Height,
		pixels: make([]byte, pixelSize),
	}

	return g.makeOKResponse(cmd.Hdr)
}

func (g *GPU) handleResourceUnref(cmdBuf []byte) []byte {
	cmd := parseResourceUnref(cmdBuf)

	g.mu.Lock()
	defer g.mu.Unlock()

	if _, exists := g.resources[cmd.ResourceID]; !exists {
		return g.makeErrorResponse(cmd.Hdr, VIRTIO_GPU_RESP_ERR_INVALID_RESOURCE_ID)
	}

	delete(g.resources, cmd.ResourceID)
	return g.makeOKResponse(cmd.Hdr)
}

func (g *GPU) handleSetScanout(cmdBuf []byte) []byte {
	cmd := parseSetScanout(cmdBuf)

	g.mu.Lock()
	defer g.mu.Unlock()

	if cmd.ScanoutID >= VIRTIO_GPU_MAX_SCANOUTS {
		return g.makeErrorResponse(cmd.Hdr, VIRTIO_GPU_RESP_ERR_INVALID_SCANOUT_ID)
	}

	if cmd.ResourceID == 0 {
		// Disable scanout
		g.scanouts[cmd.ScanoutID] = nil
	} else {
		if _, exists := g.resources[cmd.ResourceID]; !exists {
			return g.makeErrorResponse(cmd.Hdr, VIRTIO_GPU_RESP_ERR_INVALID_RESOURCE_ID)
		}

		g.scanouts[cmd.ScanoutID] = &gpuScanout{
			resourceID: cmd.ResourceID,
			x:          cmd.R.X,
			y:          cmd.R.Y,
			width:      cmd.R.Width,
			height:     cmd.R.Height,
			enabled:    true,
		}
	}

	return g.makeOKResponse(cmd.Hdr)
}

func (g *GPU) handleResourceFlush(dev device, cmdBuf []byte) []byte {
	cmd := parseResourceFlush(cmdBuf)

	g.mu.Lock()
	resource, exists := g.resources[cmd.ResourceID]
	if !exists {
		g.mu.Unlock()
		return g.makeErrorResponse(cmd.Hdr, VIRTIO_GPU_RESP_ERR_INVALID_RESOURCE_ID)
	}

	// Make a copy of the pixels for the callback
	pixels := make([]byte, len(resource.pixels))
	copy(pixels, resource.pixels)
	width := resource.width
	format := resource.format
	g.mu.Unlock()

	// Call the flush callback if set
	if g.OnFlush != nil {
		stride := width * bytesPerPixel(format)
		g.OnFlush(cmd.ResourceID, cmd.R.X, cmd.R.Y, cmd.R.Width, cmd.R.Height, pixels, stride)
	}

	return g.makeOKResponse(cmd.Hdr)
}

func (g *GPU) handleTransferToHost2D(dev device, cmdBuf []byte) []byte {
	cmd := parseTransferToHost2D(cmdBuf)

	g.mu.Lock()
	defer g.mu.Unlock()

	resource, exists := g.resources[cmd.ResourceID]
	if !exists {
		return g.makeErrorResponse(cmd.Hdr, VIRTIO_GPU_RESP_ERR_INVALID_RESOURCE_ID)
	}

	if len(resource.backing) == 0 {
		return g.makeErrorResponse(cmd.Hdr, VIRTIO_GPU_RESP_ERR_UNSPEC)
	}

	// Copy pixels from guest backing memory to host buffer
	bpp := bytesPerPixel(resource.format)
	stride := resource.width * bpp

	// Calculate the region to transfer
	srcOffset := cmd.Offset
	dstY := cmd.R.Y
	dstX := cmd.R.X
	copyWidth := cmd.R.Width
	copyHeight := cmd.R.Height

	// Clamp to resource bounds
	if dstX+copyWidth > resource.width {
		copyWidth = resource.width - dstX
	}
	if dstY+copyHeight > resource.height {
		copyHeight = resource.height - dstY
	}

	// Build a flat view of the backing memory
	var backingData []byte
	for _, entry := range resource.backing {
		data, err := dev.readGuest(entry.Addr, entry.Length)
		if err != nil {
			slog.Error("virtio-gpu: failed to read backing memory", "err", err)
			return g.makeErrorResponse(cmd.Hdr, VIRTIO_GPU_RESP_ERR_UNSPEC)
		}
		backingData = append(backingData, data...)
	}

	// Copy the rectangle from backing to host pixels
	for y := uint32(0); y < copyHeight; y++ {
		srcRowOffset := srcOffset + uint64(y)*uint64(stride) + uint64(dstX)*uint64(bpp)
		dstRowOffset := (dstY+y)*stride + dstX*bpp
		copyLen := copyWidth * bpp

		if srcRowOffset+uint64(copyLen) > uint64(len(backingData)) {
			break
		}
		if dstRowOffset+copyLen > uint32(len(resource.pixels)) {
			break
		}

		copy(resource.pixels[dstRowOffset:dstRowOffset+copyLen], backingData[srcRowOffset:srcRowOffset+uint64(copyLen)])
	}

	return g.makeOKResponse(cmd.Hdr)
}

func (g *GPU) handleResourceAttachBacking(dev device, cmdBuf []byte) []byte {
	cmd := parseResourceAttachBacking(cmdBuf)

	g.mu.Lock()
	defer g.mu.Unlock()

	resource, exists := g.resources[cmd.ResourceID]
	if !exists {
		return g.makeErrorResponse(cmd.Hdr, VIRTIO_GPU_RESP_ERR_INVALID_RESOURCE_ID)
	}

	// Parse memory entries following the header
	entries := make([]virtioGPUMemEntry, cmd.NrEntries)
	entryOffset := 32 // After header + resourceID + nrEntries
	for i := uint32(0); i < cmd.NrEntries; i++ {
		if entryOffset+virtioGPUMemEntrySize > len(cmdBuf) {
			return g.makeErrorResponse(cmd.Hdr, VIRTIO_GPU_RESP_ERR_INVALID_PARAMETER)
		}
		entries[i] = parseMemEntry(cmdBuf[entryOffset:])
		entryOffset += virtioGPUMemEntrySize
	}

	resource.backing = entries
	return g.makeOKResponse(cmd.Hdr)
}

func (g *GPU) handleResourceDetachBacking(cmdBuf []byte) []byte {
	cmd := parseResourceDetachBacking(cmdBuf)

	g.mu.Lock()
	defer g.mu.Unlock()

	resource, exists := g.resources[cmd.ResourceID]
	if !exists {
		return g.makeErrorResponse(cmd.Hdr, VIRTIO_GPU_RESP_ERR_INVALID_RESOURCE_ID)
	}

	resource.backing = nil
	return g.makeOKResponse(cmd.Hdr)
}

func (g *GPU) processCursorQueue(dev device, q *queue) error {
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

		// For now, just acknowledge cursor commands without processing
		if err := dev.recordUsedElement(q, head, 0); err != nil {
			return err
		}
		q.lastAvailIdx++
		interruptNeeded = true
	}

	if interruptNeeded && (availFlags&1) == 0 {
		dev.raiseInterrupt(gpuInterruptBit)
	}

	return nil
}

func (g *GPU) makeOKResponse(hdr virtioGPUCtrlHdr) []byte {
	resp := virtioGPUCtrlHdr{
		Type:    VIRTIO_GPU_RESP_OK_NODATA,
		Flags:   hdr.Flags,
		FenceID: hdr.FenceID,
		CtxID:   hdr.CtxID,
	}
	buf := make([]byte, virtioGPUCtrlHdrSize)
	resp.encode(buf)
	return buf
}

func (g *GPU) makeErrorResponse(hdr virtioGPUCtrlHdr, errType uint32) []byte {
	resp := virtioGPUCtrlHdr{
		Type:    errType,
		Flags:   hdr.Flags,
		FenceID: hdr.FenceID,
		CtxID:   hdr.CtxID,
	}
	buf := make([]byte, virtioGPUCtrlHdrSize)
	resp.encode(buf)
	return buf
}

// SetDisplaySize updates the display size and triggers a config change interrupt
func (g *GPU) SetDisplaySize(width, height uint32) {
	g.mu.Lock()
	g.width = width
	g.height = height
	g.mu.Unlock()

	if g.device != nil {
		g.device.raiseInterrupt(gpuConfigInterrupt)
	}
}

// GetFramebuffer returns the current framebuffer pixels for the primary scanout
func (g *GPU) GetFramebuffer() (pixels []byte, width, height, format uint32, ok bool) {
	g.mu.Lock()
	defer g.mu.Unlock()

	scanout := g.scanouts[0]
	if scanout == nil || !scanout.enabled {
		return nil, 0, 0, 0, false
	}

	resource, exists := g.resources[scanout.resourceID]
	if !exists {
		return nil, 0, 0, 0, false
	}

	// Return a copy of the pixels
	pixels = make([]byte, len(resource.pixels))
	copy(pixels, resource.pixels)
	return pixels, resource.width, resource.height, resource.format, true
}

// AllocatedMMIOBase implements AllocatedVirtioMMIODevice.
func (g *GPU) AllocatedMMIOBase() uint64 {
	return g.base
}

// AllocatedMMIOSize implements AllocatedVirtioMMIODevice.
func (g *GPU) AllocatedMMIOSize() uint64 {
	return g.size
}

// AllocatedIRQLine implements AllocatedVirtioMMIODevice.
func (g *GPU) AllocatedIRQLine() uint32 {
	return g.irqLine
}

var (
	_ hv.MemoryMappedIODevice = (*GPU)(nil)
	_ deviceHandler           = (*GPU)(nil)
)
