//go:build darwin && arm64

package hvf

// CounterTicks returns the host clock used by HVF for CNTVCT_EL0 before
// subtracting the vCPU's virtual timer offset. The physical ARM counter is
// a different clock domain on macOS.
func (v *VM) CounterTicks() uint64 { return machAbsoluteTime() }
