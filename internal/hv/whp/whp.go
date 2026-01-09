//go:build windows && (amd64 || arm64)

package whp

import (
	"context"
	"encoding/binary"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"

	corechipset "github.com/tinyrange/cc/internal/chipset"
	"github.com/tinyrange/cc/internal/devices/amd64/chipset"
	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/hv/whp/bindings"
	"github.com/tinyrange/cc/internal/timeslice"
)

var (
	tsWhpStartTime = time.Now()
)

var (
	tsWhpHostTime    = timeslice.RegisterKind("whp_host_time", 0)
	tsWhpGuestTime   = timeslice.RegisterKind("whp_guest_time", 0)
	tsWhpUnknownExit = timeslice.RegisterKind("whp_unknown_exit", 0)
)

type exitContext struct {
	timeslice timeslice.TimesliceID
}

func (c *exitContext) SetExitTimeslice(id timeslice.TimesliceID) {
	c.timeslice = id
}

type virtualCPU struct {
	rec      *timeslice.Recorder
	vm       *virtualMachine
	id       int
	runQueue chan func()
	done     chan struct{} // closed when the vCPU goroutine exits

	pendingError error

	exitCtx *exitContext
}

func (v *virtualCPU) start() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(v.done)

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
	hv.RegisterARM64X29:      bindings.Arm64RegisterFp,
	hv.RegisterARM64X30:      bindings.Arm64RegisterLr,
	hv.RegisterARM64Sp:       bindings.Arm64RegisterSp,
	hv.RegisterARM64Pc:       bindings.Arm64RegisterPc,
	hv.RegisterARM64Pstate:   bindings.Arm64RegisterPstate,
	hv.RegisterARM64Vbar:     bindings.Arm64RegisterVbarEl1,
	hv.RegisterARM64GicrBase: bindings.Arm64RegisterGicrBaseGpa,

	// ARM64 system registers for snapshots
	hv.RegisterARM64SctlrEl1:      bindings.Arm64RegisterSctlrEl1,
	hv.RegisterARM64TcrEl1:        bindings.Arm64RegisterTcrEl1,
	hv.RegisterARM64Ttbr0El1:      bindings.Arm64RegisterTtbr0El1,
	hv.RegisterARM64Ttbr1El1:      bindings.Arm64RegisterTtbr1El1,
	hv.RegisterARM64MairEl1:       bindings.Arm64RegisterMairEl1,
	hv.RegisterARM64ElrEl1:        bindings.Arm64RegisterElrEl1,
	hv.RegisterARM64SpsrEl1:       bindings.Arm64RegisterSpsrEl1,
	hv.RegisterARM64EsrEl1:        bindings.Arm64RegisterEsrEl1,
	hv.RegisterARM64FarEl1:        bindings.Arm64RegisterFarEl1,
	hv.RegisterARM64SpEl0:         bindings.Arm64RegisterSpEl0,
	hv.RegisterARM64SpEl1:         bindings.Arm64RegisterSpEl1,
	hv.RegisterARM64CntkctlEl1:    bindings.Arm64RegisterCntkctlEl1,
	hv.RegisterARM64CntvCtlEl0:    bindings.Arm64RegisterCntvCtlEl0,
	hv.RegisterARM64CntvCvalEl0:   bindings.Arm64RegisterCntvCvalEl0,
	hv.RegisterARM64CpacrEl1:      bindings.Arm64RegisterCpacrEl1,
	hv.RegisterARM64ContextidrEl1: bindings.Arm64RegisterContextidrEl1,
	hv.RegisterARM64TpidrEl0:      bindings.Arm64RegisterTpidrEl0,
	hv.RegisterARM64TpidrEl1:      bindings.Arm64RegisterTpidrEl1,
	hv.RegisterARM64TpidrroEl0:    bindings.Arm64RegisterTpidrroEl0,
	hv.RegisterARM64ParEl1:        bindings.Arm64RegisterParEl1,
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

