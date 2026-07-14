//go:build windows

package vm

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

func hostMemoryMB() uint64 {
	var status windows.MemStatusEx
	status.Length = uint32(unsafe.Sizeof(status))
	if windows.GlobalMemoryStatusEx(&status) != nil {
		return 0
	}
	return status.TotalPhys >> 20
}
