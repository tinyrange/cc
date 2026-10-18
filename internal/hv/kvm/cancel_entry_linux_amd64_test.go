//go:build linux && amd64

package kvm

import (
	"context"
	"errors"
	"testing"
	"unsafe"
)

func TestCanceledContextBeforeKVMEntry(t *testing.T) {
	// fd=-1 is deliberate: reaching ioctl produces EBADF, not cancellation.
	// Model cancellation whose kick/flag happened just before entry resets the
	// old flag. Without the entry-local context check this loses the only kick.
	cpu := &VCPU{fd: -1, run: make([]byte, unsafe.Sizeof(kvmRunData{}))}
	vm := &VM{vcpus: []*VCPU{cpu}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	vm.RequestVCPUExit(0)
	var exit Exit
	if err := vm.RunVCPUContext(ctx, 0, &exit); !errors.Is(err, context.Canceled) {
		t.Fatalf("entered KVM after cancellation: %v", err)
	}
	// A fresh context must not inherit the preceding canceled run.
	if err := vm.RunVCPUContext(context.Background(), 0, &exit); err == nil || errors.Is(err, context.Canceled) {
		t.Fatalf("fresh entry: %v", err)
	}
}
