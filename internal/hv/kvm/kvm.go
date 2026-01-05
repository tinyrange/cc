//go:build linux

package kvm

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"github.com/tinyrange/cc/internal/acpi"
	corechipset "github.com/tinyrange/cc/internal/chipset"
	x86chipset "github.com/tinyrange/cc/internal/devices/amd64/chipset"
	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/timeslice"
	"golang.org/x/sys/unix"
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
	vm       *virtualMachine
	runQueue chan func()
	id       int
	fd       int
	run      []byte

	traceStart time.Time

	// trace is a ring buffer
	traceBuffer []string
	traceOffset int
}

// implements hv.VirtualCPU.
func (v *virtualCPU) ID() int                           { return v.id }
func (v *virtualCPU) VirtualMachine() hv.VirtualMachine { return v.vm }

func (v *virtualCPU) trace(s string) {
	if v.traceBuffer == nil {
		return
	}

	v.traceBuffer[v.traceOffset] = fmt.Sprintf("%s: %s", time.Since(v.traceStart), s)
	v.traceOffset = (v.traceOffset + 1) % len(v.traceBuffer)
}

func (v *virtualCPU) EnableTrace(maxEntries int) error {
	if maxEntries <= 0 {
		return fmt.Errorf("maxEntries must be positive")
	}

	v.traceBuffer = make([]string, maxEntries)
	v.traceOffset = 0
	v.traceStart = time.Now()

	return nil
}

