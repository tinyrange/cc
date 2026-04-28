//go:build linux && arm64

package kvm

import (
	"encoding/binary"
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/unix"
)

type ExitReason uint32

const (
	ExitUnknown     ExitReason = 0
	ExitException   ExitReason = 1
	ExitMMIO        ExitReason = 6
	ExitShutdown    ExitReason = 8
	ExitSystemEvent ExitReason = 24
)

type Exit struct {
	Reason      ExitReason
	PhysicalIPA uint64
	Syndrome    uint64
	MMIO        MMIOExit
	SystemEvent uint32
}

type MMIOExit struct {
	Addr  uint64
	Data  [8]byte
	Len   uint32
	Write bool
}

type VM struct {
	kvm       *Bootstrap
	vmfd      int
	vcpufd    int
	vgicfd    int
	run       []byte
	mem       []byte
	memRegion kvmUserspaceMemoryRegion
}

func NewVM() (*VM, error) {
	k, err := Open()
	if err != nil {
		return nil, err
	}
	vmfd, err := k.CreateVM()
	if err != nil {
		_ = k.Close()
		return nil, fmt.Errorf("create vm: %w", err)
	}
	vgicfd, err := initVGIC(vmfd)
	if err != nil {
		_ = k.CloseVM(vmfd)
		_ = k.Close()
		return nil, fmt.Errorf("configure vgic: %w", err)
	}
	vcpufd, err := k.CreateVCPU(vmfd, 0)
	if err != nil {
		_ = unix.Close(vgicfd)
		_ = k.CloseVM(vmfd)
		_ = k.Close()
		return nil, fmt.Errorf("create vcpu: %w", err)
	}
	if err := finalizeVGIC(vgicfd); err != nil {
		_ = k.CloseVCPU(vcpufd)
		_ = unix.Close(vgicfd)
		_ = k.CloseVM(vmfd)
		_ = k.Close()
		return nil, fmt.Errorf("finalize vgic: %w", err)
	}
	if err := k.InitVCPU(vmfd, vcpufd); err != nil {
		_ = k.CloseVCPU(vcpufd)
		_ = unix.Close(vgicfd)
		_ = k.CloseVM(vmfd)
		_ = k.Close()
		return nil, fmt.Errorf("init vcpu: %w", err)
	}
	mmapSize, err := k.VcpuMmapSize()
	if err != nil {
		_ = k.CloseVCPU(vcpufd)
		_ = unix.Close(vgicfd)
		_ = k.CloseVM(vmfd)
		_ = k.Close()
		return nil, fmt.Errorf("get kvm_run mmap size: %w", err)
	}
	run, err := unix.Mmap(vcpufd, 0, mmapSize, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		_ = k.CloseVCPU(vcpufd)
		_ = unix.Close(vgicfd)
		_ = k.CloseVM(vmfd)
		_ = k.Close()
		return nil, fmt.Errorf("mmap kvm_run: %w", err)
	}
	return &VM{kvm: k, vmfd: vmfd, vcpufd: vcpufd, vgicfd: vgicfd, run: run}, nil
}

func (v *VM) Close() error {
	if v == nil {
		return nil
	}
	if len(v.run) != 0 {
		_ = unix.Munmap(v.run)
		v.run = nil
	}
	if len(v.mem) != 0 {
		_ = unix.Munmap(v.mem)
		v.mem = nil
	}
	if v.kvm != nil {
		_ = v.kvm.CloseVCPU(v.vcpufd)
		if v.vgicfd >= 0 {
			_ = unix.Close(v.vgicfd)
			v.vgicfd = -1
		}
		_ = v.kvm.CloseVM(v.vmfd)
		_ = v.kvm.Close()
		v.vcpufd = -1
		v.vmfd = -1
		v.kvm = nil
	}
	return nil
}

func (v *VM) MapAnonymousMemory(size uint64, guestPhysAddr uint64) ([]byte, error) {
	mem, err := unix.Mmap(-1, 0, int(size), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_ANONYMOUS|unix.MAP_PRIVATE)
	if err != nil {
		return nil, fmt.Errorf("mmap guest memory: %w", err)
	}
	region := kvmUserspaceMemoryRegion{
		Slot:          0,
		GuestPhysAddr: guestPhysAddr,
		MemorySize:    size,
		UserspaceAddr: uint64(uintptr(unsafe.Pointer(&mem[0]))),
	}
	if err := setUserMemoryRegion(v.vmfd, &region); err != nil {
		_ = unix.Munmap(mem)
		return nil, fmt.Errorf("set user memory region: %w", err)
	}
	v.mem = mem
	v.memRegion = region
	return mem, nil
}

func (v *VM) SetX(index int, value uint64) error {
	if index < 0 || index > 30 {
		return fmt.Errorf("invalid x register %d", index)
	}
	return setOneReg(v.vcpufd, regX(index), unsafe.Pointer(&value))
}

func (v *VM) SetPC(value uint64) error {
	return setOneReg(v.vcpufd, regPC, unsafe.Pointer(&value))
}

