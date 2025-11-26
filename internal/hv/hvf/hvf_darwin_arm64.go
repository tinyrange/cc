//go:build darwin && arm64

package hvf

import (
	"context"
	"encoding/binary"
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/tinyrange/cc/internal/hv"
)

const (
	arm64InstructionSizeBytes = 4
	psciSystemOffFunctionID   = 0x84000008
)

type hypervisor struct{}

func (*hypervisor) Close() error { return nil }

func (*hypervisor) Architecture() hv.CpuArchitecture {
	return hv.ArchitectureARM64
}

func (h *hypervisor) newVirtualMachine(config hv.VMConfig) (*virtualMachine, error) {
	if config == nil {
		return nil, fmt.Errorf("hvf: VMConfig is nil")
	}

	if config.CPUCount() != 1 {
		return nil, fmt.Errorf("hvf: only 1 vCPU is supported (requested %d)", config.CPUCount())
	}

	memSize := config.MemorySize()
	if memSize == 0 {
		return nil, fmt.Errorf("hvf: memory size must be greater than 0")
	}

	pageSize := uint64(unix.Getpagesize())
	if memSize%pageSize != 0 {
		return nil, fmt.Errorf("hvf: memory size (%d) must be aligned to host page size (%d)", memSize, pageSize)
	}

	if err := ensureInitialized(); err != nil {
		return nil, err
	}

	if err := hvVmCreate(0).toError("hv_vm_create"); err != nil {
		return nil, err
	}

	vm := &virtualMachine{
		hv:         h,
		vcpus:      make(map[int]*virtualCPU),
		memoryBase: config.MemoryBase(),
	}
	vm.vmCreated = true

	mem, err := unix.Mmap(
		-1,
		0,
		int(memSize),
		unix.PROT_READ|unix.PROT_WRITE,
		unix.MAP_ANON|unix.MAP_PRIVATE,
	)
	if err != nil {
		vm.closeInternal()
		return nil, fmt.Errorf("hvf: mmap guest memory: %w", err)
	}
	vm.memory = mem

	if err := hvVmMap(unsafe.Pointer(&mem[0]), vm.memoryBase, memSize, hvMemoryRead|hvMemoryWrite|hvMemoryExec).toError("hv_vm_map"); err != nil {
		vm.closeInternal()
		return nil, err
	}
	vm.memoryMapped = true

	if err := config.Callbacks().OnCreateVM(vm); err != nil {
		vm.closeInternal()
		return nil, fmt.Errorf("hvf: VM callback OnCreateVM: %w", err)
	}

	for i := 0; i < config.CPUCount(); i++ {
		vcpu, err := vm.createVCPU(i)
		if err != nil {
			vm.closeInternal()
			return nil, err
		}
		vm.vcpus[i] = vcpu

		if err := config.Callbacks().OnCreateVCPU(vcpu); err != nil {
			vm.closeInternal()
			return nil, fmt.Errorf("hvf: VM callback OnCreateVCPU %d: %w", i, err)
		}
	}

	if loader := config.Loader(); loader != nil {
		if err := loader.Load(vm); err != nil {
			vm.closeInternal()
			return nil, fmt.Errorf("hvf: load VM: %w", err)
		}
	}

	return vm, nil
}

func (h *hypervisor) NewVirtualMachine(config hv.VMConfig) (hv.VirtualMachine, error) {
	return h.newVirtualMachine(config)
}

type virtualMachine struct {
	hv         hv.Hypervisor
	vcpus      map[int]*virtualCPU
	memory     []byte
	memoryBase uint64
	devices    []hv.Device

	vmCreated    bool
	memoryMapped bool

	closeOnce sync.Once
}

func (v *virtualMachine) Hypervisor() hv.Hypervisor { return v.hv }

func (v *virtualMachine) AddDevice(dev hv.Device) error {
	v.devices = append(v.devices, dev)
	return dev.Init(v)
}

