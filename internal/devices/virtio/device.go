package virtio

import "github.com/tinyrange/cc/internal/hv"

// VirtioDevice is the core interface for virtio devices, abstracting device-specific
// operations that are independent of the transport (MMIO or PCI).
//
// This interface provides a clean separation between device logic and transport logic,
// allowing the same device implementation to work with both MMIO and PCI transports.
type VirtioDevice interface {
	// DeviceID returns the virtio device type identifier.
	// Common values:
	//   1 = network card
	//   2 = block device
	//   3 = console
	//   4 = entropy source
	//   5 = memory balloon
	//   9 = filesystem
	DeviceID() uint16

	// DeviceFeatures returns the device's feature bitset.
	// This should return the full 64-bit feature set supported by the device.
	// Features are negotiated between device and driver during initialization.
	DeviceFeatures() uint64

	// MaxQueues returns the maximum number of virtqueues this device supports.
	MaxQueues() uint16

	// ReadConfig reads a 32-bit value from the device-specific configuration space.
	// offset is the byte offset into the device config space.
	// Returns the 32-bit value at that offset.
	ReadConfig(ctx hv.ExitContext, offset uint16) uint32

	// WriteConfig writes a 32-bit value to the device-specific configuration space.
	// offset is the byte offset into the device config space.
	// val is the 32-bit value to write.
	WriteConfig(ctx hv.ExitContext, offset uint16, val uint32)

	// Enable is called when the device is enabled after feature negotiation.
	// features is the negotiated feature set (intersection of device and driver features).
	// queues is a slice of enabled virtqueues, one per queue index.
	// The device should initialize its state based on the negotiated features and queues.
	Enable(features uint64, queues []*VirtQueue)

	// Disable is called when the device is disabled or reset.
	// The device should clean up its state and stop processing requests.
	Disable()
}
