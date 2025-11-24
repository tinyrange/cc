//go:build windows

package whp

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
	"unsafe"

	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/hv/whp/bindings"
)

type virtualCPU struct {
	vm       *virtualMachine
	id       int
	runQueue chan func()
	emu      bindings.EmulatorHandle

	pendingError error
}

func (v *virtualCPU) start() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	for fn := range v.runQueue {
		fn()
	}
}

// implements hv.VirtualCPU.
func (v *virtualCPU) ID() int                           { return v.id }
func (v *virtualCPU) VirtualMachine() hv.VirtualMachine { return v.vm }

// GetRegisters implements hv.VirtualCPU.
func (v *virtualCPU) GetRegisters(regs map[hv.Register]hv.RegisterValue) error {
	var names []bindings.RegisterName

	for reg := range regs {
		name, ok := whpRegisterMap[reg]
		if !ok {
			return fmt.Errorf("whp: unsupported register %v", reg)
		}
		names = append(names, name)
	}

	values := make([]bindings.RegisterValue, len(names))

	if err := bindings.GetVirtualProcessorRegisters(v.vm.part, uint32(v.id), names, values); err != nil {
		return fmt.Errorf("whp: GetVirtualProcessorRegisters failed: %w", err)
	}

	for i, name := range names {
		var reg hv.Register
		for r, n := range whpRegisterMap {
			if n == name {
				reg = r
				break
			}
		}
		switch val := regs[reg].(type) {
		case hv.Register64:
			val = hv.Register64(*values[i].AsUint64())
			regs[reg] = val
		default:
			return fmt.Errorf("whp: unsupported register value type %T for register %v", val, reg)
		}
	}

	return nil
}

// Run implements hv.VirtualCPU.
func (v *virtualCPU) Run(ctx context.Context) error {
	var exit bindings.RunVPExitContext

	if err := bindings.RunVirtualProcessorContext(v.vm.part, uint32(v.id), &exit); err != nil {
		return fmt.Errorf("whp: RunVirtualProcessorContext failed: %w", err)
	}

	switch exit.ExitReason {
	case bindings.RunVPExitReasonX64Halt:
		return hv.ErrVMHalted
	case bindings.RunVPExitReasonMemoryAccess:
		// Handle memory access via the emulator

		mem := exit.MemoryAccess()

		var status bindings.EmulatorStatus

		v.pendingError = nil
		if err := bindings.EmulatorTryMmioEmulation(
			v.emu,
			unsafe.Pointer(v),
			&exit.VpContext,
			mem,
			&status,
		); err != nil {
			return fmt.Errorf("EmulatorTryMmioEmulation failed: %w", err)
		}

		if v.pendingError != nil {
			return v.pendingError
		}

		if status == 0 {
			return nil
		}

		if status&bindings.EmulatorStatusSuccess == 0 {
			return fmt.Errorf("whp: invalid emulator status %s", status)
		}

		return nil
	default:
		return fmt.Errorf("whp: unsupported vCPU exit reason %s", exit.ExitReason)
	}
}

var whpRegisterMap = map[hv.Register]bindings.RegisterName{
	hv.RegisterAMD64Rax:    bindings.RegisterRax,
	hv.RegisterAMD64Rbx:    bindings.RegisterRbx,
	hv.RegisterAMD64Rcx:    bindings.RegisterRcx,
	hv.RegisterAMD64Rdx:    bindings.RegisterRdx,
	hv.RegisterAMD64Rsi:    bindings.RegisterRsi,
	hv.RegisterAMD64Rdi:    bindings.RegisterRdi,
	hv.RegisterAMD64Rsp:    bindings.RegisterRsp,
	hv.RegisterAMD64Rbp:    bindings.RegisterRbp,
	hv.RegisterAMD64R8:     bindings.RegisterR8,
	hv.RegisterAMD64R9:     bindings.RegisterR9,
	hv.RegisterAMD64R10:    bindings.RegisterR10,
	hv.RegisterAMD64R11:    bindings.RegisterR11,
	hv.RegisterAMD64R12:    bindings.RegisterR12,
	hv.RegisterAMD64R13:    bindings.RegisterR13,
	hv.RegisterAMD64R14:    bindings.RegisterR14,
	hv.RegisterAMD64R15:    bindings.RegisterR15,
	hv.RegisterAMD64Rip:    bindings.RegisterRip,
	hv.RegisterAMD64Rflags: bindings.RegisterRflags,
}

