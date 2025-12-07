//go:build linux && amd64

package kvm

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/unix"
)

func getRegisters(vcpuFd int) (kvmRegs, error) {
	var regs kvmRegs

	if _, err := ioctlWithRetry(uintptr(vcpuFd), uint64(kvmGetRegs), uintptr(unsafe.Pointer(&regs))); err != nil {
		return kvmRegs{}, err
	}

	return regs, nil
}

func setRegisters(vcpuFd int, regs *kvmRegs) error {
	_, err := ioctlWithRetry(uintptr(vcpuFd), uint64(kvmSetRegs), uintptr(unsafe.Pointer(regs)))
	return err
}

func getSpecialRegisters(vcpuFd int) (kvmSRegs, error) {
	var sregs kvmSRegs

	if _, err := ioctlWithRetry(uintptr(vcpuFd), uint64(kvmGetSregs), uintptr(unsafe.Pointer(&sregs))); err != nil {
		return kvmSRegs{}, err
	}

	return sregs, nil
}

func setSpecialRegisters(vcpuFd int, sregs *kvmSRegs) error {
	_, err := ioctlWithRetry(uintptr(vcpuFd), uint64(kvmSetSregs), uintptr(unsafe.Pointer(sregs)))
	return err
}

func setTSSAddr(vmFd int, addr uint64) error {
	_, err := ioctlWithRetry(uintptr(vmFd), uint64(kvmSetTssAddr), uintptr(addr))
	return err
}

func getSupportedCpuId(hvFd int) (*kvmCPUID2, error) {
	// get the size of the cpuid structure
	size := unsafe.Sizeof(kvmCPUID2{}) + unsafe.Sizeof(kvmCPUIDEntry2{})*255
	cpuidData := make([]byte, size)
	cpuid := (*kvmCPUID2)(unsafe.Pointer(&cpuidData[0]))
	cpuid.Nr = 255

	if _, err := ioctlWithRetry(uintptr(hvFd), kvmGetSupportedCpuid, uintptr(unsafe.Pointer(cpuid))); err != nil {
		return nil, fmt.Errorf("KVM_GET_SUPPORTED_CPUID: %w", err)
	}

	return cpuid, nil
}

func setVCPUID(vcpuFd int, cpuId *kvmCPUID2) error {
	_, err := ioctlWithRetry(uintptr(vcpuFd), uint64(kvmSetCpuid2), uintptr(unsafe.Pointer(cpuId)))
	return err
}

func getSRegs(vcpuFd int) (kvmSRegs, error) {
	var sregs kvmSRegs

	if _, err := ioctlWithRetry(uintptr(vcpuFd), uint64(kvmGetSregs), uintptr(unsafe.Pointer(&sregs))); err != nil {
		return kvmSRegs{}, err
	}

	return sregs, nil
}

func setSRegs(vcpuFd int, sregs *kvmSRegs) error {
	_, err := ioctlWithRetry(uintptr(vcpuFd), uint64(kvmSetSregs), uintptr(unsafe.Pointer(sregs)))
	return err
}

func getFPU(vcpuFd int) (kvmFPU, error) {
	var fpu kvmFPU

	if _, err := ioctlWithRetry(uintptr(vcpuFd), uint64(kvmGetFpu), uintptr(unsafe.Pointer(&fpu))); err != nil {
		return kvmFPU{}, err
	}

	return fpu, nil
}

func setFPU(vcpuFd int, fpu *kvmFPU) error {
	_, err := ioctlWithRetry(uintptr(vcpuFd), uint64(kvmSetFpu), uintptr(unsafe.Pointer(fpu)))
	return err
}

func getXsave(vcpuFd int) (kvmXsave, error) {
	var xsave kvmXsave

	if _, err := ioctlWithRetry(uintptr(vcpuFd), uint64(kvmGetXsave), uintptr(unsafe.Pointer(&xsave))); err != nil {
		return kvmXsave{}, err
	}

	return xsave, nil
}

