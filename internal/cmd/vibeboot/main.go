// Package main provides the vibeboot command for booting VibeOS.

// VibeOS from https://github.com/kaansenol5/VibeOS
// Requires modifications to VibeOS to support GICv3 (supplied in VibeOS.diff)

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"time"
	"unsafe"

	"github.com/tinyrange/cc/internal/chipset"
	arm64serial "github.com/tinyrange/cc/internal/devices/arm64/serial"
	"github.com/tinyrange/cc/internal/devices/fwcfg"
	"github.com/tinyrange/cc/internal/devices/pl031"
	"github.com/tinyrange/cc/internal/devices/ramfb"
	"github.com/tinyrange/cc/internal/devices/virtio"
	"github.com/tinyrange/cc/internal/fdt"
	gll "github.com/tinyrange/cc/internal/gowin/gl"
	"github.com/tinyrange/cc/internal/gowin/window"
	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/hv/factory"
)

const (
	// Memory layout
	// Code is loaded at 0x0, RAM starts at 0x40000000
	codeBase      = 0x00000000       // Code/binary loaded here
	codeSize      = 64 * 1024 * 1024 // 64MB for code
	gicBase       = 0x08000000       // GIC distributor
	gicRedistBase = 0x080a0000       // GIC redistributor
	uartBase      = 0x09000000       // PL011 UART
	rtcBase       = 0x09010000       // PL031 RTC
	fwcfgBaseAddr = 0x09020000       // fw_cfg
	ramBase       = 0x40000000       // Main memory
	ramSize       = 512 * 1024 * 1024

	// Virtio MMIO bus layout (matches VibeOS expectations)
	virtioMMIOBase  = 0x0a000000 // Virtio MMIO region base
	virtioSlotSize  = 0x200      // Size per virtio slot
	virtioSlotCount = 32         // Number of virtio slots

	// IRQ numbers (SPI offsets)
	uartIRQ           = 33
	rtcIRQ            = 34
	virtioBlkIRQ      = 48 // IRQ for virtio-blk at slot 0
	virtioKeyboardIRQ = 49 // IRQ for virtio-input keyboard at slot 1
	virtioTabletIRQ   = 50 // IRQ for virtio-input tablet at slot 2

	// GIC phandle
	gicPhandle = 1
)

func main() {
	// Check for debug flag early (before flag.Parse)
	for _, arg := range os.Args {
		if arg == "-debug" {
			slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
				Level: slog.LevelDebug,
			})))
			break
		}
	}

	if err := run(); err != nil {
		slog.Error("vibeboot failed", "err", err)
		os.Exit(1)
	}
}

// chipsetMMIOAdapter wraps a chipset.ChipsetDevice to implement hv.MemoryMappedIODevice.
// This is needed because HVF ARM64 looks for hv.MemoryMappedIODevice, but our devices
// implement chipset.ChipsetDevice.
type chipsetMMIOAdapter struct {
	dev     hv.Device
	mmio    *chipset.MmioIntercept
	regions []hv.MMIORegion
}

func wrapChipsetDevice(dev chipset.ChipsetDevice) *chipsetMMIOAdapter {
	mmio := dev.SupportsMmio()
	if mmio == nil {
		return nil
	}
	regions := make([]hv.MMIORegion, len(mmio.Regions))
	copy(regions, mmio.Regions)
	return &chipsetMMIOAdapter{
		dev:     dev.(hv.Device),
		mmio:    mmio,
		regions: regions,
	}
}

func (a *chipsetMMIOAdapter) Init(vm hv.VirtualMachine) error {
	return a.dev.Init(vm)
}

func (a *chipsetMMIOAdapter) MMIORegions() []hv.MMIORegion {
	return a.regions
}

func (a *chipsetMMIOAdapter) ReadMMIO(ctx hv.ExitContext, addr uint64, data []byte) error {
	return a.mmio.Handler.ReadMMIO(ctx, addr, data)
}

func (a *chipsetMMIOAdapter) WriteMMIO(ctx hv.ExitContext, addr uint64, data []byte) error {
	return a.mmio.Handler.WriteMMIO(ctx, addr, data)
}

