package hypervisor

import "fmt"

// InputDevice delivers Linux input-event keycodes and absolute pointer positions
// into guest-owned virtqueues, with hardware interrupts. Calls must be serialized
// with Run and MMIO dispatch by the owner of the VM.
type InputDevice interface {
	Key(code uint16, down bool) error
	PointerEvent(x, y uint32, buttons, previous uint8) error
	ScrollEvent(x120, y120 int32) error
	SetDimensions(width, height uint32)
	Stats() map[string]uint64
}
type InputPCIConfiguration struct {
	Device        uint8
	BAR           uint64
	Interrupt     uint32
	Pointer       bool
	Width, Height uint32
}

// AddInputPCI adds a modern virtio input function to a bus returned by NewNVMePCI.
// Call before running the VM so guest enumeration observes a stable topology.
func AddInputPCI(vm ARM64, bus MMIODevice, c InputPCIConfiguration) (InputDevice, error) {
	if v, ok := vm.(interface {
		addInputPCI(MMIODevice, InputPCIConfiguration) (InputDevice, error)
	}); ok {
		return v.addInputPCI(bus, c)
	}
	return nil, fmt.Errorf("PCI input unavailable on this backend")
}
