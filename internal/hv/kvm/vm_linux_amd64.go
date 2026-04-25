//go:build linux && amd64

package kvm

import (
	"encoding/binary"
	"fmt"
	"unsafe"

	"golang.org/x/sys/unix"
	"j5.nz/cc/internal/amd64vm"
)

type ExitReason uint32

const (
	ExitUnknown     ExitReason = 0
	ExitIO          ExitReason = 2
	ExitHLT         ExitReason = 5
	ExitMMIO        ExitReason = 6
	ExitShutdown    ExitReason = 8
	ExitInternal    ExitReason = 17
	ExitSystemEvent ExitReason = 24
)

type Exit struct {
	Reason      ExitReason
	IO          IOExit
	MMIO        MMIOExit
	SystemEvent uint32
}

type IOExit struct {
	Port  uint16
	Data  []byte
	Size  uint8
	Count uint32
	Write bool
}

type MMIOExit struct {
	Addr  uint64
	Data  [8]byte
	Len   uint32
	Write bool
}

type VM struct {
	kvm     *Bootstrap
	vmfd    int
	vcpufd  int
	run     []byte
	mem     []byte
	regions []memoryMapping
}

type memoryMapping struct {
	guestPhysAddr uint64
	mem           []byte
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
	if err := k.InitVM(vmfd); err != nil {
		_ = k.CloseVM(vmfd)
		_ = k.Close()
		return nil, fmt.Errorf("init vm: %w", err)
	}
	vcpufd, err := k.CreateVCPU(vmfd, 0)
	if err != nil {
		_ = k.CloseVM(vmfd)
		_ = k.Close()
		return nil, fmt.Errorf("create vcpu: %w", err)
	}
	if err := k.InitVCPU(vmfd, vcpufd); err != nil {
		_ = k.CloseVCPU(vcpufd)
		_ = k.CloseVM(vmfd)
		_ = k.Close()
		return nil, fmt.Errorf("init vcpu: %w", err)
	}
	mmapSize, err := k.VcpuMmapSize()
	if err != nil {
		_ = k.CloseVCPU(vcpufd)
		_ = k.CloseVM(vmfd)
		_ = k.Close()
		return nil, fmt.Errorf("get kvm_run mmap size: %w", err)
	}
	run, err := unix.Mmap(vcpufd, 0, mmapSize, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		_ = k.CloseVCPU(vcpufd)
		_ = k.CloseVM(vmfd)
		_ = k.Close()
		return nil, fmt.Errorf("mmap kvm_run: %w", err)
	}
	return &VM{kvm: k, vmfd: vmfd, vcpufd: vcpufd, run: run}, nil
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
	for _, region := range v.regions {
		if len(region.mem) != 0 {
			_ = unix.Munmap(region.mem)
		}
	}
	v.regions = nil
	if v.kvm != nil {
		_ = v.kvm.CloseVCPU(v.vcpufd)
		_ = v.kvm.CloseVM(v.vmfd)
		_ = v.kvm.Close()
		v.kvm = nil
	}
	return nil
}

func (v *VM) MapAnonymousMemory(size uint64, guestPhysAddr uint64) ([]byte, error) {
	return v.MapAnonymousMemorySlot(0, size, guestPhysAddr)
}

func (v *VM) MapAnonymousMemorySlot(slot uint32, size uint64, guestPhysAddr uint64) ([]byte, error) {
	mem, err := unix.Mmap(-1, 0, int(size), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_ANONYMOUS|unix.MAP_PRIVATE)
	if err != nil {
		return nil, fmt.Errorf("mmap guest memory: %w", err)
	}
	region := kvmUserspaceMemoryRegion{
		Slot:          slot,
		GuestPhysAddr: guestPhysAddr,
		MemorySize:    size,
		UserspaceAddr: uint64(uintptr(unsafe.Pointer(&mem[0]))),
	}
	if err := setUserMemoryRegion(v.vmfd, &region); err != nil {
		_ = unix.Munmap(mem)
		return nil, fmt.Errorf("set user memory region: %w", err)
	}
	if slot == 0 {
		v.mem = mem
	} else {
		v.regions = append(v.regions, memoryMapping{guestPhysAddr: guestPhysAddr, mem: mem})
	}
	return mem, nil
}

func mapAMD64GuestMemory(vm *VM, memoryMB uint64) ([]byte, error) {
	lowSize := amd64vm.LowMemorySizeBytes(memoryMB)
	mem, err := vm.MapAnonymousMemorySlot(0, lowSize, amd64vm.MemoryBase)
	if err != nil {
		return nil, err
	}
	if highSize := amd64vm.HighMemorySizeBytes(memoryMB); highSize > 0 {
		if _, err := vm.MapAnonymousMemorySlot(1, highSize, amd64vm.HighMemoryBase); err != nil {
			return nil, err
		}
	}
	return mem, nil
}

func (v *VM) Run() (*Exit, error) {
	run := (*kvmRunData)(unsafe.Pointer(&v.run[0]))
	run.immediateExit = 0
	if _, err := ioctlWithRetry(uintptr(v.vcpufd), uint64(kvmRun), 0); err != nil {
		return nil, fmt.Errorf("run vcpu: %w", err)
	}
	reason := ExitReason(run.exitReason)
	switch reason {
	case ExitIO:
		ioData := (*kvmExitIoData)(unsafe.Pointer(&run.anon0[0]))
		dataLen := uint64(ioData.size) * uint64(ioData.count)
		data := v.run[ioData.dataOffset : ioData.dataOffset+dataLen]
		return &Exit{
			Reason: reason,
			IO: IOExit{
				Port:  ioData.port,
				Data:  data,
				Size:  ioData.size,
				Count: ioData.count,
				Write: ioData.direction != 0,
			},
		}, nil
	case ExitMMIO:
		mmio := (*kvmExitMMIOData)(unsafe.Pointer(&run.anon0[0]))
		return &Exit{Reason: reason, MMIO: MMIOExit{Addr: mmio.physAddr, Data: mmio.data, Len: mmio.len, Write: mmio.isWrite != 0}}, nil
	case ExitInternal:
		ie := (*internalError)(unsafe.Pointer(&run.anon0[0]))
		return nil, fmt.Errorf("internal error: %d", ie.Suberror)
	case ExitSystemEvent:
		system := (*kvmSystemEvent)(unsafe.Pointer(&run.anon0[0]))
		return &Exit{Reason: reason, SystemEvent: system.typ}, nil
	default:
		return &Exit{Reason: reason}, nil
	}
}