var _ hv.MemoryMappedIODevice = (*chipsetMMIOAdapter)(nil)

// vibeBootRunConfig implements hv.RunConfig for running VibeOS.
type vibeBootRunConfig struct {
	loadAddr uint64
	dtbAddr  uint64
	stackTop uint64
}

func (c *vibeBootRunConfig) Run(ctx context.Context, vcpu hv.VirtualCPU) error {
	// Set initial registers
	regs := map[hv.Register]hv.RegisterValue{
		hv.RegisterARM64Pc:     hv.Register64(c.loadAddr),
		hv.RegisterARM64Sp:     hv.Register64(c.stackTop),
		hv.RegisterARM64X0:     hv.Register64(c.dtbAddr),
		hv.RegisterARM64X1:     hv.Register64(0),
		hv.RegisterARM64X2:     hv.Register64(0),
		hv.RegisterARM64X3:     hv.Register64(0),
		hv.RegisterARM64Pstate: hv.Register64(0x3c5), // EL1h, interrupts masked
	}
	if err := vcpu.SetRegisters(regs); err != nil {
		return fmt.Errorf("set registers: %w", err)
	}

	slog.Info("Starting VibeOS", "entry", fmt.Sprintf("0x%x", c.loadAddr), "dtb", fmt.Sprintf("0x%x", c.dtbAddr))

	// Run VM loop
	for {
		if err := vcpu.Run(ctx); err != nil {
			if errors.Is(err, hv.ErrVMHalted) {
				slog.Info("VM halted")
				return nil
			}
			if errors.Is(err, hv.ErrGuestRequestedReboot) {
				slog.Info("Guest requested reboot")
				return nil
			}
			return fmt.Errorf("vcpu run: %w", err)
		}
	}
}