func (v *virtualCPU) GetTraceBuffer() ([]string, error) {
	if v.traceBuffer == nil {
		return nil, fmt.Errorf("trace buffer not enabled")
	}

	var trace []string
	for i := 0; i < len(v.traceBuffer); i++ {
		idx := (v.traceOffset + i) % len(v.traceBuffer)
		if v.traceBuffer[idx] != "" {
			trace = append(trace, v.traceBuffer[idx])
		}
	}

	return trace, nil
}

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
	hv             *hypervisor
	vmFd           int
	vcpus          map[int]*virtualCPU
	memMu          sync.RWMutex
	memory         []byte
	memoryBase     uint64
	devices        []hv.Device
	lastMemorySlot uint32

	// amd64-specific fields
	hasIRQChip bool
	hasPIT     bool
	ioapic     *x86chipset.IOAPIC
	chipset    *corechipset.Chipset

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

	timeslice.Record(tsKvmAllocateMemory)

	if v.hv.Architecture() == hv.ArchitectureX86_64 {
		if err := unix.Madvise(mem, unix.MADV_MERGEABLE); err != nil {
			unix.Munmap(mem)
			return nil, fmt.Errorf("madvise memory: %w", err)
		}

		timeslice.Record(tsKvmMadviseMemory)
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

	timeslice.Record(tsKvmSetUserMemoryRegion)

	return &memoryRegion{mem: mem}, nil
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
func (v *virtualMachine) AddDeviceFromTemplate(template hv.DeviceTemplate) error {
	dev, err := template.Create(v)
	if err != nil {
		return fmt.Errorf("create device from template: %w", err)
	}

	return v.AddDevice(dev)
}

// Close implements hv.VirtualMachine.
func (v *virtualMachine) Close() error {
	for _, vcpu := range v.vcpus {
		close(vcpu.runQueue)

		if err := unix.Close(vcpu.fd); err != nil {
			return fmt.Errorf("close vCPU %d fd: %w", vcpu.id, err)
		}

		if err := unix.Munmap(vcpu.run); err != nil {
			return fmt.Errorf("munmap vCPU %d run area: %w", vcpu.id, err)
		}
	}

	v.memMu.Lock()
	mem := v.memory
	v.memory = nil
	v.memMu.Unlock()

	if mem != nil {
		if err := unix.Munmap(mem); err != nil {
			return fmt.Errorf("munmap vm memory: %w", err)
		}
	}

	if err := unix.Close(v.vmFd); err != nil {
		return fmt.Errorf("close kvm vm fd: %w", err)
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
	off = off - int64(v.memoryBase)

	if off < 0 || int(off) >= len(v.memory) {
		return 0, fmt.Errorf("kvm: ReadAt offset out of bounds")
	}

	n = copy(p, v.memory[off:])
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
	off = off - int64(v.memoryBase)

	if off < 0 || int(off) >= len(v.memory) {
		return 0, fmt.Errorf("kvm: WriteAt offset 0x%x out of bounds 0x%x", off, len(v.memory))
	}

	n = copy(v.memory[off:], p)
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

	timeslice.Record(tsKvmEnsureChipset)

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

	timeslice.Record(tsKvmBuiltChipset)

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
		vcpus: make(map[int]*virtualCPU),
	}

	timeslice.Record(tsKvmPreInit)

	// On M1 this fails unless an argument is passed to set the IPA size.
	var ipaSize uint32 = 0
	if runtime.GOARCH == "arm64" {
		cap, err := checkExtension(h.fd, kvmCapArmVmIpaSize)
		if err != nil {
			return nil, fmt.Errorf("kvm: get cap: %w", err)
		}
		ipaSize = uint32(cap)
	}

	vmFd, err := createVm(h.fd, ipaSize)
	if err != nil {
		return nil, fmt.Errorf("kvm: create VM: %w", err)
	}

	timeslice.Record(tsKvmCreateVm)

	vm.vmFd = vmFd

	if err := h.archVMInit(vm, config); err != nil {
		unix.Close(vmFd)
		return nil, fmt.Errorf("initialize VM: %w", err)
	}

	timeslice.Record(tsKvmArchVMInit)

	if err := config.Callbacks().OnCreateVM(vm); err != nil {
		unix.Close(vmFd)
		return nil, fmt.Errorf("VM callback OnCreateVM: %w", err)
	}

	timeslice.Record(tsKvmOnCreateVM)

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

	timeslice.Record(tsKvmMmapGuestMemory)

	if h.Architecture() == hv.ArchitectureX86_64 {
		if err := unix.Madvise(mem, unix.MADV_MERGEABLE); err != nil {
			unix.Munmap(mem)
			return nil, fmt.Errorf("madvise memory: %w", err)
		}

		timeslice.Record(tsKvmMadviseGuestMemory)
	}

	vm.memory = mem
	vm.memoryBase = config.MemoryBase()

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

	timeslice.Record(tsKvmSetUserMemoryRegion)

	if h.Architecture() == hv.ArchitectureX86_64 && config.NeedsInterruptSupport() {
		if err := acpi.Install(vm, acpi.Config{
			MemoryBase: config.MemoryBase(),
			MemorySize: config.MemorySize(),
		}); err != nil {
			unix.Close(vmFd)
			return nil, fmt.Errorf("install ACPI tables: %w", err)
		}

		timeslice.Record(tsKvmInstallACPI)
	}

	if err := config.Callbacks().OnCreateVMWithMemory(vm); err != nil {
		unix.Close(vmFd)
		return nil, fmt.Errorf("VM callback OnCreateVMWithMemory: %w", err)
	}

	timeslice.Record(tsKvmOnCreateVMWithMemory)

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

		timeslice.Record(tsKvmCreateVCPU)

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

		timeslice.Record(tsKvmMmapVCPU)

		vcpu := &virtualCPU{
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

		timeslice.Record(tsKvmArchVCPUInit)

		go vcpu.start()

		if err := config.Callbacks().OnCreateVCPU(vcpu); err != nil {
			unix.Close(vcpuFd)
			unix.Close(vmFd)
			return nil, fmt.Errorf("VM callback OnCreateVCPU %d: %w", i, err)
		}

		timeslice.Record(tsKvmOnCreateVCPU)
	}

	// Post-vCPU architecture-specific initialization (e.g., vGIC finalization on ARM64)
	if err := h.archPostVCPUInit(vm, config); err != nil {
		unix.Close(vmFd)
		return nil, fmt.Errorf("post-vCPU initialization: %w", err)
	}

	timeslice.Record(tsKvmArchPostVCPUInit)

	// Run Loader
	loader := config.Loader()

	if loader != nil {
		if err := loader.Load(vm); err != nil {
			unix.Close(vmFd)
			return nil, fmt.Errorf("load VM: %w", err)
		}

		timeslice.Record(tsKvmLoaded)
	}

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