func setXsave(vcpuFd int, xsave *kvmXsave) error {
	_, err := ioctlWithRetry(uintptr(vcpuFd), uint64(kvmSetXsave), uintptr(unsafe.Pointer(xsave)))
	return err
}

func getXcrs(vcpuFd int) (kvmXcrs, error) {
	var xcrs kvmXcrs

	if _, err := ioctlWithRetry(uintptr(vcpuFd), uint64(kvmGetXcrs), uintptr(unsafe.Pointer(&xcrs))); err != nil {
		return kvmXcrs{}, err
	}

	return xcrs, nil
}

func setXcrs(vcpuFd int, xcrs *kvmXcrs) error {
	_, err := ioctlWithRetry(uintptr(vcpuFd), uint64(kvmSetXcrs), uintptr(unsafe.Pointer(xcrs)))
	return err
}

func getLapic(vcpuFd int) (kvmLapicState, error) {
	var lapic kvmLapicState

	if _, err := ioctlWithRetry(uintptr(vcpuFd), uint64(kvmGetLapic), uintptr(unsafe.Pointer(&lapic))); err != nil {
		return kvmLapicState{}, err
	}

	return lapic, nil
}

func setLapic(vcpuFd int, lapic *kvmLapicState) error {
	_, err := ioctlWithRetry(uintptr(vcpuFd), uint64(kvmSetLapic), uintptr(unsafe.Pointer(lapic)))
	return err
}

func createIRQChip(vmFd int) error {
	_, err := ioctlWithRetry(uintptr(vmFd), uint64(kvmCreateIrqchip), 0)
	return err
}

func getIRQChip(vmFd int, chipID uint32) (kvmIRQChip, error) {
	var chip kvmIRQChip
	chip.ChipID = chipID

	if _, err := ioctlWithRetry(uintptr(vmFd), uint64(kvmGetIrqchip), uintptr(unsafe.Pointer(&chip))); err != nil {
		return kvmIRQChip{}, err
	}

	return chip, nil
}

func setIRQChip(vmFd int, chip *kvmIRQChip) error {
	_, err := ioctlWithRetry(uintptr(vmFd), uint64(kvmSetIrqchip), uintptr(unsafe.Pointer(chip)))
	return err
}

func createPIT(vmFd int) error {
	var cfg kvmPitConfig
	_, err := ioctlWithRetry(uintptr(vmFd), uint64(kvmCreatePit2), uintptr(unsafe.Pointer(&cfg)))
	return err
}

func enableSplitIRQChip(vmFd int, numIrqs int) error {
	var cap kvmEnableCapArgs
	cap.Cap = kvmCapSplitIrqchip
	cap.Flags = 0
	cap.Args[0] = uint64(numIrqs)
	_, err := ioctlWithRetry(uintptr(vmFd), uint64(kvmEnableCap), uintptr(unsafe.Pointer(&cap)))
	return err
}

func getPitState(vmFd int) (kvmPitState2, error) {
	var state kvmPitState2

	if _, err := ioctlWithRetry(uintptr(vmFd), uint64(kvmGetPit2), uintptr(unsafe.Pointer(&state))); err != nil {
		return kvmPitState2{}, err
	}

	return state, nil
}

func setPitState(vmFd int, state *kvmPitState2) error {
	_, err := ioctlWithRetry(uintptr(vmFd), uint64(kvmSetPit2), uintptr(unsafe.Pointer(state)))
	return err
}

func irqLevel(vmFd int, irqLine uint32, level bool) error {
	var line kvmIRQLevel

	line.IRQOrStatus = irqLine
	if level {
		line.Level = 1
	} else {
		line.Level = 0
	}

	_, err := ioctlWithRetry(uintptr(vmFd), uint64(kvmIrqLine), uintptr(unsafe.Pointer(&line)))
	return err
}

