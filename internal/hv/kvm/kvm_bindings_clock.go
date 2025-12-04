//go:build linux

package kvm

import "unsafe"

func getClock(vmFd int) (kvmClockData, error) {
	var clock kvmClockData

	if _, err := ioctlWithRetry(uintptr(vmFd), uint64(kvmGetClock), uintptr(unsafe.Pointer(&clock))); err != nil {
		return kvmClockData{}, err
	}

	return clock, nil
}

func setClock(vmFd int, clock *kvmClockData) error {
	_, err := ioctlWithRetry(uintptr(vmFd), uint64(kvmSetClock), uintptr(unsafe.Pointer(clock)))
	return err
}
