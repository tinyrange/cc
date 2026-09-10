//go:build darwin && arm64

package hvf

import (
	"fmt"
	"github.com/ebitengine/purego"
	"sync"
)

var debugBinding struct {
	once sync.Once
	trap func(VCPU, bool) Return
}

func bindDebug() {
	debugBinding.once.Do(func() {
		purego.RegisterLibFunc(&debugBinding.trap, hvLib, "hv_vcpu_set_trap_debug_exceptions")
	})
}

// SetDebugExceptionTrapping controls whether guest breakpoints and debug
// exceptions exit to the caller instead of being delivered to the guest.
func (v *VM) SetDebugExceptionTrapping(enabled bool) error {
	bindDebug()
	var result Return
	if err := v.callOnThread(func() { result = debugBinding.trap(v.vcpus[0].vcpu, enabled) }); err != nil {
		return err
	}
	if result != hvSuccess {
		return fmt.Errorf("trap debug exceptions: %w", result)
	}
	return nil
}
