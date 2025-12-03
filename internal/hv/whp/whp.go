//go:build windows && (amd64 || arm64)

package whp

import (
	"context"
	"encoding/binary"
	"fmt"
	"runtime"

	"github.com/tinyrange/cc/internal/devices/amd64/chipset"
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

	hv.RegisterARM64X0:       bindings.Arm64RegisterX0,
	hv.RegisterARM64X1:       bindings.Arm64RegisterX1,
	hv.RegisterARM64X2:       bindings.Arm64RegisterX2,
	hv.RegisterARM64X3:       bindings.Arm64RegisterX3,
	hv.RegisterARM64X4:       bindings.Arm64RegisterX4,
	hv.RegisterARM64X5:       bindings.Arm64RegisterX5,
	hv.RegisterARM64X6:       bindings.Arm64RegisterX6,
	hv.RegisterARM64X7:       bindings.Arm64RegisterX7,
	hv.RegisterARM64X8:       bindings.Arm64RegisterX8,
	hv.RegisterARM64X9:       bindings.Arm64RegisterX9,
	hv.RegisterARM64X10:      bindings.Arm64RegisterX10,
	hv.RegisterARM64X11:      bindings.Arm64RegisterX11,
	hv.RegisterARM64X12:      bindings.Arm64RegisterX12,
	hv.RegisterARM64X13:      bindings.Arm64RegisterX13,
	hv.RegisterARM64X14:      bindings.Arm64RegisterX14,
	hv.RegisterARM64X15:      bindings.Arm64RegisterX15,
	hv.RegisterARM64X16:      bindings.Arm64RegisterX16,
	hv.RegisterARM64X17:      bindings.Arm64RegisterX17,
	hv.RegisterARM64X18:      bindings.Arm64RegisterX18,
	hv.RegisterARM64X19:      bindings.Arm64RegisterX19,
	hv.RegisterARM64X20:      bindings.Arm64RegisterX20,
	hv.RegisterARM64X21:      bindings.Arm64RegisterX21,
	hv.RegisterARM64X22:      bindings.Arm64RegisterX22,
	hv.RegisterARM64X23:      bindings.Arm64RegisterX23,
	hv.RegisterARM64X24:      bindings.Arm64RegisterX24,
	hv.RegisterARM64X25:      bindings.Arm64RegisterX25,
	hv.RegisterARM64X26:      bindings.Arm64RegisterX26,
	hv.RegisterARM64X27:      bindings.Arm64RegisterX27,
	hv.RegisterARM64X28:      bindings.Arm64RegisterX28,
	hv.RegisterARM64Sp:       bindings.Arm64RegisterSp,
	hv.RegisterARM64Pc:       bindings.Arm64RegisterPc,
	hv.RegisterARM64Pstate:   bindings.Arm64RegisterPstate,
	hv.RegisterARM64Vbar:     bindings.Arm64RegisterVbarEl1,
	hv.RegisterARM64GicrBase: bindings.Arm64RegisterGicrBaseGpa,
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
	if access.AccessSize != 1 && access.AccessSize != 2 && access.AccessSize != 4 {
		return fmt.Errorf("whp: unsupported IO port access size %d", access.AccessSize)
	}

	// slog.Info(
	// 	"vCPU I/O port access",
	// 	"port", fmt.Sprintf("0x%04X", access.Port),
	// 	"size", access.AccessSize,
	// 	"direction", access.Direction,
	// )

	// Buffer to bridge the gap between WHP's uint32 Data and Go's []byte device interfaces.
	// We initialize it to zero to ensure clean upper bytes when reading partial sizes.
	var data [4]byte

	for _, dev := range v.vm.devices {
		if kvmIoPortDevice, ok := dev.(hv.X86IOPortDevice); ok {
			ports := kvmIoPortDevice.IOPorts()
			for _, port := range ports {
				if port == access.Port {
					if access.Direction == bindings.EmulatorIOAccessDirectionIn {
						// READ: Device -> Emulator (Guest IN instruction)

						// Pass a slice of the exact size requested to the device.
						if err := kvmIoPortDevice.ReadIOPort(access.Port, data[:access.AccessSize]); err != nil {
							return fmt.Errorf("I/O port 0x%04x read: %w", access.Port, err)
						}

						// Convert the byte slice back to uint32 for the C struct.
						// Since 'data' was zeroed, we can safely read the whole uint32
						// even if AccessSize < 4.
						access.Data = binary.LittleEndian.Uint32(data[:])

					} else {
						// WRITE: Emulator -> Device (Guest OUT instruction)

						// Put the C uint32 data into the byte buffer.
						binary.LittleEndian.PutUint32(data[:], access.Data)

						// Pass the relevant bytes to the device.
						if err := kvmIoPortDevice.WriteIOPort(access.Port, data[:access.AccessSize]); err != nil {
							return fmt.Errorf("I/O port 0x%04x write: %w", access.Port, err)
						}
						// For writes, we don't need to update access.Data.
					}
					return nil
				}
			}
		}
	}

	return fmt.Errorf("whp: unhandled IO port access at port 0x%X", access.Port)
}

