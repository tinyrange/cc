//go:build darwin && arm64

package hvf

import (
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
)

type Return int32

func (r Return) Error() string {
	switch r {
	case hvSuccess:
		return ""
	case hvError:
		return "error"
	case hvBusy:
		return "busy"
	case hvBadArgument:
		return "bad argument"
	case hvIllegalGuestState:
		return "illegal guest state"
	case hvNoResources:
		return "no resources"
	case hvNoDevice:
		return "no device"
	case hvDenied:
		return "denied"
	case hvUnsupported:
		return "unsupported"
	default:
		return fmt.Sprintf("unknown error: %d", r)
	}
}

type VMConfig uintptr
type VcpuConfig uintptr
type GICConfig uintptr
type IPA uint64
type VCPU uint64
type MemoryFlags uint64
type ExitReason uint32
type Reg uint32
type SysReg uint16
type GICDistributorReg uint16
type GICRedistributorReg uint32
type GICICCReg uint16

type VcpuExitException struct {
	Syndrome        uint64
	VirtualAddress  uint64
	PhysicalAddress IPA
}

type VcpuExit struct {
	Reason    ExitReason
	_         uint32
	Exception VcpuExitException
}

const (
	hvSuccess             Return      = 0
	hvError               Return      = -0x516bfff
	hvBusy                Return      = -0x516bffe
	hvBadArgument         Return      = -0x516bffd
	hvIllegalGuestState   Return      = -0x516bffc
	hvNoResources         Return      = -0x516bffb
	hvNoDevice            Return      = -0x516bffa
	hvDenied              Return      = -0x516bff9
	hvUnsupported         Return      = -0x516bff1
	hvMemoryRead          MemoryFlags = 0x1
	hvMemoryWrite         MemoryFlags = 0x2
	hvMemoryExec          MemoryFlags = 0x4
	hvExitReasonException ExitReason  = 1
	hvRegX0               Reg         = 0
	hvRegX1               Reg         = 1
	hvRegX2               Reg         = 2
	hvRegX3               Reg         = 3
	hvRegX4               Reg         = 4
	hvRegX5               Reg         = 5
	hvRegX6               Reg         = 6
	hvRegX7               Reg         = 7
	hvRegX8               Reg         = 8
	hvRegX9               Reg         = 9
	hvRegX10              Reg         = 10
	hvRegX11              Reg         = 11
	hvRegX12              Reg         = 12
	hvRegX13              Reg         = 13
	hvRegX14              Reg         = 14
	hvRegX15              Reg         = 15
	hvRegX16              Reg         = 16
	hvRegX17              Reg         = 17
	hvRegX18              Reg         = 18
	hvRegX19              Reg         = 19
	hvRegX20              Reg         = 20
	hvRegX21              Reg         = 21
	hvRegX22              Reg         = 22
	hvRegX23              Reg         = 23
	hvRegX24              Reg         = 24
	hvRegX25              Reg         = 25
	hvRegX26              Reg         = 26
	hvRegX27              Reg         = 27
	hvRegX28              Reg         = 28
	hvRegX29              Reg         = 29
	hvRegX30              Reg         = 30
	hvRegPC               Reg         = 31
	hvRegCPSR             Reg         = 34
	hvRegXZR              Reg         = 0xffffffff

	hvSysRegSP_EL1   SysReg = 0xe208
	hvSysRegTTBR1EL1 SysReg = 0xc101

	hvGICICCRegPMR_EL1     GICICCReg = 0xc230
	hvGICICCRegCTLR_EL1    GICICCReg = 0xc664
	hvGICICCRegSRE_EL1     GICICCReg = 0xc665
	hvGICICCRegIGRPEN0_EL1 GICICCReg = 0xc666
	hvGICICCRegIGRPEN1_EL1 GICICCReg = 0xc667
)

