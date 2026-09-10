//go:build darwin && arm64

package hvf

import (
	"fmt"
	"github.com/ebitengine/purego"
	"sync"
)

// The host ABI takes the vector in Q0. purego supplies the integer arguments
// and host stack; the leaf assembly bridge loads Q0 and tail-calls Hypervisor.
func vectorTrampolineAddr() uintptr

var vectorBinding struct {
	once   sync.Once
	set    func(VCPU, uint32, *[16]byte, uintptr) Return
	target uintptr
	err    error
}

// SetVector restores one architectural 128-bit SIMD register on its owner thread.
func (v *VM) SetVector(index int, value [16]byte) error {
	if index < 0 || index >= 32 {
		return fmt.Errorf("invalid SIMD register %d", index)
	}
	vectorBinding.once.Do(func() {
		vectorBinding.target, vectorBinding.err = purego.Dlsym(hvLib, "hv_vcpu_set_simd_fp_reg")
		if vectorBinding.err == nil {
			purego.RegisterFunc(&vectorBinding.set, vectorTrampolineAddr())
		}
	})
	if vectorBinding.err != nil {
		return vectorBinding.err
	}
	var result Return
	if err := v.callOnThread(func() {
		result = vectorBinding.set(v.vcpus[0].vcpu, uint32(index), &value, vectorBinding.target)
	}); err != nil {
		return err
	}
	if result != hvSuccess {
		return fmt.Errorf("set SIMD register %d: %w", index, result)
	}
	return nil
}