func run() error {
	runtime.LockOSThread()

	binaryPath := flag.String("binary", "", "Path to vibeos.bin")
	diskPath := flag.String("disk", "", "Path to disk.img")
	noGUI := flag.Bool("no-gui", false, "Disable GUI (serial only)")
	timeout := flag.Duration("timeout", 0, "Timeout for the VM")
	_ = flag.Bool("debug", false, "Enable debug logging")
	flag.Parse()

	if *binaryPath == "" {
		return errors.New("vibeboot: -binary flag is required")
	}

	// Load the binary
	binary, err := os.ReadFile(*binaryPath)
	if err != nil {
		return fmt.Errorf("vibeboot: read binary: %w", err)
	}
	slog.Info("Loaded binary", "path", *binaryPath, "size", len(binary))

	// Open disk if provided
	var diskFile *os.File
	if *diskPath != "" {
		diskFile, err = os.OpenFile(*diskPath, os.O_RDWR, 0)
		if err != nil {
			return fmt.Errorf("vibeboot: open disk: %w", err)
		}
		defer diskFile.Close()
		fi, _ := diskFile.Stat()
		slog.Info("Opened disk", "path", *diskPath, "size", fi.Size())
	}

	// Create hypervisor
	dev, err := factory.NewWithArchitecture(hv.ArchitectureARM64)
	if err != nil {
		return fmt.Errorf("vibeboot: create hypervisor: %w", err)
	}
	defer dev.Close()

	// Create VM with memory at ramBase
	vm, err := dev.NewVirtualMachine(hv.SimpleVMConfig{
		NumCPUs:          1,
		MemSize:          ramSize,
		MemBase:          ramBase,
		InterruptSupport: true,
	})
	if err != nil {
		return fmt.Errorf("vibeboot: create VM: %w", err)
	}
	defer vm.Close()

	// Allocate additional memory at 0x0 for the code
	// VibeOS expects to be loaded at address 0x0
	codeMem, err := vm.AllocateMemory(codeBase, codeSize)
	if err != nil {
		return fmt.Errorf("vibeboot: allocate code memory: %w", err)
	}

	// Load binary into code memory at offset 0 (physical address 0x0)
	loadAddr := uint64(codeBase)
	if _, err := codeMem.WriteAt(binary, 0); err != nil {
		return fmt.Errorf("vibeboot: write binary to code memory: %w", err)
	}

	// Create and add devices
	// UART (PL011)
	uart := arm64serial.NewPL011Device(uartBase, 0x1000, os.Stdout)
	if err := vm.AddDevice(uart); err != nil {
		return fmt.Errorf("vibeboot: add uart: %w", err)
	}

	// RTC (PL031)
	rtc := pl031.New(rtcBase, nil)
	if err := vm.AddDevice(wrapChipsetDevice(rtc)); err != nil {
		return fmt.Errorf("vibeboot: add rtc: %w", err)
	}

	// fw_cfg
	fwCfg := fwcfg.New(fwcfgBaseAddr)
	if err := vm.AddDevice(wrapChipsetDevice(fwCfg)); err != nil {
		return fmt.Errorf("vibeboot: add fwcfg: %w", err)
	}

	// ramfb (registers with fw_cfg)
	ramFB := ramfb.New()
	ramFB.Register(fwCfg)
	if err := ramFB.Init(vm); err != nil {
		return fmt.Errorf("vibeboot: init ramfb: %w", err)
	}

	// Collect device tree nodes
	var deviceNodes []fdt.Node

	// Create virtio MMIO bus at 0x0a000000 with 32 slots
	// This covers the region VibeOS scans for virtio devices
	virtioBus := virtio.NewVirtioMMIOBus(virtioMMIOBase, virtioSlotSize, virtioSlotCount)

	// Add virtio-blk to slot 0 if disk is provided
	if diskFile != nil {
		slotAddr := virtioBus.SlotAddress(0)
		blkTemplate := virtio.NewBlkTemplate(diskFile, false)
		blkDevice, err := virtio.NewBlkForBusSlot(vm, slotAddr, virtioBlkIRQ, blkTemplate)
		if err != nil {
			return fmt.Errorf("vibeboot: create virtio-blk: %w", err)
		}
		virtioBus.AttachDevice(0, blkDevice)

		// Add device tree node for virtio-blk
		deviceNodes = append(deviceNodes, fdt.Node{
			Name: fmt.Sprintf("virtio@%x", slotAddr),
			Properties: map[string]fdt.Property{
				"compatible": {Strings: []string{"virtio,mmio"}},
				"reg":        {U64: []uint64{slotAddr, virtioSlotSize}},
				"interrupts": {U32: []uint32{0, virtioBlkIRQ, 4}},
				"status":     {Strings: []string{"okay"}},
			},
		})
		slog.Info("Added virtio-blk", "slot", 0, "addr", fmt.Sprintf("0x%x", slotAddr), "irq", virtioBlkIRQ)
	}

	// Add virtio-input keyboard to slot 1
	keyboardSlotAddr := virtioBus.SlotAddress(1)
	keyboardDevice, err := virtio.NewInputForBusSlot(vm, keyboardSlotAddr, virtioKeyboardIRQ, virtio.InputTypeKeyboard, "VibeOS Keyboard")
	if err != nil {
		return fmt.Errorf("vibeboot: create virtio-input keyboard: %w", err)
	}
	virtioBus.AttachDevice(1, keyboardDevice)

	// Add device tree node for virtio-input keyboard
	deviceNodes = append(deviceNodes, fdt.Node{
		Name: fmt.Sprintf("virtio@%x", keyboardSlotAddr),
		Properties: map[string]fdt.Property{
			"compatible": {Strings: []string{"virtio,mmio"}},
			"reg":        {U64: []uint64{keyboardSlotAddr, virtioSlotSize}},
			"interrupts": {U32: []uint32{0, virtioKeyboardIRQ, 4}},
			"status":     {Strings: []string{"okay"}},
		},
	})
	slog.Info("Added virtio-input keyboard", "slot", 1, "addr", fmt.Sprintf("0x%x", keyboardSlotAddr), "irq", virtioKeyboardIRQ)

	// Add virtio-input tablet (absolute mouse) to slot 2
	// Name must be "QEMU Virtio Tablet" - VibeOS checks specific character positions
	tabletSlotAddr := virtioBus.SlotAddress(2)
	tabletDevice, err := virtio.NewInputForBusSlot(vm, tabletSlotAddr, virtioTabletIRQ, virtio.InputTypeTablet, "QEMU Virtio Tablet")
	if err != nil {
		return fmt.Errorf("vibeboot: create virtio-input tablet: %w", err)
	}
	virtioBus.AttachDevice(2, tabletDevice)

	// Add device tree node for virtio-input tablet
	deviceNodes = append(deviceNodes, fdt.Node{
		Name: fmt.Sprintf("virtio@%x", tabletSlotAddr),
		Properties: map[string]fdt.Property{
			"compatible": {Strings: []string{"virtio,mmio"}},
			"reg":        {U64: []uint64{tabletSlotAddr, virtioSlotSize}},
			"interrupts": {U32: []uint32{0, virtioTabletIRQ, 4}},
			"status":     {Strings: []string{"okay"}},
		},
	})
	slog.Info("Added virtio-input tablet", "slot", 2, "addr", fmt.Sprintf("0x%x", tabletSlotAddr), "irq", virtioTabletIRQ)

	// Add the virtio bus device (handles all 32 slots)
	if err := vm.AddDevice(virtioBus); err != nil {
		return fmt.Errorf("vibeboot: add virtio bus: %w", err)
	}

	// Build device tree
	dtb, err := buildVibeOSDeviceTree(deviceNodes)
	if err != nil {
		return fmt.Errorf("vibeboot: build device tree: %w", err)
	}

	// Place DTB at the start of RAM (0x40000000) - VibeOS expects it there
	dtbAddr := uint64(ramBase)
	if _, err := vm.WriteAt(dtb, int64(dtbAddr)); err != nil {
		return fmt.Errorf("vibeboot: write dtb to guest: %w", err)
	}
	slog.Info("Placed DTB", "addr", fmt.Sprintf("0x%x", dtbAddr), "size", len(dtb))

	// Stack at end of RAM
	stackTop := uint64(ramBase + ramSize - 0x1000)

	// Run VM
	runConfig := &vibeBootRunConfig{
		loadAddr: loadAddr,
		dtbAddr:  dtbAddr,
		stackTop: stackTop,
	}

	var ctx context.Context
	var cancel context.CancelFunc
	if *timeout != 0 {
		ctx, cancel = context.WithTimeout(context.Background(), *timeout)
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}
	defer cancel()

	if *noGUI {
		// No GUI - just run the VM directly
		return vm.Run(ctx, runConfig)
	}

	// GUI mode: create window on main thread, run VM in goroutine
	win, err := window.New("VibeOS", 800, 600, true)
	if err != nil {
		slog.Warn("Failed to create window, falling back to serial only", "err", err)
		return vm.Run(ctx, runConfig)
	}
	defer win.Close()

	// Set up display manager
	dm := newRAMFBDisplayManager(ramFB, win)

	// Run VM in background goroutine
	vmDone := make(chan error, 1)
	go func() {
		vmDone <- vm.Run(ctx, runConfig)
	}()

	// Main loop on main thread for window events
	ticker := time.NewTicker(16 * time.Millisecond) // ~60 FPS
	defer ticker.Stop()

	for {
		select {
		case err := <-vmDone:
			return err
		case <-ticker.C:
			if !win.Poll() {
				cancel()   // Signal VM to stop
				return nil // Window closed
			}

			// Process input events and forward to virtio-input devices
			processInputEvents(win, keyboardDevice, tabletDevice)

			dm.Render()
			win.Swap()
		}
	}
}

