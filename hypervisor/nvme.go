package hypervisor

import "j5.nz/cc/internal/nvme"

// NVMeMemory maps guest physical addresses for queue and data DMA. Implementations
// may also provide ReadIPAInto and SliceIPA to avoid copying mapped RAM.
type NVMeMemory interface {
	ReadIPA(uint64, int) ([]byte, error)
	WriteIPA(uint64, []byte) error
}

// NVMeIRQ delivers the controller's level-triggered interrupt.
type NVMeIRQ interface{ SetIRQ(uint32, bool) error }

const NVMeMMIOSize = nvme.MMIOSize

// NVMeController is a transport-independent controller. The machine supplies
// PCI discovery, MMIO address decoding, guest RAM and interrupt routing.
type NVMeController struct{ controller *nvme.Controller }

func NewNVMeController(disk BlockDevice, memory NVMeMemory, irq NVMeIRQ, line uint32) *NVMeController {
	c := nvme.NewController(disk)
	c.IRQ = line
	c.Attach(memory, irq)
	return &NVMeController{controller: c}
}
func (c *NVMeController) ReadMMIO(offset uint64, size int) (uint64, error) {
	return c.controller.ReadMMIO(offset, size)
}
func (c *NVMeController) WriteMMIO(offset uint64, size int, value uint64) error {
	return c.controller.WriteMMIO(offset, size, value)
}

// ConfigureMSI selects one PCI message. When enabled, irq supplied at construction
// must also implement SetMSI(uint64, uint32) error.
func (c *NVMeController) ConfigureMSI(enabled bool, addr uint64, data uint32) {
	c.controller.ConfigureMSI(enabled, addr, data)
}

// NVMeIOStatistics counts completed controller commands and their payload bytes.
type NVMeIOStatistics = nvme.IOStatistics

func (c *NVMeController) IOStatistics() NVMeIOStatistics { return c.controller.IOStatistics() }
