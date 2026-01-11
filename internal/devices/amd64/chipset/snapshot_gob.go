package chipset

import "encoding/gob"

func init() {
	// Register snapshot types for gob encoding/decoding.
	// This is needed for VM snapshot serialization to work with device snapshots.
	gob.Register(&ioapicSnapshot{})
	gob.Register(&ioapicEntrySnapshot{})
	gob.Register(&dualPicSnapshot{})
	gob.Register(&picSnapshot{})
}
