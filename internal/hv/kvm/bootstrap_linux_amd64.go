//go:build linux && amd64

package kvm

import (
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

const (
	kvmAPIVersion    = 12
	kvmGetAPIVersion = 0xae00
)

type Bootstrap struct {
	fd         int
	apiVersion int
}

type ProbeInfo struct {
	APIVersion   int
	VcpuMmapSize int
	VMCreateOK   bool
	VCPUCreateOK bool
	VCPUInitOK   bool
}

func Open() (*Bootstrap, error) {
	fd, err := unix.Open("/dev/kvm", unix.O_CLOEXEC|unix.O_RDWR, 0)
	if err != nil {
		switch {
		case errors.Is(err, unix.ENOENT):
			return nil, fmt.Errorf("kvm unavailable: /dev/kvm not found")
		case errors.Is(err, unix.EACCES), errors.Is(err, unix.EPERM):
			return nil, fmt.Errorf("kvm unavailable: cannot open /dev/kvm: %w", err)
		default:
			return nil, fmt.Errorf("kvm unavailable: open /dev/kvm: %w", err)
		}
	}
	version, err := ioctlInt(fd, kvmGetAPIVersion)
	if err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("kvm unavailable: query API version: %w", err)
	}
	if version != kvmAPIVersion {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("kvm unavailable: unsupported API version %d, want %d", version, kvmAPIVersion)
	}
	return &Bootstrap{fd: fd, apiVersion: version}, nil
}

func Probe() (ProbeInfo, error) {
	kvm, err := Open()
	if err != nil {
		return ProbeInfo{}, err
	}
	defer kvm.Close()
	mmapSize, err := kvm.VcpuMmapSize()
	if err != nil {
		return ProbeInfo{}, fmt.Errorf("kvm unavailable: query vcpu mmap size: %w", err)
	}
	info := ProbeInfo{APIVersion: kvm.APIVersion(), VcpuMmapSize: mmapSize}
	vmfd, err := kvm.CreateVM()
	if err != nil {
		return ProbeInfo{}, fmt.Errorf("kvm unavailable: create VM: %w", err)
	}
	info.VMCreateOK = true
	if err := kvm.InitVM(vmfd); err != nil {
		_ = kvm.CloseVM(vmfd)
		return ProbeInfo{}, fmt.Errorf("kvm unavailable: initialize VM: %w", err)
	}
	vcpufd, err := kvm.CreateVCPU(vmfd, 0)
	if err != nil {
		_ = kvm.CloseVM(vmfd)
		return ProbeInfo{}, fmt.Errorf("kvm unavailable: create vcpu: %w", err)
	}
	info.VCPUCreateOK = true
	if err := kvm.InitVCPU(vmfd, vcpufd); err != nil {
		_ = kvm.CloseVCPU(vcpufd)
		_ = kvm.CloseVM(vmfd)
		return ProbeInfo{}, fmt.Errorf("kvm unavailable: initialize vcpu: %w", err)
	}
	info.VCPUInitOK = true
	_ = kvm.CloseVCPU(vcpufd)
	_ = kvm.CloseVM(vmfd)
	return info, nil
}

func (b *Bootstrap) Close() error {
	if b == nil || b.fd < 0 {
		return nil
	}
	err := unix.Close(b.fd)
	b.fd = -1
	return err
}

func (b *Bootstrap) APIVersion() int {
	if b == nil {
		return 0
	}
	return b.apiVersion
}

func (b *Bootstrap) VcpuMmapSize() (int, error) {
	return ioctlInt(b.fd, kvmGetVcpuMmapSize)
}

func (b *Bootstrap) CreateVM() (int, error) {
	return createVM(b.fd, 0)
}

func (b *Bootstrap) CloseVM(fd int) error {
	if fd < 0 {
		return nil
	}
	return unix.Close(fd)
}

func (b *Bootstrap) CreateVCPU(vmfd int, id int) (int, error) {
	return createVCPU(vmfd, id)
}

func (b *Bootstrap) InitVM(vmfd int) error {
	if err := setTSSAddr(vmfd, 0xfffbd000); err != nil {
		return err
	}
	if err := createIRQChip(vmfd); err != nil && !errors.Is(err, unix.EEXIST) {
		return err
	}
	if err := createPIT(vmfd); err != nil && !errors.Is(err, unix.EEXIST) {
		return err
	}
	return nil
}

func (b *Bootstrap) InitVCPU(vmfd, vcpufd int) error {
	_ = vmfd
	cpuid, err := getSupportedCPUID(b.fd)
	if err != nil {
		return err
	}
	return setVCPUID(vcpufd, cpuid)
}

func (b *Bootstrap) CloseVCPU(fd int) error {
	if fd < 0 {
		return nil
	}
	return unix.Close(fd)
}

func ioctlInt(fd int, request int) (int, error) {
	ret, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(request), 0)
	if errno != 0 {
		return 0, errno
	}
	return int(ret), nil
}
