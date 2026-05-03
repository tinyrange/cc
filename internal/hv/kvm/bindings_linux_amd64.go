//go:build linux && amd64

package kvm

import (
	"math/bits"
	"unsafe"

	"golang.org/x/sys/unix"
)

const cpuidMaxEntries = 255

const kvmCPUIDFlagSignificantIndex = 1

const (
	kvmCreateVM            = 0xae01
	kvmGetVcpuMmapSize     = 0xae04
	kvmGetSupportedCpuid   = 0xc008ae05
	kvmSetUserMemoryRegion = 0x4020ae46
	kvmIrqLine             = 0x4008ae61
	kvmCreateVCPU          = 0xae41
	kvmSetTssAddr          = 0xae47
	kvmCreateIrqchip       = 0xae60
	kvmCreatePit2          = 0x4040ae77
	kvmRun                 = 0xae80
	kvmGetRegs             = 0x8090ae81
	kvmSetRegs             = 0x4090ae82
	kvmGetSregs            = 0x8138ae83
	kvmSetSregs            = 0x4138ae84
	kvmSetCpuid2           = 0x4008ae90
)

func ioctl(fd uintptr, request uint64, arg uintptr) (uintptr, error) {
	v1, _, err := unix.Syscall(unix.SYS_IOCTL, fd, uintptr(request), arg)
	if err != 0 {
		return 0, err
	}
	return v1, nil
}

func ioctlWithRetry(fd uintptr, request uint64, arg uintptr) (uintptr, error) {
	for {
		v1, err := ioctl(fd, request, arg)
		if err == unix.EINTR {
			continue
		}
		return v1, err
	}
}

func ioctlRunVCPU(fd uintptr) (uintptr, error) {
	for {
		v1, err := ioctl(fd, uint64(kvmRun), 0)
		if err == unix.EINTR || err == unix.EAGAIN {
			continue
		}
		return v1, err
	}
}

func createVM(fd int, machineType uint32) (int, error) {
	v1, err := ioctlWithRetry(uintptr(fd), uint64(kvmCreateVM), uintptr(machineType))
	if err != nil {
		return 0, err
	}
	return int(v1), nil
}

func createVCPU(fd int, id int) (int, error) {
	v1, err := ioctlWithRetry(uintptr(fd), uint64(kvmCreateVCPU), uintptr(id))
	if err != nil {
		return 0, err
	}
	return int(v1), nil
}

func setUserMemoryRegion(fd int, region *kvmUserspaceMemoryRegion) error {
	_, err := ioctlWithRetry(uintptr(fd), uint64(kvmSetUserMemoryRegion), uintptr(unsafe.Pointer(region)))
	return err
}

func irqLevel(vmFd int, irq uint32, high bool) error {
	level := kvmIRQLevel{IRQ: irq}
	if high {
		level.Level = 1
	}
	_, err := ioctlWithRetry(uintptr(vmFd), uint64(kvmIrqLine), uintptr(unsafe.Pointer(&level)))
	return err
}

func setTSSAddr(vmFd int, addr uint64) error {
	_, err := ioctlWithRetry(uintptr(vmFd), uint64(kvmSetTssAddr), uintptr(addr))
	return err
}

func createIRQChip(vmFd int) error {
	_, err := ioctlWithRetry(uintptr(vmFd), uint64(kvmCreateIrqchip), 0)
	return err
}

func createPIT(vmFd int) error {
	var cfg kvmPitConfig
	_, err := ioctlWithRetry(uintptr(vmFd), uint64(kvmCreatePit2), uintptr(unsafe.Pointer(&cfg)))
	return err
}

func getSupportedCPUID(kvmFd int) (*kvmCPUID2, error) {
	size := unsafe.Sizeof(kvmCPUID2{}) + unsafe.Sizeof(kvmCPUIDEntry2{})*cpuidMaxEntries
	buf := make([]byte, size)
	cpuid := (*kvmCPUID2)(unsafe.Pointer(&buf[0]))
	cpuid.Nr = cpuidMaxEntries
	if _, err := ioctlWithRetry(uintptr(kvmFd), uint64(kvmGetSupportedCpuid), uintptr(unsafe.Pointer(cpuid))); err != nil {
		return nil, err
	}
	return cpuid, nil
}

func setVCPUID(vcpuFd int, cpuid *kvmCPUID2) error {
	_, err := ioctlWithRetry(uintptr(vcpuFd), uint64(kvmSetCpuid2), uintptr(unsafe.Pointer(cpuid)))
	return err
}