// SetRegisters implements hv.VirtualCPU.
func (v *virtualCPU) SetRegisters(regs map[hv.Register]hv.RegisterValue) error {
	var names []bindings.RegisterName
	var values []bindings.RegisterValue

	for reg, val := range regs {
		name, ok := whpRegisterMap[reg]
		if !ok {
			return fmt.Errorf("whp: unsupported register %v", reg)
		}
		names = append(names, name)

		value := bindings.RegisterValue{}
		switch val := val.(type) {
		case hv.Register64:
			value.SetUint64(uint64(val))
		default:
			return fmt.Errorf("whp: unsupported register value type %T for register %v", val, reg)
		}
		values = append(values, value)
	}

	return bindings.SetVirtualProcessorRegisters(v.vm.part, uint32(v.id), names, values)
}

func (v *virtualCPU) handleMemoryAccess(access *bindings.EmulatorMemoryAccessInfo) error {
	// 1. Check if the access falls within the Guest RAM range
	if uint64(access.GpaAddress) >= v.vm.memoryBase && uint64(access.GpaAddress) < v.vm.memoryBase+uint64(v.vm.memory.Size()) {
		offset := uint64(access.GpaAddress) - v.vm.memoryBase
		length := uint64(access.AccessSize)

		// Bounds check (just to be safe)
		if offset+length > uint64(v.vm.memory.Size()) {
			return fmt.Errorf("whp: memory access out of bounds")
		}

		ram := v.vm.memory.Slice()

		if access.Direction == bindings.EmulatorMemoryAccessDirectionRead {
			// Read: Copy FROM RAM -> TO Emulator buffer
			copy(access.Data[:length], ram[offset:offset+length])
		} else {
			// Write: Copy FROM Emulator buffer -> TO RAM
			copy(ram[offset:offset+length], access.Data[:length])
		}

		return nil
	}

	// 2. Logging for actual unhandled MMIO
	slog.Info("memory access",
		"gpa_address", fmt.Sprintf("0x%X", access.GpaAddress),
		"direction", access.Direction,
		"access_size", access.AccessSize,
	)

	return fmt.Errorf("whp: unhandled memory access at guest physical address 0x%X", access.GpaAddress)
}

var (
	_ hv.VirtualCPU = &virtualCPU{}
)

type virtualMachine struct {
	hv   *hypervisor
	part bindings.PartitionHandle

	vcpus map[int]*virtualCPU

	memory     *bindings.Allocation
	memoryBase uint64

	devices []hv.Device
}

// Hypervisor implements hv.VirtualMachine.
func (v *virtualMachine) Hypervisor() hv.Hypervisor { return v.hv }

// AddDevice implements hv.VirtualMachine.
func (v *virtualMachine) AddDevice(dev hv.Device) error {
	v.devices = append(v.devices, dev)

	return dev.Init(v)
}

// Close implements hv.VirtualMachine.
func (v *virtualMachine) Close() error {
	if v.memory != nil {
		v.memory = nil
	}
	return bindings.DeletePartition(v.part)
}