// processInputEvents processes window input events and forwards them to the virtio-input devices.
func processInputEvents(win window.Window, keyboard, tablet *virtio.Input) {
	events := win.DrainInputEvents()
	winWidth, winHeight := win.BackingSize()

	for _, ev := range events {
		switch ev.Type {
		case window.InputEventKeyDown:
			if linuxKey, ok := virtio.GowinKeyToLinux[ev.Key]; ok && linuxKey != 0 {
				keyboard.InjectKeyEvent(linuxKey, true)
			}
		case window.InputEventKeyUp:
			if linuxKey, ok := virtio.GowinKeyToLinux[ev.Key]; ok && linuxKey != 0 {
				keyboard.InjectKeyEvent(linuxKey, false)
			}
		case window.InputEventMouseDown:
			if linuxBtn, ok := virtio.GowinButtonToLinux[ev.Button]; ok {
				tablet.InjectButtonEvent(linuxBtn, true)
				tablet.InjectSynReport()
			}
		case window.InputEventMouseUp:
			if linuxBtn, ok := virtio.GowinButtonToLinux[ev.Button]; ok {
				tablet.InjectButtonEvent(linuxBtn, false)
				tablet.InjectSynReport()
			}
		}
	}

	// Send current mouse position as absolute coordinates
	cursorX, cursorY := win.Cursor()
	absX := virtio.NormalizeTabletCoord(cursorX, winWidth)
	absY := virtio.NormalizeTabletCoord(cursorY, winHeight)
	tablet.InjectMouseMove(absX, absY)
}