func (v *virtualMachine) Run(ctx context.Context, cfg hv.RunConfig) error {
	if cfg == nil {
		return fmt.Errorf("hvf: RunConfig is nil")
	}

	if _, ok := ctx.Deadline(); ok {
		return fmt.Errorf("hvf: Run does not support context deadlines or timeouts")
	}

	vcpu, ok := v.vcpus[0]
	if !ok {
		return fmt.Errorf("hvf: vCPU 0 not found")
	}

	done := make(chan error, 1)
	vcpu.runQueue <- func() {
		done <- cfg.Run(ctx, vcpu)
	}

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (v *virtualMachine) VirtualCPUCall(id int, f func(vcpu hv.VirtualCPU) error) error {
	vcpu, ok := v.vcpus[id]
	if !ok {
		return fmt.Errorf("hvf: vCPU %d not found", id)
	}

	done := make(chan error, 1)
	vcpu.runQueue <- func() {
		done <- f(vcpu)
	}
	return <-done
}

func (v *virtualMachine) ReadAt(p []byte, off int64) (int, error) {
	offset := off - int64(v.memoryBase)
	if offset < 0 || int(offset) >= len(v.memory) {
		return 0, fmt.Errorf("hvf: ReadAt offset 0x%x out of bounds", off)
	}

	n := copy(p, v.memory[offset:])
	if n < len(p) {
		return n, fmt.Errorf("hvf: ReadAt short read")
	}
	return n, nil
}

func (v *virtualMachine) WriteAt(p []byte, off int64) (int, error) {
	offset := off - int64(v.memoryBase)
	if offset < 0 || int(offset) >= len(v.memory) {
		return 0, fmt.Errorf("hvf: WriteAt offset 0x%x out of bounds", off)
	}

	n := copy(v.memory[offset:], p)
	if n < len(p) {
		return n, fmt.Errorf("hvf: WriteAt short write")
	}
	return n, nil
}

func (v *virtualMachine) Close() error {
	var closeErr error

	v.closeOnce.Do(func() {
		closeErr = v.closeInternal()
	})

	return closeErr
}

func (v *virtualMachine) closeInternal() error {
	var firstErr error

	for _, vcpu := range v.vcpus {
		if err := vcpu.shutdown(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	v.vcpus = nil

	if v.memoryMapped {
		if err := hvVmUnmap(v.memoryBase, uint64(len(v.memory))).toError("hv_vm_unmap"); err != nil && firstErr == nil {
			firstErr = err
		}
		v.memoryMapped = false
	}

	if v.memory != nil {
		if err := unix.Munmap(v.memory); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("hvf: munmap memory: %w", err)
		}
		v.memory = nil
	}

	if v.vmCreated {
		if err := hvVmDestroy().toError("hv_vm_destroy"); err != nil && firstErr == nil {
			firstErr = err
		}
		v.vmCreated = false
	}

	return firstErr
}

func (v *virtualMachine) createVCPU(id int) (*virtualCPU, error) {
	vcpu := &virtualCPU{
		vm:       v,
		index:    id,
		runQueue: make(chan func(), 16),
		initErr:  make(chan error, 1),
	}

	go vcpu.start()

	if err := <-vcpu.initErr; err != nil {
		return nil, err
	}

	return vcpu, nil
}

type virtualCPU struct {
	vm       *virtualMachine
	hostID   uint64
	index    int
	exit     *hvVcpuExit
	runQueue chan func()
	initErr  chan error
}

func (v *virtualCPU) start() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	var exitPtr *hvVcpuExit
	var hostID uint64
	ret := hvVcpuCreate(&hostID, &exitPtr, 0)
	if err := ret.toError("hv_vcpu_create"); err != nil {
		v.initErr <- err
		close(v.initErr)
		close(v.runQueue)
		return
	}
	v.hostID = hostID
	v.exit = exitPtr
	v.initErr <- nil
	close(v.initErr)

	for fn := range v.runQueue {
		fn()
	}
}

func (v *virtualCPU) shutdown() error {
	if v.runQueue == nil {
		return nil
	}

	done := make(chan error, 1)
	v.runQueue <- func() {
		done <- hvVcpuDestroy(v.hostID).toError("hv_vcpu_destroy")
	}
	close(v.runQueue)
	return <-done
}

func (v *virtualCPU) VirtualMachine() hv.VirtualMachine { return v.vm }
func (v *virtualCPU) ID() int                           { return v.index }

func (v *virtualCPU) SetRegisters(regs map[hv.Register]hv.RegisterValue) error {
	for reg, value := range regs {
		raw, ok := value.(hv.Register64)
		if !ok {
			return fmt.Errorf("hvf: unsupported register value type %T for %v", value, reg)
		}

		if sys, ok := hvSysRegFromRegister(reg); ok {
			if err := hvVcpuSetSys(v.hostID, sys, uint64(raw)).toError("hv_vcpu_set_sys_reg"); err != nil {
				return err
			}
			continue
		}

		hreg, ok := hvRegFromRegister(reg)
		if !ok {
			return fmt.Errorf("hvf: unsupported register %v", reg)
		}
		if err := hvVcpuSetReg(v.hostID, hreg, uint64(raw)).toError("hv_vcpu_set_reg"); err != nil {
			return err
		}
	}
	return nil
}

func (v *virtualCPU) GetRegisters(regs map[hv.Register]hv.RegisterValue) error {
	for reg := range regs {
		if sys, ok := hvSysRegFromRegister(reg); ok {
			var val uint64
			if err := hvVcpuGetSys(v.hostID, sys, &val).toError("hv_vcpu_get_sys_reg"); err != nil {
				return err
			}
			regs[reg] = hv.Register64(val)
			continue
		}

		hreg, ok := hvRegFromRegister(reg)
		if !ok {
			return fmt.Errorf("hvf: unsupported register %v", reg)
		}

		var val uint64
		if err := hvVcpuGetReg(v.hostID, hreg, &val).toError("hv_vcpu_get_reg"); err != nil {
			return err
		}
		regs[reg] = hv.Register64(val)
	}
	return nil
}

func (v *virtualCPU) Run(ctx context.Context) error {
	if err := hvVcpuRun(v.hostID).toError("hv_vcpu_run"); err != nil {
		return err
	}

	switch v.exit.Reason {
	case hvExitReasonCanceled:
		return context.Canceled
	case hvExitReasonException:
		return v.handleException()
	case hvExitReasonVTimerActivated, hvExitReasonVTimerDeactivated:
		return nil
	default:
		return fmt.Errorf("hvf: unsupported vCPU exit reason %d", v.exit.Reason)
	}
}

func (v *virtualCPU) handleException() error {
	const (
		exceptionClassMask             = 0x3F
		exceptionClassShift            = 26
		exceptionClassHvc              = 0x16
		exceptionClassSmc              = 0x17
		exceptionClassDataAbortLowerEL = 0x24
	)

	syndrome := v.exit.Exception.Syndrome
	ec := (syndrome >> exceptionClassShift) & exceptionClassMask

	switch ec {
	case exceptionClassHvc, exceptionClassSmc:
		return v.handleHypercall()
	case exceptionClassDataAbortLowerEL:
		return v.handleDataAbort(syndrome, v.exit.Exception.PhysicalAddress, v.exit.Exception.VirtualAddress)
	default:
		return fmt.Errorf("hvf: unsupported exception class 0x%x (syndrome=0x%x)", ec, syndrome)
	}
}

func (v *virtualCPU) handleHypercall() error {
	val, err := v.readRegister(hv.RegisterARM64X0)
	if err != nil {
		return err
	}

	if err := v.advanceProgramCounter(); err != nil {
		return err
	}

	switch val {
	case psciSystemOffFunctionID:
		return hv.ErrVMHalted
	default:
		return fmt.Errorf("hvf: unhandled hypercall 0x%x", val)
	}
}

type dataAbortInfo struct {
	sizeBytes int
	write     bool
	target    hv.Register
}

func decodeDataAbort(syndrome uint64) (dataAbortInfo, error) {
	const (
		dataAbortISSMask uint64 = (1 << 25) - 1
		isvBit                  = 24
		sasShift                = 22
		sasMask          uint64 = 0x3
		srtShift                = 16
		srtMask          uint64 = 0x1F
		wnrBit                  = 6
	)

	iss := syndrome & dataAbortISSMask
	if ((iss >> isvBit) & 0x1) == 0 {
		return dataAbortInfo{}, fmt.Errorf("hvf: data abort without ISV set (syndrome=0x%x)", syndrome)
	}

	sas := (iss >> sasShift) & sasMask
	size := 1 << sas
	if sas > 3 {
		return dataAbortInfo{}, fmt.Errorf("hvf: invalid SAS value %d", sas)
	}

	srt := int((iss >> srtShift) & srtMask)
	reg, ok := arm64RegisterFromIndex(srt)
	if !ok {
		return dataAbortInfo{}, fmt.Errorf("hvf: unsupported data abort target register index %d", srt)
	}

	write := ((iss >> wnrBit) & 0x1) == 1

	return dataAbortInfo{
		sizeBytes: int(size),
		write:     write,
		target:    reg,
	}, nil
}

func (v *virtualCPU) handleDataAbort(syndrome, physAddr, virtAddr uint64) error {
	access, err := decodeDataAbort(syndrome)
	if err != nil {
		return err
	}

	addr := physAddr
	if addr == 0 {
		addr = virtAddr
	}

	dev, err := v.findMMIODevice(addr, uint64(access.sizeBytes))
	if err != nil {
		return err
	}

	data := make([]byte, access.sizeBytes)
	if access.write {
		value, err := v.readRegister(access.target)
		if err != nil {
			return err
		}
		for i := 0; i < access.sizeBytes; i++ {
			data[i] = byte(value >> (8 * i))
		}

		if err := dev.WriteMMIO(addr, data); err != nil {
			return fmt.Errorf("hvf: MMIO write 0x%x (%d bytes): %w", addr, access.sizeBytes, err)
		}
	} else {
		if err := dev.ReadMMIO(addr, data); err != nil {
			return fmt.Errorf("hvf: MMIO read 0x%x (%d bytes): %w", addr, access.sizeBytes, err)
		}

		var tmp [8]byte
		copy(tmp[:], data)
		value := binary.LittleEndian.Uint64(tmp[:])
		if err := v.writeRegister(access.target, value); err != nil {
			return err
		}
	}

	return v.advanceProgramCounter()
}

func (v *virtualCPU) findMMIODevice(addr, size uint64) (hv.MemoryMappedIODevice, error) {
	for _, dev := range v.vm.devices {
		mmio, ok := dev.(hv.MemoryMappedIODevice)
		if !ok {
			continue
		}
		for _, region := range mmio.MMIORegions() {
			if addr >= region.Address && addr+size <= region.Address+region.Size {
				return mmio, nil
			}
		}
	}
	return nil, fmt.Errorf("hvf: no MMIO device handles address 0x%x (size=%d)", addr, size)
}

func (v *virtualCPU) readRegister(reg hv.Register) (uint64, error) {
	if sys, ok := hvSysRegFromRegister(reg); ok {
		var val uint64
		if err := hvVcpuGetSys(v.hostID, sys, &val).toError("hv_vcpu_get_sys_reg"); err != nil {
			return 0, err
		}
		return val, nil
	}

	hreg, ok := hvRegFromRegister(reg)
	if !ok {
		return 0, fmt.Errorf("hvf: unsupported register %v", reg)
	}

	var val uint64
	if err := hvVcpuGetReg(v.hostID, hreg, &val).toError("hv_vcpu_get_reg"); err != nil {
		return 0, err
	}
	return val, nil
}

func (v *virtualCPU) writeRegister(reg hv.Register, value uint64) error {
	if sys, ok := hvSysRegFromRegister(reg); ok {
		return hvVcpuSetSys(v.hostID, sys, value).toError("hv_vcpu_set_sys_reg")
	}

	hreg, ok := hvRegFromRegister(reg)
	if !ok {
		return fmt.Errorf("hvf: unsupported register %v", reg)
	}
	return hvVcpuSetReg(v.hostID, hreg, value).toError("hv_vcpu_set_reg")
}

func (v *virtualCPU) advanceProgramCounter() error {
	pc, err := v.readRegister(hv.RegisterARM64Pc)
	if err != nil {
		return fmt.Errorf("hvf: read PC: %w", err)
	}
	return v.writeRegister(hv.RegisterARM64Pc, pc+arm64InstructionSizeBytes)
}

var (
	_ hv.Hypervisor     = (*hypervisor)(nil)
	_ hv.VirtualCPU     = (*virtualCPU)(nil)
	_ hv.VirtualMachine = (*virtualMachine)(nil)
)

func Open() (hv.Hypervisor, error) {
	if err := ensureInitialized(); err != nil {
		return nil, err
	}
	return &hypervisor{}, nil
}
