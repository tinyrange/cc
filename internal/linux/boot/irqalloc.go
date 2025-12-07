package boot

import "sync"

// GSIAllocator hands out global system interrupts, avoiding collisions between
// devices that need unique lines (e.g. virtio-mmio instances).
type GSIAllocator struct {
	mu       sync.Mutex
	next     uint32
	reserved map[uint32]struct{}
}

func NewGSIAllocator(start uint32, reserved []uint32) *GSIAllocator {
	r := make(map[uint32]struct{}, len(reserved))
	for _, v := range reserved {
		r[v] = struct{}{}
	}
	return &GSIAllocator{
		next:     start,
		reserved: r,
	}
}

func (a *GSIAllocator) Allocate() uint32 {
	a.mu.Lock()
	defer a.mu.Unlock()
	for {
		if _, used := a.reserved[a.next]; !used {
			v := a.next
			a.reserved[v] = struct{}{}
			a.next++
			return v
		}
		a.next++
	}
}
