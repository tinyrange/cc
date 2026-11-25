//go:build linux && amd64

package hypervisor

import (
	"context"
	"fmt"
	"sync"

	"j5.nz/cc/internal/hv/kvm"
)

// DirectWorkers owns fixed-thread workers for single-vCPU handoffs that do
// not use the legacy PIT. Each VM keeps the same worker until it is closed.
type DirectWorkers struct {
	mu      sync.Mutex
	threads []*x86Thread
	next    int
	active  int
}

func NewDirectWorkers(count int) (*DirectWorkers, error) {
	if count < 1 || count > 64 {
		return nil, fmt.Errorf("direct worker count must be 1..64")
	}
	pool := &DirectWorkers{threads: make([]*x86Thread, count)}
	for index := range pool.threads {
		pool.threads[index] = newX86Thread()
	}
	return pool, nil
}

func (p *DirectWorkers) NewCPU(ctx context.Context) (X86, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.threads) == 0 {
		return nil, fmt.Errorf("direct workers are closed")
	}
	thread := p.threads[p.next]
	vm, err := kvm.NewDirectVM(func(_ int, fn func() error) error { return thread.call(fn) })
	if err != nil {
		return nil, err
	}
	p.next = (p.next + 1) % len(p.threads)
	p.active++
	return &directCPU{nativeX86: &nativeX86{vm: vm, threads: []*x86Thread{thread}, borrowed: true}, pool: p}, nil
}

type directCPU struct {
	*nativeX86
	pool *DirectWorkers
	once sync.Once
	err  error
}

func (cpu *directCPU) Close() error {
	cpu.once.Do(func() {
		cpu.err = cpu.nativeX86.Close()
		cpu.pool.mu.Lock()
		cpu.pool.active--
		cpu.pool.mu.Unlock()
	})
	return cpu.err
}

func (p *DirectWorkers) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.active != 0 {
		return fmt.Errorf("cannot close direct workers with %d live VMs", p.active)
	}
	for _, thread := range p.threads {
		thread.close()
	}
	p.threads = nil
	return nil
}

func (cpu *directCPU) PrepareRewind(memory, template []byte) (func([]uint64) (int, error), error) {
	thread := cpu.threads[0]
	var restore func([]uint64) (int, error)
	err := thread.call(func() error {
		var err error
		restore, err = cpu.vm.PrepareRewind(memory, template)
		return err
	})
	if err != nil {
		return nil, err
	}
	return func(pages []uint64) (count int, err error) {
		err = thread.call(func() error {
			var restoreErr error
			count, restoreErr = restore(pages)
			return restoreErr
		})
		return count, err
	}, nil
}

func (cpu *directCPU) RunSlot() chan struct{} { return cpu.threads[0].slot }

// OnWorker keeps a sequence of serialized direct-vCPU operations on its
// fixed thread. The callback may call the CPU's normal methods reentrantly.
func (cpu *directCPU) OnWorker(run func() error) error {
	return cpu.threads[0].call(run)
}