// RAMFBDisplayManager handles rendering the ramfb framebuffer.
type RAMFBDisplayManager struct {
	fb     *ramfb.RAMFB
	window window.Window

	// GL resources
	initialized   bool
	textureID     uint32
	shaderProgram uint32
	vao           uint32
	vbo           uint32
}

func newRAMFBDisplayManager(fb *ramfb.RAMFB, win window.Window) *RAMFBDisplayManager {
	return &RAMFBDisplayManager{
		fb:     fb,
		window: win,
	}
}

const ramfbVertexShaderSource = `#version 150
in vec2 position;
in vec2 texCoord;
out vec2 fragTexCoord;
void main() {
    gl_Position = vec4(position, 0.0, 1.0);
    fragTexCoord = texCoord;
}
` + "\x00"

const ramfbFragmentShaderSource = `#version 150
in vec2 fragTexCoord;
out vec4 fragColor;
uniform sampler2D tex;
void main() {
    fragColor = texture(tex, fragTexCoord);
}
` + "\x00"

func (dm *RAMFBDisplayManager) initGL(gl gll.OpenGL) error {
	if dm.initialized {
		return nil
	}

	// Compile shaders
	vertexShader := gl.CreateShader(gll.VertexShader)
	gl.ShaderSource(vertexShader, ramfbVertexShaderSource)
	gl.CompileShader(vertexShader)

	var status int32
	gl.GetShaderiv(vertexShader, gll.CompileStatus, &status)
	if status == 0 {
		log := gl.GetShaderInfoLog(vertexShader)
		return fmt.Errorf("vertex shader compile failed: %s", log)
	}

	fragmentShader := gl.CreateShader(gll.FragmentShader)
	gl.ShaderSource(fragmentShader, ramfbFragmentShaderSource)
	gl.CompileShader(fragmentShader)

	gl.GetShaderiv(fragmentShader, gll.CompileStatus, &status)
	if status == 0 {
		log := gl.GetShaderInfoLog(fragmentShader)
		return fmt.Errorf("fragment shader compile failed: %s", log)
	}

	dm.shaderProgram = gl.CreateProgram()
	gl.AttachShader(dm.shaderProgram, vertexShader)
	gl.AttachShader(dm.shaderProgram, fragmentShader)
	gl.LinkProgram(dm.shaderProgram)

	gl.GetProgramiv(dm.shaderProgram, gll.LinkStatus, &status)
	if status == 0 {
		log := gl.GetProgramInfoLog(dm.shaderProgram)
		return fmt.Errorf("program link failed: %s", log)
	}

	gl.DeleteShader(vertexShader)
	gl.DeleteShader(fragmentShader)

	// Create VAO and VBO
	gl.GenVertexArrays(1, &dm.vao)
	gl.BindVertexArray(dm.vao)

	gl.GenBuffers(1, &dm.vbo)
	gl.BindBuffer(gll.ArrayBuffer, dm.vbo)

	// Fullscreen quad vertices
	vertices := []float32{
		-1.0, -1.0, 0.0, 1.0,
		1.0, -1.0, 1.0, 1.0,
		-1.0, 1.0, 0.0, 0.0,
		1.0, 1.0, 1.0, 0.0,
	}

	gl.BufferData(gll.ArrayBuffer, len(vertices)*4, unsafe.Pointer(&vertices[0]), gll.StaticDraw)

	positionLoc := gl.GetAttribLocation(dm.shaderProgram, "position\x00")
	texCoordLoc := gl.GetAttribLocation(dm.shaderProgram, "texCoord\x00")

	if positionLoc >= 0 {
		gl.EnableVertexAttribArray(uint32(positionLoc))
		gl.VertexAttribPointer(uint32(positionLoc), 2, gll.Float, false, 4*4, 0)
	}
	if texCoordLoc >= 0 {
		gl.EnableVertexAttribArray(uint32(texCoordLoc))
		gl.VertexAttribPointer(uint32(texCoordLoc), 2, gll.Float, false, 4*4, uintptr(2*4))
	}

	gl.BindVertexArray(0)

	// Create texture
	gl.GenTextures(1, &dm.textureID)
	gl.BindTexture(gll.Texture2D, dm.textureID)
	gl.TexParameteri(gll.Texture2D, gll.TextureMinFilter, int32(gll.Linear))
	gl.TexParameteri(gll.Texture2D, gll.TextureMagFilter, int32(gll.Linear))
	gl.TexParameteri(gll.Texture2D, gll.TextureWrapS, int32(gll.ClampToEdge))
	gl.TexParameteri(gll.Texture2D, gll.TextureWrapT, int32(gll.ClampToEdge))

	dm.initialized = true
	return nil
}

