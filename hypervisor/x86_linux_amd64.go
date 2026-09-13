//go:build linux && amd64

package hypervisor

import (
	"context"
	"errors"
	"fmt"
	"runtime"

	"golang.org/x/sys/unix"
	"j5.nz/cc/internal/hv/kvm"
)

type nativeX86 struct {
	*kvm.VM
	slot        uint32
	allocations [][]byte
	mappings    []RAMRegion
}

func (n *nativeX86) MapRAMRegions(size uint64, regions []RAMRegion) ([]byte, error) {
	if size == 0 || size > uint64(^uint(0)>>1) || size%4096 != 0 || len(regions) == 0 || n.slot != 0 || len(n.allocations) != 0 {
		return nil, fmt.Errorf("invalid RAM layout or RAM already mapped")
	}
	for i, r := range regions {
		if r.Size == 0 || r.Offset > size || r.Size > size-r.Offset || r.Address > ^uint64(0)-r.Size || (r.Address|r.Offset|r.Size)%4096 != 0 {
			return nil, fmt.Errorf("invalid RAM region %d", i)
		}
		for _, prior := range regions[:i] {
			if r.Address < prior.Address+prior.Size && prior.Address < r.Address+r.Size {
				return nil, fmt.Errorf("overlapping RAM regions")
			}
		}
	}
	ram, err := unix.Mmap(-1, 0, int(size), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_ANONYMOUS|unix.MAP_PRIVATE)
	if err != nil {
		return nil, err
	}
	n.allocations = append(n.allocations, ram)
	for _, r := range regions {
		if err = n.MapSharedMemory(ram[r.Offset:r.Offset+r.Size], r.Address); err != nil {
			return nil, err
		}
	}
	return ram, nil
}

func (n *nativeX86) Close() error {
	err := n.VM.Close()
	for _, ram := range n.allocations {
		err = errors.Join(err, unix.Munmap(ram))
	}
	n.allocations = nil
	return err
}

func NewX86(ctx context.Context) (X86, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	vm, err := kvm.NewVM()
	if err != nil {
		return nil, err
	}
	return &nativeX86{VM: vm}, nil
}
func (n *nativeX86) MapRAM(base, size uint64) ([]byte, error) {
	if len(n.allocations) != 0 {
		return nil, fmt.Errorf("RAM layout already mapped")
	}
	if size == 0 || size > uint64(^uint(0)>>1) || base > ^uint64(0)-size || (base|size)%4096 != 0 {
		return nil, fmt.Errorf("invalid page-aligned RAM mapping")
	}
	for _, r := range n.mappings {
		if base < r.Address+r.Size && r.Address < base+size {
			return nil, fmt.Errorf("RAM mapping overlaps existing RAM")
		}
	}
	ram, err := n.MapAnonymousMemorySlot(n.slot, size, base)
	if err == nil {
		n.slot++
		n.mappings = append(n.mappings, RAMRegion{Address: base, Size: size})
	}
	return ram, err
}
func (n *nativeX86) Cancel() error { n.RequestImmediateExit(); return nil }
func (n *nativeX86) Run(ctx context.Context) (X86Exit, error) {
	if err := ctx.Err(); err != nil {
		return X86Exit{}, err
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	n.SetVCPUTID(0, unix.Gettid())
	defer n.SetVCPUTID(0, 0)
	// Most runs finish at an IO/MMIO exit long before cancellation. Starting
	// and joining a goroutine for every VGA access forces scheduler handoffs
	// in the hottest device path. AfterFunc runs a goroutine only if cancelled.
	joined := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		n.RequestImmediateExit()
		close(joined)
	})
	defer func() {
		if !stop() {
			// A late callback must not interrupt a subsequent run or race Close.
			<-joined
		}
	}()
	if err := ctx.Err(); err != nil {
		return X86Exit{}, err
	}
	var ex kvm.Exit
	err := n.RunVCPUInterruptible(0, &ex)
	if errors.Is(err, unix.EINTR) {
		return X86Exit{}, ctx.Err()
	}
	if err != nil {
		return X86Exit{}, err
	}
	ret := X86Exit{Reason: uint32(ex.Reason)}
	if ex.Reason == kvm.ExitUnknown {
		return X86Exit{}, fmt.Errorf("unknown KVM hardware exit")
	}
	if ex.Reason == kvm.ExitIO {
		ret.Port, ret.Size, ret.Count, ret.Write, ret.Data = ex.IO.Port, ex.IO.Size, ex.IO.Count, ex.IO.Write, ex.IO.Data
	} else if ex.Reason == kvm.ExitMMIO {
		if ex.MMIO.Len == 0 || ex.MMIO.Len > 8 {
			return X86Exit{}, fmt.Errorf("invalid KVM MMIO width %d", ex.MMIO.Len)
		}
		ret.Address, ret.Size, ret.Write = ex.MMIO.Addr, uint8(ex.MMIO.Len), ex.MMIO.Write
		ret.Data = append([]byte(nil), ex.MMIO.Data[:ex.MMIO.Len]...)
	}
	return ret, nil
}
