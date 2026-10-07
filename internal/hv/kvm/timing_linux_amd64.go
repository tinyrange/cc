//go:build linux && amd64

package kvm

// TSCFrequency returns the accelerator's guest timestamp-counter rate in Hz.
// It is an observation, not a request to change the guest clock.
func (v *VM) TSCFrequency() (uint64, error) {
	khz, err := getVCPUTSCKHz(v.vcpufd)
	return uint64(khz) * 1000, err
}

// ReadMSR observes one model-specific register from the stopped owning VCPU.
func (v *VM) ReadMSR(index uint32) (uint64, error) { return getVCPUMSR(v.vcpufd, index) }
