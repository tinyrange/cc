//go:build linux

package kvm

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
	"sync"
	"unsafe"

	"github.com/tinyrange/cc/internal/acpi"
	corechipset "github.com/tinyrange/cc/internal/chipset"
	x86chipset "github.com/tinyrange/cc/internal/devices/amd64/chipset"
	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/timeslice"
	"golang.org/x/sys/unix"
)

// x86_64 memory layout constants for split memory (PCI hole at 3GB-4GB)
const (
	x86PCIHoleStart    uint64 = 0xC0000000  // 3GB - start of PCI/MMIO hole
	x86HighMemoryStart uint64 = 0x100000000 // 4GB - start of high memory above PCI hole
)

var (
	tsKvmHostTime  = timeslice.RegisterKind("kvm_host_time", 0)
	tsKvmGuestTime = timeslice.RegisterKind("kvm_guest_time", timeslice.SliceFlagGuestTime)
)

type exitContext struct {
	timeslice timeslice.TimesliceID
}

func (c *exitContext) SetExitTimeslice(id timeslice.TimesliceID) {
	c.timeslice = id
}

type virtualCPU struct {
	rec *timeslice.Recorder

	vm       *virtualMachine
	runQueue chan func()
	id       int
	fd       int
	run      []byte
}

// implements hv.VirtualCPU.
func (v *virtualCPU) ID() int                           { return v.id }
func (v *virtualCPU) VirtualMachine() hv.VirtualMachine { return v.vm }

func (v *virtualCPU) start() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	for fn := range v.runQueue {
		fn()
	}
}

func (v *virtualCPU) RequestImmediateExit(tid int) error {
	run := (*kvmRunData)(unsafe.Pointer(&v.run[0]))

	// set immediate_exit to request vCPU exit
	run.immediate_exit = 1

	// send signal to the vCPU thread to interrupt it
	if err := unix.Tgkill(unix.Getpid(), tid, unix.SIGUSR1); err != nil {
		return fmt.Errorf("kvm: request immediate exit: %w", err)
	}

	return nil
}

var (
	_ hv.VirtualCPU = &virtualCPU{}
)

type memoryRegion struct {
	mem []byte
}

// implements hv.MemoryRegion.
func (m *memoryRegion) Size() uint64 {
	return uint64(len(m.mem))
}

func (m *memoryRegion) ReadAt(p []byte, off int64) (n int, err error) {
	if off < 0 || int(off) >= len(m.mem) {
		return 0, fmt.Errorf("kvm: ReadAt offset out of bounds")
	}

	n = copy(p, m.mem[off:])
	if n < len(p) {
		err = fmt.Errorf("kvm: ReadAt short read")
	}

	return n, err
}

func (m *memoryRegion) WriteAt(p []byte, off int64) (n int, err error) {
	if off < 0 || int(off) >= len(m.mem) {
		return 0, fmt.Errorf("kvm: WriteAt offset out of bounds")
	}

	n = copy(m.mem[off:], p)
	if n < len(p) {
		err = fmt.Errorf("kvm: WriteAt short write")
	}

	return n, err
}

type virtualMachine struct {
	rec *timeslice.Recorder

	hv             *hypervisor
	vmFd           int
	vcpus          map[int]*virtualCPU
	memMu          sync.RWMutex
	memory         []byte
	memoryBase     uint64
	devices        []hv.Device
	lastMemorySlot uint32

	// Physical address space allocator for MMIO regions
	addressSpace *hv.AddressSpace

	// Split memory layout tracking (x86_64 only, for >3GB RAM)
	// When highMemSize > 0, memory is split around the PCI hole:
	//   - Low memory: GPA [memoryBase, memoryBase+lowMemSize) -> host mmap [0, lowMemSize)
	//   - High memory: GPA [0x100000000, 0x100000000+highMemSize) -> host mmap [lowMemSize, lowMemSize+highMemSize)
	lowMemSize  uint64 // Size of memory below PCI hole (0 means contiguous layout)
	highMemSize uint64 // Size of memory above 4GB (0 means no high memory)

	// amd64-specific fields
	hasIRQChip   bool
	splitIRQChip bool // true if using split IRQ chip mode (LAPIC in kernel, PIC/IOAPIC in userspace)
	hasPIT       bool
	ioapic       *x86chipset.IOAPIC
	chipset      *corechipset.Chipset

	// arm64-specific fields
	arm64GICInfo hv.Arm64GICInfo
	arm64VGICFd  int // vGIC device file descriptor
}