var (
	loadOnce sync.Once
	loadErr  error
	hvLib    uintptr
	sysLib   uintptr

	hvVMConfigCreate func() VMConfig
	hvVMCreate       func(config VMConfig) Return
	hvVMDestroy      func() Return
	hvVMMap          func(addr unsafe.Pointer, ipa IPA, size uintptr, flags MemoryFlags) Return
	hvVMUnmap        func(ipa IPA, size uintptr) Return

	hvVcpuConfigCreate func() VcpuConfig
	hvVcpuCreate       func(vcpu *VCPU, exit **VcpuExit, config VcpuConfig) Return
	hvVcpuDestroy      func(vcpu VCPU) Return
	hvVcpuGetReg       func(vcpu VCPU, reg Reg, value *uint64) Return
	hvVcpuSetReg       func(vcpu VCPU, reg Reg, value uint64) Return
	hvVcpuGetSysReg    func(vcpu VCPU, reg SysReg, value *uint64) Return
	hvVcpuSetSysReg    func(vcpu VCPU, reg SysReg, value uint64) Return
	hvVcpuRun          func(vcpu VCPU) Return

	hvGICConfigCreate                  func() GICConfig
	hvGICGetDistributorReg             func(reg GICDistributorReg, value *uint64) Return
	hvGICSetDistributorReg             func(reg GICDistributorReg, value uint64) Return
	hvGICGetRedistributorBase          func(vcpu VCPU, redistributorBaseAddress *IPA) Return
	hvGICGetRedistributorReg           func(vcpu VCPU, reg GICRedistributorReg, value *uint64) Return
	hvGICSetRedistributorReg           func(vcpu VCPU, reg GICRedistributorReg, value uint64) Return
	hvGICGetICCReg                     func(vcpu VCPU, reg GICICCReg, value *uint64) Return
	hvGICSetICCReg                     func(vcpu VCPU, reg GICICCReg, value uint64) Return
	hvGICConfigSetDistributorBase      func(config GICConfig, distributorBase IPA) Return
	hvGICConfigSetRedistributorBase    func(config GICConfig, redistributorBase IPA) Return
	hvGICGetDistributorBaseAlignment   func(alignment *uintptr) Return
	hvGICGetRedistributorBaseAlignment func(alignment *uintptr) Return
	hvGICCreate                        func(config GICConfig) Return

	osRelease func(obj uintptr)
)

func load() error {
	loadOnce.Do(func() {
		var err error
		hvLib, err = purego.Dlopen("/System/Library/Frameworks/Hypervisor.framework/Hypervisor", purego.RTLD_GLOBAL|purego.RTLD_LAZY)
		if err != nil {
			loadErr = fmt.Errorf("open Hypervisor.framework: %w", err)
			return
		}

		sysLib, err = purego.Dlopen("/usr/lib/libSystem.B.dylib", purego.RTLD_GLOBAL|purego.RTLD_LAZY)
		if err != nil {
			loadErr = fmt.Errorf("open libSystem: %w", err)
			return
		}

		purego.RegisterLibFunc(&hvVMConfigCreate, hvLib, "hv_vm_config_create")
		purego.RegisterLibFunc(&hvVMCreate, hvLib, "hv_vm_create")
		purego.RegisterLibFunc(&hvVMDestroy, hvLib, "hv_vm_destroy")
		purego.RegisterLibFunc(&hvVMMap, hvLib, "hv_vm_map")
		purego.RegisterLibFunc(&hvVMUnmap, hvLib, "hv_vm_unmap")

		purego.RegisterLibFunc(&hvVcpuConfigCreate, hvLib, "hv_vcpu_config_create")
		purego.RegisterLibFunc(&hvVcpuCreate, hvLib, "hv_vcpu_create")
		purego.RegisterLibFunc(&hvVcpuDestroy, hvLib, "hv_vcpu_destroy")
		purego.RegisterLibFunc(&hvVcpuGetReg, hvLib, "hv_vcpu_get_reg")
		purego.RegisterLibFunc(&hvVcpuSetReg, hvLib, "hv_vcpu_set_reg")
		purego.RegisterLibFunc(&hvVcpuGetSysReg, hvLib, "hv_vcpu_get_sys_reg")
		purego.RegisterLibFunc(&hvVcpuSetSysReg, hvLib, "hv_vcpu_set_sys_reg")
		purego.RegisterLibFunc(&hvVcpuRun, hvLib, "hv_vcpu_run")

		purego.RegisterLibFunc(&hvGICConfigCreate, hvLib, "hv_gic_config_create")
		purego.RegisterLibFunc(&hvGICGetDistributorReg, hvLib, "hv_gic_get_distributor_reg")
		purego.RegisterLibFunc(&hvGICSetDistributorReg, hvLib, "hv_gic_set_distributor_reg")
		purego.RegisterLibFunc(&hvGICGetRedistributorBase, hvLib, "hv_gic_get_redistributor_base")
		purego.RegisterLibFunc(&hvGICGetRedistributorReg, hvLib, "hv_gic_get_redistributor_reg")
		purego.RegisterLibFunc(&hvGICSetRedistributorReg, hvLib, "hv_gic_set_redistributor_reg")
		purego.RegisterLibFunc(&hvGICGetICCReg, hvLib, "hv_gic_get_icc_reg")
		purego.RegisterLibFunc(&hvGICSetICCReg, hvLib, "hv_gic_set_icc_reg")
		purego.RegisterLibFunc(&hvGICConfigSetDistributorBase, hvLib, "hv_gic_config_set_distributor_base")
		purego.RegisterLibFunc(&hvGICConfigSetRedistributorBase, hvLib, "hv_gic_config_set_redistributor_base")
		purego.RegisterLibFunc(&hvGICGetDistributorBaseAlignment, hvLib, "hv_gic_get_distributor_base_alignment")
		purego.RegisterLibFunc(&hvGICGetRedistributorBaseAlignment, hvLib, "hv_gic_get_redistributor_base_alignment")
		purego.RegisterLibFunc(&hvGICCreate, hvLib, "hv_gic_create")

		purego.RegisterLibFunc(&osRelease, sysLib, "os_release")
	})
	return loadErr
}