func (v *virtualCPU) handleIOPortAccess(exitCtx *exitContext, access *bindings.EmulatorIOAccessInfo) error {
	if access.AccessSize != 1 && access.AccessSize != 2 && access.AccessSize != 4 {
		return fmt.Errorf("whp: unsupported IO port access size %d", access.AccessSize)
	}

	cs, err := v.vm.ensureChipset()
	if err != nil {
		return fmt.Errorf("initialize chipset: %w", err)
	}

	// Buffer to bridge the gap between WHP's uint32 Data and Go's []byte device interfaces.
	// We initialize it to zero to ensure clean upper bytes when reading partial sizes.
	var data [4]byte

	if access.Direction != bindings.EmulatorIOAccessDirectionIn {
		// WRITE: Put the C uint32 data into the byte buffer.
		binary.LittleEndian.PutUint32(data[:], access.Data)
	}

	isWrite := access.Direction != bindings.EmulatorIOAccessDirectionIn
	if err := cs.HandlePIO(exitCtx, access.Port, data[:access.AccessSize], isWrite); err != nil {
		return fmt.Errorf("I/O port 0x%04x: %w", access.Port, err)
	}

	if access.Direction == bindings.EmulatorIOAccessDirectionIn {
		// READ: Convert the byte slice back to uint32 for the C struct.
		// Since 'data' was zeroed, we can safely read the whole uint32
		// even if AccessSize < 4.
		access.Data = binary.LittleEndian.Uint32(data[:])
	}

	return nil
}

func (v *virtualCPU) handleMemoryAccess(exitCtx *exitContext, access *bindings.EmulatorMemoryAccessInfo) error {
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

	// 2. MMIO Device Access via chipset
	cs, err := v.vm.ensureChipset()
	if err != nil {
		return fmt.Errorf("initialize chipset: %w", err)
	}

	isWrite := access.Direction != bindings.EmulatorMemoryAccessDirectionRead
	if err := cs.HandleMMIO(exitCtx, gpa, dataSlice, isWrite); err != nil {
		return fmt.Errorf("MMIO at 0x%016x: %w", gpa, err)
	}
	return nil
}

var (
	_ hv.VirtualCPU = &virtualCPU{}
)

type memoryRegion struct {
	memory *bindings.Allocation
}

// Size implements hv.MemoryRegion.
func (m *memoryRegion) Size() uint64 {
	return uint64(m.memory.Size())
}

// ReadAt implements hv.MemoryRegion.
func (m *memoryRegion) ReadAt(p []byte, off int64) (n int, err error) {
	slice := m.memory.Slice()
	if off < 0 || uint64(off) >= uint64(len(slice)) {
		return 0, fmt.Errorf("whp: ReadAt offset out of bounds")
	}

	n = copy(p, slice[off:])
	if n < len(p) {
		err = fmt.Errorf("whp: ReadAt short read")
	}
	return n, err
}

// WriteAt implements hv.MemoryRegion.
func (m *memoryRegion) WriteAt(p []byte, off int64) (n int, err error) {
	slice := m.memory.Slice()
	if off < 0 || uint64(off) >= uint64(len(slice)) {
		return 0, fmt.Errorf("whp: WriteAt offset out of bounds")
	}

	n = copy(slice[off:], p)
	if n < len(p) {
		err = fmt.Errorf("whp: WriteAt short write")
	}
	return n, err
}

var (
	_ hv.MemoryRegion = &memoryRegion{}
)

type virtualMachine struct {
	rec *timeslice.Recorder

	hv   *hypervisor
	part bindings.PartitionHandle

	vcpus map[int]*virtualCPU

	memMu      sync.RWMutex // protects memory during snapshot
	memory     *bindings.Allocation
	memoryBase uint64

	devices []hv.Device

	emu bindings.EmulatorHandle

	chipset *corechipset.Chipset
	ioapic  *chipset.IOAPIC
	// arm64GICInfo caches the configured interrupt controller details when available.
	arm64GICInfo hv.Arm64GICInfo
	// arm64 interrupt bookkeeping for level-aware delivery.
	arm64GICMu       sync.Mutex
	arm64GICAsserted map[uint32]bool
}

