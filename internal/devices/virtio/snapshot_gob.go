package virtio

import "encoding/gob"

func init() {
	// Register snapshot types for gob encoding/decoding.
	// This is needed for VM snapshot serialization to work with device snapshots.
	gob.Register(&consoleSnapshot{})
	gob.Register(&fsSnapshot{})
	gob.Register(&QueueSnapshot{})
	gob.Register(&MMIODeviceSnapshot{})
}