type VM struct {
	vcpu     VCPU
	exitInfo *VcpuExit
	mappings []mapping
	threadCh chan func()
}

type mapping struct {
	ipa       IPA
	size      uintptr
	mem       []byte
	anonymous bool
}

func NewVM() (*VM, error) {
	if err := load(); err != nil {
		return nil, err
	}

	v := &VM{
		threadCh: make(chan func()),
	}
	go v.threadMain()

	errCh := make(chan error, 1)
	v.threadCh <- func() {
		vmCfg := hvVMConfigCreate()
		ret := createVMWithRetry(vmCfg)
		if ret != hvSuccess {
			osRelease(uintptr(vmCfg))
			errCh <- fmt.Errorf("create vm: %w", ret)
			return
		}
		osRelease(uintptr(vmCfg))

		if err := createMinimalGIC(); err != nil {
			_ = hvVMDestroy()
			errCh <- err
			return
		}

		vcpuCfg := hvVcpuConfigCreate()
		var id VCPU
		exitInfo := new(VcpuExit)
		if ret := hvVcpuCreate(&id, &exitInfo, vcpuCfg); ret != hvSuccess {
			osRelease(uintptr(vcpuCfg))
			_ = hvVMDestroy()
			errCh <- fmt.Errorf("create vcpu: %w", ret)
			return
		}
		osRelease(uintptr(vcpuCfg))

		v.vcpu = id
		v.exitInfo = exitInfo
		errCh <- initMinimalGICCPUInterface(v)
	}
	if err := <-errCh; err != nil {
		close(v.threadCh)
		return nil, err
	}
	return v, nil
}

func createVMWithRetry(cfg VMConfig) Return {
	const (
		maxAttempts = 100
		retryDelay  = 50 * time.Millisecond
	)
	for attempt := 0; attempt < maxAttempts; attempt++ {
		ret := hvVMCreate(cfg)
		if ret != hvBusy {
			return ret
		}
		time.Sleep(retryDelay)
	}
	return hvBusy
}

