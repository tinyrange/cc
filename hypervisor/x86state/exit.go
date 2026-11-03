package x86state

// Exit describes an architectural I/O, memory, or execution stop. Data is
// borrowed until the pending operation is completed or execution resumes.
type Exit struct {
	Reason  uint32
	Port    uint16
	Size    uint8
	Count   uint32
	Write   bool
	Data    []byte
	Address uint64
}

// RAMRegion maps part of a shared backing allocation into guest physical space.
type RAMRegion struct{ Address, Offset, Size uint64 }