// implements hv.VirtualMachine.
func (v *virtualMachine) MemoryBase() uint64        { return v.memoryBase }
func (v *virtualMachine) MemorySize() uint64        { return uint64(v.memory.Size()) }
func (v *virtualMachine) Hypervisor() hv.Hypervisor { return v.hv }

// AllocateMemory implements hv.VirtualMachine.
func (v *virtualMachine) AllocateMemory(physAddr uint64, size uint64) (hv.MemoryRegion, error) {
	mem, err := bindings.VirtualAlloc(
		0,
		uintptr(size),
		bindings.MEM_RESERVE|bindings.MEM_COMMIT,
		bindings.PAGE_EXECUTE_READWRITE,
	)
	if err != nil {
		return nil, fmt.Errorf("whp: VirtualAlloc failed: %w", err)
	}

	if err := bindings.MapGPARange(
		v.part,
		mem.Pointer(),
		bindings.GuestPhysicalAddress(physAddr),
		uint64(mem.Size()),
		bindings.MapGPARangeFlagRead|bindings.MapGPARangeFlagWrite|bindings.MapGPARangeFlagExecute,
	); err != nil {
		return nil, fmt.Errorf("whp: MapGPARange failed: %w", err)
	}

	return &memoryRegion{
		memory: mem,
	}, nil
}

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
	// Step 1: Cancel all running vCPUs to break them out of WHvRunVirtualProcessor.
	for id := range v.vcpus {
		_ = bindings.CancelRunVirtualProcessor(v.part, uint32(id), 0)
	}

	// Step 2: Close all runQueue channels to stop the vCPU goroutines.
	for _, vcpu := range v.vcpus {
		close(vcpu.runQueue)
	}

	// Step 3: Wait for all vCPU goroutines to exit.
	for _, vcpu := range v.vcpus {
		<-vcpu.done
	}

	// Step 4: Delete all vCPUs before deleting the partition.
	for id := range v.vcpus {
		_ = bindings.DeleteVirtualProcessor(v.part, uint32(id))
	}

	if v.memory != nil {
		v.memory = nil
	}

	// Step 5: Delete the partition.
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
	v.memMu.RLock()
	defer v.memMu.RUnlock()

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
	v.memMu.RLock()
	defer v.memMu.RUnlock()

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

// ensureChipset builds the chipset dispatch tables from registered devices on demand.
func (v *virtualMachine) ensureChipset() (*corechipset.Chipset, error) {
	if v.chipset != nil {
		return v.chipset, nil
	}

	builder := corechipset.NewBuilder()
	for idx, dev := range v.devices {
		name := fmt.Sprintf("%T#%d", dev, idx)

		if cdev, ok := dev.(corechipset.ChipsetDevice); ok {
			if err := builder.RegisterDevice(name, cdev); err != nil {
				// If registration fails due to overlap, the region is already handled by another device
				// Skip this device rather than failing entirely
				if strings.Contains(err.Error(), "overlaps existing region") {
					continue
				}
				return nil, fmt.Errorf("register chipset device %q: %w", name, err)
			}
			continue
		}

		adapter := newLegacyChipsetAdapter(name, dev)
		if adapter == nil {
			continue
		}
		if err := builder.RegisterDevice(name, adapter); err != nil {
			// If registration fails due to overlap, the region is already handled by another device
			// Skip this device rather than failing entirely
			if strings.Contains(err.Error(), "overlaps existing region") {
				continue
			}
			return nil, fmt.Errorf("register legacy device %q: %w", name, err)
		}
	}

	chipset, err := builder.Build()
	if err != nil {
		return nil, fmt.Errorf("build chipset: %w", err)
	}
	v.chipset = chipset
	return chipset, nil
}

