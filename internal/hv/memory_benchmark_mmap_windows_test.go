//go:build windows && arm64

package hv

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

type windowsBenchmarkGuestMapping struct {
	addr    uintptr
	size    int
	mapping windows.Handle
}

func mapBenchmarkGuestFile(path string, size int) (benchmarkGuestMapping, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	file, err := windows.CreateFile(
		name,
		windows.GENERIC_READ|windows.GENERIC_WRITE|windows.GENERIC_EXECUTE,
		windows.FILE_SHARE_READ,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(file)

	mapping, err := windows.CreateFileMapping(file, nil, windows.PAGE_EXECUTE_WRITECOPY, 0, 0, nil)
	if err != nil {
		return nil, err
	}
	addr, err := windows.MapViewOfFile(mapping, windows.FILE_MAP_COPY|windows.FILE_MAP_EXECUTE, 0, 0, uintptr(size))
	if err != nil {
		_ = windows.CloseHandle(mapping)
		return nil, err
	}
	return windowsBenchmarkGuestMapping{addr: addr, size: size, mapping: mapping}, nil
}

func (m windowsBenchmarkGuestMapping) Bytes() []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(m.addr)), m.size)
}

func (m windowsBenchmarkGuestMapping) Close() error {
	var first error
	if m.addr != 0 {
		if err := windows.UnmapViewOfFile(m.addr); err != nil && first == nil {
			first = err
		}
	}
	if m.mapping != 0 {
		if err := windows.CloseHandle(m.mapping); err != nil && first == nil {
			first = err
		}
	}
	return first
}