// implements hv.VirtualMachine.
func (v *virtualMachine) MemoryBase() uint64        { return v.memoryBase }
func (v *virtualMachine) MemorySize() uint64        { return uint64(len(v.memory)) }
func (v *virtualMachine) Hypervisor() hv.Hypervisor { return v.hv }

var (
	tsKvmAllocateMemory      = timeslice.RegisterKind("kvm_allocate_memory", 0)
	tsKvmMadviseMemory       = timeslice.RegisterKind("kvm_madvise_memory", 0)
	tsKvmSetUserMemoryRegion = timeslice.RegisterKind("kvm_set_user_memory_region", 0)
)

// AllocateMemory implements hv.VirtualMachine.
func (v *virtualMachine) AllocateMemory(physAddr uint64, size uint64) (hv.MemoryRegion, error) {
	maxInt := uint64(^uint(0) >> 1)
	if size > maxInt {
		return nil, fmt.Errorf("allocate memory: size %d exceeds host address limit", size)
	}

	mem, err := unix.Mmap(
		-1,
		0,
		int(size),
		unix.PROT_READ|unix.PROT_WRITE,
		unix.MAP_ANONYMOUS|unix.MAP_PRIVATE,
	)
	if err != nil {
		return nil, fmt.Errorf("allocate memory: %w", err)
	}

	v.rec.Record(tsKvmAllocateMemory)

	if v.hv.Architecture() == hv.ArchitectureX86_64 {
		if err := unix.Madvise(mem, unix.MADV_MERGEABLE); err != nil {
			unix.Munmap(mem)
			return nil, fmt.Errorf("madvise memory: %w", err)
		}

		v.rec.Record(tsKvmMadviseMemory)
	}

	v.lastMemorySlot++
	if err := setUserMemoryRegion(v.vmFd, &kvmUserspaceMemoryRegion{
		Slot:          v.lastMemorySlot,
		Flags:         0,
		GuestPhysAddr: physAddr,
		MemorySize:    size,
		UserspaceAddr: uint64(uintptr(unsafe.Pointer(&mem[0]))),
	}); err != nil {
		return nil, fmt.Errorf("set user memory region: %w", err)
	}

	v.rec.Record(tsKvmSetUserMemoryRegion)

	return &memoryRegion{mem: mem}, nil
}

// AllocateMMIO implements hv.VirtualMachine.
func (v *virtualMachine) AllocateMMIO(req hv.MMIOAllocationRequest) (hv.MMIOAllocation, error) {
	if v.addressSpace == nil {
		return hv.MMIOAllocation{}, fmt.Errorf("kvm: address space not initialized")
	}
	return v.addressSpace.Allocate(req)
}

// RegisterFixedMMIO implements hv.VirtualMachine.
func (v *virtualMachine) RegisterFixedMMIO(name string, base, size uint64) error {
	if v.addressSpace == nil {
		return fmt.Errorf("kvm: address space not initialized")
	}
	return v.addressSpace.RegisterFixed(name, base, size)
}

// GetAllocatedMMIORegions implements hv.VirtualMachine.
func (v *virtualMachine) GetAllocatedMMIORegions() []hv.MMIOAllocation {
	if v.addressSpace == nil {
		return nil
	}
	return v.addressSpace.Allocations()
}

// AddDevice implements hv.VirtualMachine.
func (v *virtualMachine) AddDevice(dev hv.Device) error {
	v.devices = append(v.devices, dev)
	v.chipset = nil

	// Capture IOAPIC device for routing integration.
	if ioa, ok := dev.(*x86chipset.IOAPIC); ok {
		v.ioapic = ioa
	}

	return dev.Init(v)
}

