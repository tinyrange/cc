//go:build windows && (amd64 || arm64)

package whp

import "log/slog"

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
			slog.Debug("whp: arm64ShouldFire: already asserted",
				"intid", intid,
				"prev", prev,
				"level", level,
				"result", false)
			return false
		}
		v.arm64GICAsserted[intid] = true
		slog.Debug("whp: arm64ShouldFire: rising edge - will fire",
			"intid", intid,
			"prev", prev,
			"level", level,
			"result", true)
		return true
	}

	// level == false (deassert)
	if !prev {
		slog.Debug("whp: arm64ShouldFire: already deasserted",
			"intid", intid,
			"prev", prev,
			"level", level,
			"result", false)
		return false
	}
	v.arm64GICAsserted[intid] = false
	slog.Debug("whp: arm64ShouldFire: falling edge - state cleared",
		"intid", intid,
		"prev", prev,
		"level", level,
		"result", false)
	return false
}
