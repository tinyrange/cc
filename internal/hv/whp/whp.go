//go:build windows

package whp

import (
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"time"
	"unsafe"

	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/hv/whp/bindings"
)

type virtualCPU struct {
	vm       *virtualMachine
	id       int
	runQueue chan func()

	firstTickDone bool

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

type w struct {
}

func (w) Write(p []byte) (n int, err error) {
	// console escape noop
	os.Stdout.WriteString("\x1b[0m")
	return len(p), nil
}

// Run implements hv.VirtualCPU.
func (v *virtualCPU) Run(ctx context.Context) error {
	var exit bindings.RunVPExitContext

	// HORRIBLE HACK TO GET BRING UP WORKING
	if !v.firstTickDone {
		slog.NewJSONHandler(w{}, nil).Handle(context.Background(), slog.NewRecord(time.Now(), slog.LevelDebug, "", 0))
		v.firstTickDone = true
	}

	if err := bindings.RunVirtualProcessorContext(v.vm.part, uint32(v.id), &exit); err != nil {
		return fmt.Errorf("whp: RunVirtualProcessorContext failed: %w", err)
	}

	switch exit.ExitReason {
	case bindings.RunVPExitReasonX64Halt:
		return hv.ErrVMHalted
	case bindings.RunVPExitReasonMemoryAccess:
		mem := exit.MemoryAccess()

		var status bindings.EmulatorStatus

		v.pendingError = nil

		// Attempt to emulate the instruction that caused the memory access exit.
		// The emulator will call the registered callbacks (MemoryCallback, TranslateGva, etc.)
		// to fetch instructions and perform the memory operation.
		if err := bindings.EmulatorTryMmioEmulation(
			v.vm.emu,
			unsafe.Pointer(v),
			&exit.VpContext,
			mem,
			&status,
		); err != nil {
			return fmt.Errorf("EmulatorTryMmioEmulation failed: %w", err)
		}

		// Check if an error occurred inside a callback (e.g. MMIO write failure)
		if v.pendingError != nil {
			return v.pendingError
		}

		// If the emulator returns success but the status indicates failure (0),
		// it usually means it couldn't fetch the instruction or the instruction
		// didn't match the exit reason.
		if !status.EmulationSuccessful() {
			// We return a detailed error to help debugging, including the RIP and GPA
			rip := exit.VpContext.Rip
			gpa := mem.Gpa
			return fmt.Errorf("whp: emulation failed (Status=%v) at RIP=0x%x accessing GPA=0x%x.", status, rip, gpa)
		}

		return nil

	case bindings.RunVPExitReasonX64IoPortAccess:
		io := exit.IoPortAccess()

		var status bindings.EmulatorStatus

		v.pendingError = nil
		if err := bindings.EmulatorTryIoEmulation(
			v.vm.emu,
			unsafe.Pointer(v),
			&exit.VpContext,
			io,
			&status,
		); err != nil {
			return fmt.Errorf("EmulatorTryIoEmulation failed: %w", err)
		}

		if v.pendingError != nil {
			return v.pendingError
		}

		if !status.EmulationSuccessful() {
			return fmt.Errorf("whp: io emulation failed with status %v at port 0x%x", status, io.Port)
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

func (v *virtualCPU) handleIOPortAccess(access *bindings.EmulatorIOAccessInfo) error {
	if access.AccessSize != 1 {
		return fmt.Errorf("whp: unsupported IO port access size %d", access.AccessSize)
	}

	var data [4]byte
	binary.LittleEndian.PutUint32(data[:], access.Data[0])

	for _, dev := range v.vm.devices {
		if kvmIoPortDevice, ok := dev.(hv.X86IOPortDevice); ok {
			ports := kvmIoPortDevice.IOPorts()
			for _, port := range ports {
				if port == access.Port {
					if access.Direction == 0 {
						if err := kvmIoPortDevice.ReadIOPort(access.Port, data[:access.AccessSize]); err != nil {
							return fmt.Errorf("I/O port 0x%04x read: %w", access.Port, err)
						}
						access.Data[0] = uint32(binary.LittleEndian.Uint32(data[:]))
					} else {
						if err := kvmIoPortDevice.WriteIOPort(access.Port, data[:access.AccessSize]); err != nil {
							return fmt.Errorf("I/O port 0x%04x write: %w", access.Port, err)
						}
						access.Data[0] = uint32(binary.LittleEndian.Uint32(data[:]))
					}
					return nil
				}
			}
		}
	}

	return fmt.Errorf("whp: unhandled IO port access at port 0x%X", access.Port)
}

func (v *virtualCPU) handleMemoryAccess(access *bindings.EmulatorMemoryAccessInfo) error {
	gpa := uint64(access.GpaAddress)
	size := uint64(access.AccessSize)

	// Access the Data field safely.
	// Assuming bindings define Data as [8]uint8.
	// We use the slice operator to get a view of the array in the struct.
	dataSlice := access.Data[:size]

	// 1. RAM Access (Instruction Fetch or Data Access)
	if gpa >= v.vm.memoryBase && gpa < v.vm.memoryBase+uint64(v.vm.memory.Size()) {
		offset := gpa - v.vm.memoryBase

		// Bounds check
		if offset+size > uint64(v.vm.memory.Size()) {
			return fmt.Errorf("whp: memory access out of bounds: gpa=0x%x size=%d", gpa, size)
		}

		ram := v.vm.memory.Slice()

		if access.Direction == bindings.EmulatorMemoryAccessDirectionRead {
			// Copy FROM RAM -> TO Emulator
			copy(dataSlice, ram[offset:offset+size])
		} else {
			// Copy FROM Emulator -> TO RAM
			copy(ram[offset:offset+size], dataSlice)
		}

		return nil
	}

	// 2. MMIO Device Access
	for _, dev := range v.vm.devices {
		if kvmMmioDevice, ok := dev.(hv.MemoryMappedIODevice); ok {
			regions := kvmMmioDevice.MMIORegions()
			for _, region := range regions {
				if gpa >= region.Address && gpa+size <= region.Address+region.Size {
					if access.Direction == bindings.EmulatorMemoryAccessDirectionRead {
						if err := kvmMmioDevice.ReadMMIO(gpa, dataSlice); err != nil {
							return fmt.Errorf("MMIO read at 0x%016x: %w", gpa, err)
						}
						// dataSlice is backed by access.Data, so writing to it updates the struct
					} else {
						if err := kvmMmioDevice.WriteMMIO(gpa, dataSlice); err != nil {
							return fmt.Errorf("MMIO write at 0x%016x: %w", gpa, err)
						}
					}
					return nil
				}
			}
		}
	}

	return fmt.Errorf("whp: unhandled memory access at guest physical address 0x%X", gpa)
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

	emu bindings.EmulatorHandle
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
		vcpu := (*virtualCPU)(ctx)

		if err := vcpu.handleIOPortAccess(access); err != nil {
			vcpu.pendingError = err
			return bindings.HRESULTFail
		}

		return 0
	}

	memFn := func(
		ctx unsafe.Pointer,
		access *bindings.EmulatorMemoryAccessInfo,
	) bindings.HRESULT {
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

		// Note: We use the backing store (bindings.GetVirtualProcessorRegisters) to populate the emulator.
		if err := bindings.GetVirtualProcessorRegisters(vcpu.vm.part, uint32(vcpu.id), names, values); err != nil {
			// Panic here is risky but acceptable if we can't get registers the emulator critically needs.
			panic(fmt.Sprintf("GetVirtualProcessorRegisters failed: %v", err))
		}

		return 0
	}

	setFn := func(
		ctx unsafe.Pointer,
		names []bindings.RegisterName,
		values []bindings.RegisterValue,
	) bindings.HRESULT {
		vcpu := (*virtualCPU)(ctx)

		if err := bindings.SetVirtualProcessorRegisters(vcpu.vm.part, uint32(vcpu.id), names, values); err != nil {
			panic(fmt.Sprintf("SetVirtualProcessorRegisters failed: %v", err))
		}

		return 0
	}

	translateFn := func(
		ctx unsafe.Pointer,
		gva bindings.GuestVirtualAddress,
		flags bindings.TranslateGVAFlags,
		result *bindings.TranslateGVAResultCode,
		gpa *bindings.GuestPhysicalAddress,
	) bindings.HRESULT {
		vcpu := (*virtualCPU)(ctx)

		var tResult bindings.TranslateGVAResult
		var tGPA bindings.GuestPhysicalAddress

		// Ensure we pass flags correctly, specifically allowing override if needed by the bindings.
		if err := bindings.TranslateGVA(
			vcpu.vm.part,
			uint32(vcpu.id),
			gva,
			flags,
			&tResult,
			&tGPA,
		); err != nil {
			// Panic is too strong here, but bindings panic if we return Fail usually.
			// Ideally we log this.
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

	emu, err := createEmulator()
	if err != nil {
		bindings.DeletePartition(vm.part)
		return nil, fmt.Errorf("failed to create emulator for vm: %w", err)
	}
	vm.emu = emu

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