func setCPUIDTopology(cpuid *kvmCPUID2, id, cpus int) {
	if cpuid == nil {
		return
	}
	if cpus < 1 {
		cpus = 1
	}
	if cpus > 255 {
		cpus = 255
	}
	logical := uint32(1)
	if cpus > 1 {
		logical = uint32(1 << bits.Len(uint(cpus-1)))
	}
	ensureCPUIDEntry(cpuid, 0xb, 0)
	ensureCPUIDEntry(cpuid, 0xb, 1)
	ensureCPUIDEntry(cpuid, 0xb, 2)
	ensureCPUIDEntry(cpuid, 0x1f, 0)
	ensureCPUIDEntry(cpuid, 0x1f, 1)
	ensureCPUIDEntry(cpuid, 0x1f, 2)
	configureExtendedTopology := func(e *kvmCPUIDEntry2) {
		switch e.Index {
		case 0:
			e.Eax = 0
			e.Ebx = 1
			e.Ecx = (1 << 8) | e.Index
			e.Edx = uint32(id)
		case 1:
			e.Eax = uint32(bits.Len(uint(logical - 1)))
			e.Ebx = uint32(cpus)
			e.Ecx = (2 << 8) | e.Index
			e.Edx = uint32(id)
		default:
			e.Eax, e.Ebx, e.Ecx, e.Edx = 0, 0, e.Index, uint32(id)
		}
	}
	entries := cpuidEntries(cpuid)
	for i := range entries {
		e := &entries[i]
		switch e.Function {
		case 1:
			e.Ebx = (e.Ebx &^ (uint32(0xffff) << 16)) | (logical << 16) | (uint32(id&0xff) << 24)
			if cpus > 1 {
				e.Edx |= 1 << 28
			}
		case 0:
			if cpus > 1 && e.Eax < 0x1f {
				e.Eax = 0x1f
			}
		case 4:
			e.Eax = (e.Eax &^ (uint32(0x3f) << 26)) | (uint32(cpus-1) << 26)
		case 0xb:
			configureExtendedTopology(e)
		case 0x1f:
			configureExtendedTopology(e)
		}
	}
}

func cpuidEntries(cpuid *kvmCPUID2) []kvmCPUIDEntry2 {
	if cpuid == nil {
		return nil
	}
	return unsafe.Slice(
		(*kvmCPUIDEntry2)(unsafe.Pointer(uintptr(unsafe.Pointer(cpuid))+unsafe.Sizeof(*cpuid))),
		cpuid.Nr,
	)
}

func cpuidStorage(cpuid *kvmCPUID2) []kvmCPUIDEntry2 {
	return unsafe.Slice(
		(*kvmCPUIDEntry2)(unsafe.Pointer(uintptr(unsafe.Pointer(cpuid))+unsafe.Sizeof(*cpuid))),
		cpuidMaxEntries,
	)
}

func ensureCPUIDEntry(cpuid *kvmCPUID2, function, index uint32) *kvmCPUIDEntry2 {
	entries := cpuidEntries(cpuid)
	for i := range entries {
		entry := &entries[i]
		if entry.Function == function && entry.Index == index {
			return entry
		}
	}
	if cpuid.Nr >= cpuidMaxEntries {
		return nil
	}
	storage := cpuidStorage(cpuid)
	entry := &storage[cpuid.Nr]
	*entry = kvmCPUIDEntry2{
		Function: function,
		Index:    index,
		Flags:    kvmCPUIDFlagSignificantIndex,
	}
	cpuid.Nr++
	return entry
}

func getRegs(vcpuFd int) (kvmRegs, error) {
	var regs kvmRegs
	if _, err := ioctlWithRetry(uintptr(vcpuFd), uint64(kvmGetRegs), uintptr(unsafe.Pointer(&regs))); err != nil {
		return kvmRegs{}, err
	}
	return regs, nil
}

func setRegs(vcpuFd int, regs *kvmRegs) error {
	_, err := ioctlWithRetry(uintptr(vcpuFd), uint64(kvmSetRegs), uintptr(unsafe.Pointer(regs)))
	return err
}

func getSRegs(vcpuFd int) (kvmSRegs, error) {
	var regs kvmSRegs
	if _, err := ioctlWithRetry(uintptr(vcpuFd), uint64(kvmGetSregs), uintptr(unsafe.Pointer(&regs))); err != nil {
		return kvmSRegs{}, err
	}
	return regs, nil
}

func setSRegs(vcpuFd int, regs *kvmSRegs) error {
	_, err := ioctlWithRetry(uintptr(vcpuFd), uint64(kvmSetSregs), uintptr(unsafe.Pointer(regs)))
	return err
}