func (dm *RAMFBDisplayManager) Render() {
	if dm.window == nil {
		return
	}

	gl, err := dm.window.GL()
	if err != nil {
		return
	}

	if !dm.initialized {
		if err := dm.initGL(gl); err != nil {
			slog.Error("Failed to initialize GL", "err", err)
			return
		}
	}

	winWidth, winHeight := dm.window.BackingSize()
	gl.Viewport(0, 0, int32(winWidth), int32(winHeight))

	// Check if ramfb has a frame
	if !dm.fb.HasFrame() {
		gl.ClearColor(0, 0, 0, 1)
		gl.Clear(gll.ColorBufferBit)
		return
	}

	// Read framebuffer from guest memory
	pixels, config, err := dm.fb.ReadFramebuffer()
	if err != nil {
		gl.ClearColor(0.1, 0, 0, 1)
		gl.Clear(gll.ColorBufferBit)
		return
	}

	// Convert to RGBA
	rgbaPixels := ramfb.ConvertToRGBA(pixels, config)

	// Update texture
	var pixelPtr unsafe.Pointer
	if len(rgbaPixels) > 0 {
		pixelPtr = unsafe.Pointer(&rgbaPixels[0])
	}

	gl.BindTexture(gll.Texture2D, dm.textureID)
	gl.TexImage2D(
		gll.Texture2D,
		0,
		int32(gll.RGBA),
		int32(config.Width),
		int32(config.Height),
		0,
		gll.RGBA,
		gll.UnsignedByte,
		pixelPtr,
	)

	// Render
	gl.ClearColor(0, 0, 0, 1)
	gl.Clear(gll.ColorBufferBit)

	gl.UseProgram(dm.shaderProgram)
	gl.ActiveTexture(gll.Texture0)
	gl.BindTexture(gll.Texture2D, dm.textureID)

	texLoc := gl.GetUniformLocation(dm.shaderProgram, "tex\x00")
	if texLoc >= 0 {
		gl.Uniform1i(texLoc, 0)
	}

	gl.BindVertexArray(dm.vao)
	gl.DrawArrays(gll.TriangleStrip, 0, 4)
	gl.BindVertexArray(0)

	gl.UseProgram(0)
}