// AddDeviceFromTemplate implements hv.VirtualMachine.
func (v *virtualMachine) AddDeviceFromTemplate(template hv.DeviceTemplate) (hv.Device, error) {
	dev, err := template.Create(v)
	if err != nil {
		return nil, fmt.Errorf("create device from template: %w", err)
	}

	if err := v.AddDevice(dev); err != nil {
		return nil, err
	}
	return dev, nil
}

// Close implements hv.VirtualMachine.
// Cleanup is performed asynchronously in a background goroutine to avoid
// blocking on kernel resource cleanup (which can take 10-20ms on Linux).
func (v *virtualMachine) Close() error {
	// Capture resources to clean up
	vcpus := v.vcpus
	v.vcpus = nil

	v.memMu.Lock()
	mem := v.memory
	v.memory = nil
	v.memMu.Unlock()

	vmFd := v.vmFd
	v.vmFd = -1

	// Close vCPU run queues synchronously (just channel ops, fast)
	for _, vcpu := range vcpus {
		close(vcpu.runQueue)
	}

	cleanup := func() {
		for _, vcpu := range vcpus {
			if err := unix.Close(vcpu.fd); err != nil {
				slog.Error("kvm: close vcpu fd", "error", err)
			}
			if err := unix.Munmap(vcpu.run); err != nil {
				slog.Error("kvm: munmap vcpu run", "error", err)
			}
		}

		if mem != nil {
			if err := unix.Munmap(mem); err != nil {
				slog.Error("kvm: munmap memory", "error", err)
			}
		}

		if vmFd >= 0 {
			if err := unix.Close(vmFd); err != nil {
				slog.Error("kvm: close vm fd", "error", err)
			}
		}
	}

	// On arm64 Linux, perform cleanup synchronously to prevent accumulation
	// of pending cleanups that slow down subsequent VM operations.
	// On other platforms, use background cleanup for better latency.
	if runtime.GOARCH == "arm64" && runtime.GOOS == "linux" {
		cleanup()
	} else {
		go cleanup()
	}

	return nil
}

// Run implements hv.VirtualMachine.
func (v *virtualMachine) Run(ctx context.Context, cfg hv.RunConfig) error {
	if cfg == nil {
		return fmt.Errorf("kvm: RunConfig is nil")
	}

	vcpu, ok := v.vcpus[0]
	if !ok {
		return fmt.Errorf("kvm: no vCPU 0 found")
	}

	done := make(chan error, 1)

	vcpu.runQueue <- func() {
		done <- cfg.Run(ctx, vcpu)
	}

	err := <-done
	return err
}

func (v *virtualMachine) ReadAt(p []byte, off int64) (n int, err error) {
	v.memMu.RLock()
	defer v.memMu.RUnlock()
	if v.memory == nil {
		return 0, fmt.Errorf("kvm: ReadAt after close")
	}

	gpa := uint64(off)
	hostOff, ok := v.gpaToHostOffset(gpa)
	if !ok {
		return 0, fmt.Errorf("kvm: ReadAt GPA 0x%x out of bounds or in PCI hole", gpa)
	}

	if hostOff < 0 || int(hostOff) >= len(v.memory) {
		return 0, fmt.Errorf("kvm: ReadAt offset out of bounds")
	}

	n = copy(p, v.memory[hostOff:])
	if n < len(p) {
		err = fmt.Errorf("kvm: ReadAt short read")
	}

	return n, err
}

func (v *virtualMachine) WriteAt(p []byte, off int64) (n int, err error) {
	v.memMu.RLock()
	defer v.memMu.RUnlock()
	if v.memory == nil {
		return 0, fmt.Errorf("kvm: WriteAt after close")
	}

	gpa := uint64(off)
	hostOff, ok := v.gpaToHostOffset(gpa)
	if !ok {
		return 0, fmt.Errorf("kvm: WriteAt GPA 0x%x out of bounds or in PCI hole", gpa)
	}

	if hostOff < 0 || int(hostOff) >= len(v.memory) {
		return 0, fmt.Errorf("kvm: WriteAt offset 0x%x out of bounds 0x%x", hostOff, len(v.memory))
	}

	n = copy(v.memory[hostOff:], p)
	if n < len(p) {
		err = fmt.Errorf("kvm: WriteAt short write")
	}

	return n, err
}