// legacyChipsetAdapter bridges existing hv.Device implementations into the chipset builder.
type legacyChipsetAdapter struct {
	name   string
	device hv.Device
	io     hv.X86IOPortDevice
	mmio   hv.MemoryMappedIODevice
}

func newLegacyChipsetAdapter(name string, dev hv.Device) *legacyChipsetAdapter {
	var ioDev hv.X86IOPortDevice
	if d, ok := dev.(hv.X86IOPortDevice); ok {
		ioDev = d
	}

	var mmioDev hv.MemoryMappedIODevice
	if d, ok := dev.(hv.MemoryMappedIODevice); ok {
		mmioDev = d
	}

	if ioDev == nil && mmioDev == nil {
		return nil
	}

	return &legacyChipsetAdapter{
		name:   name,
		device: dev,
		io:     ioDev,
		mmio:   mmioDev,
	}
}

func (a *legacyChipsetAdapter) Init(vm hv.VirtualMachine) error { return nil }
func (a *legacyChipsetAdapter) Start() error                    { return nil }
func (a *legacyChipsetAdapter) Stop() error                     { return nil }
func (a *legacyChipsetAdapter) Reset() error                    { return nil }

func (a *legacyChipsetAdapter) SupportsPortIO() *corechipset.PortIOIntercept {
	if a.io == nil {
		return nil
	}
	return &corechipset.PortIOIntercept{
		Ports:   a.io.IOPorts(),
		Handler: portIOAdapter{dev: a.io},
	}
}

func (a *legacyChipsetAdapter) SupportsMmio() *corechipset.MmioIntercept {
	if a.mmio == nil {
		return nil
	}
	return &corechipset.MmioIntercept{
		Regions: a.mmio.MMIORegions(),
		Handler: mmioAdapter{dev: a.mmio},
	}
}

func (a *legacyChipsetAdapter) SupportsPollDevice() *corechipset.PollDevice {
	return nil
}

type portIOAdapter struct {
	dev hv.X86IOPortDevice
}

func (p portIOAdapter) ReadIOPort(exitCtx hv.ExitContext, port uint16, data []byte) error {
	return p.dev.ReadIOPort(exitCtx, port, data)
}

func (p portIOAdapter) WriteIOPort(exitCtx hv.ExitContext, port uint16, data []byte) error {
	return p.dev.WriteIOPort(exitCtx, port, data)
}

type mmioAdapter struct {
	dev hv.MemoryMappedIODevice
}

func (m mmioAdapter) ReadMMIO(exitCtx hv.ExitContext, addr uint64, data []byte) error {
	return m.dev.ReadMMIO(exitCtx, addr, data)
}

func (m mmioAdapter) WriteMMIO(exitCtx hv.ExitContext, addr uint64, data []byte) error {
	return m.dev.WriteMMIO(exitCtx, addr, data)
}

type hypervisor struct{}

// Close implements hv.Hypervisor.
func (h *hypervisor) Close() error {
	return nil
}

var (
	tsWhpPreInit              = timeslice.RegisterKind("whp_pre_init", 0)
	tsWhpCreatePartition      = timeslice.RegisterKind("whp_create_partition", 0)
	tsWhpSetPartitionProperty = timeslice.RegisterKind("whp_set_partition_property", 0)
	tsWhpArchVMInit           = timeslice.RegisterKind("whp_arch_vm_init", 0)
	tsWhpOnCreateVM           = timeslice.RegisterKind("whp_on_create_vm", 0)
	tsWhpSetupPartition       = timeslice.RegisterKind("whp_setup_partition", 0)
	tsWhpAllocateMemory       = timeslice.RegisterKind("whp_allocate_memory", 0)
	tsWhpMapGPARange          = timeslice.RegisterKind("whp_map_gpa_range", 0)
	tsWhpArchVMInitWithMemory = timeslice.RegisterKind("whp_arch_vm_init_with_memory", 0)
	tsWhpOnCreateVMWithMemory = timeslice.RegisterKind("whp_on_create_vm_with_memory", 0)
	tsWhpCreateVCPU           = timeslice.RegisterKind("whp_create_vcpu", 0)
	tsWhpArchVCPUInit         = timeslice.RegisterKind("whp_arch_vcpu_init", 0)
	tsWhpOnCreateVCPU         = timeslice.RegisterKind("whp_on_create_vcpu", 0)
	tsWhpLoaded               = timeslice.RegisterKind("whp_loaded", 0)
)

