//go:build darwin && arm64

package hvf

import (
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

const hypervisorFrameworkPath = "/System/Library/Frameworks/Hypervisor.framework/Hypervisor"

type hvReturn uint32

const (
	hvSuccess      hvReturn = 0x00000000
	hvError        hvReturn = 0xFAE94001
	hvBusy         hvReturn = 0xFAE94002
	hvBadArgument  hvReturn = 0xFAE94003
	hvNoResources  hvReturn = 0xFAE94005
	hvNoDevice     hvReturn = 0xFAE94006
	hvDenied       hvReturn = 0xFAE94007
	hvUnsupported  hvReturn = 0xFAE9400F
	hvAlignmentErr hvReturn = 0xFAE94010
)

func (r hvReturn) Error() string {
	switch r {
	case hvSuccess:
		return "success"
	case hvError:
		return "error"
	case hvBusy:
		return "busy"
	case hvBadArgument:
		return "bad argument"
	case hvNoResources:
		return "no resources"
	case hvNoDevice:
		return "no device"
	case hvDenied:
		return "denied"
	case hvUnsupported:
		return "unsupported"
	case hvAlignmentErr:
		return "alignment error"
	default:
		return fmt.Sprintf("0x%08x", uint32(r))
	}
}

func (r hvReturn) toError(op string) error {
	if r == hvSuccess {
		return nil
	}
	return fmt.Errorf("hvf: %s: %w", op, r)
}

type hvMemoryFlags uint64

const (
	hvMemoryRead  hvMemoryFlags = 1 << 0
	hvMemoryWrite hvMemoryFlags = 1 << 1
	hvMemoryExec  hvMemoryFlags = 1 << 2
)

type hvExitReason uint32

const (
	hvExitReasonCanceled          hvExitReason = 0
	hvExitReasonException         hvExitReason = 1
	hvExitReasonVTimerActivated   hvExitReason = 2
	hvExitReasonVTimerDeactivated hvExitReason = 3
)

type hvVcpuExitException struct {
	Syndrome        uint64
	VirtualAddress  uint64
	PhysicalAddress uint64
}

type hvVcpuExit struct {
	Reason    hvExitReason
	_         uint32
	Exception hvVcpuExitException
}

type hvReg uint32

const (
	hvRegX0 hvReg = iota
	hvRegX1
	hvRegX2
	hvRegX3
	hvRegX4
	hvRegX5
	hvRegX6
	hvRegX7
	hvRegX8
	hvRegX9
	hvRegX10
	hvRegX11
	hvRegX12
	hvRegX13
	hvRegX14
	hvRegX15
	hvRegX16
	hvRegX17
	hvRegX18
	hvRegX19
	hvRegX20
	hvRegX21
	hvRegX22
	hvRegX23
	hvRegX24
	hvRegX25
	hvRegX26
	hvRegX27
	hvRegX28
	hvRegX29
	hvRegX30
	hvRegPc
	hvRegFpcr
	hvRegFpsr
	hvRegCpsr
)

type hvSysReg uint32

func makeHvSysReg(op0, op1, crn, crm, op2 uint32) hvSysReg {
	return hvSysReg(((op0 & 0x3) << 14) |
		((op1 & 0x7) << 11) |
		((crn & 0xF) << 7) |
		((crm & 0xF) << 3) |
		(op2 & 0x7))
}

var hvSysRegVBAR = makeHvSysReg(3, 0, 12, 0, 0)
var hvSysRegSpEl1 = hvSysReg(0xe208)

var (
	hvOnce sync.Once
	hvErr  error

	libHypervisor uintptr

	hvVmCreate    func(config uintptr) hvReturn
	hvVmDestroy   func() hvReturn
	hvVmMap       func(addr unsafe.Pointer, ipa uint64, size uint64, flags hvMemoryFlags) hvReturn
	hvVmUnmap     func(ipa uint64, size uint64) hvReturn
	hvVmProtect   func(ipa uint64, size uint64, flags hvMemoryFlags) hvReturn
	hvVcpuCreate  func(vcpu *uint64, exit **hvVcpuExit, config uintptr) hvReturn
	hvVcpuDestroy func(vcpu uint64) hvReturn
	hvVcpuRun     func(vcpu uint64) hvReturn
	hvVcpusExit   func(vcpus *uint64, count uint32) hvReturn
	hvVcpuGetReg  func(vcpu uint64, reg hvReg, value *uint64) hvReturn
	hvVcpuSetReg  func(vcpu uint64, reg hvReg, value uint64) hvReturn
	hvVcpuGetSys  func(vcpu uint64, reg hvSysReg, value *uint64) hvReturn
	hvVcpuSetSys  func(vcpu uint64, reg hvSysReg, value uint64) hvReturn
)

func ensureInitialized() error {
	hvOnce.Do(func() {
		if runtime.GOARCH != "arm64" || runtime.GOOS != "darwin" {
			hvErr = fmt.Errorf("hvf: unsupported platform %s/%s", runtime.GOOS, runtime.GOARCH)
			return
		}

		var err error
		libHypervisor, err = purego.Dlopen(hypervisorFrameworkPath, purego.RTLD_GLOBAL|purego.RTLD_NOW)
		if err != nil {
			hvErr = fmt.Errorf("hvf: dlopen Hypervisor.framework: %w", err)
			return
		}

		register := func(sym any, name string) {
			if hvErr != nil {
				return
			}
			purego.RegisterLibFunc(sym, libHypervisor, name)
		}

		register(&hvVmCreate, "hv_vm_create")
		register(&hvVmDestroy, "hv_vm_destroy")
		register(&hvVmMap, "hv_vm_map")
		register(&hvVmUnmap, "hv_vm_unmap")
		register(&hvVmProtect, "hv_vm_protect")
		register(&hvVcpuCreate, "hv_vcpu_create")
		register(&hvVcpuDestroy, "hv_vcpu_destroy")
		register(&hvVcpuRun, "hv_vcpu_run")
		register(&hvVcpusExit, "hv_vcpus_exit")
		register(&hvVcpuGetReg, "hv_vcpu_get_reg")
		register(&hvVcpuSetReg, "hv_vcpu_set_reg")
		register(&hvVcpuGetSys, "hv_vcpu_get_sys_reg")
		register(&hvVcpuSetSys, "hv_vcpu_set_sys_reg")
	})

	return hvErr
}