func createMinimalGIC() error {
	var distAlign uintptr
	if ret := hvGICGetDistributorBaseAlignment(&distAlign); ret != hvSuccess {
		return fmt.Errorf("get gic distributor alignment: %w", ret)
	}
	var redistAlign uintptr
	if ret := hvGICGetRedistributorBaseAlignment(&redistAlign); ret != hvSuccess {
		return fmt.Errorf("get gic redistributor alignment: %w", ret)
	}

	const distributorBase IPA = 0x08000000
	const redistributorBase IPA = 0x080a0000

	if distAlign != 0 && uintptr(distributorBase)%distAlign != 0 {
		return fmt.Errorf("gic distributor base %#x not aligned to %#x", distributorBase, distAlign)
	}
	if redistAlign != 0 && uintptr(redistributorBase)%redistAlign != 0 {
		return fmt.Errorf("gic redistributor base %#x not aligned to %#x", redistributorBase, redistAlign)
	}

	cfg := hvGICConfigCreate()
	if ret := hvGICConfigSetDistributorBase(cfg, distributorBase); ret != hvSuccess {
		osRelease(uintptr(cfg))
		return fmt.Errorf("set gic distributor base: %w", ret)
	}
	if ret := hvGICConfigSetRedistributorBase(cfg, redistributorBase); ret != hvSuccess {
		osRelease(uintptr(cfg))
		return fmt.Errorf("set gic redistributor base: %w", ret)
	}
	if ret := hvGICCreate(cfg); ret != hvSuccess {
		osRelease(uintptr(cfg))
		return fmt.Errorf("create gic: %w", ret)
	}
	osRelease(uintptr(cfg))
	return nil
}

func initMinimalGICCPUInterface(v *VM) error {
	if err := v.setGICICCRegOnOwnerThread(hvGICICCRegSRE_EL1, 0x1); err != nil {
		return err
	}
	if err := v.setGICICCRegOnOwnerThread(hvGICICCRegPMR_EL1, 0xff); err != nil {
		return err
	}
	if err := v.setGICICCRegOnOwnerThread(hvGICICCRegCTLR_EL1, 0x0); err != nil {
		return err
	}
	if err := v.setGICICCRegOnOwnerThread(hvGICICCRegIGRPEN0_EL1, 0x1); err != nil {
		return err
	}
	if err := v.setGICICCRegOnOwnerThread(hvGICICCRegIGRPEN1_EL1, 0x1); err != nil {
		return err
	}
	return nil
}

func (v *VM) MapMemory(mem []byte, ipa IPA, flags MemoryFlags) error {
	if len(mem) == 0 {
		return fmt.Errorf("memory mapping cannot be empty")
	}
	errCh := make(chan error, 1)
	v.threadCh <- func() {
		if ret := hvVMMap(unsafe.Pointer(&mem[0]), ipa, uintptr(len(mem)), flags); ret != hvSuccess {
			errCh <- fmt.Errorf("map memory: %w", ret)
			return
		}
		v.mappings = append(v.mappings, mapping{ipa: ipa, size: uintptr(len(mem)), mem: mem})
		errCh <- nil
	}
	return <-errCh
}

func (v *VM) MapAnonymousMemory(size uintptr, ipa IPA, flags MemoryFlags) ([]byte, error) {
	mem, err := syscall.Mmap(-1, 0, int(size), syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_ANON|syscall.MAP_PRIVATE)
	if err != nil {
		return nil, fmt.Errorf("allocate anonymous memory: %w", err)
	}
	if err := v.MapMemory(mem, ipa, flags); err != nil {
		_ = syscall.Munmap(mem)
		return nil, err
	}
	done := make(chan struct{}, 1)
	v.threadCh <- func() {
		if len(v.mappings) > 0 {
			v.mappings[len(v.mappings)-1].anonymous = true
		}
		done <- struct{}{}
	}
	<-done
	return mem, nil
}

func (v *VM) SetReg(reg Reg, value uint64) error {
	errCh := make(chan error, 1)
	v.threadCh <- func() {
		if ret := hvVcpuSetReg(v.vcpu, reg, value); ret != hvSuccess {
			errCh <- fmt.Errorf("set reg %d: %w", reg, ret)
			return
		}
		errCh <- nil
	}
	return <-errCh
}

func (v *VM) GetReg(reg Reg) (uint64, error) {
	respCh := make(chan struct {
		val uint64
		err error
	}, 1)
	v.threadCh <- func() {
		var value uint64
		if ret := hvVcpuGetReg(v.vcpu, reg, &value); ret != hvSuccess {
			respCh <- struct {
				val uint64
				err error
			}{err: fmt.Errorf("get reg %d: %w", reg, ret)}
			return
		}
		respCh <- struct {
			val uint64
			err error
		}{val: value}
	}
	res := <-respCh
	return res.val, res.err
}

