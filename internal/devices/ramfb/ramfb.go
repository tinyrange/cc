// Package ramfb implements a simple RAM-based framebuffer for QEMU fw_cfg.
package ramfb

import (
	"encoding/binary"
	"fmt"
	"sync"

	"github.com/tinyrange/cc/internal/devices/fwcfg"
	"github.com/tinyrange/cc/internal/hv"
)

// FourCC pixel format codes (stored in big-endian in config)
const (
	DRM_FORMAT_XRGB8888 = 0x34325258 // XR24
	DRM_FORMAT_BGRX8888 = 0x34325842 // BX24
	DRM_FORMAT_RGB888   = 0x34324752 // RG24
)

// RAMFBConfig represents the ramfb configuration structure.
// This is written by the guest via fw_cfg DMA.
type RAMFBConfig struct {
	Addr   uint64 // Guest physical address of framebuffer
	FourCC uint32 // Pixel format (big-endian in wire format)
	Flags  uint32
	Width  uint32
	Height uint32
	Stride uint32
}

// RAMFB implements a RAM-based framebuffer.
type RAMFB struct {
	mu sync.Mutex
	vm hv.VirtualMachine

	config   RAMFBConfig
	hasFrame bool

	// Callback for frame updates
	onFlush func(config RAMFBConfig, pixels []byte)
}

// New creates a new RAMFB device.
func New() *RAMFB {
	return &RAMFB{}
}

// Register registers the RAMFB with a fw_cfg device.
// The fw_cfg device will call our callback when the guest writes configuration.
func (r *RAMFB) Register(fw *fwcfg.FwCfg) {
	// Register "etc/ramfb" file with write callback
	// Initial data is 28 bytes of zeros (the config structure)
	initialData := make([]byte, 28)
	fw.AddFileWithCallback("etc/ramfb", initialData, r.handleWrite)
}

// Init implements hv.Device.
func (r *RAMFB) Init(vm hv.VirtualMachine) error {
	r.vm = vm
	return nil
}

// handleWrite is called when the guest writes configuration via fw_cfg DMA.
func (r *RAMFB) handleWrite(data []byte) error {
	if len(data) < 28 {
		return fmt.Errorf("ramfb: config too short: %d bytes", len(data))
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Parse the configuration (big-endian wire format)
	r.config = RAMFBConfig{
		Addr:   binary.BigEndian.Uint64(data[0:8]),
		FourCC: binary.BigEndian.Uint32(data[8:12]),
		Flags:  binary.BigEndian.Uint32(data[12:16]),
		Width:  binary.BigEndian.Uint32(data[16:20]),
		Height: binary.BigEndian.Uint32(data[20:24]),
		Stride: binary.BigEndian.Uint32(data[24:28]),
	}
	r.hasFrame = true

	return nil
}

// GetConfig returns the current framebuffer configuration.
func (r *RAMFB) GetConfig() (RAMFBConfig, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.config, r.hasFrame
}

// HasFrame returns true if the guest has configured the framebuffer.
func (r *RAMFB) HasFrame() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.hasFrame
}

// ReadFramebuffer reads the framebuffer pixels from guest memory.
// Returns nil if no framebuffer is configured or VM is not available.
func (r *RAMFB) ReadFramebuffer() ([]byte, RAMFBConfig, error) {
	r.mu.Lock()
	config := r.config
	hasFrame := r.hasFrame
	vm := r.vm
	r.mu.Unlock()

	if !hasFrame {
		return nil, RAMFBConfig{}, fmt.Errorf("ramfb: no framebuffer configured")
	}

	if vm == nil {
		return nil, RAMFBConfig{}, fmt.Errorf("ramfb: VM not initialized")
	}

	// Calculate framebuffer size
	size := uint64(config.Stride) * uint64(config.Height)
	if size == 0 {
		return nil, config, fmt.Errorf("ramfb: invalid dimensions: %dx%d stride=%d",
			config.Width, config.Height, config.Stride)
	}

	// Cap maximum size to prevent huge allocations (256MB limit)
	const maxSize = 256 * 1024 * 1024
	if size > maxSize {
		return nil, config, fmt.Errorf("ramfb: framebuffer too large: %d bytes", size)
	}

	// Read framebuffer from guest memory
	pixels := make([]byte, size)
	n, err := vm.ReadAt(pixels, int64(config.Addr))
	if err != nil {
		return nil, config, fmt.Errorf("ramfb: failed to read framebuffer: %v", err)
	}
	if n != len(pixels) {
		return nil, config, fmt.Errorf("ramfb: short read: %d/%d bytes", n, len(pixels))
	}

	return pixels, config, nil
}

// SetOnFlush sets a callback that is called when a new frame is available.
func (r *RAMFB) SetOnFlush(fn func(config RAMFBConfig, pixels []byte)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onFlush = fn
}

// BytesPerPixel returns the number of bytes per pixel for a given FourCC format.
func BytesPerPixel(fourcc uint32) int {
	switch fourcc {
	case DRM_FORMAT_XRGB8888, DRM_FORMAT_BGRX8888:
		return 4
	case DRM_FORMAT_RGB888:
		return 3
	default:
		return 4 // Default to 32-bit
	}
}

// ConvertToRGBA converts framebuffer pixels to RGBA format.
// This handles different FourCC formats.
func ConvertToRGBA(pixels []byte, config RAMFBConfig) []byte {
	width := int(config.Width)
	height := int(config.Height)
	stride := int(config.Stride)
	bpp := BytesPerPixel(config.FourCC)

	result := make([]byte, width*height*4)

	for y := 0; y < height; y++ {
		srcRow := pixels[y*stride:]
		dstRow := result[y*width*4:]

		for x := 0; x < width; x++ {
			srcOff := x * bpp
			dstOff := x * 4

			switch config.FourCC {
			case DRM_FORMAT_XRGB8888:
				// BGRX -> RGBA (X is padding, stored as BGRA in memory)
				dstRow[dstOff+0] = srcRow[srcOff+2] // R
				dstRow[dstOff+1] = srcRow[srcOff+1] // G
				dstRow[dstOff+2] = srcRow[srcOff+0] // B
				dstRow[dstOff+3] = 255              // A

			case DRM_FORMAT_BGRX8888:
				// RGBX -> RGBA
				dstRow[dstOff+0] = srcRow[srcOff+0] // R
				dstRow[dstOff+1] = srcRow[srcOff+1] // G
				dstRow[dstOff+2] = srcRow[srcOff+2] // B
				dstRow[dstOff+3] = 255              // A

			case DRM_FORMAT_RGB888:
				// RGB -> RGBA
				dstRow[dstOff+0] = srcRow[srcOff+0] // R
				dstRow[dstOff+1] = srcRow[srcOff+1] // G
				dstRow[dstOff+2] = srcRow[srcOff+2] // B
				dstRow[dstOff+3] = 255              // A

			default:
				// Unknown format, assume XRGB8888
				dstRow[dstOff+0] = srcRow[srcOff+2] // R
				dstRow[dstOff+1] = srcRow[srcOff+1] // G
				dstRow[dstOff+2] = srcRow[srcOff+0] // B
				dstRow[dstOff+3] = 255              // A
			}
		}
	}

	return result
}