func getMsrIndexList(fd int) ([]uint32, error) {
	baseSize := unsafe.Sizeof(kvmMsrList{})
	buf := make([]byte, baseSize)
	list := (*kvmMsrList)(unsafe.Pointer(&buf[0]))

	if _, err := ioctlWithRetry(uintptr(fd), uint64(kvmGetMsrIndexList), uintptr(unsafe.Pointer(list))); err == nil {
		return nil, fmt.Errorf("KVM_GET_MSR_INDEX_LIST: unexpected success without space for indices")
	} else if !errors.Is(err, unix.E2BIG) {
		return nil, fmt.Errorf("KVM_GET_MSR_INDEX_LIST: %w", err)
	}

	count := list.Nmsrs
	if count == 0 {
		return nil, fmt.Errorf("KVM_GET_MSR_INDEX_LIST: kernel reported zero MSRs")
	}

	size := baseSize + uintptr(count)*unsafe.Sizeof(uint32(0))
	buf = make([]byte, size)
	list = (*kvmMsrList)(unsafe.Pointer(&buf[0]))
	list.Nmsrs = count

	if _, err := ioctlWithRetry(uintptr(fd), uint64(kvmGetMsrIndexList), uintptr(unsafe.Pointer(list))); err != nil {
		return nil, fmt.Errorf("KVM_GET_MSR_INDEX_LIST: %w", err)
	}

	firstIndex := (*uint32)(unsafe.Pointer(uintptr(unsafe.Pointer(list)) + unsafe.Sizeof(kvmMsrList{})))
	rawIndices := unsafe.Slice(firstIndex, count)

	indices := make([]uint32, count)
	copy(indices, rawIndices)

	return indices, nil
}

func getMsrs(vcpuFd int, indices []uint32) ([]kvmMsrEntry, error) {
	if len(indices) == 0 {
		return nil, nil
	}

	buf, msrsHdr, entries := makeMsrsBuffer(len(indices))
	for i, idx := range indices {
		entries[i].Index = idx
	}

	n, err := ioctlWithRetry(uintptr(vcpuFd), uint64(kvmGetMsrs), uintptr(unsafe.Pointer(msrsHdr)))
	if err != nil {
		return nil, fmt.Errorf("KVM_GET_MSRS: %w", err)
	}
	if int(n) != len(indices) {
		return nil, fmt.Errorf("KVM_GET_MSRS: read %d entries, expected %d", n, len(indices))
	}

	result := make([]kvmMsrEntry, len(indices))
	copy(result, entries)

	_ = buf

	return result, nil
}

func setMsrs(vcpuFd int, entriesToSet []kvmMsrEntry) error {
	if len(entriesToSet) == 0 {
		return nil
	}

	buf, msrsHdr, entries := makeMsrsBuffer(len(entriesToSet))
	copy(entries, entriesToSet)

	n, err := ioctlWithRetry(uintptr(vcpuFd), uint64(kvmSetMsrs), uintptr(unsafe.Pointer(msrsHdr)))
	if err != nil {
		return fmt.Errorf("KVM_SET_MSRS: %w", err)
	}
	if int(n) != len(entriesToSet) {
		return fmt.Errorf("KVM_SET_MSRS: wrote %d entries, expected %d", n, len(entriesToSet))
	}

	_ = buf

	return nil
}

func makeMsrsBuffer(count int) ([]byte, *kvmMsrs, []kvmMsrEntry) {
	size := unsafe.Sizeof(kvmMsrs{}) + uintptr(count)*unsafe.Sizeof(kvmMsrEntry{})
	buf := make([]byte, size)
	msrsHdr := (*kvmMsrs)(unsafe.Pointer(&buf[0]))
	msrsHdr.Nmsrs = uint32(count)

	if count == 0 {
		return buf, msrsHdr, nil
	}

	first := (*kvmMsrEntry)(unsafe.Pointer(uintptr(unsafe.Pointer(msrsHdr)) + unsafe.Sizeof(kvmMsrs{})))
	entries := unsafe.Slice(first, count)

	return buf, msrsHdr, entries
}