func (v *VM) GetSysReg(reg SysReg) (uint64, error) {
	respCh := make(chan struct {
		val uint64
		err error
	}, 1)
	v.threadCh <- func() {
		var value uint64
		if ret := hvVcpuGetSysReg(v.vcpu, reg, &value); ret != hvSuccess {
			respCh <- struct {
				val uint64
				err error
			}{err: fmt.Errorf("get sys reg %d: %w", reg, ret)}
			return
		}
		respCh <- struct {
			val uint64
			err error
		}{val: value}
	}
	res := <-respCh
	return res.val, res.err
}

func (v *VM) GetGICDistributorReg(reg GICDistributorReg) (uint64, error) {
	respCh := make(chan struct {
		val uint64
		err error
	}, 1)
	v.threadCh <- func() {
		var value uint64
		if ret := hvGICGetDistributorReg(reg, &value); ret != hvSuccess {
			respCh <- struct {
				val uint64
				err error
			}{err: fmt.Errorf("get gic distributor reg %#x: %w", reg, ret)}
			return
		}
		respCh <- struct {
			val uint64
			err error
		}{val: value}
	}
	res := <-respCh
	return res.val, res.err
}

func (v *VM) SetGICDistributorReg(reg GICDistributorReg, value uint64) error {
	errCh := make(chan error, 1)
	v.threadCh <- func() {
		if ret := hvGICSetDistributorReg(reg, value); ret != hvSuccess {
			errCh <- fmt.Errorf("set gic distributor reg %#x: %w", reg, ret)
			return
		}
		errCh <- nil
	}
	return <-errCh
}

func (v *VM) GetGICRedistributorReg(reg GICRedistributorReg) (uint64, error) {
	respCh := make(chan struct {
		val uint64
		err error
	}, 1)
	v.threadCh <- func() {
		var value uint64
		if ret := hvGICGetRedistributorReg(v.vcpu, reg, &value); ret != hvSuccess {
			respCh <- struct {
				val uint64
				err error
			}{err: fmt.Errorf("get gic redistributor reg %#x: %w", reg, ret)}
			return
		}
		respCh <- struct {
			val uint64
			err error
		}{val: value}
	}
	res := <-respCh
	return res.val, res.err
}

func (v *VM) SetGICRedistributorReg(reg GICRedistributorReg, value uint64) error {
	errCh := make(chan error, 1)
	v.threadCh <- func() {
		if ret := hvGICSetRedistributorReg(v.vcpu, reg, value); ret != hvSuccess {
			errCh <- fmt.Errorf("set gic redistributor reg %#x: %w", reg, ret)
			return
		}
		errCh <- nil
	}
	return <-errCh
}

func (v *VM) GetGICICCReg(reg GICICCReg) (uint64, error) {
	respCh := make(chan struct {
		val uint64
		err error
	}, 1)
	v.threadCh <- func() {
		value, err := v.getGICICCRegOnOwnerThread(reg)
		respCh <- struct {
			val uint64
			err error
		}{val: value, err: err}
	}
	res := <-respCh
	return res.val, res.err
}

func (v *VM) getGICICCRegOnOwnerThread(reg GICICCReg) (uint64, error) {
	var value uint64
	if ret := hvGICGetICCReg(v.vcpu, reg, &value); ret != hvSuccess {
		return 0, fmt.Errorf("get gic icc reg %#x: %w", reg, ret)
	}
	return value, nil
}

func (v *VM) SetGICICCReg(reg GICICCReg, value uint64) error {
	errCh := make(chan error, 1)
	v.threadCh <- func() {
		errCh <- v.setGICICCRegOnOwnerThread(reg, value)
	}
	return <-errCh
}

func (v *VM) setGICICCRegOnOwnerThread(reg GICICCReg, value uint64) error {
	if ret := hvGICSetICCReg(v.vcpu, reg, value); ret != hvSuccess {
		return fmt.Errorf("set gic icc reg %#x: %w", reg, ret)
	}
	return nil
}

func (v *VM) SetSysReg(reg SysReg, value uint64) error {
	errCh := make(chan error, 1)
	v.threadCh <- func() {
		if ret := hvVcpuSetSysReg(v.vcpu, reg, value); ret != hvSuccess {
			errCh <- fmt.Errorf("set sys reg %d: %w", reg, ret)
			return
		}
		errCh <- nil
	}
	return <-errCh
}

