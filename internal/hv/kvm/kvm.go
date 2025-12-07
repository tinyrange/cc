//go:build linux

package kvm

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"github.com/tinyrange/cc/internal/acpi"
	"github.com/tinyrange/cc/internal/devices/amd64/chipset"
	"github.com/tinyrange/cc/internal/hv"
	"golang.org/x/sys/unix"
)

type virtualCPU struct {
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
	hv             *hypervisor
	vmFd           int
	vcpus          map[int]*virtualCPU
	memory         []byte
	memoryBase     uint64
	devices        []hv.Device
	lastMemorySlot uint32

	// amd64-specific fields
	hasIRQChip bool
	hasPIT     bool
	ioapic     *chipset.IOAPIC

	// arm64-specific fields
	arm64GICInfo hv.Arm64GICInfo
	arm64VGICFd  int // vGIC device file descriptor
}

// implements hv.VirtualMachine.
func (v *virtualMachine) MemoryBase() uint64        { return v.memoryBase }
func (v *virtualMachine) MemorySize() uint64        { return uint64(len(v.memory)) }
func (v *virtualMachine) Hypervisor() hv.Hypervisor { return v.hv }

// AllocateMemory implements hv.VirtualMachine.
func (v *virtualMachine) AllocateMemory(physAddr uint64, size uint64) (hv.MemoryRegion, error) {
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

	if v.hv.Architecture() == hv.ArchitectureX86_64 {
		if err := unix.Madvise(mem, unix.MADV_MERGEABLE); err != nil {
			unix.Munmap(mem)
			return nil, fmt.Errorf("madvise memory: %w", err)
		}
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

	return &memoryRegion{mem: mem}, nil
}

// AddDevice implements hv.VirtualMachine.
func (v *virtualMachine) AddDevice(dev hv.Device) error {
	v.devices = append(v.devices, dev)

	// Capture IOAPIC device for routing integration.
	if ioa, ok := dev.(*chipset.IOAPIC); ok {
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

	if err := unix.Munmap(v.memory); err != nil {
		return fmt.Errorf("munmap vm memory: %w", err)
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

// NewVirtualMachine implements hv.Hypervisor.
func (h *hypervisor) NewVirtualMachine(config hv.VMConfig) (hv.VirtualMachine, error) {
	vm := &virtualMachine{
		hv:    h,
		vcpus: make(map[int]*virtualCPU),
	}

	vmFd, err := createVm(h.fd)
	if err != nil {
		return nil, fmt.Errorf("create VM: %w", err)
	}

	vm.vmFd = vmFd

	if err := h.archVMInit(vm, config); err != nil {
		unix.Close(vmFd)
		return nil, fmt.Errorf("initialize VM: %w", err)
	}

	if err := config.Callbacks().OnCreateVM(vm); err != nil {
		unix.Close(vmFd)
		return nil, fmt.Errorf("VM callback OnCreateVM: %w", err)
	}

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

	if h.Architecture() == hv.ArchitectureX86_64 {
		if err := unix.Madvise(mem, unix.MADV_MERGEABLE); err != nil {
			unix.Munmap(mem)
			return nil, fmt.Errorf("madvise memory: %w", err)
		}
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

	if h.Architecture() == hv.ArchitectureX86_64 && config.NeedsInterruptSupport() {
		if err := acpi.Install(vm, acpi.Config{
			MemoryBase: config.MemoryBase(),
			MemorySize: config.MemorySize(),
		}); err != nil {
			unix.Close(vmFd)
			return nil, fmt.Errorf("install ACPI tables: %w", err)
		}
	}

	if err := config.Callbacks().OnCreateVMWithMemory(vm); err != nil {
		unix.Close(vmFd)
		return nil, fmt.Errorf("VM callback OnCreateVMWithMemory: %w", err)
	}

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

		go vcpu.start()

		if err := config.Callbacks().OnCreateVCPU(vcpu); err != nil {
			unix.Close(vcpuFd)
			unix.Close(vmFd)
			return nil, fmt.Errorf("VM callback OnCreateVCPU %d: %w", i, err)
		}
	}

	// Post-vCPU architecture-specific initialization (e.g., vGIC finalization on ARM64)
	if err := h.archPostVCPUInit(vm, config); err != nil {
		unix.Close(vmFd)
		return nil, fmt.Errorf("post-vCPU initialization: %w", err)
	}

	// Run Loader
	loader := config.Loader()

	if loader != nil {
		if err := loader.Load(vm); err != nil {
			unix.Close(vmFd)
			return nil, fmt.Errorf("load VM: %w", err)
		}
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
