package hypervisor

import (
	"image"

	"j5.nz/cc/internal/virtio"
)

// VirtioGPU is a 2D scanout on the standard virtio-MMIO transport.
type VirtioGPU struct {
	*virtio.GPU
}

func NewVirtioGPU(address, length uint64, interrupt uint32, width, height int, memory NVMeMemory, irq NVMeIRQ) (*VirtioGPU, error) {
	screen, err := virtio.NewFramebuffer(width, height)
	if err != nil {
		return nil, err
	}
	device := virtio.NewGPU(address, length, interrupt, screen)
	device.Attach(memory, irq)
	return &VirtioGPU{GPU: device}, nil
}

// Capture returns a host-owned image once a guest has flushed the scanout.
func (g *VirtioGPU) Capture() *image.RGBA {
	width, height := g.Framebuffer().Size()
	update := g.Framebuffer().Snapshot(image.Rect(0, 0, width, height), 0, false)
	if update.Generation == 0 {
		return nil
	}
	img := image.NewRGBA(image.Rect(0, 0, update.Width, update.Height))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = update.Pixels[i+2], update.Pixels[i+1], update.Pixels[i], 0xff
	}
	return img
}
