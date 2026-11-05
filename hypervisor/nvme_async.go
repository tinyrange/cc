package hypervisor

// EnableAsyncIO moves only backing ReadAt/WriteAt off the device owner. Call
// before controller enable. wake may wake native execution but must not access
// guest memory or call back into this controller. The owner must PollIO on
// IOReady, and CloseIO before destroying RAM, the disk, or the wake target.
// At most one 32 MiB transfer is in flight, preserving write/flush ordering.
func (c *NVMeController) EnableAsyncIO(wake func()) { c.controller.EnableAsyncIO(wake) }

// IOReady returns nil when idle, or an event closed when backing I/O is ready.
func (c *NVMeController) IOReady() <-chan struct{} { return c.controller.IOReady() }

// PollIO delivers DMA, CQ entries and interrupts on the caller's device loop.
// With wait=false it does not wait for storage. wait=true drains submitted work
// that fits in the available CQs; guest acknowledgements can free further space.
func (c *NVMeController) PollIO(wait bool) error { return c.controller.PollIO(wait) }

// CloseIO joins in-flight storage and wake activity without delivering stale
// guest DMA. An already-started backing write cannot be canceled.
func (c *NVMeController) CloseIO() { c.controller.CloseIO() }
