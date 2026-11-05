package whp

import (
	"fmt"
	"unsafe"
)

var procWHvGetVirtualProcessorCounters = winhvplatform.NewProc("WHvGetVirtualProcessorCounters")

// ExecutionCounters returns cumulative native execution observations. The
// caller must stop and join this CPU before inspection, as with Registers.
// Runtime values are nanoseconds; exit values count returns to user mode.
func (c *NativeCPU) ExecutionCounters() (map[string]uint64, error) {
	var runtime [2]uint64
	var written uint32
	if err := callHRESULT(procWHvGetVirtualProcessorCounters, uintptr(c.machine.vm.part), uintptr(c.index), 0,
		uintptr(unsafe.Pointer(&runtime[0])), unsafe.Sizeof(runtime), uintptr(unsafe.Pointer(&written))); err != nil {
		return nil, err
	}
	if written != uint32(unsafe.Sizeof(runtime)) {
		return nil, fmt.Errorf("WHP runtime counter size %d", written)
	}
	out := map[string]uint64{"runtime_ns": runtime[0] * 100, "hypervisor_ns": runtime[1] * 100}
	for reason, count := range c.exitCounts {
		out["exit_"+reason.String()] = count
	}
	return out, nil
}
