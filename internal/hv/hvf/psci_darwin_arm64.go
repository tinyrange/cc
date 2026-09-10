//go:build darwin && arm64

package hvf

// HandlePSCI services the current VCPU's PSCI call after an HVC exit. HVF has
// already advanced the PC. A true result requests the caller to stop the VM.
func (v *VM) HandlePSCI() (bool, error) { return handleContainerHVC(v, 0) }
