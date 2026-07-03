package hv

import (
	"errors"
	"fmt"
)

const (
	memoryBenchmarkBase     = 0xa0000000
	memoryBenchmarkExitAddr = 0xf0000000
)

var errMemoryBenchmarkUnsupported = errors.New("arm64 memory benchmark is unsupported on this platform")

type memoryBenchmarkVM struct {
	memory       []byte
	close        func() error
	runUntilExit func() error
	setEntry     func(entry, stackTop uint64) error
}

func (v *memoryBenchmarkVM) Close() error {
	if v == nil || v.close == nil {
		return nil
	}
	return v.close()
}

func (v *memoryBenchmarkVM) RunUntilExit() error {
	if v == nil || v.runUntilExit == nil {
		return fmt.Errorf("memory benchmark VM is nil")
	}
	return v.runUntilExit()
}

func (v *memoryBenchmarkVM) SetEntry(entry, stackTop uint64) error {
	if v == nil || v.setEntry == nil {
		return fmt.Errorf("memory benchmark VM is nil")
	}
	return v.setEntry(entry, stackTop)
}