// NewVirtualMachine implements hv.Hypervisor.
func (h *hypervisor) NewVirtualMachine(config hv.VMConfig) (hv.VirtualMachine, error) {
	vm := &virtualMachine{
		rec:   timeslice.NewState(),
		hv:    h,
		vcpus: make(map[int]*virtualCPU),
	}

	timeslice.Record(tsWhpPreInit, time.Since(tsWhpStartTime))

	part, err := bindings.CreatePartition()
	if err != nil {
		return nil, fmt.Errorf("whp: CreatePartition failed: %w", err)
	}
	vm.part = part

	vm.rec.Record(tsWhpCreatePartition)

	if err := bindings.SetPartitionPropertyUnsafe(
		vm.part,
		bindings.PartitionPropertyCodeProcessorCount,
		uint32(config.CPUCount()),
	); err != nil {
		bindings.DeletePartition(vm.part)
		return nil, fmt.Errorf("whp: SetPartitionPropertyUnsafe failed: %w", err)
	}

	vm.rec.Record(tsWhpSetPartitionProperty)

	if err := h.archVMInit(vm, config); err != nil {
		return nil, fmt.Errorf("whp: archVMInit failed: %w", err)
	}

	vm.rec.Record(tsWhpArchVMInit)

	if err := config.Callbacks().OnCreateVM(vm); err != nil {
		bindings.DeletePartition(vm.part)
		return nil, fmt.Errorf("VM callback OnCreateVM: %w", err)
	}

	vm.rec.Record(tsWhpOnCreateVM)

	if err := bindings.SetupPartition(vm.part); err != nil {
		bindings.DeletePartition(vm.part)
		return nil, fmt.Errorf("whp: SetupPartition failed: %w", err)
	}

	vm.rec.Record(tsWhpSetupPartition)

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

	vm.rec.Record(tsWhpAllocateMemory)

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

	vm.rec.Record(tsWhpMapGPARange)

	if err := h.archVMInitWithMemory(vm, config); err != nil {
		return nil, fmt.Errorf("whp: archVMInit failed: %w", err)
	}

	vm.rec.Record(tsWhpArchVMInitWithMemory)

	if err := config.Callbacks().OnCreateVMWithMemory(vm); err != nil {
		bindings.DeletePartition(vm.part)
		return nil, fmt.Errorf("VM callback OnCreateVM: %w", err)
	}

	vm.rec.Record(tsWhpOnCreateVMWithMemory)

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

		vm.rec.Record(tsWhpCreateVCPU)

		vcpu := &virtualCPU{
			rec:      timeslice.NewState(),
			vm:       vm,
			id:       i,
			runQueue: make(chan func(), 16),
			done:     make(chan struct{}),
		}

		vm.vcpus[i] = vcpu

		if err := h.archVCPUInit(vm, vcpu); err != nil {
			bindings.DeletePartition(vm.part)
			return nil, fmt.Errorf("initialize VM: %w", err)
		}

		vm.rec.Record(tsWhpArchVCPUInit)

		go vcpu.start()

		if err := config.Callbacks().OnCreateVCPU(vcpu); err != nil {
			bindings.DeletePartition(vm.part)
			return nil, fmt.Errorf("VM callback OnCreateVCPU %d: %w", i, err)
		}

		vm.rec.Record(tsWhpOnCreateVCPU)
	}

	// Run Loader
	loader := config.Loader()

	if loader != nil {
		if err := loader.Load(vm); err != nil {
			bindings.DeletePartition(vm.part)
			return nil, fmt.Errorf("load VM: %w", err)
		}

		vm.rec.Record(tsWhpLoaded)
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
