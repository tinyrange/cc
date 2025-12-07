//go:build windows && (amd64 || arm64)

package whp

// arm64ShouldFire implements simple level bookkeeping: return true only on a
// rising edge (level asserted when it was previously deasserted). Deassert just
// updates state so a later assert can fire again. This is intentionally minimal
// and does not model full GIC pending/active state.
func (v *virtualMachine) arm64ShouldFire(intid uint32, level bool) bool {
	v.arm64GICMu.Lock()
	defer v.arm64GICMu.Unlock()

	if v.arm64GICAsserted == nil {
		v.arm64GICAsserted = make(map[uint32]bool)
	}

	prev := v.arm64GICAsserted[intid]
	if level {
		if prev {
			return false
		}
		v.arm64GICAsserted[intid] = true
		return true
	}

	// level == false
	if !prev {
		return false
	}
	v.arm64GICAsserted[intid] = false
	return false
}