func (v *VM) GetPC() (uint64, error) {
	regs, err := getRegs(v.vcpufd)
	if err != nil {
		return 0, err
	}
	return regs.Rip, nil
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
	if err := v.copyFromGuest(addr, out); err != nil {
		return nil, fmt.Errorf("read guest memory %#x size %d: unmapped", addr, size)
	}
	return out, nil
}

func (v *VM) WriteIPA(addr uint64, data []byte) error {
	if v == nil {
		return fmt.Errorf("vm is nil")
	}
	if len(data) == 0 {
		return nil
	}
	if err := v.copyToGuest(addr, data); err != nil {
		return fmt.Errorf("write guest memory %#x size %d: unmapped", addr, len(data))
	}
	return nil
}

func (v *VM) copyFromGuest(addr uint64, out []byte) error {
	for len(out) > 0 {
		region, off, ok := v.findMemoryRegion(addr)
		if !ok {
			return fmt.Errorf("unmapped")
		}
		n := copy(out, region[off:])
		out = out[n:]
		addr += uint64(n)
	}
	return nil
}

func (v *VM) copyToGuest(addr uint64, data []byte) error {
	for len(data) > 0 {
		region, off, ok := v.findMemoryRegion(addr)
		if !ok {
			return fmt.Errorf("unmapped")
		}
		n := copy(region[off:], data)
		data = data[n:]
		addr += uint64(n)
	}
	return nil
}

func (v *VM) findMemoryRegion(addr uint64) ([]byte, uint64, bool) {
	if addr < uint64(len(v.mem)) {
		return v.mem, addr, true
	}
	for _, region := range v.regions {
		start := region.guestPhysAddr
		end := start + uint64(len(region.mem))
		if addr >= start && addr < end {
			return region.mem, addr - start, true
		}
	}
	return nil, 0, false
}

func (v *VM) SetLongMode(entry, zeroPage, stack, pagingBase uint64) error {
	if err := v.setupPageTables(pagingBase, 4); err != nil {
		return err
	}
	sregs, err := getSRegs(v.vcpufd)
	if err != nil {
		return err
	}
	const (
		cr0PE   = 1 << 0
		cr0MP   = 1 << 1
		cr0ET   = 1 << 4
		cr0NE   = 1 << 5
		cr0WP   = 1 << 16
		cr0AM   = 1 << 18
		cr0PG   = 1 << 31
		cr4PAE  = 1 << 5
		eferLME = 1 << 8
		eferLMA = 1 << 10
	)
	sregs.Cr3 = pagingBase
	sregs.Cr4 |= cr4PAE
	sregs.Cr0 |= cr0PE | cr0MP | cr0ET | cr0NE | cr0WP | cr0AM | cr0PG
	sregs.Efer = eferLME | eferLMA

	code := kvmSegment{Base: 0, Limit: 0xffffffff, Selector: 0x10, Type: 11, Present: 1, S: 1, L: 1, G: 1}
	data := kvmSegment{Base: 0, Limit: 0xffffffff, Selector: 0x18, Type: 3, Present: 1, S: 1, Db: 1, G: 1}
	sregs.Cs = code
	sregs.Ds = data
	sregs.Es = data
	sregs.Fs = data
	sregs.Gs = data
	sregs.Ss = data
	if err := setSRegs(v.vcpufd, &sregs); err != nil {
		return err
	}
	return setRegs(v.vcpufd, &kvmRegs{
		Rip:    entry,
		Rsi:    zeroPage,
		Rsp:    stack,
		Rflags: 0x2,
	})
}

func (v *VM) setupPageTables(pagingBase uint64, giB int) error {
	if pagingBase+uint64(0x3000+giB*0x1000) > uint64(len(v.mem)) {
		return fmt.Errorf("paging structures do not fit")
	}
	put64 := func(addr, value uint64) {
		binary.LittleEndian.PutUint64(v.mem[addr:addr+8], value)
	}
	pml4 := pagingBase
	pdpt := pagingBase + 0x1000
	pdBase := pagingBase + 0x2000
	const (
		p  = 1 << 0
		rw = 1 << 1
		us = 1 << 2
		ps = 1 << 7
	)
	put64(pml4, pdpt|p|rw|us)
	for g := 0; g < giB; g++ {
		pd := pdBase + uint64(g)*0x1000
		put64(pdpt+uint64(g)*8, pd|p|rw|us)
		for i := 0; i < 512; i++ {
			phys := (uint64(g) << 30) | (uint64(i) << 21)
			put64(pd+uint64(i)*8, phys|p|rw|us|ps)
		}
	}
	return nil
}

func (v *VM) SetIRQ(line uint32, level bool) error {
	if v == nil {
		return fmt.Errorf("vm is nil")
	}
	if line >= kvmNrInterrupts {
		return fmt.Errorf("irq %d out of range", line)
	}
	if err := irqLevel(v.vmfd, line, level); err != nil {
		return fmt.Errorf("set irq %d level=%v: %w", line, level, err)
	}
	return nil
}