func buildVibeOSDeviceTree(devices []fdt.Node) ([]byte, error) {
	root := fdt.Node{
		Name: "",
		Properties: map[string]fdt.Property{
			"#address-cells":   {U32: []uint32{2}},
			"#size-cells":      {U32: []uint32{2}},
			"compatible":       {Strings: []string{"vibeos,arm64"}},
			"model":            {Strings: []string{"VibeOS Virtual Machine"}},
			"interrupt-parent": {U32: []uint32{gicPhandle}},
		},
	}

	// CPUs
	cpus := fdt.Node{
		Name: "cpus",
		Properties: map[string]fdt.Property{
			"#address-cells": {U32: []uint32{2}},
			"#size-cells":    {U32: []uint32{0}},
		},
		Children: []fdt.Node{
			{
				Name: "cpu@0",
				Properties: map[string]fdt.Property{
					"device_type":   {Strings: []string{"cpu"}},
					"compatible":    {Strings: []string{"arm,armv8"}},
					"reg":           {U64: []uint64{0}},
					"enable-method": {Strings: []string{"psci"}},
				},
			},
		},
	}
	root.Children = append(root.Children, cpus)

	// Memory
	root.Children = append(root.Children, fdt.Node{
		Name: fmt.Sprintf("memory@%x", ramBase),
		Properties: map[string]fdt.Property{
			"device_type": {Strings: []string{"memory"}},
			"reg":         {U64: []uint64{ramBase, ramSize}},
		},
	})

	// GIC
	root.Children = append(root.Children, fdt.Node{
		Name: fmt.Sprintf("interrupt-controller@%x", gicBase),
		Properties: map[string]fdt.Property{
			"compatible":           {Strings: []string{"arm,gic-v3"}},
			"#interrupt-cells":     {U32: []uint32{3}},
			"#address-cells":       {U32: []uint32{2}},
			"#size-cells":          {U32: []uint32{2}},
			"interrupt-controller": {Flag: true},
			"phandle":              {U32: []uint32{gicPhandle}},
			"reg": {U64: []uint64{
				gicBase, 0x10000,
				gicRedistBase, 0x20000,
			}},
		},
	})

	// UART (PL011)
	root.Children = append(root.Children, fdt.Node{
		Name: fmt.Sprintf("serial@%x", uartBase),
		Properties: map[string]fdt.Property{
			"compatible": {Strings: []string{"arm,pl011", "arm,primecell"}},
			"reg":        {U64: []uint64{uartBase, 0x1000}},
			"interrupts": {U32: []uint32{0, uartIRQ, 4}},
			"status":     {Strings: []string{"okay"}},
		},
	})

	// RTC (PL031)
	root.Children = append(root.Children, fdt.Node{
		Name: fmt.Sprintf("rtc@%x", rtcBase),
		Properties: map[string]fdt.Property{
			"compatible": {Strings: []string{"arm,pl031", "arm,primecell"}},
			"reg":        {U64: []uint64{rtcBase, 0x1000}},
			"interrupts": {U32: []uint32{0, rtcIRQ, 4}},
			"status":     {Strings: []string{"okay"}},
		},
	})

	// fw_cfg
	root.Children = append(root.Children, fdt.Node{
		Name: fmt.Sprintf("fw-cfg@%x", fwcfgBaseAddr),
		Properties: map[string]fdt.Property{
			"compatible": {Strings: []string{"qemu,fw-cfg-mmio"}},
			"reg":        {U64: []uint64{fwcfgBaseAddr, 0x1000}},
			"status":     {Strings: []string{"okay"}},
		},
	})

	// PSCI
	root.Children = append(root.Children, fdt.Node{
		Name: "psci",
		Properties: map[string]fdt.Property{
			"compatible": {Strings: []string{"arm,psci-0.2", "arm,psci"}},
			"method":     {Strings: []string{"hvc"}},
		},
	})

	// Timer
	root.Children = append(root.Children, fdt.Node{
		Name: "timer",
		Properties: map[string]fdt.Property{
			"compatible": {Strings: []string{"arm,armv8-timer"}},
			"always-on":  {Flag: true},
			"interrupts": {U32: []uint32{1, 13, 4, 1, 14, 4, 1, 11, 4, 1, 10, 4}},
		},
	})

	// Chosen
	root.Children = append(root.Children, fdt.Node{
		Name: "chosen",
		Properties: map[string]fdt.Property{
			"stdout-path": {Strings: []string{fmt.Sprintf("/serial@%x", uartBase)}},
		},
	})

	// Aliases
	root.Children = append(root.Children, fdt.Node{
		Name: "aliases",
		Properties: map[string]fdt.Property{
			"serial0": {Strings: []string{fmt.Sprintf("/serial@%x", uartBase)}},
		},
	})

	// Additional devices
	root.Children = append(root.Children, devices...)

	return fdt.Build(root)
}