func (v *VM) SetPState(value uint64) error {
	return setOneReg(v.vcpufd, regPState, unsafe.Pointer(&value))
}

func (v *VM) SetSP(value uint64) error {
	return setOneReg(v.vcpufd, regSP, unsafe.Pointer(&value))
}

func (v *VM) SetSpEl1(value uint64) error {
	err := setOneReg(v.vcpufd, regSpEl1, unsafe.Pointer(&value))
	if err == nil {
		return nil
	}
	if errors.Is(err, unix.ENOENT) {
		return v.SetSP(value)
	}
	return err
}

func (v *VM) GetPC() (uint64, error) {
	var value uint64
	if err := getOneReg(v.vcpufd, regPC, unsafe.Pointer(&value)); err != nil {
		return 0, err
	}
	return value, nil
}

func (v *VM) Run(exit *Exit) error {
	if exit == nil {
		return fmt.Errorf("exit is nil")
	}
	run := (*kvmRunData)(unsafe.Pointer(&v.run[0]))
	run.immediateExit = 0
	if _, err := ioctlWithRetry(uintptr(v.vcpufd), uint64(kvmRun), 0); err != nil {
		return fmt.Errorf("run vcpu: %w", err)
	}
	reason := ExitReason(run.exitReason)
	*exit = Exit{Reason: reason}
	switch reason {
	case ExitMMIO:
		mmio := (*kvmExitMMIOData)(unsafe.Pointer(&run.anon0[0]))
		exit.MMIO = MMIOExit{
			Addr:  mmio.physAddr,
			Data:  mmio.data,
			Len:   mmio.len,
			Write: mmio.isWrite != 0,
		}
	case ExitSystemEvent:
		system := (*kvmSystemEvent)(unsafe.Pointer(&run.anon0[0]))
		exit.SystemEvent = system.typ
	case ExitShutdown:
	case ExitException:
		// KVM arm64 MMIO should normally be surfaced as KVM_EXIT_MMIO.
	default:
		if reason == 17 {
			ie := (*internalError)(unsafe.Pointer(&run.anon0[0]))
			return fmt.Errorf("internal error: %d", ie.Suberror)
		}
	}
	return nil
}

func (v *VM) CancelRun() error {
	if v == nil || len(v.run) == 0 {
		return nil
	}
	run := (*kvmRunData)(unsafe.Pointer(&v.run[0]))
	run.immediateExit = 1
	return nil
}

func (v *VM) CompleteMMIORead(value uint64, size uint32) {
	run := (*kvmRunData)(unsafe.Pointer(&v.run[0]))
	mmio := (*kvmExitMMIOData)(unsafe.Pointer(&run.anon0[0]))
	for i := range mmio.data {
		mmio.data[i] = 0
	}
	switch size {
	case 1:
		mmio.data[0] = byte(value)
	case 2:
		binary.LittleEndian.PutUint16(mmio.data[:2], uint16(value))
	case 4:
		binary.LittleEndian.PutUint32(mmio.data[:4], uint32(value))
	default:
		binary.LittleEndian.PutUint64(mmio.data[:8], value)
	}
}

func (v *VM) SetIRQ(line uint32, level bool) error {
	if v == nil {
		return fmt.Errorf("vm is nil")
	}
	if line >= 1020 {
		return fmt.Errorf("irq %d out of range", line)
	}
	kvmIRQ := (kvmArmIRQTypeSPI << 24) | ((line + 32) & 0xffff)
	if err := irqLevel(v.vmfd, kvmIRQ, level); err != nil {
		return fmt.Errorf("set irq %d level=%v: %w", line, level, err)
	}
	return nil
}

func (v *VM) ReadIPA(addr uint64, size int) ([]byte, error) {
	if v == nil {
		return nil, fmt.Errorf("vm is nil")
	}
	if size < 0 {
		return nil, fmt.Errorf("invalid read size %d", size)
	}
	if size == 0 {
		return []byte{}, nil
	}
	out := make([]byte, size)
	if err := v.ReadIPAInto(addr, out); err != nil {
		return nil, err
	}
	return out, nil
}

func (v *VM) ReadIPAInto(addr uint64, dst []byte) error {
	if v == nil {
		return fmt.Errorf("vm is nil")
	}
	if len(dst) == 0 {
		return nil
	}
	start := v.memRegion.GuestPhysAddr
	end := start + v.memRegion.MemorySize
	if addr < start || addr+uint64(len(dst)) > end {
		return fmt.Errorf("read guest memory %#x size %d: unmapped", addr, len(dst))
	}
	off := addr - start
	copy(dst, v.mem[off:off+uint64(len(dst))])
	return nil
}

func (v *VM) WriteIPA(addr uint64, data []byte) error {
	if v == nil {
		return fmt.Errorf("vm is nil")
	}
	start := v.memRegion.GuestPhysAddr
	end := start + v.memRegion.MemorySize
	if addr < start || addr+uint64(len(data)) > end {
		return fmt.Errorf("write guest memory %#x size %d: unmapped", addr, len(data))
	}
	off := addr - start
	copy(v.mem[off:off+uint64(len(data))], data)
	return nil
}
