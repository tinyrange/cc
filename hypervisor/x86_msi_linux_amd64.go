//go:build linux && amd64

package hypervisor

// SetMSI is machine-wide interrupt delivery and does not access vCPU state.
func (n *nativeX86) SetMSI(addr uint64, data uint32) error { return n.vm.SetMSI(addr, data) }