func (v *virtualMachine) Arm64GICInfo() (hv.Arm64GICInfo, bool) {
	if v.hv.Architecture() != hv.ArchitectureARM64 {
		return hv.Arm64GICInfo{}, false
	}
	if v.arm64GICInfo.Version == hv.Arm64GICVersionUnknown {
		return hv.Arm64GICInfo{}, false
	}
	return v.arm64GICInfo, true
}

// gpaToHostOffset translates a guest physical address to a host memory offset.
// For split memory layouts (x86_64 with >3GB RAM), this handles the PCI hole gap:
//   - Low memory [memoryBase, memoryBase+lowMemSize) -> host offset [0, lowMemSize)
//   - High memory [0x100000000, 0x100000000+highMemSize) -> host offset [lowMemSize, lowMemSize+highMemSize)
//
// Returns the host offset and true if valid, or 0 and false if the GPA is invalid
// (e.g., in the PCI hole or out of range).
func (v *virtualMachine) gpaToHostOffset(gpa uint64) (int64, bool) {
	// Contiguous layout (no split memory)
	if v.highMemSize == 0 {
		if gpa < v.memoryBase || gpa >= v.memoryBase+uint64(len(v.memory)) {
			return 0, false
		}
		return int64(gpa - v.memoryBase), true
	}

	// Split layout: low memory below PCI hole
	lowMemEnd := v.memoryBase + v.lowMemSize
	if gpa >= v.memoryBase && gpa < lowMemEnd {
		return int64(gpa - v.memoryBase), true
	}

	// Split layout: high memory above 4GB
	highMemEnd := x86HighMemoryStart + v.highMemSize
	if gpa >= x86HighMemoryStart && gpa < highMemEnd {
		// High memory is stored after low memory in the host mmap
		return int64(v.lowMemSize + (gpa - x86HighMemoryStart)), true
	}

	// GPA is in the PCI hole or out of range
	return 0, false
}

func (v *virtualMachine) VirtualCPUCall(id int, f func(vcpu hv.VirtualCPU) error) error {
	vcpu, ok := v.vcpus[id]
	if !ok {
		return fmt.Errorf("kvm: no vCPU %d found", id)
	}

	done := make(chan error, 1)

	vcpu.runQueue <- func() {
		done <- f(vcpu)
	}

	return <-done
}

var (
	tsKvmEnsureChipset = timeslice.RegisterKind("kvm_ensure_chipset", 0)
	tsKvmBuiltChipset  = timeslice.RegisterKind("kvm_built_chipset", 0)
)

