//go:build darwin && arm64

package hvf

import "github.com/tinyrange/cc/internal/hv"

var arm64GeneralRegisterMap = func() map[hv.Register]hvReg {
	regs := make(map[hv.Register]hvReg, 35)

	for i := 0; i <= 30; i++ {
		regs[hv.Register(int(hv.RegisterARM64X0)+i)] = hvReg(hvRegX0 + hvReg(i))
	}

	regs[hv.RegisterARM64Pc] = hvRegPc
	regs[hv.RegisterARM64Pstate] = hvRegCpsr

	return regs
}()

var arm64SysRegisterMap = map[hv.Register]hvSysReg{
	hv.RegisterARM64Vbar: hvSysRegVBAR,
	hv.RegisterARM64Sp:   hvSysRegSpEl1,
}

func arm64RegisterFromIndex(idx int) (hv.Register, bool) {
	switch {
	case idx >= 0 && idx <= 30:
		return hv.Register(int(hv.RegisterARM64X0) + idx), true
	case idx == 31:
		return hv.RegisterARM64Sp, true
	default:
		return hv.RegisterInvalid, false
	}
}

func hvRegFromRegister(reg hv.Register) (hvReg, bool) {
	r, ok := arm64GeneralRegisterMap[reg]
	return r, ok
}

func hvSysRegFromRegister(reg hv.Register) (hvSysReg, bool) {
	r, ok := arm64SysRegisterMap[reg]
	return r, ok
}