func (v *virtualCPU) handleMemoryAccess(access *bindings.EmulatorMemoryAccessInfo) error {
	gpa := access.GpaAddress
	size := uint64(access.AccessSize)

	// access.Data is now [8]byte in the bindings, backed directly by C memory.
	// We slice it to the operation size to read/write directly without extra allocation.
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
			// Read: RAM -> Emulator (Guest Load)
			copy(dataSlice, ram[offset:offset+size])
		} else {
			// Write: Emulator -> RAM (Guest Store)
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

					// Virtio Console debug logging
					// if gpa >= 0xd000_0000 && gpa <= 0xd000_00ff {
					// 	fmt.Printf("Virtio Console Config Access GPA=%x Size=%d Dir=%v\n", gpa, size, access.Direction)
					// }

					if access.Direction == bindings.EmulatorMemoryAccessDirectionRead {
						// Read: Device -> Emulator
						if err := kvmMmioDevice.ReadMMIO(gpa, dataSlice); err != nil {
							return fmt.Errorf("MMIO read at 0x%016x: %w", gpa, err)
						}
					} else {
						// Write: Emulator -> Device
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

	ioapic *chipset.IOAPIC
	// arm64GICInfo caches the configured interrupt controller details when available.
	arm64GICInfo hv.Arm64GICInfo
}

// implements hv.VirtualMachine.
func (v *virtualMachine) MemoryBase() uint64        { return v.memoryBase }
func (v *virtualMachine) MemorySize() uint64        { return uint64(v.memory.Size()) }
func (v *virtualMachine) Hypervisor() hv.Hypervisor { return v.hv }

// AddDevice implements hv.VirtualMachine.
func (v *virtualMachine) AddDevice(dev hv.Device) error {
	v.devices = append(v.devices, dev)

	return dev.Init(v)
}

// AddDeviceFromTemplate implements hv.VirtualMachine.
func (v *virtualMachine) AddDeviceFromTemplate(template hv.DeviceTemplate) error {
	dev, err := template.Create(v)
	if err != nil {
		return fmt.Errorf("create device from template: %w", err)
	}

	return v.AddDevice(dev)
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
	offset := off - int64(v.memoryBase)
	if offset < 0 || uint64(offset) >= v.memory.Size() {
		return 0, fmt.Errorf("whp: WriteAt offset out of bounds")
	}

	n = copy(v.memory.Slice()[offset:], p)
	if n < len(p) {
		err = fmt.Errorf("whp: WriteAt short write")
	}
	return n, err
}

// ReadAt implements hv.VirtualMachine.
func (v *virtualMachine) ReadAt(p []byte, off int64) (n int, err error) {
	offset := off - int64(v.memoryBase)
	if offset < 0 || uint64(offset) >= v.memory.Size() {
		return 0, fmt.Errorf("whp: ReadAt offset out of bounds")
	}

	n = copy(p, v.memory.Slice()[offset:])
	if n < len(p) {
		err = fmt.Errorf("whp: ReadAt short read")
	}
	return n, err
}

var (
	_ hv.VirtualMachine   = &virtualMachine{}
	_ hv.Arm64GICProvider = &virtualMachine{}
)

func (v *virtualMachine) Arm64GICInfo() (hv.Arm64GICInfo, bool) {
	if v == nil || v.arm64GICInfo.Version == hv.Arm64GICVersionUnknown {
		return hv.Arm64GICInfo{}, false
	}
	return v.arm64GICInfo, true
}

type hypervisor struct{}

// Close implements hv.Hypervisor.
func (h *hypervisor) Close() error {
	return nil
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

	if err := h.archVMInitWithMemory(vm, config); err != nil {
		return nil, fmt.Errorf("whp: archVMInit failed: %w", err)
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
	present, err := bindings.IsHypervisorPresent()
	if err != nil {
		return nil, fmt.Errorf("whp: check hypervisor present: %w", err)
	}

	if !present {
		return nil, fmt.Errorf("whp: hypervisor not present")
	}

	return &hypervisor{}, nil
}
