package whp

import (
	"fmt"
	"unsafe"

	"j5.nz/cc/hypervisor/x86state"
)

const nativeHyperVClocks uint64 = 1<<0 | 1<<1 | 1<<3 | 1<<7 | 1<<9
const nativeHyperVTimers uint64 = 1<<4 | 1<<5 | 1<<8

type syntheticFeatures struct {
	Count, Reserved uint32
	Bank            uint64
}

func configureNativeHyperV(part partitionHandle) (uint64, error) {
	features := syntheticFeatures{Count: 1}
	var written uint32
	if err := callHRESULT(procWHvGetCapability, 0x1008, uintptr(unsafe.Pointer(&features)), unsafe.Sizeof(features), uintptr(unsafe.Pointer(&written))); err != nil {
		return 0, fmt.Errorf("query WHP synthetic processor features: %w", err)
	}
	if written != uint32(unsafe.Sizeof(features)) || features.Count != 1 {
		return 0, fmt.Errorf("invalid WHP synthetic processor feature banks")
	}
	if features.Bank&nativeHyperVClocks != nativeHyperVClocks {
		return 0, nil
	}
	selected := nativeHyperVClocks
	if features.Bank&nativeHyperVTimers == nativeHyperVTimers {
		selected |= nativeHyperVTimers | features.Bank&(1<<22) // direct synthetic timers
	}
	features.Bank = selected
	if err := setPartitionProperty(part, partitionPropertyCode(0x100c), features); err != nil {
		return 0, fmt.Errorf("enable WHP clock and timer features: %w", err)
	}
	return selected, nil
}

// SupportedHyperVCPUID reports only facilities activated before partition setup.
// WHP owns the reference counter/page, hypercall page and synthetic timers;
// there is no userspace clock emulation or unimplemented feature advertisement.
func (c *NativeCPU) SupportedHyperVCPUID() ([]x86state.CPUIDEntry, error) {
	features := c.machine.hyperVFeatures
	if features&nativeHyperVClocks != nativeHyperVClocks {
		return nil, nil
	}
	eax, edx := uint32(1<<1|1<<5|1<<9), uint32(0)
	if features&nativeHyperVTimers == nativeHyperVTimers {
		eax |= 1<<2 | 1<<3 | 1<<6
		if features&(1<<22) != 0 {
			edx = 1 << 19
		}
	}
	return []x86state.CPUIDEntry{
		{Function: 0x40000000, Eax: 0x40000005, Ebx: 0x7263694d, Ecx: 0x666f736f, Edx: 0x76482074},
		{Function: 0x40000001, Eax: 0x31237648},
		{Function: 0x40000003, Eax: eax, Edx: edx},
	}, nil
}

func (c *NativeCPU) EnableHyperVSynIC() (bool, error) {
	return c.machine.hyperVFeatures&nativeHyperVTimers == nativeHyperVTimers, nil
}
