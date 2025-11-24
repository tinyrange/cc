//go:build linux

package kvm

import (
	"context"
	"fmt"
	"runtime"
	"unsafe"

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

var (
	_ hv.VirtualCPU = &virtualCPU{}
)

type virtualMachine struct {
	hv         *hypervisor
	vmFd       int
	vcpus      map[int]*virtualCPU
	memory     []byte
	memoryBase uint64
	devices    []hv.Device
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

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
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
		return 0, fmt.Errorf("kvm: WriteAt offset out of bounds")
	}

	n = copy(v.memory[off:], p)
	if n < len(p) {
		err = fmt.Errorf("kvm: WriteAt short write")
	}

	return n, err
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
	_ hv.VirtualMachine = &virtualMachine{}
)

type hypervisor struct {
	fd int
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

	if err := h.archVMInit(vmFd); err != nil {
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

	for i := 0; i < config.CPUCount(); i++ {
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