// ensureChipset builds the chipset dispatch tables from registered devices on demand.
func (v *virtualMachine) ensureChipset() (*corechipset.Chipset, error) {
	if v.chipset != nil {
		return v.chipset, nil
	}

	v.rec.Record(tsKvmEnsureChipset)

	builder := corechipset.NewBuilder()
	for idx, dev := range v.devices {
		name := fmt.Sprintf("%T#%d", dev, idx)

		if cdev, ok := dev.(corechipset.ChipsetDevice); ok {
			if err := builder.RegisterDevice(name, cdev); err != nil {
				return nil, fmt.Errorf("register chipset device %q: %w", name, err)
			}
			continue
		}

		adapter := newLegacyChipsetAdapter(name, dev)
		if adapter == nil {
			continue
		}
		if err := builder.RegisterDevice(name, adapter); err != nil {
			return nil, fmt.Errorf("register legacy device %q: %w", name, err)
		}
	}

	chipset, err := builder.Build()
	if err != nil {
		return nil, fmt.Errorf("build chipset: %w", err)
	}
	v.chipset = chipset

	v.rec.Record(tsKvmBuiltChipset)

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

func (p portIOAdapter) ReadIOPort(ctx hv.ExitContext, port uint16, data []byte) error {
	return p.dev.ReadIOPort(ctx, port, data)
}

func (p portIOAdapter) WriteIOPort(ctx hv.ExitContext, port uint16, data []byte) error {
	return p.dev.WriteIOPort(ctx, port, data)
}

type mmioAdapter struct {
	dev hv.MemoryMappedIODevice
}

func (m mmioAdapter) ReadMMIO(ctx hv.ExitContext, addr uint64, data []byte) error {
	return m.dev.ReadMMIO(ctx, addr, data)
}

func (m mmioAdapter) WriteMMIO(ctx hv.ExitContext, addr uint64, data []byte) error {
	return m.dev.WriteMMIO(ctx, addr, data)
}

var (
	_ hv.VirtualMachine   = &virtualMachine{}
	_ hv.Arm64GICProvider = &virtualMachine{}
)

type hypervisor struct {
	fd int

	supportedMsrsOnce sync.Once
	supportedMsrs     []uint32
	supportedMsrsErr  error

	snapshotMsrsOnce sync.Once
	snapshotMsrs     []uint32
	snapshotMsrsErr  error
}

func (h *hypervisor) Close() error {
	if err := unix.Close(h.fd); err != nil {
		return fmt.Errorf("close kvm fd: %w", err)
	}

	return nil
}

var (
	tsKvmPreInit              = timeslice.RegisterKind("kvm_pre_init", 0)
	tsKvmCheckIpaSize         = timeslice.RegisterKind("kvm_check_ipa_size", 0)
	tsKvmCreateVm             = timeslice.RegisterKind("kvm_create_vm", 0)
	tsKvmArchVMInit           = timeslice.RegisterKind("kvm_arch_vm_init", 0)
	tsKvmOnCreateVM           = timeslice.RegisterKind("kvm_on_create_vm", 0)
	tsKvmMmapGuestMemory      = timeslice.RegisterKind("kvm_mmap_guest_memory", 0)
	tsKvmMadviseGuestMemory   = timeslice.RegisterKind("kvm_madvise_guest_memory", 0)
	tsKvmInstallACPI          = timeslice.RegisterKind("kvm_install_acpi", 0)
	tsKvmOnCreateVMWithMemory = timeslice.RegisterKind("kvm_on_create_vm_with_memory", 0)
	tsKvmCreateVCPU           = timeslice.RegisterKind("kvm_create_vcpu", 0)
	tsKvmMmapVCPU             = timeslice.RegisterKind("kvm_mmap_vcpu", 0)
	tsKvmArchVCPUInit         = timeslice.RegisterKind("kvm_arch_vcpu_init", 0)
	tsKvmOnCreateVCPU         = timeslice.RegisterKind("kvm_on_create_vcpu", 0)
	tsKvmArchPostVCPUInit     = timeslice.RegisterKind("kvm_arch_post_vcpu_init", 0)
	tsKvmLoaded               = timeslice.RegisterKind("kvm_loaded", 0)
)

// NewVirtualMachine implements hv.Hypervisor.
func (h *hypervisor) NewVirtualMachine(config hv.VMConfig) (hv.VirtualMachine, error) {
	vm := &virtualMachine{
		hv:    h,
		rec:   timeslice.NewState(),
		vcpus: make(map[int]*virtualCPU),
	}

	vm.rec.Record(tsKvmPreInit)

	// On M1 this fails unless an argument is passed to set the IPA size.
	var ipaSize uint32 = 0
	if runtime.GOARCH == "arm64" {
		cap, err := checkExtension(h.fd, kvmCapArmVmIpaSize)
		if err != nil {
			return nil, fmt.Errorf("kvm: get cap: %w", err)
		}
		ipaSize = uint32(cap)
	}

	vm.rec.Record(tsKvmCheckIpaSize)

	vmFd, err := createVm(h.fd, ipaSize)
	if err != nil {
		return nil, fmt.Errorf("kvm: create VM: %w", err)
	}

	vm.rec.Record(tsKvmCreateVm)

	vm.vmFd = vmFd

	if err := h.archVMInit(vm, config); err != nil {
		unix.Close(vmFd)
		return nil, fmt.Errorf("initialize VM: %w", err)
	}

	vm.rec.Record(tsKvmArchVMInit)

	if err := config.Callbacks().OnCreateVM(vm); err != nil {
		unix.Close(vmFd)
		return nil, fmt.Errorf("VM callback OnCreateVM: %w", err)
	}

	vm.rec.Record(tsKvmOnCreateVM)

	// Allocate guest memory
	if config.MemorySize() == 0 {
		unix.Close(vmFd)
		return nil, fmt.Errorf("kvm: memory size must be greater than 0")
	}

	mem, err := unix.Mmap(
		-1,
		0,
		int(config.MemorySize()),
		unix.PROT_READ|unix.PROT_WRITE,
		unix.MAP_ANONYMOUS|unix.MAP_PRIVATE,
	)
	if err != nil {
		unix.Close(vmFd)
		return nil, fmt.Errorf("mmap guest memory: %w", err)
	}

	vm.rec.Record(tsKvmMmapGuestMemory)

	if h.Architecture() == hv.ArchitectureX86_64 {
		if err := unix.Madvise(mem, unix.MADV_MERGEABLE); err != nil {
			unix.Munmap(mem)
			return nil, fmt.Errorf("madvise memory: %w", err)
		}

		vm.rec.Record(tsKvmMadviseGuestMemory)
	}

	vm.memory = mem
	vm.memoryBase = config.MemoryBase()

	// Determine if we need split memory layout (x86_64 with memory extending into PCI hole)
	memEnd := config.MemoryBase() + config.MemorySize()
	needsSplitMemory := h.Architecture() == hv.ArchitectureX86_64 && memEnd > x86PCIHoleStart

	if needsSplitMemory {
		// Split memory layout: low memory below PCI hole, high memory above 4GB
		vm.lowMemSize = x86PCIHoleStart - config.MemoryBase()
		vm.highMemSize = config.MemorySize() - vm.lowMemSize

		// Initialize physical address space allocator for split layout
		// The address space needs to know about both memory regions
		vm.addressSpace = hv.NewAddressSpaceSplit(
			h.Architecture(),
			config.MemoryBase(),
			vm.lowMemSize,
			x86HighMemoryStart,
			vm.highMemSize,
		)

		// Register slot 0: low memory [memoryBase, x86PCIHoleStart)
		if err := setUserMemoryRegion(vm.vmFd, &kvmUserspaceMemoryRegion{
			Slot:          0,
			Flags:         0,
			GuestPhysAddr: config.MemoryBase(),
			MemorySize:    vm.lowMemSize,
			UserspaceAddr: uint64(uintptr(unsafe.Pointer(&mem[0]))),
		}); err != nil {
			unix.Close(vmFd)
			return nil, fmt.Errorf("set user memory region (low): %w", err)
		}

		// Register slot 1: high memory [0x100000000, 0x100000000+highMemSize)
		// Points to the host mmap at offset lowMemSize
		if err := setUserMemoryRegion(vm.vmFd, &kvmUserspaceMemoryRegion{
			Slot:          1,
			Flags:         0,
			GuestPhysAddr: x86HighMemoryStart,
			MemorySize:    vm.highMemSize,
			UserspaceAddr: uint64(uintptr(unsafe.Pointer(&mem[vm.lowMemSize]))),
		}); err != nil {
			unix.Close(vmFd)
			return nil, fmt.Errorf("set user memory region (high): %w", err)
		}

		vm.lastMemorySlot = 1
	} else {
		// Contiguous memory layout (default)
		vm.addressSpace = hv.NewAddressSpace(h.Architecture(), config.MemoryBase(), config.MemorySize())

		if err := setUserMemoryRegion(vm.vmFd, &kvmUserspaceMemoryRegion{
			Slot:          0,
			Flags:         0,
			GuestPhysAddr: config.MemoryBase(),
			MemorySize:    config.MemorySize(),
			UserspaceAddr: uint64(uintptr(unsafe.Pointer(&mem[0]))),
		}); err != nil {
			unix.Close(vmFd)
			return nil, fmt.Errorf("set user memory region: %w", err)
		}
	}

	vm.rec.Record(tsKvmSetUserMemoryRegion)

	if h.Architecture() == hv.ArchitectureX86_64 && config.NeedsInterruptSupport() {
		if err := acpi.Install(vm, acpi.Config{
			MemoryBase: config.MemoryBase(),
			MemorySize: config.MemorySize(),
		}); err != nil {
			unix.Close(vmFd)
			return nil, fmt.Errorf("install ACPI tables: %w", err)
		}

		vm.rec.Record(tsKvmInstallACPI)
	}

	if err := config.Callbacks().OnCreateVMWithMemory(vm); err != nil {
		unix.Close(vmFd)
		return nil, fmt.Errorf("VM callback OnCreateVMWithMemory: %w", err)
	}

	vm.rec.Record(tsKvmOnCreateVMWithMemory)

	// Create vCPUs
	if config.CPUCount() != 1 {
		unix.Close(vmFd)
		return nil, fmt.Errorf("kvm: only 1 vCPU supported, got %d", config.CPUCount())
	}

	mmapSize, err := getVcpuMmapSize(h.fd)
	if err != nil {
		unix.Close(vmFd)
		return nil, fmt.Errorf("get kvm_run mmap size: %w", err)
	}

	for i := range config.CPUCount() {
		vcpuFd, err := createVCPU(vm.vmFd, i)
		if err != nil {
			unix.Close(vmFd)
			return nil, fmt.Errorf("create vCPU %d: %w", i, err)
		}

		vm.rec.Record(tsKvmCreateVCPU)

		run, err := unix.Mmap(
			vcpuFd,
			0,
			mmapSize,
			unix.PROT_READ|unix.PROT_WRITE,
			unix.MAP_SHARED,
		)
		if err != nil {
			unix.Close(vcpuFd)
			unix.Close(vmFd)
			return nil, fmt.Errorf("mmap vCPU %d kvm_run: %w", i, err)
		}

		vm.rec.Record(tsKvmMmapVCPU)

		vcpu := &virtualCPU{
			rec:      timeslice.NewState(),
			vm:       vm,
			id:       i,
			fd:       vcpuFd,
			run:      run,
			runQueue: make(chan func(), 16),
		}

		vm.vcpus[i] = vcpu

		if err := h.archVCPUInit(vm, vcpuFd); err != nil {
			unix.Close(vmFd)
			return nil, fmt.Errorf("initialize VM: %w", err)
		}

		vm.rec.Record(tsKvmArchVCPUInit)

		go vcpu.start()

		if err := config.Callbacks().OnCreateVCPU(vcpu); err != nil {
			unix.Close(vcpuFd)
			unix.Close(vmFd)
			return nil, fmt.Errorf("VM callback OnCreateVCPU %d: %w", i, err)
		}

		vm.rec.Record(tsKvmOnCreateVCPU)
	}

	// Post-vCPU architecture-specific initialization (e.g., vGIC finalization on ARM64)
	if err := h.archPostVCPUInit(vm, config); err != nil {
		unix.Close(vmFd)
		return nil, fmt.Errorf("post-vCPU initialization: %w", err)
	}

	vm.rec.Record(tsKvmArchPostVCPUInit)

	// Run Loader
	loader := config.Loader()

	if loader != nil {
		if err := loader.Load(vm); err != nil {
			unix.Close(vmFd)
			return nil, fmt.Errorf("load VM: %w", err)
		}

		vm.rec.Record(tsKvmLoaded)
	}

	// Set finalizer to catch VMs that are garbage collected without being closed
	runtime.SetFinalizer(vm, func(v *virtualMachine) {
		if v.vmFd >= 0 {
			slog.Debug("kvm: VM was not closed before garbage collection, cleaning up")
			v.Close()
		}
	})

	return vm, nil
}

var (
	_ hv.Hypervisor = &hypervisor{}
)

func Open() (hv.Hypervisor, error) {
	fd, err := unix.Open("/dev/kvm", unix.O_CLOEXEC|unix.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("open /dev/kvm: %w", err)
	}

	// validate API version
	version, err := getApiVersion(fd)
	if err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("get KVM API version: %w", err)
	}
	if version != kvmApiVersion {
		unix.Close(fd)
		return nil, fmt.Errorf("kvm: unsupported API version %d, want %d", version, kvmApiVersion)
	}

	return &hypervisor{fd: fd}, nil
}