// Run implements hv.VirtualMachine.
func (v *virtualMachine) Run(ctx context.Context, cfg hv.RunConfig) error {
	if cfg == nil {
		return fmt.Errorf("whp: RunConfig cannot be nil")
	}

	vcpu, ok := v.vcpus[0]
	if !ok {
		return fmt.Errorf("whp: no vCPU 0 found")
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

// VirtualCPUCall implements hv.VirtualMachine.
func (v *virtualMachine) VirtualCPUCall(id int, f func(vcpu hv.VirtualCPU) error) error {
	vcpu, ok := v.vcpus[id]
	if !ok {
		return fmt.Errorf("whp: no vCPU %d found", id)
	}

	done := make(chan error, 1)

	vcpu.runQueue <- func() {
		done <- f(vcpu)
	}

	return <-done
}

// WriteAt implements hv.VirtualMachine.
func (v *virtualMachine) WriteAt(p []byte, off int64) (n int, err error) {
	if off < 0 || uint64(off) >= v.memory.Size() {
		return 0, fmt.Errorf("whp: WriteAt offset out of bounds")
	}

	n = copy(v.memory.Slice()[off:], p)
	if n < len(p) {
		err = fmt.Errorf("whp: WriteAt short write")
	}
	return n, err
}

// ReadAt implements hv.VirtualMachine.
func (v *virtualMachine) ReadAt(p []byte, off int64) (n int, err error) {
	if off < 0 || uint64(off) >= v.memory.Size() {
		return 0, fmt.Errorf("whp: ReadAt offset out of bounds")
	}

	n = copy(p, v.memory.Slice()[off:])
	if n < len(p) {
		err = fmt.Errorf("whp: ReadAt short read")
	}
	return n, err
}

var (
	_ hv.VirtualMachine = &virtualMachine{}
)

type hypervisor struct{}

// Close implements hv.Hypervisor.
func (h *hypervisor) Close() error {
	return nil
}

func createEmulator() (bindings.EmulatorHandle, error) {
	ioFn := func(ctx unsafe.Pointer, access *bindings.EmulatorIOAccessInfo) bindings.HRESULT {
		panic("NewEmulatorIoPortCallback unimplemented")
	}
	memFn := func(ctx unsafe.Pointer, access *bindings.EmulatorMemoryAccessInfo) bindings.HRESULT {
		vcpu := (*virtualCPU)(ctx)

		if err := vcpu.handleMemoryAccess(access); err != nil {
			vcpu.pendingError = err
			return bindings.HRESULTFail
		}

		return 0
	}
	getFn := func(
		ctx unsafe.Pointer,
		names []bindings.RegisterName,
		values []bindings.RegisterValue,
	) bindings.HRESULT {
		vcpu := (*virtualCPU)(ctx)

		if err := bindings.GetVirtualProcessorRegisters(vcpu.vm.part, uint32(vcpu.id), names, values); err != nil {
			panic(fmt.Sprintf("GetVirtualProcessorRegisters failed: %v", err))
		}

		return 0
	}
	setFn := func(ctx unsafe.Pointer, names []bindings.RegisterName, values []bindings.RegisterValue) bindings.HRESULT {
		vcpu := (*virtualCPU)(ctx)

		if err := bindings.SetVirtualProcessorRegisters(vcpu.vm.part, uint32(vcpu.id), names, values); err != nil {
			panic(fmt.Sprintf("SetVirtualProcessorRegisters failed: %v", err))
		}

		return 0
	}
	translateFn := func(ctx unsafe.Pointer, gva bindings.GuestVirtualAddress, flags bindings.TranslateGVAFlags, result *bindings.TranslateGVAResultCode, gpa *bindings.GuestPhysicalAddress) bindings.HRESULT {
		vcpu := (*virtualCPU)(ctx)

		var tResult bindings.TranslateGVAResult
		var tGPA bindings.GuestPhysicalAddress

		if err := bindings.TranslateGVA(
			vcpu.vm.part,
			uint32(vcpu.id),
			gva,
			flags,
			&tResult,
			&tGPA,
		); err != nil {
			panic(fmt.Sprintf("TranslateGvaPage failed: %v", err))
		}

		*result = tResult.ResultCode
		*gpa = tGPA

		return 0
	}

	emu, err := bindings.EmulatorCreate(&bindings.EmulatorCallbacks{
		IoPortCallback:               bindings.NewEmulatorIoPortCallback(ioFn),
		MemoryCallback:               bindings.NewEmulatorMemoryCallback(memFn),
		GetVirtualProcessorRegisters: bindings.NewEmulatorGetRegistersCallback(getFn),
		SetVirtualProcessorRegisters: bindings.NewEmulatorSetRegistersCallback(setFn),
		TranslateGvaPage:             bindings.NewEmulatorTranslateGvaCallback(translateFn),
	})
	if err != nil {
		return 0, fmt.Errorf("failed to create emulator: %w", err)
	}

	return emu, nil
}

// NewVirtualMachine implements hv.Hypervisor.
func (h *hypervisor) NewVirtualMachine(config hv.VMConfig) (hv.VirtualMachine, error) {
	vm := &virtualMachine{
		hv:    h,
		vcpus: make(map[int]*virtualCPU),
	}

	part, err := bindings.CreatePartition()
	if err != nil {
		return nil, fmt.Errorf("whp: CreatePartition failed: %w", err)
	}
	vm.part = part

	if err := bindings.SetPartitionPropertyUnsafe(
		vm.part,
		bindings.PartitionPropertyCodeProcessorCount,
		uint32(config.CPUCount()),
	); err != nil {
		bindings.DeletePartition(vm.part)
		return nil, fmt.Errorf("whp: SetPartitionPropertyUnsafe failed: %w", err)
	}

	if err := h.archVMInit(vm, config); err != nil {
		return nil, fmt.Errorf("whp: archVMInit failed: %w", err)
	}

	if err := config.Callbacks().OnCreateVM(vm); err != nil {
		bindings.DeletePartition(vm.part)
		return nil, fmt.Errorf("VM callback OnCreateVM: %w", err)
	}

	if err := bindings.SetupPartition(vm.part); err != nil {
		bindings.DeletePartition(vm.part)
		return nil, fmt.Errorf("whp: SetupPartition failed: %w", err)
	}

	// Allocate guest memory
	if config.MemorySize() == 0 {
		bindings.DeletePartition(vm.part)
		return nil, fmt.Errorf("kvm: memory size must be greater than 0")
	}

	mem, err := bindings.VirtualAlloc(
		0,
		uintptr(config.MemorySize()),
		bindings.MEM_RESERVE|bindings.MEM_COMMIT,
		bindings.PAGE_EXECUTE_READWRITE,
	)
	if err != nil {
		bindings.DeletePartition(vm.part)
		return nil, fmt.Errorf("whp: VirtualAlloc failed: %w", err)
	}

	vm.memory = mem
	vm.memoryBase = config.MemoryBase()

	if err := bindings.MapGPARange(
		vm.part,
		vm.memory.Pointer(),
		bindings.GuestPhysicalAddress(vm.memoryBase),
		uint64(vm.memory.Size()),
		bindings.MapGPARangeFlagRead|bindings.MapGPARangeFlagWrite|bindings.MapGPARangeFlagExecute,
	); err != nil {
		bindings.DeletePartition(vm.part)
		return nil, fmt.Errorf("whp: MapGPARange failed: %w", err)
	}

	// Create vCPUs
	if config.CPUCount() != 1 {
		bindings.DeletePartition(vm.part)
		return nil, fmt.Errorf("kvm: only 1 vCPU supported, got %d", config.CPUCount())
	}

	for i := range config.CPUCount() {
		if err := bindings.CreateVirtualProcessor(
			vm.part,
			uint32(i),
			0,
		); err != nil {
			bindings.DeletePartition(vm.part)
			return nil, fmt.Errorf("whp: CreateVirtualProcessor failed: %w", err)
		}

		vcpu := &virtualCPU{
			vm:       vm,
			id:       i,
			runQueue: make(chan func(), 16),
		}

		emu, err := createEmulator()
		if err != nil {
			bindings.DeletePartition(vm.part)
			return nil, fmt.Errorf("failed to create emulator for vCPU %d: %w", i, err)
		}
		vcpu.emu = emu

		vm.vcpus[i] = vcpu

		if err := h.archVCPUInit(vm, vcpu); err != nil {
			bindings.DeletePartition(vm.part)
			return nil, fmt.Errorf("initialize VM: %w", err)
		}

		go vcpu.start()

		if err := config.Callbacks().OnCreateVCPU(vcpu); err != nil {
			bindings.DeletePartition(vm.part)
			return nil, fmt.Errorf("VM callback OnCreateVCPU %d: %w", i, err)
		}
	}

	// Run Loader
	loader := config.Loader()

	if loader != nil {
		if err := loader.Load(vm); err != nil {
			bindings.DeletePartition(vm.part)
			return nil, fmt.Errorf("load VM: %w", err)
		}
	}

	return vm, nil
}

func Open() (hv.Hypervisor, error) {
	return &hypervisor{}, nil
}