func (v *VM) Run() (*VcpuExit, error) {
	type result struct {
		exit *VcpuExit
		err  error
	}
	resCh := make(chan result, 1)
	v.threadCh <- func() {
		if ret := hvVcpuRun(v.vcpu); ret != hvSuccess {
			resCh <- result{err: fmt.Errorf("run vcpu: %w", ret)}
			return
		}
		resCh <- result{exit: v.exitInfo}
	}
	res := <-resCh
	return res.exit, res.err
}

func (v *VM) threadMain() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	for fn := range v.threadCh {
		fn()
	}
}

func (v *VM) Close() error {
	var firstErr error
	if v.vcpu != 0 {
		errCh := make(chan error, 1)
		v.threadCh <- func() {
			if ret := hvVcpuDestroy(v.vcpu); ret != hvSuccess {
				errCh <- fmt.Errorf("destroy vcpu: %w", ret)
				return
			}
			errCh <- nil
		}
		if err := <-errCh; err != nil && firstErr == nil {
			firstErr = err
		}
		v.vcpu = 0
	}
	for _, m := range v.mappings {
		m := m
		errCh := make(chan error, 1)
		v.threadCh <- func() {
			if ret := hvVMUnmap(m.ipa, m.size); ret != hvSuccess {
				errCh <- fmt.Errorf("unmap memory: %w", ret)
				return
			}
			errCh <- nil
		}
		if err := <-errCh; err != nil && firstErr == nil {
			firstErr = err
		}
		if m.anonymous {
			if err := syscall.Munmap(m.mem); err != nil && firstErr == nil {
				firstErr = fmt.Errorf("munmap memory: %w", err)
			}
		}
	}
	v.mappings = nil
	errCh := make(chan error, 1)
	v.threadCh <- func() {
		if ret := hvVMDestroy(); ret != hvSuccess {
			errCh <- fmt.Errorf("destroy vm: %w", ret)
			return
		}
		errCh <- nil
	}
	if err := <-errCh; err != nil && firstErr == nil {
		firstErr = err
	}
	close(v.threadCh)
	return firstErr
}

type ExceptionClass uint64

const (
	ExceptionClassHVC64            ExceptionClass = 0x16
	ExceptionClassDataAbortLowerEL ExceptionClass = 0x24
)

type DataAbortInfo struct {
	SizeBytes int
	Write     bool
	Target    Reg
}

func DecodeExceptionClass(syndrome uint64) ExceptionClass {
	return ExceptionClass((syndrome >> 26) & 0x3f)
}

func DecodeDataAbort(syndrome uint64) (DataAbortInfo, error) {
	const (
		dataAbortISSMask uint64 = (1 << 25) - 1
		isvBit                  = 24
		sasShift                = 22
		sasMask          uint64 = 0x3
		srtShift                = 16
		srtMask          uint64 = 0x1f
		wnrBit                  = 6
	)

	iss := syndrome & dataAbortISSMask
	if ((iss >> isvBit) & 0x1) == 0 {
		return DataAbortInfo{}, fmt.Errorf("data abort without ISV set: syndrome=0x%x", syndrome)
	}
	sas := (iss >> sasShift) & sasMask
	if sas > 3 {
		return DataAbortInfo{}, fmt.Errorf("invalid SAS value %d", sas)
	}
	srt := int((iss >> srtShift) & srtMask)
	write := ((iss >> wnrBit) & 0x1) == 1

	var reg Reg
	switch {
	case srt >= 0 && srt <= 31:
		reg = Reg(srt)
		if srt == 31 {
			reg = hvRegXZR
		}
	default:
		return DataAbortInfo{}, fmt.Errorf("unsupported register index %d", srt)
	}

	return DataAbortInfo{
		SizeBytes: 1 << sas,
		Write:     write,
		Target:    reg,
	}, nil
}

func (v *VM) AdvanceProgramCounter() error {
	pc, err := v.GetReg(hvRegPC)
	if err != nil {
		return err
	}
	return v.SetReg(hvRegPC, pc+4)
}
